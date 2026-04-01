package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"vsc-taskrunner/internal/git"
	"vsc-taskrunner/internal/tasks"
	"vsc-taskrunner/internal/uiconfig"
)

const defaultSchedulerPollInterval = time.Minute

type Scheduler struct {
	repo         git.RepositoryStore
	config       *uiconfig.UIConfig
	manager      *TaskManager
	state        ScheduleStateStore
	now          func() time.Time
	pollInterval time.Duration
}

type HeartbeatRunResult struct {
	Branch    string    `json:"branch"`
	TaskLabel string    `json:"taskLabel"`
	Cron      string    `json:"cron"`
	Slot      time.Time `json:"slot"`
	RunID     string    `json:"runId,omitempty"`
}

type HeartbeatResult struct {
	EvaluatedAt time.Time            `json:"evaluatedAt"`
	Checked     int                  `json:"checked"`
	Triggered   int                  `json:"triggered"`
	Runs        []HeartbeatRunResult `json:"runs,omitempty"`
}

type scheduledCandidate struct {
	TaskLabel   string
	Branch      string
	Cron        string
	InputValues map[string]string
}

type dueCandidate struct {
	Candidate scheduledCandidate
	Slot      time.Time
}

func NewScheduler(repo git.RepositoryStore, config *uiconfig.UIConfig, manager *TaskManager, state ScheduleStateStore) *Scheduler {
	return &Scheduler{
		repo:         repo,
		config:       config,
		manager:      manager,
		state:        state,
		now:          time.Now,
		pollInterval: defaultSchedulerPollInterval,
	}
}

func (s *Scheduler) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Scheduler) RunLoop(ctx context.Context) {
	if s == nil || s.state == nil {
		return
	}
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	_, _ = s.TriggerDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.TriggerDue(ctx); err != nil {
				log.Printf("runtask scheduler evaluation failed: %v", err)
			}
		}
	}
}

func (s *Scheduler) TriggerDue(ctx context.Context) (*HeartbeatResult, error) {
	if s == nil || s.repo == nil || s.manager == nil || s.state == nil {
		return &HeartbeatResult{EvaluatedAt: s.currentTime()}, nil
	}
	candidates, err := s.collectCandidates(ctx)
	if err != nil {
		return nil, err
	}
	now := s.currentTime()
	dueRuns := make([]dueCandidate, 0)
	err = s.state.UpdateState(ctx, func(index *ScheduleStateIndex) error {
		if index.Items == nil {
			index.Items = make(map[string]ScheduleState)
		}
		for _, candidate := range candidates {
			stateKey := scheduledStateKey(candidate)
			state := index.Items[stateKey]
			slot, shouldTrigger, nextState, err := evaluateScheduleDue(candidate.Cron, now, state)
			if err != nil {
				return fmt.Errorf("evaluate schedule for %s on %s: %w", candidate.TaskLabel, candidate.Branch, err)
			}
			index.Items[stateKey] = nextState
			if !shouldTrigger {
				continue
			}
			dueRuns = append(dueRuns, dueCandidate{Candidate: candidate, Slot: slot})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]HeartbeatRunResult, 0, len(dueRuns))
	for _, due := range dueRuns {
		log.Printf(
			"runtask scheduler starting run branch=%q task=%q cron=%q slot=%s",
			due.Candidate.Branch,
			due.Candidate.TaskLabel,
			due.Candidate.Cron,
			due.Slot.Format(time.RFC3339),
		)
		meta, err := s.manager.StartRunWithTriggerAndInputs(
			ctx,
			due.Candidate.Branch,
			due.Candidate.TaskLabel,
			"scheduler",
			"",
			RunTriggerScheduled,
			cloneStringMap(due.Candidate.InputValues),
		)
		if err != nil {
			return nil, fmt.Errorf("start scheduled run %s on %s: %w", due.Candidate.TaskLabel, due.Candidate.Branch, err)
		}
		log.Printf(
			"runtask scheduler started run run_id=%q branch=%q task=%q cron=%q slot=%s",
			meta.RunID,
			due.Candidate.Branch,
			due.Candidate.TaskLabel,
			due.Candidate.Cron,
			due.Slot.Format(time.RFC3339),
		)
		results = append(results, HeartbeatRunResult{
			Branch:    due.Candidate.Branch,
			TaskLabel: due.Candidate.TaskLabel,
			Cron:      due.Candidate.Cron,
			Slot:      due.Slot,
			RunID:     meta.RunID,
		})
	}

	return &HeartbeatResult{
		EvaluatedAt: now,
		Checked:     len(candidates),
		Triggered:   len(results),
		Runs:        results,
	}, nil
}

func (s *Scheduler) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now().In(time.Local)
	}
	return time.Now().In(time.Local)
}

