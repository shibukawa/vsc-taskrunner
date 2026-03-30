package web

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const historyIndexObjectName = "run-history-index.json"

type RunHistorySummary struct {
	RunID        string    `json:"runId"`
	RunKey       string    `json:"runKey"`
	Branch       string    `json:"branch"`
	TaskLabel    string    `json:"taskLabel"`
	RunNumber    int       `json:"runNumber"`
	Status       RunStatus `json:"status"`
	StartTime    time.Time `json:"startTime"`
	EndTime      time.Time `json:"endTime,omitempty"`
	ExitCode     int       `json:"exitCode"`
	WorktreeKept bool      `json:"worktreeKept"`
	User         string    `json:"user,omitempty"`
	TokenLabel   string    `json:"tokenLabel,omitempty"`
	HasArtifacts bool      `json:"hasArtifacts,omitempty"`
}

type RunHistoryGroup struct {
	Branch        string               `json:"branch"`
	TaskLabel     string               `json:"taskLabel"`
	NextRunNumber int                  `json:"nextRunNumber"`
	Runs          []*RunHistorySummary `json:"runs,omitempty"`
}

type RunHistoryIndex struct {
	Groups map[string]*RunHistoryGroup `json:"groups"`
}

func newRunHistoryIndex() *RunHistoryIndex {
	return &RunHistoryIndex{Groups: make(map[string]*RunHistoryGroup)}
}

func (idx *RunHistoryIndex) ensureGroup(branch, taskLabel string) *RunHistoryGroup {
	if idx.Groups == nil {
		idx.Groups = make(map[string]*RunHistoryGroup)
	}
	key := historyGroupKey(branch, taskLabel)
	group := idx.Groups[key]
	if group != nil {
		if group.NextRunNumber < 1 {
			group.NextRunNumber = 1
		}
		return group
	}
	group = &RunHistoryGroup{
		Branch:        branch,
		TaskLabel:     taskLabel,
		NextRunNumber: 1,
	}
	idx.Groups[key] = group
	return group
}

func (idx *RunHistoryIndex) startRun(branch, taskLabel, user, tokenLabel, runID string) *RunHistorySummary {
	group := idx.ensureGroup(branch, taskLabel)
	runNumber := group.NextRunNumber
	if runNumber < 1 {
		runNumber = 1
	}
	group.NextRunNumber = runNumber + 1

	summary := &RunHistorySummary{
		RunID:        runID,
		RunKey:       RunRef{Branch: branch, TaskLabel: taskLabel, RunNumber: runNumber}.Key(),
		Branch:       branch,
		TaskLabel:    taskLabel,
		RunNumber:    runNumber,
		Status:       RunStatusRunning,
		StartTime:    time.Now().UTC(),
		WorktreeKept: false,
		User:         user,
		TokenLabel:   tokenLabel,
	}
	group.Runs = append([]*RunHistorySummary{summary}, group.Runs...)
	return summary
}

func (idx *RunHistoryIndex) updateRun(meta *RunMeta, keepCount int) []string {
	group := idx.ensureGroup(meta.Branch, meta.TaskLabel)
	for _, run := range group.Runs {
		if run.RunID != meta.RunID {
			continue
		}
		run.RunID = meta.RunID
		run.Status = meta.Status
		run.StartTime = meta.StartTime
		run.EndTime = meta.EndTime
		run.ExitCode = meta.ExitCode
		run.WorktreeKept = meta.WorktreeKept
		run.User = meta.User
		run.TokenLabel = meta.TokenLabel
		run.HasArtifacts = len(meta.Artifacts) > 0
		run.RunKey = meta.RunKey
		return trimCompletedHistoryGroup(group, keepCount)
	}

	group.Runs = append([]*RunHistorySummary{{
		RunID:        meta.RunID,
		RunKey:       meta.RunKey,
		Branch:       meta.Branch,
		TaskLabel:    meta.TaskLabel,
		RunNumber:    meta.RunNumber,
		Status:       meta.Status,
		StartTime:    meta.StartTime,
		EndTime:      meta.EndTime,
		ExitCode:     meta.ExitCode,
		WorktreeKept: meta.WorktreeKept,
		User:         meta.User,
		TokenLabel:   meta.TokenLabel,
		HasArtifacts: len(meta.Artifacts) > 0,
	}}, group.Runs...)
	return trimCompletedHistoryGroup(group, keepCount)
}

