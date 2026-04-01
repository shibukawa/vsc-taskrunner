package web

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vsc-taskrunner/internal/git"
	"vsc-taskrunner/internal/tasks"
	"vsc-taskrunner/internal/uiconfig"
)

type fakeRepositoryStore struct {
	prepareRunWorkspace func(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error)
	readTasksJSON       func(ctx context.Context, branch string) ([]byte, error)
	lastSparsePaths     []string
}

func (f *fakeRepositoryStore) BasePath() string { return "" }
func (f *fakeRepositoryStore) ListBranches(ctx context.Context) ([]git.Branch, error) {
	return nil, nil
}
func (f *fakeRepositoryStore) Refresh(ctx context.Context) error                    { return nil }
func (f *fakeRepositoryStore) FetchBranch(ctx context.Context, branch string) error { return nil }
func (f *fakeRepositoryStore) ReadBranchMetadata(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
	return "", time.Time{}, nil, nil
}
func (f *fakeRepositoryStore) ReadTasksJSON(ctx context.Context, branch string) ([]byte, error) {
	if f.readTasksJSON != nil {
		return f.readTasksJSON(ctx, branch)
	}
	return nil, nil
}
func (f *fakeRepositoryStore) PrepareRunWorkspace(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
	f.lastSparsePaths = append([]string(nil), sparsePaths...)
	if f.prepareRunWorkspace != nil {
		return f.prepareRunWorkspace(ctx, branch, workspacePath, sparsePaths)
	}
	return workspacePath, nil
}
func (f *fakeRepositoryStore) CleanupWorkspace(path string) error    { return nil }
func (f *fakeRepositoryStore) Maintenance(ctx context.Context) error { return nil }

func allowedTaskSpecs(specs ...uiconfig.AllowedTaskSpec) uiconfig.AllowedTaskSpecs {
	return uiconfig.AllowedTaskSpecs(specs)
}

func TestRunPreRunTasksExecutesMatchingHooksInOrder(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	target := filepath.Join(worktree, "hook.txt")
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "npm-*", Config: uiconfig.TaskUIConfig{PreRunTasks: []uiconfig.PreRunTaskConfig{
					{
						Command: "sh",
						Args:    []string{"-c", "printf first > hook.txt"},
						CWD:     "${workspaceFolder}",
					},
					{
						Command: "sh",
						Args:    []string{"-c", "printf second >> hook.txt"},
						CWD:     "${workspaceFolder}",
					},
				}}},
				uiconfig.AllowedTaskSpec{Pattern: "go-*", Config: uiconfig.TaskUIConfig{PreRunTasks: []uiconfig.PreRunTaskConfig{{
					Command: "sh",
					Args:    []string{"-c", "exit 99"},
				}}}},
			),
		},
	}

	var log bytes.Buffer
	active := &ActiveRun{
		Meta:         &RunMeta{RunID: "run-pre-success", TaskLabel: "npm-build"},
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm.history = history
	hooks := tm.config.MatchingPreRunTasks("npm-build")
	initializeSetupTaskRuns(active.Meta, active, len(hooks))
	if err := tm.runPreRunTasks(context.Background(), active, &log, "npm-build", worktree, hooks); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "firstsecond" {
		t.Fatalf("hook output = %q, want %q", string(data), "firstsecond")
	}
	logData, err := history.ReadTaskLog(active.Meta.RunID, "pre-run #1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logData, []byte("running pre-run task 1")) {
		t.Fatalf("pre-run #1 log = %q, want task output", string(logData))
	}
}

func TestRunPreRunTasksSkipsNonMatchingHooks(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "npm-*", Config: uiconfig.TaskUIConfig{PreRunTasks: []uiconfig.PreRunTaskConfig{{
					Command: "sh",
					Args:    []string{"-c", "exit 99"},
				}}}},
			),
		},
	}

	active := &ActiveRun{
		Meta:         &RunMeta{RunID: "run-pre-skip", TaskLabel: "go-test"},
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm.history = history
	hooks := tm.config.MatchingPreRunTasks("go-test")
	initializeSetupTaskRuns(active.Meta, active, len(hooks))
	if err := tm.runPreRunTasks(context.Background(), active, &bytes.Buffer{}, "go-test", worktree, hooks); err != nil {
		t.Fatalf("expected no-op for non-matching hook, got %v", err)
	}
}