func (s *Scheduler) collectCandidates(ctx context.Context) ([]scheduledCandidate, error) {
	branches := s.config.ScheduledBranches()
	items := make([]scheduledCandidate, 0)
	for _, branch := range branches {
		data, err := s.repo.ReadTasksJSON(ctx, branch)
		if err != nil {
			log.Printf("runtask scheduler branch load failed branch=%q error=%v", branch, err)
			continue
		}
		file, err := tasks.LoadFileFromBytes(data, tasks.LoadOptions{
			Path:          filepath.ToSlash(filepath.Join(uiconfig.DefaultTasksSparsePath, "tasks.json")),
			WorkspaceRoot: "",
		})
		if err != nil {
			log.Printf("runtask scheduler tasks parse failed branch=%q error=%v", branch, err)
			continue
		}
		for _, task := range file.Tasks {
			for _, schedule := range s.config.MatchingSchedules(task.Label) {
				if schedule.Branch != branch {
					continue
				}
				items = append(items, scheduledCandidate{
					TaskLabel:   task.Label,
					Branch:      branch,
					Cron:        schedule.Cron,
					InputValues: cloneStringMap(schedule.InputValues),
				})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Branch == items[j].Branch {
			if items[i].TaskLabel == items[j].TaskLabel {
				return items[i].Cron < items[j].Cron
			}
			return items[i].TaskLabel < items[j].TaskLabel
		}
		return items[i].Branch < items[j].Branch
	})
	return items, nil
}

func evaluateScheduleDue(expr string, now time.Time, state ScheduleState) (time.Time, bool, ScheduleState, error) {
	schedule, err := cron.ParseStandard(strings.TrimSpace(expr))
	if err != nil {
		return time.Time{}, false, state, err
	}
	if state.LastEvaluatedAt.IsZero() {
		state.LastEvaluatedAt = now
		return time.Time{}, false, state, nil
	}
	next := schedule.Next(state.LastEvaluatedAt)
	if next.After(now) {
		state.LastEvaluatedAt = now
		return time.Time{}, false, state, nil
	}
	latest := next
	for steps := 0; steps < 100000; steps++ {
		upcoming := schedule.Next(latest)
		if upcoming.After(now) {
			break
		}
		latest = upcoming
		if steps == 99999 {
			return time.Time{}, false, state, fmt.Errorf("too many missed schedule slots between %s and %s", state.LastEvaluatedAt.Format(time.RFC3339), now.Format(time.RFC3339))
		}
	}
	state.LastEvaluatedAt = now
	if !state.LastTriggeredSlot.IsZero() && !latest.After(state.LastTriggeredSlot) {
		return time.Time{}, false, state, nil
	}
	state.LastTriggeredSlot = latest
	return latest, true, state, nil
}

func scheduledStateKey(candidate scheduledCandidate) string {
	b := strings.Builder{}
	b.WriteString(candidate.TaskLabel)
	b.WriteByte('\n')
	b.WriteString(candidate.Branch)
	b.WriteByte('\n')
	b.WriteString(strings.TrimSpace(candidate.Cron))
	keys := make([]string, 0, len(candidate.InputValues))
	for key := range candidate.InputValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteByte('\n')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(candidate.InputValues[key])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
