package web

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type RunStatus string

const (
	RunStatusRunning RunStatus = "running"
	RunStatusSuccess RunStatus = "success"
	RunStatusFailed  RunStatus = "failed"
)

type RunRef struct {
	Branch    string
	TaskLabel string
	RunNumber int
}

func (r RunRef) Key() string {
	return fmt.Sprintf("%s\t%s\t%d", r.Branch, r.TaskLabel, r.RunNumber)
}

type RunMeta struct {
	RunID        string            `yaml:"runId" json:"runId"`
	RunKey       string            `yaml:"runKey" json:"runKey"`
	Branch       string            `yaml:"branch" json:"branch"`
	TaskLabel    string            `yaml:"taskLabel" json:"taskLabel"`
	RunNumber    int               `yaml:"runNumber" json:"runNumber"`
	Status       RunStatus         `yaml:"status" json:"status"`
	StartTime    time.Time         `yaml:"startTime" json:"startTime"`
	EndTime      time.Time         `yaml:"endTime" json:"endTime,omitempty"`
	ExitCode     int               `yaml:"exitCode" json:"exitCode"`
	CommitHash   string            `yaml:"commitHash,omitempty" json:"commitHash,omitempty"`
	HasArtifacts bool              `yaml:"hasArtifacts,omitempty" json:"hasArtifacts,omitempty"`
	WorktreeKept bool              `yaml:"worktreeKept" json:"worktreeKept"`
	Artifacts    []ArtifactRef     `yaml:"artifacts" json:"artifacts,omitempty"`
	User         string            `yaml:"user,omitempty" json:"user,omitempty"`
	InputValues  map[string]string `yaml:"-" json:"-"`
	Tasks        []*TaskRunMeta    `yaml:"tasks,omitempty" json:"tasks,omitempty"`
}

func (m *RunMeta) Ref() RunRef {
	return RunRef{
		Branch:    m.Branch,
		TaskLabel: m.TaskLabel,
		RunNumber: m.RunNumber,
	}
}

func (m *RunMeta) ensureRunKey() {
	switch {
	case m.RunKey != "":
		return
	case m.RunID != "":
		m.RunKey = m.RunID
	default:
		m.RunKey = m.Ref().Key()
	}
}

type ArtifactRef struct {
	Source string `yaml:"source" json:"source"`
	Dest   string `yaml:"dest" json:"dest"`
	Format string `yaml:"format,omitempty" json:"format,omitempty"`
}

type TaskRunStatus string

const (
	TaskRunStatusPending TaskRunStatus = "pending"
	TaskRunStatusRunning TaskRunStatus = "running"
	TaskRunStatusSuccess TaskRunStatus = "success"
	TaskRunStatusFailed  TaskRunStatus = "failed"
	TaskRunStatusSkipped TaskRunStatus = "skipped"
)