func TestRunPreRunTasksFailsRunOnHookError(t *testing.T) {
	t.Parallel()

	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{PreRunTasks: []uiconfig.PreRunTaskConfig{{
					Command: "sh",
					Args:    []string{"-c", "exit 7"},
				}}}},
			),
		},
	}

	active := &ActiveRun{
		Meta:         &RunMeta{RunID: "run-pre-fail", TaskLabel: "build"},
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
	}
	history, historyErr := NewHistoryStore(t.TempDir())
	if historyErr != nil {
		t.Fatal(historyErr)
	}
	tm.history = history
	hooks := tm.config.MatchingPreRunTasks("build")
	initializeSetupTaskRuns(active.Meta, active, len(hooks))
	err := tm.runPreRunTasks(context.Background(), active, &bytes.Buffer{}, "build", t.TempDir(), hooks)
	if err == nil {
		t.Fatal("expected hook failure")
	}
	if got := active.taskRuns["pre-run #1"].Status; got != TaskRunStatusFailed {
		t.Fatalf("pre-run status = %q, want %q", got, TaskRunStatusFailed)
	}
}

func TestRunPreRunTasksRedactsSecretLikeNamesInDisplayLog(t *testing.T) {
	worktree := t.TempDir()
	t.Setenv("AWS_SECRET_ACCESS_KEY", "super-secret")
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "deploy", Config: uiconfig.TaskUIConfig{PreRunTasks: []uiconfig.PreRunTaskConfig{{
					Command: "sh",
					Args:    []string{"-c", "printf ok ${env:AWS_SECRET_ACCESS_KEY}"},
				}}}},
			),
		},
	}

	active := &ActiveRun{
		Meta:         &RunMeta{RunID: "run-pre-redact", TaskLabel: "deploy"},
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm.history = history
	hooks := tm.config.MatchingPreRunTasks("deploy")
	initializeSetupTaskRuns(active.Meta, active, len(hooks))
	if err := tm.runPreRunTasks(context.Background(), active, &bytes.Buffer{}, "deploy", worktree, hooks); err != nil {
		t.Fatal(err)
	}
	logData, err := history.ReadTaskLog(active.Meta.RunID, "pre-run #1")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logData, []byte("super-secret")) {
		t.Fatalf("pre-run log leaked secret: %q", string(logData))
	}
	if !bytes.Contains(logData, []byte("***")) {
		t.Fatalf("pre-run log missing redaction: %q", string(logData))
	}
}

func TestInitializeTaskRunsAddsSetupDependenciesToRootTasks(t *testing.T) {
	t.Parallel()

	meta := &RunMeta{TaskLabel: "build"}
	active := &ActiveRun{Meta: meta, taskRuns: make(map[string]*TaskRunMeta)}
	setupTasks := initializeSetupTaskRuns(meta, active, 2)
	catalog := &tasks.Catalog{
		Tasks: map[string]tasks.ResolvedTask{
			"build": {
				Label:        "build",
				DependsOn:    []string{"lint", "test"},
				DependsOrder: "parallel",
			},
			"lint": {Label: "lint"},
			"test": {Label: "test"},
		},
	}

	initializeTaskRuns(meta, active, catalog, "build", setupTailTaskLabel(setupTasks))

	if got := active.taskRuns["lint"].DependsOn; len(got) != 1 || got[0] != "pre-run #2" {
		t.Fatalf("lint dependsOn = %#v, want pre-run #2", got)
	}
	if got := active.taskRuns["test"].DependsOn; len(got) != 1 || got[0] != "pre-run #2" {
		t.Fatalf("test dependsOn = %#v, want pre-run #2", got)
	}
	if got := active.taskRuns["build"].DependsOn; len(got) != 2 {
		t.Fatalf("build dependsOn = %#v, want lint/test", got)
	}
}

