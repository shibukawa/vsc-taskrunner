package web

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAllocateRunUsesBranchTaskScopedSequence(t *testing.T) {
	t.Parallel()

	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.AllocateRun("main", "build")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AllocateRun("main", "build")
	if err != nil {
		t.Fatal(err)
	}
	otherBranch, err := store.AllocateRun("feature/demo", "build")
	if err != nil {
		t.Fatal(err)
	}
	otherTask, err := store.AllocateRun("main", "test")
	if err != nil {
		t.Fatal(err)
	}

	if first.RunNumber != 1 || second.RunNumber != 2 {
		t.Fatalf("main/build sequence = %d,%d, want 1,2", first.RunNumber, second.RunNumber)
	}
	if otherBranch.RunNumber != 1 {
		t.Fatalf("feature/demo build runNumber = %d, want 1", otherBranch.RunNumber)
	}
	if otherTask.RunNumber != 1 {
		t.Fatalf("main test runNumber = %d, want 1", otherTask.RunNumber)
	}

	wantPath := filepath.Join(store.historyDir, first.RunID)
	if got := store.RunDir(first.RunID); got != wantPath {
		t.Fatalf("RunDir() = %q, want %q", got, wantPath)
	}
}

func TestAllocateRunCreatesMissingHistoryIndexLockDir(t *testing.T) {
	t.Parallel()

	historyDir := filepath.Join(t.TempDir(), "nested", "history")
	store, err := NewHistoryStore(historyDir)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := store.AllocateRun("main", "build")
	if err != nil {
		t.Fatal(err)
	}
	if ref.RunID == "" {
		t.Fatal("expected run allocation to succeed")
	}
	if _, err := os.Stat(filepath.Join(historyDir, historyIndexObjectName+".lock")); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}
}

func TestAllocateRunDoesNotReuseNumberAfterPrune(t *testing.T) {
	t.Parallel()

	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 3; i++ {
		ref, allocErr := store.AllocateRun("main", "build")
		if allocErr != nil {
			t.Fatal(allocErr)
		}
		meta := &RunMeta{
			RunID:     ref.RunID,
			RunKey:    ref.RunID,
			Branch:    ref.Branch,
			TaskLabel: ref.TaskLabel,
			RunNumber: ref.RunNumber,
			Status:    RunStatusSuccess,
			StartTime: time.Unix(int64(i), 0).UTC(),
		}
		if writeErr := store.WriteMeta(meta); writeErr != nil {
			t.Fatal(writeErr)
		}
		if updateErr := store.RecordRunCompletion(meta, 1); updateErr != nil {
			t.Fatal(updateErr)
		}
	}

	ref, err := store.AllocateRun("main", "build")
	if err != nil {
		t.Fatal(err)
	}
	if ref.RunNumber != 4 {
		t.Fatalf("runNumber after prune = %d, want 4", ref.RunNumber)
	}
}

func TestPruneKeepsRunsPerBranchTaskGroup(t *testing.T) {
	t.Parallel()

	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	writeRun := func(branch, task string, start int64) {
		t.Helper()
		ref, allocErr := store.AllocateRun(branch, task)
		if allocErr != nil {
			t.Fatal(allocErr)
		}
		meta := &RunMeta{
			RunID:     ref.RunID,
			RunKey:    ref.RunID,
			Branch:    branch,
			TaskLabel: task,
			RunNumber: ref.RunNumber,
			Status:    RunStatusSuccess,
			StartTime: time.Unix(start, 0).UTC(),
		}
		if writeErr := store.WriteMeta(meta); writeErr != nil {
			t.Fatal(writeErr)
		}
		if updateErr := store.RecordRunCompletion(meta, 2); updateErr != nil {
			t.Fatal(updateErr)
		}
	}

	writeRun("main", "build", 1)
	writeRun("main", "build", 2)
	writeRun("main", "build", 3)
	writeRun("develop", "build", 4)
	writeRun("develop", "build", 5)
	writeRun("main", "test", 6)

	metas, err := store.List()
	if err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{}
	for _, meta := range metas {
		counts[meta.Branch+"|"+meta.TaskLabel]++
	}
	if got := counts["main|build"]; got != 2 {
		t.Fatalf("main|build retained %d runs, want 2", got)
	}
	if got := counts["develop|build"]; got != 2 {
		t.Fatalf("develop|build retained %d runs, want 2", got)
	}
	if got := counts["main|test"]; got != 1 {
		t.Fatalf("main|test retained %d runs, want 1", got)
	}
}

