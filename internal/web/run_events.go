package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type RuntimeMode string

const (
	RuntimeModeAlwaysOn   RuntimeMode = "always-on"
	RuntimeModeServerless RuntimeMode = "serverless"
)

func normalizeRuntimeMode(value string) RuntimeMode {
	switch RuntimeMode(strings.TrimSpace(value)) {
	case RuntimeModeServerless:
		return RuntimeModeServerless
	default:
		return RuntimeModeAlwaysOn
	}
}

func (m RuntimeMode) String() string {
	if m == RuntimeModeServerless {
		return string(RuntimeModeServerless)
	}
	return string(RuntimeModeAlwaysOn)
}

type runEventType string

const (
	runEventCreated  runEventType = "run-created"
	runEventUpdated  runEventType = "run-updated"
	runEventFinished runEventType = "run-finished"
)

type runEventMessage struct {
	eventType runEventType
	data      []byte
}

type runEventBroker struct {
	mu          sync.Mutex
	subscribers []chan runEventMessage
}

func newRunEventBroker() *runEventBroker {
	return &runEventBroker{}
}

func (b *runEventBroker) subscribe() chan runEventMessage {
	ch := make(chan runEventMessage, 32)
	b.mu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()
	return ch
}

func (b *runEventBroker) unsubscribe(target chan runEventMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for index, ch := range b.subscribers {
		if ch == target {
			b.subscribers = append(b.subscribers[:index], b.subscribers[index+1:]...)
			close(ch)
			return
		}
	}
}

func (b *runEventBroker) broadcast(eventType runEventType, meta *RunMeta) {
	if b == nil || meta == nil {
		return
	}
	payload, err := json.Marshal(runMetaSummary(meta))
	if err != nil {
		return
	}
	message := runEventMessage{
		eventType: eventType,
		data:      payload,
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- message:
		default:
			// Drop for slow subscribers rather than blocking task execution.
		}
	}
}

func runMetaSummary(meta *RunMeta) *RunMeta {
	if meta == nil {
		return nil
	}
	copyMeta := *meta
	copyMeta.Tasks = nil
	copyMeta.Artifacts = nil
	copyMeta.InputValues = nil
	copyMeta.HasArtifacts = copyMeta.HasArtifacts || len(meta.Artifacts) > 0
	copyMeta.ensureRunKey()
	copyMeta.ensureTrigger()
	return &copyMeta
}

func runsETag(metas []*RunMeta) string {
	hash := sha256.New()
	for _, meta := range metas {
		if meta == nil {
			continue
		}
		summary := runMetaSummary(meta)
		fmt.Fprintf(
			hash,
			"%s|%s|%s|%s|%d|%s|%s|%s|%d|%t|%s|%s\n",
			summary.RunID,
			summary.RunKey,
			summary.Branch,
			summary.TaskLabel,
			summary.RunNumber,
			summary.Status,
			summary.StartTime.UTC().Format(time.RFC3339Nano),
			summary.EndTime.UTC().Format(time.RFC3339Nano),
			summary.ExitCode,
			summary.HasArtifacts,
			summary.User,
			summary.Trigger,
		)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func etagMatches(headerValue string, etag string) bool {
	if strings.TrimSpace(headerValue) == "" || strings.TrimSpace(etag) == "" {
		return false
	}
	expected := strings.TrimPrefix(strings.TrimSpace(etag), "W/")
	expected = normalizeETag(expected)
	for _, item := range strings.Split(headerValue, ",") {
		candidate := strings.TrimSpace(item)
		if candidate == "*" {
			return true
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		if normalizeETag(candidate) == expected {
			return true
		}
	}
	return false
}

func serveRunEventsSSE(w http.ResponseWriter, r *http.Request, broker *runEventBroker) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)

	ch := broker.subscribe()
	defer broker.unsubscribe(ch)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, rc, string(message.eventType), message.data)
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			_ = rc.Flush()
		}
	}
}