func TestInitializeRunGraphBuildsPendingTasksBeforeExecution(t *testing.T) {
	t.Parallel()

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		repo: &fakeRepositoryStore{
			readTasksJSON: func(ctx context.Context, branch string) ([]byte, error) {
				return []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"process","command":"echo","dependsOn":["lint","test"]},{"label":"lint","type":"process","command":"echo"},{"label":"test","type":"process","command":"echo"}]}`), nil
			},
		},
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{PreRunTasks: []uiconfig.PreRunTaskConfig{{Command: "echo"}}}},
			),
		},
		history: history,
	}
	active := &ActiveRun{
		Meta: &RunMeta{
			RunID:     "run-init-graph",
			Branch:    "main",
			TaskLabel: "build",
			StartTime: time.Now().UTC(),
		},
		taskRuns: make(map[string]*TaskRunMeta),
	}

	if err := tm.initializeRunGraph(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{workspacePrepareTaskLabel, "pre-run #1", "lint", "test", "build"} {
		task := active.taskRuns[label]
		if task == nil {
			t.Fatalf("missing task %q", label)
		}
		if task.Status != TaskRunStatusPending {
			t.Fatalf("task %q status = %q, want pending", label, task.Status)
		}
	}
	if got := active.taskRuns["lint"].DependsOn; len(got) != 1 || got[0] != "pre-run #1" {
		t.Fatalf("lint dependsOn = %#v, want pre-run #1", got)
	}
	if got := active.taskRuns["build"].DependsOn; len(got) != 2 {
		t.Fatalf("build dependsOn = %#v, want lint/test", got)
	}
	if len(active.Meta.Tasks) != 5 {
		t.Fatalf("meta.Tasks = %d, want 5", len(active.Meta.Tasks))
	}
}

func TestDoRunMarksGitPrepareTaskFailed(t *testing.T) {
	t.Parallel()

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := "run-git-fail"
	if err := os.MkdirAll(history.RunDir(runID), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(history.LogPath(runID))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	tm := &TaskManager{
		repo: &fakeRepositoryStore{
			prepareRunWorkspace: func(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
				return "", fmt.Errorf("boom")
			},
		},
		config:  uiconfig.DefaultConfig(),
		history: history,
	}
	active := &ActiveRun{
		Meta: &RunMeta{
			RunID:     runID,
			Branch:    "main",
			TaskLabel: "build",
			StartTime: time.Now().UTC(),
		},
		logFile:      logFile,
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
	}

	err = tm.doRun(context.Background(), active)
	if err == nil {
		t.Fatal("expected git prepare error")
	}
	task := active.taskRuns[workspacePrepareTaskLabel]
	if task == nil {
		t.Fatal("missing prepare workspace task")
	}
	if task.Status != TaskRunStatusFailed {
		t.Fatalf("prepare workspace status = %q, want %q", task.Status, TaskRunStatusFailed)
	}
	data, err := os.ReadFile(history.TaskLogPath(runID, workspacePrepareTaskLabel))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("workspace for branch")) {
		t.Fatalf("prepare workspace log = %q, want workspace message", string(data))
	}
}

func TestDoRunUsesRunSparsePathsByDefault(t *testing.T) {
	t.Parallel()

	repo := &fakeRepositoryStore{
		prepareRunWorkspace: func(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
			if err := os.MkdirAll(filepath.Join(workspacePath, ".vscode"), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(workspacePath, ".vscode", "tasks.json"), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"process","command":"sh","args":["-c","printf ok"]}]}`), 0o644); err != nil {
				return "", err
			}
			return workspacePath, nil
		},
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := "run-default-sparse-paths"
	if err := os.MkdirAll(history.RunDir(runID), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(history.LogPath(runID))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	tm := &TaskManager{
		repo: repo,
		config: &uiconfig.UIConfig{
			Repository: uiconfig.RepositoryConfig{
				Source: "/tmp/local-repo",
			},
		},
		history: history,
	}
	active := &ActiveRun{
		Meta: &RunMeta{
			RunID:     runID,
			Branch:    "feature/demo",
			TaskLabel: "build",
			StartTime: time.Now().UTC(),
		},
		logFile:      logFile,
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
	}

	if err := tm.doRun(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(repo.lastSparsePaths, ","), ""; got != want {
		t.Fatalf("PrepareRunWorkspace sparse paths = %q, want %q", got, want)
	}
}

func TestDoRunUsesTasksSparsePathsWhenConfigured(t *testing.T) {
	t.Parallel()

	repo := &fakeRepositoryStore{
		prepareRunWorkspace: func(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
			if err := os.MkdirAll(filepath.Join(workspacePath, ".vscode"), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(workspacePath, ".vscode", "tasks.json"), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"process","command":"sh","args":["-c","printf ok"]}]}`), 0o644); err != nil {
				return "", err
			}
			return workspacePath, nil
		},
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := "run-tasks-sparse-paths"
	if err := os.MkdirAll(history.RunDir(runID), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(history.LogPath(runID))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	tm := &TaskManager{
		repo: repo,
		config: &uiconfig.UIConfig{
			Repository: uiconfig.RepositoryConfig{
				Source: "/tmp/local-repo",
			},
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Worktree: &uiconfig.TaskWorktreeConfig{Disabled: boolPtr(true)}}},
			),
		},
		history: history,
	}
	active := &ActiveRun{
		Meta: &RunMeta{
			RunID:     runID,
			Branch:    "ops/deploy",
			TaskLabel: "build",
			StartTime: time.Now().UTC(),
		},
		logFile:      logFile,
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
	}

	if err := tm.doRun(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(repo.lastSparsePaths, ","), uiconfig.DefaultTasksSparsePath; got != want {
		t.Fatalf("PrepareRunWorkspace sparse paths = %q, want %q", got, want)
	}
}

func TestResolveRunCatalogSkipsInputsOutsideSelectedTaskGraph(t *testing.T) {
	t.Parallel()

	tm := &TaskManager{}
	file := &tasks.File{
		Version: "2.0.0",
		Inputs: []tasks.Input{
			{ID: "unused", Type: "promptString", Description: "unused value"},
		},
		Tasks: []tasks.Task{
			{
				Label:   "build",
				Type:    "process",
				Command: tasks.TokenValue{Value: "echo", Set: true},
				Args:    []tasks.TokenValue{{Value: "ok", Set: true}},
			},
			{
				Label:   "other",
				Type:    "process",
				Command: tasks.TokenValue{Value: "echo", Set: true},
				Args:    []tasks.TokenValue{{Value: "${input:unused}", Set: true}},
			},
		},
	}

	catalog, err := tm.resolveRunCatalog(file, t.TempDir(), "/tmp/demo/.vscode/tasks.json", "build", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tasks) != 1 {
		t.Fatalf("resolved tasks = %d, want 1", len(catalog.Tasks))
	}
	if _, ok := catalog.Tasks["other"]; ok {
		t.Fatal("unexpected unrelated task in resolved catalog")
	}
}

func TestCollectArtifactsAsZipByDefault(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "assets", "app.js"), []byte("console.log('demo')"), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{{Path: "dist", Format: "zip"}}}},
			),
		},
		history: history,
	}

	refs, err := tm.collectArtifacts(&RunMeta{
		RunID:     "run-1",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("artifact refs = %d, want 1", len(refs))
	}
	if refs[0].Dest != "artifacts.zip" || refs[0].Format != "zip" {
		t.Fatalf("unexpected ref: %+v", refs[0])
	}

	reader, err := zip.OpenReader(filepath.Join(history.ArtifactDir("run-1"), "artifacts.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if len(reader.File) != 2 {
		t.Fatalf("unexpected zip entries: %+v", reader.File)
	}
	if reader.File[0].Name != "dist/assets/app.js" || reader.File[1].Name != "dist/index.html" {
		t.Fatalf("unexpected zip entries: %+v", reader.File)
	}
}

func TestCollectArtifactsZipWildcardFlattensTopLevelDirectory(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "assets", "app.js"), []byte("console.log('demo')"), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{{Path: "dist/*", Format: "zip"}}}},
			),
		},
		history: history,
	}

	_, err = tm.collectArtifacts(&RunMeta{
		RunID:     "run-1",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(filepath.Join(history.ArtifactDir("run-1"), "artifacts.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if len(reader.File) != 2 {
		t.Fatalf("unexpected zip entries: %+v", reader.File)
	}
	if reader.File[0].Name != "assets/app.js" || reader.File[1].Name != "index.html" {
		t.Fatalf("unexpected zip entries: %+v", reader.File)
	}
}

func TestCollectArtifactsAsFilesWithExplicitFile(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{{Path: "dist/index.html", Format: "file"}}}},
			),
		},
		history: history,
	}

	refs, err := tm.collectArtifacts(&RunMeta{
		RunID:     "run-1",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("artifact refs = %d, want 1", len(refs))
	}
	if refs[0].Dest != "dist/index.html" || refs[0].Format != "file" {
		t.Fatalf("unexpected ref: %+v", refs[0])
	}
	if _, err := os.Stat(filepath.Join(history.ArtifactDir("run-1"), "dist", "index.html")); err != nil {
		t.Fatal(err)
	}
}

func TestCollectArtifactsAsFilesWildcardKeepsMatchedRelativePaths(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "assets", "app.js"), []byte("console.log('demo')"), 0o644); err != nil {
		t.Fatal(err)
	}

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{{Path: "dist/*.html", Format: "file"}, {Path: "dist/assets/*.js", Format: "file"}}}},
			),
		},
		history: history,
	}

	refs, err := tm.collectArtifacts(&RunMeta{
		RunID:     "run-file-wildcard",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("artifact refs = %d, want 2", len(refs))
	}
	got := []string{refs[0].Dest, refs[1].Dest}
	sort.Strings(got)
	if got[0] != "app.js" || got[1] != "index.html" {
		t.Fatalf("unexpected refs: %+v", refs)
	}
	if _, err := os.Stat(filepath.Join(history.ArtifactDir("run-file-wildcard"), "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(history.ArtifactDir("run-file-wildcard"), "app.js")); err != nil {
		t.Fatal(err)
	}
}

func TestCollectArtifactsAsFilesRejectsDirectoryPattern(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{{Path: "dist", Format: "file"}}}},
			),
		},
		history: history,
	}

	_, err = tm.collectArtifacts(&RunMeta{
		RunID:     "run-1",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err == nil || !strings.Contains(err.Error(), "format=file") {
		t.Fatalf("err = %v, want format=file error", err)
	}
}

func TestCollectArtifactsAsFilesRejectsWildcardDirectoryMatch(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "assets", "app.js"), []byte("console.log('demo')"), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{{Path: "dist/*", Format: "file"}}}},
			),
		},
		history: history,
	}

	_, err = tm.collectArtifacts(&RunMeta{
		RunID:     "run-1",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err == nil || !strings.Contains(err.Error(), "format=file") {
		t.Fatalf("err = %v, want format=file error", err)
	}
}

func TestCollectArtifactsZipResolvesTemplate(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = worktree
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}
	runGit("init")
	if err := os.MkdirAll(filepath.Join(worktree, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "init")

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{
					Artifacts: []uiconfig.ArtifactRuleConfig{{
						Path:         "dist",
						Format:       "zip",
						NameTemplate: "frontend-{branch}-b{buildno}-{yyyymmdd}-{hhmmss}-{hash}-{longhash}.zip",
					}},
				}},
			),
		},
		history: history,
	}

	meta := &RunMeta{
		RunID:     "run-2",
		Branch:    "feature/frontend",
		TaskLabel: "build",
		RunNumber: 17,
		StartTime: time.Date(2026, time.March, 28, 9, 30, 0, 0, time.UTC),
	}
	fullHashOutput, err := exec.Command("git", "-C", worktree, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v\n%s", err, string(fullHashOutput))
	}
	meta.CommitHash = strings.TrimSpace(string(fullHashOutput))
	refs, err := tm.collectArtifacts(meta, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("artifact refs = %d, want 1", len(refs))
	}
	wantPrefix := "frontend-feature-frontend-b17-20260328-093000"
	if !strings.HasPrefix(refs[0].Dest, wantPrefix) || !strings.HasSuffix(refs[0].Dest, ".zip") {
		t.Fatalf("archive name = %q, want prefix %q and .zip suffix", refs[0].Dest, wantPrefix)
	}
	base := strings.TrimSuffix(refs[0].Dest, ".zip")
	shortHash := meta.CommitHash[:7]
	if !strings.Contains(base, "093000-"+shortHash+"-") {
		t.Fatalf("archive name = %q, want short hash %q after hhmmss separator", refs[0].Dest, shortHash)
	}
	if !strings.HasSuffix(base, meta.CommitHash) {
		t.Fatalf("archive name = %q, want long hash suffix %q", refs[0].Dest, meta.CommitHash)
	}
}

func TestCollectArtifactsZipResolvesInputPlaceholder(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{
					Artifacts: []uiconfig.ArtifactRuleConfig{{
						Path:         "dist",
						Format:       "zip",
						NameTemplate: "frontend-{input:env}-{input:missing}.zip",
					}},
				}},
			),
		},
		history: history,
	}

	refs, err := tm.collectArtifacts(&RunMeta{
		RunID:       "run-input-template",
		Branch:      "main",
		TaskLabel:   "build",
		RunNumber:   1,
		StartTime:   time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
		InputValues: map[string]string{"env": "prod/east"},
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("artifact refs = %d, want 1", len(refs))
	}
	if got, want := refs[0].Dest, "frontend-prod-east-unknown.zip"; got != want {
		t.Fatalf("archive name = %q, want %q", got, want)
	}
}

func TestCollectArtifactsCreatesOneZipPerRule(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "assets", "app.js"), []byte("console.log('demo')"), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{
					{Path: "dist/index.html", NameTemplate: "page.zip"},
					{Path: "dist/assets/*", NameTemplate: "assets.zip"},
				}}},
			),
		},
		history: history,
	}

	refs, err := tm.collectArtifacts(&RunMeta{
		RunID:     "run-zip-rules",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("artifact refs = %d, want 2", len(refs))
	}
	if refs[0].Dest != "page.zip" || refs[1].Dest != "assets.zip" {
		t.Fatalf("unexpected refs: %+v", refs)
	}
}

func TestCollectArtifactsAllowsDuplicateMatchesAcrossRules(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dist", "index.html"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tm := &TaskManager{
		config: &uiconfig.UIConfig{
			Tasks: allowedTaskSpecs(
				uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{Artifacts: []uiconfig.ArtifactRuleConfig{
					{Path: "dist/index.html", NameTemplate: "one.zip"},
					{Path: "dist/*", NameTemplate: "two.zip"},
				}}},
			),
		},
		history: history,
	}

	refs, err := tm.collectArtifacts(&RunMeta{
		RunID:     "run-dup-rules",
		Branch:    "main",
		TaskLabel: "build",
		StartTime: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("artifact refs = %d, want 2", len(refs))
	}
	for _, name := range []string{"one.zip", "two.zip"} {
		reader, err := zip.OpenReader(filepath.Join(history.ArtifactDir("run-dup-rules"), name))
		if err != nil {
			t.Fatal(err)
		}
		if len(reader.File) != 1 || reader.File[0].Name != "dist/index.html" && reader.File[0].Name != "index.html" {
			_ = reader.Close()
			t.Fatalf("unexpected zip entries in %s: %+v", name, reader.File)
		}
		_ = reader.Close()
	}
}