type TaskRunMeta struct {
	Label        string        `yaml:"label" json:"label"`
	DependsOn    []string      `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	DependsOrder string        `yaml:"dependsOrder,omitempty" json:"dependsOrder,omitempty"`
	Status       TaskRunStatus `yaml:"status" json:"status"`
	StartTime    time.Time     `yaml:"startTime" json:"startTime,omitempty"`
	EndTime      time.Time     `yaml:"endTime" json:"endTime,omitempty"`
	ExitCode     int           `yaml:"exitCode" json:"exitCode"`
	LogPath      string        `yaml:"logPath,omitempty" json:"logPath,omitempty"`
	Historical   bool          `yaml:"historical,omitempty" json:"historical,omitempty"`
}

type HistoryStore struct {
	historyDir string
	index      HistoryIndexStore
	runs       RunStore
	mu         sync.Mutex
}

func NewHistoryStore(historyDir string) (*HistoryStore, error) {
	return NewHistoryStoreWithStores(historyDir, NewLocalIndexStore(historyDir), NewLocalRunStore(historyDir))
}

func NewHistoryStoreWithIndex(historyDir string, indexStore HistoryIndexStore) (*HistoryStore, error) {
	return NewHistoryStoreWithStores(historyDir, indexStore, NewLocalRunStore(historyDir))
}

func NewHistoryStoreWithStores(historyDir string, indexStore HistoryIndexStore, runStore RunStore) (*HistoryStore, error) {
	if indexStore == nil {
		indexStore = NewLocalIndexStore(historyDir)
	}
	if runStore == nil {
		runStore = NewLocalRunStore(historyDir)
	}
	return &HistoryStore{
		historyDir: historyDir,
		index:      indexStore,
		runs:       runStore,
	}, nil
}

func (s *HistoryStore) AllocateRun(branch, taskLabel string) (*RunMeta, error) {
	return s.AllocateRunWithUser(branch, taskLabel, "")
}

func (s *HistoryStore) AllocateRunWithUser(branch, taskLabel, user string) (*RunMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var meta *RunMeta
	if err := s.index.UpdateIndex(context.Background(), func(index *RunHistoryIndex) error {
		runID := uuid.NewString()
		summary := index.startRun(branch, taskLabel, user, runID)
		meta = &RunMeta{
			RunID:     runID,
			RunKey:    runID,
			Branch:    branch,
			TaskLabel: taskLabel,
			RunNumber: summary.RunNumber,
			Status:    RunStatusRunning,
			StartTime: summary.StartTime,
			User:      user,
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return meta, nil
}

func (s *HistoryStore) RunDir(runID string) string {
	return s.runs.RunDir(runID)
}

func (s *HistoryStore) LogPath(runID string) string {
	return s.runs.LogPath(runID)
}

func (s *HistoryStore) TaskLogPath(runID, taskLabel string) string {
	return s.runs.TaskLogPath(runID, taskLabel)
}

func (s *HistoryStore) WorktreePath(runID string) string {
	return s.runs.WorktreePath(runID)
}

func (s *HistoryStore) ArtifactDir(runID string) string {
	return s.runs.ArtifactDir(runID)
}

func (s *HistoryStore) MetaPath(runID string) string {
	return s.runs.MetaPath(runID)
}

func (s *HistoryStore) WriteMeta(meta *RunMeta) error {
	meta.ensureRunKey()
	if err := s.runs.WriteMeta(meta); err != nil {
		return err
	}
	return s.RecordRunCompletion(meta, 0)
}

func (s *HistoryStore) ReadMeta(runID string) (*RunMeta, error) {
	meta, err := s.runs.ReadMeta(runID)
	if err != nil {
		return nil, err
	}
	meta.HasArtifacts = len(meta.Artifacts) > 0
	return meta, nil
}

func (s *HistoryStore) ReadLog(runID string) ([]byte, error) {
	return s.runs.ReadLog(runID)
}

func (s *HistoryStore) ReadTaskLog(runID, taskLabel string) ([]byte, error) {
	return s.runs.ReadTaskLog(runID, taskLabel)
}

func (s *HistoryStore) TailLog(runID string, byteOffset int64) ([]byte, error) {
	return s.runs.TailLog(runID, byteOffset)
}

func (s *HistoryStore) List() ([]*RunMeta, error) {
	index, err := s.index.ReadIndex(context.Background())
	if err != nil {
		return nil, err
	}
	items := index.listRuns()
	if len(items) > 0 {
		return items, nil
	}
	return s.rebuildHistoryIndex()
}

func (s *HistoryStore) LookupRunID(branch, taskLabel string, runNumber int) (string, error) {
	index, err := s.index.ReadIndex(context.Background())
	if err != nil {
		return "", err
	}
	runID := index.findRunID(branch, taskLabel, runNumber)
	if runID == "" {
		return "", fmt.Errorf("run not found for %s/%s/%d", branch, taskLabel, runNumber)
	}
	return runID, nil
}

func (s *HistoryStore) ListWorktreeFiles(runID string) ([]string, error) {
	return s.runs.ListWorktreeFiles(runID)
}

func (s *HistoryStore) ReadWorktreeFile(runID, filePath string) ([]byte, error) {
	return s.runs.ReadWorktreeFile(runID, filePath)
}

func (s *HistoryStore) ListArtifactFiles(runID string) ([]string, error) {
	return s.runs.ListArtifactFiles(runID)
}

func (s *HistoryStore) StatArtifactFile(runID, filePath string) (ArtifactFileInfo, error) {
	return s.runs.StatArtifactFile(runID, filePath)
}

func (s *HistoryStore) ReadArtifactFile(runID, filePath string) ([]byte, error) {
	return s.runs.ReadArtifactFile(runID, filePath)
}

func (s *HistoryStore) RecordRunCompletion(meta *RunMeta, keepCount int) error {
	var evicted []string
	if err := s.index.UpdateIndex(context.Background(), func(index *RunHistoryIndex) error {
		evicted = index.updateRun(meta, keepCount)
		return nil
	}); err != nil {
		return err
	}
	for _, runID := range evicted {
		if runID == "" || runID == meta.RunID {
			continue
		}
		_ = s.runs.DeleteRun(runID)
	}
	return nil
}

func (s *HistoryStore) AbortRun(meta *RunMeta) error {
	if meta == nil {
		return nil
	}
	if err := s.index.UpdateIndex(context.Background(), func(index *RunHistoryIndex) error {
		index.removeRun(meta.Branch, meta.TaskLabel, meta.RunID)
		return nil
	}); err != nil {
		return err
	}
	return s.runs.DeleteRun(meta.RunID)
}

func (s *HistoryStore) FinalizeRun(runID string) error {
	return s.runs.FinalizeRun(runID)
}

func (s *HistoryStore) Prune(keepCount int) error {
	return nil
}

func (s *HistoryStore) PruneWorktrees(keepSuccess int, keepFailure int) error {
	metas, err := s.List()
	if err != nil {
		return err
	}
	var successes []*RunMeta
	var failures []*RunMeta
	for _, meta := range metas {
		if !meta.WorktreeKept {
			continue
		}
		switch meta.Status {
		case RunStatusSuccess:
			successes = append(successes, meta)
		case RunStatusFailed:
			failures = append(failures, meta)
		}
	}
	if err := s.pruneWorktreeGroup(successes, keepSuccess); err != nil {
		return err
	}
	return s.pruneWorktreeGroup(failures, keepFailure)
}

func (s *HistoryStore) rebuildHistoryIndex() ([]*RunMeta, error) {
	metas, err := s.runs.ListMetas(context.Background())
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, nil
	}
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Branch == metas[j].Branch {
			if metas[i].TaskLabel == metas[j].TaskLabel {
				if metas[i].StartTime.Equal(metas[j].StartTime) {
					return metas[i].RunID < metas[j].RunID
				}
				return metas[i].StartTime.Before(metas[j].StartTime)
			}
			return metas[i].TaskLabel < metas[j].TaskLabel
		}
		return metas[i].Branch < metas[j].Branch
	})
	nextRunNumber := make(map[string]int)
	for _, meta := range metas {
		key := historyGroupKey(meta.Branch, meta.TaskLabel)
		if meta.RunNumber <= 0 {
			nextRunNumber[key]++
			meta.RunNumber = nextRunNumber[key]
		} else if meta.RunNumber > nextRunNumber[key] {
			nextRunNumber[key] = meta.RunNumber
		}
		meta.ensureRunKey()
	}
	if err := s.index.UpdateIndex(context.Background(), func(index *RunHistoryIndex) error {
		index.Groups = make(map[string]*RunHistoryGroup)
		for _, meta := range metas {
			group := index.ensureGroup(meta.Branch, meta.TaskLabel)
			group.Runs = append(group.Runs, &RunHistorySummary{
				RunID:        meta.RunID,
				RunKey:       meta.RunKey,
				Branch:       meta.Branch,
				TaskLabel:    meta.TaskLabel,
				RunNumber:    meta.RunNumber,
				Status:       meta.Status,
				StartTime:    meta.StartTime,
				EndTime:      meta.EndTime,
				ExitCode:     meta.ExitCode,
				User:         meta.User,
				HasArtifacts: len(meta.Artifacts) > 0,
			})
			if group.NextRunNumber <= meta.RunNumber {
				group.NextRunNumber = meta.RunNumber + 1
			}
		}
		for _, group := range index.Groups {
			sort.Slice(group.Runs, func(i, j int) bool {
				if group.Runs[i].StartTime.Equal(group.Runs[j].StartTime) {
					return group.Runs[i].RunNumber > group.Runs[j].RunNumber
				}
				return group.Runs[i].StartTime.After(group.Runs[j].StartTime)
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	items := make([]*RunMeta, 0, len(metas))
	for _, meta := range metas {
		items = append(items, &RunMeta{
			RunID:        meta.RunID,
			RunKey:       meta.RunKey,
			Branch:       meta.Branch,
			TaskLabel:    meta.TaskLabel,
			RunNumber:    meta.RunNumber,
			Status:       meta.Status,
			StartTime:    meta.StartTime,
			EndTime:      meta.EndTime,
			ExitCode:     meta.ExitCode,
			User:         meta.User,
			HasArtifacts: len(meta.Artifacts) > 0,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartTime.Equal(items[j].StartTime) {
			return items[i].RunKey > items[j].RunKey
		}
		return items[i].StartTime.After(items[j].StartTime)
	})
	return items, nil
}

func (s *HistoryStore) pruneWorktreeGroup(metas []*RunMeta, keep int) error {
	if keep < 0 {
		keep = 0
	}
	if len(metas) <= keep {
		return nil
	}
	for _, meta := range metas[keep:] {
		if err := os.RemoveAll(s.WorktreePath(meta.RunID)); err != nil {
			return err
		}
		meta.WorktreeKept = false
		if err := s.WriteMeta(meta); err != nil {
			return err
		}
		if err := s.FinalizeRun(meta.RunID); err != nil {
			return err
		}
	}
	return nil
}

func parseRunMeta(data []byte, runID string) (*RunMeta, error) {
	var meta RunMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", runID, err)
	}
	meta.ensureRunKey()
	return &meta, nil
}

func sanitizeTaskLabel(label string) string {
	return sanitizePathComponent(label)
}

func sanitizePathComponent(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteString("_")
			builder.WriteString(strconv.FormatInt(int64(r), 16))
		}
	}
	if builder.Len() == 0 {
		return "item"
	}
	return builder.String()
}
