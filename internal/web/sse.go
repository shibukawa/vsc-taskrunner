package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// writeSSEEvent writes a single SSE event to w and flushes immediately.
// If eventType is empty, the "event:" line is omitted (default event type).
func writeSSEEvent(w http.ResponseWriter, rc *http.ResponseController, eventType string, data []byte) {
	if eventType != "" {
		fmt.Fprintf(w, "event: %s\n", eventType)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	_ = rc.Flush()
}

// ServeLogSSE handles GET /api/branches/:branch/tasks/:task/runs/:runNumber/log
// It streams log output as SSE events.  If the run is still active, it delivers
// a real-time feed; once the run finishes it sends a terminal "done" event.
func ServeLogSSE(w http.ResponseWriter, r *http.Request, runID string, tm *TaskManager) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering if present.
	rc := http.NewResponseController(w)

	ctx := r.Context()

	// Check whether the run is currently active.
	active := tm.GetActiveRunByID(runID)
	if active != nil {
		serveActiveLog(ctx, w, rc, runID, active, tm)
		return
	}

	// Run is not active — try to serve from the completed log file.
	data, err := tm.history.ReadLog(runID)
	if err != nil {
		http.Error(w, fmt.Sprintf("log not found: %v", err), http.StatusNotFound)
		return
	}
	meta, metaErr := tm.history.ReadMeta(runID)
	if metaErr == nil && len(meta.Tasks) > 0 {
		serveStoredTaskEvents(w, rc, runID, meta, tm)
		return
	}
	for _, line := range splitLines(data) {
		payload, _ := json.Marshal(sseTaskEvent{
			Type: "task-line",
			Line: string(line),
		})
		writeSSEEvent(w, rc, "task-line", payload)
	}
	writeSSEEvent(w, rc, "done", []byte("{}"))
}

func serveActiveLog(ctx context.Context, w http.ResponseWriter, rc *http.ResponseController, runID string, active *ActiveRun, tm *TaskManager) {
	serveBufferedActiveTaskLogs(w, rc, runID, active, tm)

	// Subscribe to future chunks.
	ch := active.subscribe()
	defer active.unsubscribe(ch)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-active.done:
			writeSSEEvent(w, rc, "done", []byte("{}"))
			return
		case message, open := <-ch:
			if !open {
				writeSSEEvent(w, rc, "done", []byte("{}"))
				return
			}
			writeSSEEvent(w, rc, message.eventType, message.data)
		case <-heartbeat.C:
			// Keep the connection alive.
			fmt.Fprint(w, ": heartbeat\n\n")
			_ = rc.Flush()
		}
	}
}

func serveBufferedActiveTaskLogs(w http.ResponseWriter, rc *http.ResponseController, runID string, active *ActiveRun, tm *TaskManager) {
	for _, task := range collectTaskRuns(active) {
		logData, err := tm.history.ReadTaskLog(runID, task.Label)
		if err != nil {
			continue
		}
		for _, line := range splitLines(logData) {
			linePayload, _ := json.Marshal(sseTaskEvent{
				Type:      "task-line",
				TaskLabel: task.Label,
				Line:      string(line),
			})
			writeSSEEvent(w, rc, "task-line", linePayload)
		}
	}
}

func serveStoredTaskEvents(w http.ResponseWriter, rc *http.ResponseController, runID string, meta *RunMeta, tm *TaskManager) {
	for _, task := range meta.Tasks {
		startPayload, _ := json.Marshal(sseTaskEvent{
			Type:      "task-start",
			TaskLabel: task.Label,
			Status:    string(task.Status),
			StartTime: formatTimeForSSE(task.StartTime),
		})
		writeSSEEvent(w, rc, "task-start", startPayload)

		logData, err := tm.history.ReadTaskLog(runID, task.Label)
		if err == nil {
			for _, line := range splitLines(logData) {
				linePayload, _ := json.Marshal(sseTaskEvent{
					Type:      "task-line",
					TaskLabel: task.Label,
					Line:      string(line),
				})
				writeSSEEvent(w, rc, "task-line", linePayload)
			}
		}

		eventType := "task-finish"
		if task.Status == TaskRunStatusSkipped {
			eventType = "task-skip"
		}
		finishPayload, _ := json.Marshal(sseTaskEvent{
			Type:      eventType,
			TaskLabel: task.Label,
			Status:    string(task.Status),
			ExitCode:  task.ExitCode,
			StartTime: formatTimeForSSE(task.StartTime),
			EndTime:   formatTimeForSSE(task.EndTime),
		})
		writeSSEEvent(w, rc, eventType, finishPayload)
	}
	writeSSEEvent(w, rc, "done", []byte("{}"))
}

func formatTimeForSSE(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

// splitLines splits data on newlines, preserving the trailing newline in each element.
// Empty trailing segment is dropped.  This avoids sending empty SSE data lines.
func splitLines(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i+1])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