func (idx *RunHistoryIndex) removeRun(branch, taskLabel, runID string) {
	group := idx.Groups[historyGroupKey(branch, taskLabel)]
	if group == nil {
		return
	}
	filtered := group.Runs[:0]
	for _, run := range group.Runs {
		if run.RunID == runID {
			continue
		}
		filtered = append(filtered, run)
	}
	group.Runs = filtered
	if len(group.Runs) == 0 && group.NextRunNumber <= 1 {
		delete(idx.Groups, historyGroupKey(branch, taskLabel))
	}
}

func (idx *RunHistoryIndex) listRuns() []*RunMeta {
	items := make([]*RunMeta, 0)
	for _, group := range idx.Groups {
		for _, run := range group.Runs {
			items = append(items, &RunMeta{
				RunKey:       run.RunKey,
				RunID:        run.RunID,
				Branch:       run.Branch,
				TaskLabel:    run.TaskLabel,
				RunNumber:    run.RunNumber,
				Status:       run.Status,
				StartTime:    run.StartTime,
				EndTime:      run.EndTime,
				ExitCode:     run.ExitCode,
				WorktreeKept: run.WorktreeKept,
				User:         run.User,
				TokenLabel:   run.TokenLabel,
				HasArtifacts: run.HasArtifacts,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartTime.Equal(items[j].StartTime) {
			return items[i].RunKey > items[j].RunKey
		}
		return items[i].StartTime.After(items[j].StartTime)
	})
	return items
}

func (idx *RunHistoryIndex) findRunID(branch, taskLabel string, runNumber int) string {
	group := idx.Groups[historyGroupKey(branch, taskLabel)]
	if group == nil {
		return ""
	}
	for _, run := range group.Runs {
		if run.RunNumber == runNumber {
			return run.RunID
		}
	}
	return ""
}

func trimCompletedHistoryGroup(group *RunHistoryGroup, keepCount int) []string {
	if keepCount <= 0 || len(group.Runs) == 0 {
		return nil
	}
	kept := make([]*RunHistorySummary, 0, len(group.Runs))
	var evicted []string
	completedKept := 0
	for _, run := range group.Runs {
		if run.Status == RunStatusRunning {
			kept = append(kept, run)
			continue
		}
		if completedKept >= keepCount {
			evicted = append(evicted, run.RunID)
			continue
		}
		kept = append(kept, run)
		completedKept++
	}
	group.Runs = kept
	return evicted
}

func (idx *RunHistoryIndex) applyWorktreeRetention(keepSuccess int, keepFailure int) {
	if keepSuccess < 0 {
		keepSuccess = 0
	}
	if keepFailure < 0 {
		keepFailure = 0
	}
	runs := idx.sortedRuns()
	keptSuccess := 0
	keptFailure := 0
	for _, run := range runs {
		run.WorktreeKept = false
		switch run.Status {
		case RunStatusSuccess:
			if keptSuccess < keepSuccess {
				run.WorktreeKept = true
				keptSuccess++
			}
		case RunStatusFailed:
			if keptFailure < keepFailure {
				run.WorktreeKept = true
				keptFailure++
			}
		}
	}
}

func (idx *RunHistoryIndex) worktreeKept(runID string) bool {
	for _, group := range idx.Groups {
		for _, run := range group.Runs {
			if run.RunID == runID {
				return run.WorktreeKept
			}
		}
	}
	return false
}

func (idx *RunHistoryIndex) sortedRuns() []*RunHistorySummary {
	runs := make([]*RunHistorySummary, 0)
	for _, group := range idx.Groups {
		runs = append(runs, group.Runs...)
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartTime.Equal(runs[j].StartTime) {
			return runs[i].RunKey > runs[j].RunKey
		}
		return runs[i].StartTime.After(runs[j].StartTime)
	})
	return runs
}

func historyGroupKey(branch, taskLabel string) string {
	return branch + "\t" + taskLabel
}

type HistoryIndexStore interface {
	ReadIndex(ctx context.Context) (*RunHistoryIndex, error)
	UpdateIndex(ctx context.Context, fn func(*RunHistoryIndex) error) error
}

func cloneHistoryIndex(index *RunHistoryIndex) (*RunHistoryIndex, error) {
	if index == nil {
		return newRunHistoryIndex(), nil
	}
	data, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("marshal history index: %w", err)
	}
	var cloned RunHistoryIndex
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, fmt.Errorf("unmarshal history index: %w", err)
	}
	if cloned.Groups == nil {
		cloned.Groups = make(map[string]*RunHistoryGroup)
	}
	return &cloned, nil
}