func TestAllocateRunIsUniqueUnderConcurrency(t *testing.T) {
	t.Parallel()

	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const workers = 12
	results := make(chan int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref, allocErr := store.AllocateRunWithUser("main", "build", "alice")
			if allocErr != nil {
				t.Error(allocErr)
				return
			}
			results <- ref.RunNumber
		}()
	}
	wg.Wait()
	close(results)

	seen := map[int]bool{}
	for runNumber := range results {
		if seen[runNumber] {
			t.Fatalf("duplicate runNumber allocated: %d", runNumber)
		}
		seen[runNumber] = true
	}
	if len(seen) != workers {
		t.Fatalf("allocated %d run numbers, want %d", len(seen), workers)
	}
}

func TestListUsesIndexedHistoryWithoutMetaScan(t *testing.T) {
	t.Parallel()

	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	ref, err := store.AllocateRunWithUser("main", "build", "alice")
	if err != nil {
		t.Fatal(err)
	}
	metas, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("List() count = %d, want 1", len(metas))
	}
	if metas[0].RunID != ref.RunID || metas[0].RunNumber != ref.RunNumber || metas[0].Status != RunStatusRunning {
		t.Fatalf("List()[0] = %+v", metas[0])
	}
}

func TestListRebuildsIndexFromMetaFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewHistoryStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	meta := &RunMeta{
		RunID:     "run-1",
		RunKey:    "",
		Branch:    "main",
		TaskLabel: "build",
		Status:    RunStatusFailed,
		StartTime: time.Unix(10, 0).UTC(),
		EndTime:   time.Unix(20, 0).UTC(),
		ExitCode:  1,
		User:      "alice",
	}
	if err := store.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, historyIndexObjectName)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	metas, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("List() count = %d, want 1", len(metas))
	}
	if metas[0].RunID != "run-1" || metas[0].RunNumber != 1 {
		t.Fatalf("unexpected rebuilt meta: %+v", metas[0])
	}
	if _, err := os.Stat(filepath.Join(dir, historyIndexObjectName)); err != nil {
		t.Fatalf("expected rebuilt index file: %v", err)
	}
}

func TestListRebuildsIndexFromRunStoreListing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewHistoryStoreWithStores(dir, NewLocalIndexStore(dir), &historyTestRunStore{
		metas: []*RunMeta{{
			RunID:     "run-remote-1",
			Branch:    "main",
			TaskLabel: "build",
			Status:    RunStatusSuccess,
			StartTime: time.Unix(100, 0).UTC(),
			EndTime:   time.Unix(120, 0).UTC(),
			ExitCode:  0,
			User:      "alice",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	metas, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("List() count = %d, want 1", len(metas))
	}
	if metas[0].RunID != "run-remote-1" || metas[0].RunNumber != 1 {
		t.Fatalf("unexpected rebuilt meta: %+v", metas[0])
	}
	if _, err := os.Stat(filepath.Join(dir, historyIndexObjectName)); err != nil {
		t.Fatalf("expected rebuilt index file: %v", err)
	}
}

type historyTestRunStore struct {
	metas []*RunMeta
}

func (s *historyTestRunStore) RunDir(runID string) string                            { return "" }
func (s *historyTestRunStore) LogPath(runID string) string                           { return "" }
func (s *historyTestRunStore) TaskLogPath(runID, taskLabel string) string            { return "" }
func (s *historyTestRunStore) WorktreePath(runID string) string                      { return "" }
func (s *historyTestRunStore) ArtifactDir(runID string) string                       { return "" }
func (s *historyTestRunStore) MetaPath(runID string) string                          { return "" }
func (s *historyTestRunStore) WriteMeta(meta *RunMeta) error                         { return nil }
func (s *historyTestRunStore) ListMetas(ctx context.Context) ([]*RunMeta, error)     { return s.metas, nil }
func (s *historyTestRunStore) ReadMeta(runID string) (*RunMeta, error)               { return nil, os.ErrNotExist }
func (s *historyTestRunStore) ReadLog(runID string) ([]byte, error)                  { return nil, os.ErrNotExist }
func (s *historyTestRunStore) ReadTaskLog(runID, taskLabel string) ([]byte, error)   { return nil, os.ErrNotExist }
func (s *historyTestRunStore) TailLog(runID string, byteOffset int64) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (s *historyTestRunStore) ListWorktreeFiles(runID string) ([]string, error) { return nil, nil }
func (s *historyTestRunStore) ReadWorktreeFile(runID, filePath string) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (s *historyTestRunStore) ListArtifactFiles(runID string) ([]string, error) {
	return nil, nil
}
func (s *historyTestRunStore) StatArtifactFile(runID, filePath string) (ArtifactFileInfo, error) {
	return ArtifactFileInfo{}, os.ErrNotExist
}
func (s *historyTestRunStore) ReadArtifactFile(runID, filePath string) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (s *historyTestRunStore) DeleteRun(runID string) error   { return nil }
func (s *historyTestRunStore) FinalizeRun(runID string) error { return nil }
