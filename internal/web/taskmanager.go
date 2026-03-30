package web

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vsc-taskrunner/internal/git"
	"vsc-taskrunner/internal/tasks"
	"vsc-taskrunner/internal/uiconfig"
)

// ActiveRun holds the in-memory state of a currently executing (or just-finished) run.
type ActiveRun struct {
	Meta *RunMeta

	// logFile is the open file descriptor for stdout.log (nil after close).
	logFile      *os.File
	taskLogFiles map[string]*os.File

	// mu protects sseSubscribers.
	mu             sync.Mutex
	sseSubscribers []chan sseMessage
	taskRuns       map[string]*TaskRunMeta

	// done is closed when the run finishes (success or failure).
	done chan struct{}
}

type sseMessage struct {
	eventType string
	data      []byte
}

const workspacePrepareTaskLabel = "prepare workspace"

// subscribe registers a channel to receive log chunks in real time.
// The channel is automatically removed when the run ends.
func (r *ActiveRun) subscribe() chan sseMessage {
	ch := make(chan sseMessage, 64)
	r.mu.Lock()
	r.sseSubscribers = append(r.sseSubscribers, ch)
	r.mu.Unlock()
	return ch
}

func (r *ActiveRun) unsubscribe(ch chan sseMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.sseSubscribers {
		if c == ch {
			r.sseSubscribers = append(r.sseSubscribers[:i], r.sseSubscribers[i+1:]...)
			return
		}
	}
}

// broadcast sends a chunk of log output to all SSE subscribers.
func (r *ActiveRun) broadcast(eventType string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.sseSubscribers {
		select {
		case ch <- sseMessage{eventType: eventType, data: append([]byte(nil), data...)}:
		default:
			// Subscriber is slow; drop rather than block.
		}
	}
}

// closeSubscribers closes all SSE subscriber channels, signalling stream end.
func (r *ActiveRun) closeSubscribers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.sseSubscribers {
		close(ch)
	}
	r.sseSubscribers = nil
}

// broadcastingWriter is an io.Writer that writes to a log file AND broadcasts
// each chunk to registered SSE subscribers.
type broadcastingWriter struct {
	file *os.File
}

func (w *broadcastingWriter) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

type liveTaskLineWriter struct {
	target    io.Writer
	active    *ActiveRun
	taskLabel string
	pending   []byte
}

func (w *liveTaskLineWriter) Write(p []byte) (int, error) {
	n, err := w.target.Write(p)
	if n > 0 {
		w.pending = append(w.pending, p[:n]...)
		w.flushCompleteLines()
	}
	return n, err
}

func (w *liveTaskLineWriter) Close() error {
	if len(w.pending) == 0 {
		return nil
	}
	w.broadcastLine(string(w.pending))
	w.pending = nil
	return nil
}

func (w *liveTaskLineWriter) flushCompleteLines() {
	start := 0
	for index, b := range w.pending {
		if b != '\n' {
			continue
		}
		w.broadcastLine(string(w.pending[start : index+1]))
		start = index + 1
	}
	if start > 0 {
		w.pending = append([]byte(nil), w.pending[start:]...)
	}
}

func (w *liveTaskLineWriter) broadcastLine(line string) {
	payload, err := json.Marshal(sseTaskEvent{
		Type:      string(tasks.TaskEventLine),
		TaskLabel: w.taskLabel,
		Line:      line,
	})
	if err != nil {
		return
	}
	w.active.broadcast(string(tasks.TaskEventLine), payload)
}

type setupTaskInfo struct {
	label     string
	dependsOn []string
}

// TaskManager manages task execution runs, history, and resource limits.
type TaskManager struct {
	repo    git.RepositoryStore
	config  *uiconfig.UIConfig
	history *HistoryStore

	// sem limits concurrent runs; nil means unlimited.
	sem chan struct{}

	mu         sync.RWMutex
	activeRuns map[string]*ActiveRun
}

// NewTaskManager creates a TaskManager.
func NewTaskManager(repo git.RepositoryStore, config *uiconfig.UIConfig, history *HistoryStore) *TaskManager {
	tm := &TaskManager{
		repo:       repo,
		config:     config,
		history:    history,
		activeRuns: make(map[string]*ActiveRun),
	}
	if config.Execution.MaxParallelRuns > 0 {
		tm.sem = make(chan struct{}, config.Execution.MaxParallelRuns)
	}
	return tm
}

// GetActiveRun returns the in-memory ActiveRun for a run, or nil if not found.
func (tm *TaskManager) GetActiveRun(ref RunRef) *ActiveRun {
	runID, err := tm.history.LookupRunID(ref.Branch, ref.TaskLabel, ref.RunNumber)
	if err != nil {
		return nil
	}
	return tm.GetActiveRunByID(runID)
}

func (tm *TaskManager) GetActiveRunByID(runID string) *ActiveRun {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.activeRuns[runID]
}

func (tm *TaskManager) hasActiveRuns() bool {
	if tm == nil {
		return false
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.activeRuns) > 0
}

// StartRun begins executing taskLabel from branch in the background.
// It returns the new run metadata immediately; the run itself runs asynchronously.
func (tm *TaskManager) StartRun(ctx context.Context, branch, taskLabel, user string) (*RunMeta, error) {
	return tm.StartRunWithInputs(ctx, branch, taskLabel, user, "", nil)
}

func (tm *TaskManager) StartRunWithInputs(ctx context.Context, branch, taskLabel, user, tokenLabel string, inputValues map[string]string) (*RunMeta, error) {
	meta, err := tm.history.AllocateRunWithUser(branch, taskLabel, user, tokenLabel)
	if err != nil {
		tm.logRunStartFailure(&RunMeta{Branch: branch, TaskLabel: taskLabel, User: user, TokenLabel: tokenLabel}, "allocate run", err)
		return nil, fmt.Errorf("allocate run: %w", err)
	}

	if meta.RunID == "" {
		tm.logRunStartFailure(meta, "allocate run", errors.New("missing runId"))
		return nil, fmt.Errorf("allocate run: missing runId")
	}
	meta.RunKey = meta.RunID
	meta.Branch = branch
	meta.TaskLabel = taskLabel
	meta.Status = RunStatusRunning
	meta.StartTime = time.Now().UTC()
	meta.User = user
	meta.TokenLabel = tokenLabel
	meta.InputValues = cloneStringMap(inputValues)

	active := &ActiveRun{
		Meta:         meta,
		taskLogFiles: make(map[string]*os.File),
		taskRuns:     make(map[string]*TaskRunMeta),
		done:         make(chan struct{}),
	}
	if err := tm.initializeRunGraph(ctx, active); err != nil {
		tm.logRunStartFailure(meta, "initialize run graph", err)
		_ = tm.history.AbortRun(meta)
		return nil, fmt.Errorf("initialize run graph: %w", err)
	}
	if err := os.MkdirAll(tm.history.RunDir(meta.RunID), 0o755); err != nil {
		tm.logRunStartFailure(meta, "create run dir", err)
		_ = tm.history.AbortRun(meta)
		return nil, fmt.Errorf("create run dir: %w", err)
	}
	logFile, err := os.Create(tm.history.LogPath(meta.RunID))
	if err != nil {
		tm.logRunStartFailure(meta, "create log file", err)
		_ = tm.history.AbortRun(meta)
		return nil, fmt.Errorf("create log file: %w", err)
	}
	active.logFile = logFile
	if err := tm.history.WriteMeta(meta); err != nil {
		tm.logRunStartFailure(meta, "write initial meta", err)
		logFile.Close()
		_ = tm.history.AbortRun(meta)
		return nil, fmt.Errorf("write initial meta: %w", err)
	}
	if err := tm.history.RecordRunCompletion(meta, tm.config.Storage.HistoryKeepCount); err != nil {
		tm.logRunStartFailure(meta, "write initial history index", err)
		logFile.Close()
		_ = tm.history.AbortRun(meta)
		return nil, fmt.Errorf("write initial history index: %w", err)
	}

	tm.mu.Lock()
	tm.activeRuns[meta.RunID] = active
	tm.mu.Unlock()

	log.Printf("runtask run started run_id=%q branch=%q task=%q user=%q run_number=%d", meta.RunID, meta.Branch, meta.TaskLabel, meta.User, meta.RunNumber)

	go tm.executeRun(context.Background(), active)

	return meta, nil
}

func (tm *TaskManager) logRunStartFailure(meta *RunMeta, stage string, err error) {
	if err == nil {
		return
	}
	historyDir := ""
	runID := ""
	runDir := ""
	logPath := ""
	branch := ""
	taskLabel := ""
	user := ""
	tokenLabel := ""
	if tm != nil && tm.history != nil {
		historyDir = tm.history.historyDir
	}
	if meta != nil {
		runID = meta.RunID
		branch = meta.Branch
		taskLabel = meta.TaskLabel
		user = meta.User
		tokenLabel = meta.TokenLabel
		if tm != nil && tm.history != nil && meta.RunID != "" {
			runDir = tm.history.RunDir(meta.RunID)
			logPath = tm.history.LogPath(meta.RunID)
		}
	}
	log.Printf("runtask run start failed stage=%q run_id=%q branch=%q task=%q user=%q token_label=%q history_dir=%q run_dir=%q log_path=%q error=%v", stage, runID, branch, taskLabel, user, tokenLabel, historyDir, runDir, logPath, err)
}

// executeRun performs the full lifecycle of a run inside a goroutine.
func (tm *TaskManager) executeRun(ctx context.Context, active *ActiveRun) {
	defer func() {
		if active.logFile != nil {
			_ = active.logFile.Close()
			active.logFile = nil
		}
		for label, file := range active.taskLogFiles {
			_ = file.Close()
			delete(active.taskLogFiles, label)
		}
		active.closeSubscribers()
		close(active.done)

		tm.mu.Lock()
		delete(tm.activeRuns, active.Meta.RunID)
		tm.mu.Unlock()

		_ = tm.history.FinalizeRun(active.Meta.RunID)

		// Prune old history and worktrees.
		_ = tm.history.Prune(tm.config.Storage.HistoryKeepCount)
		_ = tm.history.PruneWorktrees(tm.config.Storage.Worktree.KeepOnSuccess, tm.config.Storage.Worktree.KeepOnFailure)
	}()

	if tm.sem != nil {
		select {
		case tm.sem <- struct{}{}:
			defer func() { <-tm.sem }()
		case <-ctx.Done():
			tm.failRun(active, ctx.Err())
			return
		}
	}

	runErr := tm.doRun(ctx, active)
	if runErr != nil {
		tm.failRun(active, runErr)
	}
}

// doRun implements the core run logic:
//  1. prepare a sparse workspace
//  2. load & resolve tasks.json from worktree
//  3. execute the task
//  4. collect artifacts
//  5. update meta.yaml
func (tm *TaskManager) doRun(ctx context.Context, active *ActiveRun) error {
	meta := active.Meta
	worktreePath := tm.history.WorktreePath(meta.RunID)
	bw := &broadcastingWriter{file: active.logFile}
	preRunHooks := tm.config.MatchingPreRunTasks(meta.TaskLabel)
	setupTasks := collectSetupTaskInfo(len(preRunHooks))
	active.mu.Lock()
	needsSetupInitialization := len(active.taskRuns) == 0
	active.mu.Unlock()
	if needsSetupInitialization {
		initializeSetupTaskRuns(meta, active, len(preRunHooks))
	}
	if writeErr := tm.history.WriteMeta(meta); writeErr != nil {
		fmt.Fprintf(bw, "=== runtask: warning: failed to write setup task metadata: %v ===\n", writeErr)
	}

	// 1. Prepare workspace.
	workspaceLog, err := tm.newTaskStageWriter(active, bw, workspacePrepareTaskLabel)
	if err != nil {
		return fmt.Errorf("create workspace prepare task log: %w", err)
	}
	sparseRunWorkspace := tm.config.UseSparseRunWorkspace(meta.TaskLabel)
	workspaceSparsePaths := []string{}
	workspaceMode := "full"
	if sparseRunWorkspace {
		workspaceSparsePaths = []string{uiconfig.DefaultTasksSparsePath}
		workspaceMode = "tasks-sparse"
	}
	tm.handleTaskEvent(active, tasks.TaskEvent{
		Type:      tasks.TaskEventStart,
		TaskLabel: workspacePrepareTaskLabel,
		Status:    "running",
		StartTime: time.Now().UTC(),
	})
	fmt.Fprintf(workspaceLog, "=== runtask: preparing %s workspace for branch %s task %s ===\n", workspaceMode, meta.Branch, meta.TaskLabel)
	if _, err := tm.repo.PrepareRunWorkspace(ctx, meta.Branch, worktreePath, workspaceSparsePaths); err != nil {
		_ = workspaceLog.Close()
		tm.handleTaskEvent(active, tasks.TaskEvent{
			Type:      tasks.TaskEventFinish,
			TaskLabel: workspacePrepareTaskLabel,
			Status:    "failed",
			ExitCode:  1,
			EndTime:   time.Now().UTC(),
		})
		return fmt.Errorf("prepare workspace: %w", err)
	}
	if commitHash, err := git.CurrentCommitHash(worktreePath); err == nil {
		meta.CommitHash = strings.TrimSpace(commitHash)
	}
	_ = workspaceLog.Close()
	tm.handleTaskEvent(active, tasks.TaskEvent{
		Type:      tasks.TaskEventFinish,
		TaskLabel: workspacePrepareTaskLabel,
		Status:    "success",
		ExitCode:  0,
		EndTime:   time.Now().UTC(),
	})
	defer func() {
		if active.Meta.WorktreeKept {
			return
		}
		_ = tm.repo.CleanupWorkspace(worktreePath)
	}()

	// 2. Load tasks.json from the worktree.
	tasksPath := filepath.Join(worktreePath, ".vscode", "tasks.json")
	loadOptions := tasks.ResolveLoadOptions(tasksPath, worktreePath)
	file, err := tasks.LoadFile(loadOptions)
	if err != nil {
		return fmt.Errorf("load tasks.json: %w", err)
	}

	catalog, err := tm.resolveRunCatalog(file, worktreePath, tasksPath, meta.TaskLabel, meta.InputValues)
	if err != nil {
		return fmt.Errorf("resolve tasks: %w", err)
	}
	active.mu.Lock()
	rootInitialized := active.taskRuns[meta.TaskLabel] != nil
	active.mu.Unlock()
	if !rootInitialized {
		initializeTaskRuns(meta, active, catalog, meta.TaskLabel, setupTailTaskLabel(setupTasks))
		if writeErr := tm.history.WriteMeta(meta); writeErr != nil {
			fmt.Fprintf(bw, "=== runtask: warning: failed to write task metadata: %v ===\n", writeErr)
		}
	}

	if err := tm.runPreRunTasks(ctx, active, bw, meta.TaskLabel, worktreePath, preRunHooks); err != nil {
		return err
	}

	// 3. Execute the task.
	fmt.Fprintf(bw, "=== runtask: running task %q ===\n", meta.TaskLabel)
	runner := tasks.NewRunnerWithOptions(catalog, bw, bw, tasks.RunnerOptions{
		OutputMode: tasks.OutputModeDefault,
		ColorMode:  tasks.ColorModeAlways,
		OutputFile: active.logFile,
		EventHandler: func(event tasks.TaskEvent) {
			tm.handleTaskEvent(active, event)
		},
		TaskOutputWriter: func(task tasks.ResolvedTask) io.Writer {
			writer, err := tm.taskLogWriter(active, task.Label)
			if err != nil {
				fmt.Fprintf(bw, "=== runtask: warning: failed to create task log for %s: %v ===\n", task.Label, err)
				return nil
			}
			return writer
		},
	})

	result, runErr := runner.Run(meta.TaskLabel)

	// 4. Collect artifacts even on failure where configured.
	artifactRefs, _ := tm.collectArtifacts(meta, worktreePath)
	meta.Artifacts = artifactRefs
	meta.HasArtifacts = len(artifactRefs) > 0

	// 5. Finalise metadata.
	now := time.Now().UTC()
	meta.EndTime = now
	meta.ExitCode = result.ExitCode
	meta.Tasks = collectTaskRuns(active)

	if runErr != nil || result.Failed {
		meta.Status = RunStatusFailed
	} else {
		meta.Status = RunStatusSuccess
	}

	if persistErr := tm.history.PersistCompletedRun(
		meta,
		tm.config.Storage.HistoryKeepCount,
		tm.config.Storage.Worktree.KeepOnSuccess,
		tm.config.Storage.Worktree.KeepOnFailure,
	); persistErr != nil {
		fmt.Fprintf(bw, "=== runtask: warning: failed to persist run meta: %v ===\n", persistErr)
	}

	log.Printf("runtask run finished run_id=%q branch=%q task=%q user=%q status=%q exit_code=%d duration_ms=%d", meta.RunID, meta.Branch, meta.TaskLabel, meta.User, meta.Status, meta.ExitCode, meta.EndTime.Sub(meta.StartTime).Milliseconds())

	if runErr != nil {
		return runErr
	}
	return nil
}

func (tm *TaskManager) resolveRunCatalog(file *tasks.File, worktreePath, tasksPath, taskLabel string, inputValues map[string]string) (*tasks.Catalog, error) {
	definitions := tasks.BuildTaskDefinitionCatalog(file, worktreePath, tasksPath)
	return tasks.ResolveTaskSelection(definitions, taskLabel, tasks.ResolveOptions{
		WorkspaceRoot: worktreePath,
		TaskFilePath:  tasksPath,
		Inputs:        file.Inputs,
		InputValues:   cloneStringMap(inputValues),
		Redaction:     tm.redactionPolicy(),
	})
}

func (tm *TaskManager) initializeRunGraph(ctx context.Context, active *ActiveRun) error {
	meta := active.Meta
	preRunHooks := tm.config.MatchingPreRunTasks(meta.TaskLabel)
	active.mu.Lock()
	if active.taskRuns == nil {
		active.taskRuns = make(map[string]*TaskRunMeta)
	}
	active.mu.Unlock()

	initializeSetupTaskRuns(meta, active, len(preRunHooks))

	worktreePath := tm.history.WorktreePath(meta.RunID)
	tasksPath := filepath.Join(worktreePath, ".vscode", "tasks.json")
	content, err := tm.repo.ReadTasksJSON(ctx, meta.Branch)
	if err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}
	file, err := tasks.LoadFileFromBytes(content, tasks.ResolveLoadOptions(tasksPath, worktreePath))
	if err != nil {
		return fmt.Errorf("load tasks.json: %w", err)
	}
	catalog, err := tm.resolveRunCatalog(file, worktreePath, tasksPath, meta.TaskLabel, meta.InputValues)
	if err != nil {
		return fmt.Errorf("resolve tasks: %w", err)
	}
	initializeTaskRuns(meta, active, catalog, meta.TaskLabel, setupTailTaskLabel(collectSetupTaskInfo(len(preRunHooks))))
	return nil
}

func (tm *TaskManager) runPreRunTasks(ctx context.Context, active *ActiveRun, log io.Writer, taskLabel string, worktreePath string, hooks []uiconfig.PreRunTaskConfig) error {
	if len(hooks) == 0 {
		return nil
	}
	for index, hook := range hooks {
		stageLabel := preRunTaskLabel(index)
		rootLog, err := tm.newTaskStageWriter(active, log, stageLabel)
		if err != nil {
			return fmt.Errorf("create %s log: %w", stageLabel, err)
		}
		tm.handleTaskEvent(active, tasks.TaskEvent{
			Type:      tasks.TaskEventStart,
			TaskLabel: stageLabel,
			Status:    "running",
			StartTime: time.Now().UTC(),
		})
		command, commandDisplay := tm.resolveAndRedactHookValue(hook.Command, worktreePath, active.Meta.InputValues)
		args := make([]string, 0, len(hook.Args))
		displayArgs := make([]string, 0, len(hook.Args))
		for _, arg := range hook.Args {
			resolved, display := tm.resolveAndRedactHookValue(arg, worktreePath, active.Meta.InputValues)
			args = append(args, resolved)
			displayArgs = append(displayArgs, display)
		}
		cwd := worktreePath
		if hook.CWD != "" {
			resolved, _ := tm.resolveAndRedactHookValue(hook.CWD, worktreePath, active.Meta.InputValues)
			cwd = resolved
		}
		env := os.Environ()
		for key, value := range hook.Env {
			resolved, _ := tm.resolveAndRedactHookValue(value, worktreePath, active.Meta.InputValues)
			env = append(env, key+"="+resolved)
		}

		var cmd *exec.Cmd
		display := strings.TrimSpace(strings.Join(append([]string{commandDisplay}, displayArgs...), " "))
		if hook.Shell != nil {
			shellArgs := make([]string, 0, len(hook.Shell.Args)+1)
			for _, arg := range hook.Shell.Args {
				resolved, _ := tm.resolveAndRedactHookValue(arg, worktreePath, active.Meta.InputValues)
				shellArgs = append(shellArgs, resolved)
			}
			shellArgs = append(shellArgs, display)
			executable, _ := tm.resolveAndRedactHookValue(hook.Shell.Executable, worktreePath, active.Meta.InputValues)
			cmd = exec.CommandContext(ctx, executable, shellArgs...)
		} else {
			cmd = exec.CommandContext(ctx, command, args...)
		}
		cmd.Dir = cwd
		cmd.Env = env
		cmd.Stdout = rootLog
		cmd.Stderr = rootLog

		fmt.Fprintf(rootLog, "=== runtask: running pre-run task %d for %q ===\n", index+1, taskLabel)
		fmt.Fprintf(rootLog, "=== runtask: pre-run command: %s ===\n", display)
		if err := cmd.Run(); err != nil {
			_ = rootLog.Close()
			tm.handleTaskEvent(active, tasks.TaskEvent{
				Type:      tasks.TaskEventFinish,
				TaskLabel: stageLabel,
				Status:    "failed",
				ExitCode:  commandExitCode(err),
				EndTime:   time.Now().UTC(),
			})
			return fmt.Errorf("pre-run task %d failed: %w", index+1, err)
		}
		_ = rootLog.Close()
		tm.handleTaskEvent(active, tasks.TaskEvent{
			Type:      tasks.TaskEventFinish,
			TaskLabel: stageLabel,
			Status:    "success",
			ExitCode:  0,
			EndTime:   time.Now().UTC(),
		})
	}
	return nil
}

func resolveHookString(value string, worktreePath string) string {
	replacer := strings.NewReplacer(
		"${workspaceFolder}", worktreePath,
		"${cwd}", worktreePath,
		"${pathSeparator}", string(filepath.Separator),
	)
	return replacer.Replace(value)
}

func (tm *TaskManager) redactionPolicy() tasks.RedactionPolicy {
	if tm == nil || tm.config == nil {
		return tasks.DefaultRedactionPolicy()
	}
	return tasks.MergeRedactionPolicies(
		tasks.DefaultRedactionPolicy(),
		tasks.NewRedactionPolicy(tm.config.Logging.Redaction.Names, tm.config.Logging.Redaction.Tokens),
	)
}

func (tm *TaskManager) resolveAndRedactHookValue(value string, worktreePath string, inputValues map[string]string) (string, string) {
	resolved := value
	display := value
	for _, name := range tasks.VariableNames(value) {
		placeholder := "${" + name + "}"
		switch {
		case name == "workspaceFolder", name == "cwd":
			resolved = strings.ReplaceAll(resolved, placeholder, worktreePath)
			display = strings.ReplaceAll(display, placeholder, worktreePath)
		case name == "pathSeparator":
			separator := string(filepath.Separator)
			resolved = strings.ReplaceAll(resolved, placeholder, separator)
			display = strings.ReplaceAll(display, placeholder, separator)
		case strings.HasPrefix(name, "env:"):
			key := strings.TrimPrefix(name, "env:")
			envValue := os.Getenv(key)
			resolved = strings.ReplaceAll(resolved, placeholder, envValue)
			if tm.redactionPolicy().ShouldRedact(key) {
				display = strings.ReplaceAll(display, placeholder, tasks.RedactedPlaceholder)
			} else {
				display = strings.ReplaceAll(display, placeholder, envValue)
			}
		case strings.HasPrefix(name, "input:"):
			key := strings.TrimPrefix(name, "input:")
			inputValue := inputValues[key]
			resolved = strings.ReplaceAll(resolved, placeholder, inputValue)
			if tm.redactionPolicy().ShouldRedact(key) {
				display = strings.ReplaceAll(display, placeholder, tasks.RedactedPlaceholder)
			} else {
				display = strings.ReplaceAll(display, placeholder, inputValue)
			}
		}
	}
	return resolveHookString(resolved, worktreePath), resolveHookString(display, worktreePath)
}

// failRun transitions an active run to the failed state due to a setup/infrastructure error.
func (tm *TaskManager) failRun(active *ActiveRun, cause error) {
	meta := active.Meta
	// Write the error into the log if the file is still open.
	if active.logFile != nil {
		line := fmt.Sprintf("\n=== runtask: run failed: %v ===\n", cause)
		fmt.Fprint(active.logFile, line)
		if task := active.taskRuns[meta.TaskLabel]; task != nil {
			payload, err := json.Marshal(sseTaskEvent{
				Type:      string(tasks.TaskEventLine),
				TaskLabel: meta.TaskLabel,
				Line:      line,
			})
			if err == nil {
				active.broadcast(string(tasks.TaskEventLine), payload)
			}
		}
	}
	meta.Status = RunStatusFailed
	meta.EndTime = time.Now().UTC()
	if !hasRecordedTaskProgress(active) {
		if task := active.taskRuns[meta.TaskLabel]; task != nil && task.Status == TaskRunStatusPending {
			task.Status = TaskRunStatusFailed
			task.StartTime = meta.StartTime
			task.EndTime = meta.EndTime
			task.ExitCode = 1
		}
	}
	meta.Tasks = collectTaskRuns(active)
	_ = tm.history.PersistCompletedRun(
		meta,
		tm.config.Storage.HistoryKeepCount,
		tm.config.Storage.Worktree.KeepOnSuccess,
		tm.config.Storage.Worktree.KeepOnFailure,
	)
	log.Printf("runtask run failed run_id=%q branch=%q task=%q user=%q status=%q error=%q duration_ms=%d", meta.RunID, meta.Branch, meta.TaskLabel, meta.User, meta.Status, cause.Error(), meta.EndTime.Sub(meta.StartTime).Milliseconds())
	if active.logFile != nil {
		_ = active.logFile.Close()
		active.logFile = nil
	}
}

type sseTaskEvent struct {
	Type      string `json:"type"`
	TaskLabel string `json:"taskLabel,omitempty"`
	Line      string `json:"line,omitempty"`
	Status    string `json:"status,omitempty"`
	ExitCode  int    `json:"exitCode,omitempty"`
	StartTime string `json:"startTime,omitempty"`
	EndTime   string `json:"endTime,omitempty"`
}

func (tm *TaskManager) handleTaskEvent(active *ActiveRun, event tasks.TaskEvent) {
	active.mu.Lock()
	task := active.taskRuns[event.TaskLabel]
	if task == nil {
		task = &TaskRunMeta{
			Label:        event.TaskLabel,
			DependsOn:    append([]string(nil), event.DependsOn...),
			DependsOrder: event.DependsOrder,
			Status:       TaskRunStatusPending,
			LogPath:      taskRunLogPath(event.TaskLabel),
		}
		active.taskRuns[event.TaskLabel] = task
	}
	now := time.Now().UTC()
	switch event.Type {
	case tasks.TaskEventStart:
		task.Status = TaskRunStatusRunning
		task.StartTime = event.StartTime
		if task.StartTime.IsZero() {
			task.StartTime = now
		}
	case tasks.TaskEventFinish:
		task.EndTime = event.EndTime
		if task.EndTime.IsZero() {
			task.EndTime = now
		}
		task.ExitCode = event.ExitCode
		if event.Status == "failed" {
			task.Status = TaskRunStatusFailed
		} else {
			task.Status = TaskRunStatusSuccess
		}
	case tasks.TaskEventSkip:
		task.EndTime = event.EndTime
		if task.EndTime.IsZero() {
			task.EndTime = now
		}
		task.ExitCode = event.ExitCode
		task.Status = TaskRunStatusSkipped
	case tasks.TaskEventLine:
	}
	active.mu.Unlock()

	payload := sseTaskEvent{
		Type:      string(event.Type),
		TaskLabel: event.TaskLabel,
		Line:      event.Line,
		Status:    event.Status,
		ExitCode:  event.ExitCode,
	}
	if !event.StartTime.IsZero() {
		payload.StartTime = event.StartTime.Format(time.RFC3339Nano)
	}
	if !event.EndTime.IsZero() {
		payload.EndTime = event.EndTime.Format(time.RFC3339Nano)
	}
	if raw, err := json.Marshal(payload); err == nil {
		active.broadcast(string(event.Type), raw)
	}
}

func (tm *TaskManager) taskLogWriter(active *ActiveRun, taskLabel string) (io.Writer, error) {
	active.mu.Lock()
	defer active.mu.Unlock()
	if file := active.taskLogFiles[taskLabel]; file != nil {
		return file, nil
	}
	path := tm.history.TaskLogPath(active.Meta.RunID, taskLabel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	active.taskLogFiles[taskLabel] = file
	return file, nil
}

func (tm *TaskManager) newTaskStageWriter(active *ActiveRun, root io.Writer, taskLabel string) (io.WriteCloser, error) {
	writer, err := tm.taskLogWriter(active, taskLabel)
	if err != nil {
		return nil, err
	}
	return &liveTaskLineWriter{
		target:    io.MultiWriter(root, writer),
		active:    active,
		taskLabel: taskLabel,
	}, nil
}

func initializeSetupTaskRuns(meta *RunMeta, active *ActiveRun, preRunCount int) []setupTaskInfo {
	setupTasks := collectSetupTaskInfo(preRunCount)
	for _, stage := range setupTasks {
		active.taskRuns[stage.label] = &TaskRunMeta{
			Label:        stage.label,
			DependsOn:    append([]string(nil), stage.dependsOn...),
			DependsOrder: "sequence",
			Status:       TaskRunStatusPending,
			LogPath:      taskRunLogPath(stage.label),
		}
	}
	meta.Tasks = collectTaskRuns(active)
	return setupTasks
}

func collectSetupTaskInfo(preRunCount int) []setupTaskInfo {
	setupTasks := make([]setupTaskInfo, 0, preRunCount+1)
	setupTasks = append(setupTasks, setupTaskInfo{label: workspacePrepareTaskLabel})
	previous := workspacePrepareTaskLabel
	for index := 0; index < preRunCount; index++ {
		label := preRunTaskLabel(index)
		setupTasks = append(setupTasks, setupTaskInfo{
			label:     label,
			dependsOn: []string{previous},
		})
		previous = label
	}
	return setupTasks
}

func initializeTaskRuns(meta *RunMeta, active *ActiveRun, catalog *tasks.Catalog, root string, setupTail string) {
	visited := make(map[string]bool)
	var walk func(string)
	walk = func(label string) {
		if visited[label] {
			return
		}
		visited[label] = true
		task, ok := catalog.Tasks[label]
		if !ok {
			return
		}
		dependsOn := append([]string(nil), task.DependsOn...)
		if len(dependsOn) == 0 && setupTail != "" {
			dependsOn = []string{setupTail}
		}
		active.taskRuns[label] = &TaskRunMeta{
			Label:        label,
			DependsOn:    dependsOn,
			DependsOrder: task.DependsOrder,
			Status:       TaskRunStatusPending,
			LogPath:      taskRunLogPath(label),
		}
		for _, dep := range task.DependsOn {
			walk(dep)
		}
	}
	walk(root)
	meta.Tasks = collectTaskRuns(active)
}

func setupTailTaskLabel(tasks []setupTaskInfo) string {
	if len(tasks) == 0 {
		return ""
	}
	return tasks[len(tasks)-1].label
}

func preRunTaskLabel(index int) string {
	return fmt.Sprintf("pre-run #%d", index+1)
}

func taskRunLogPath(label string) string {
	return filepath.ToSlash(filepath.Join("tasks", sanitizeTaskLabel(label)+".log"))
}

func hasRecordedTaskProgress(active *ActiveRun) bool {
	active.mu.Lock()
	defer active.mu.Unlock()
	for _, task := range active.taskRuns {
		if task.Status != TaskRunStatusPending {
			return true
		}
	}
	return false
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func collectTaskRuns(active *ActiveRun) []*TaskRunMeta {
	active.mu.Lock()
	defer active.mu.Unlock()
	items := make([]*TaskRunMeta, 0, len(active.taskRuns))
	for _, task := range active.taskRuns {
		copyTask := *task
		items = append(items, &copyTask)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Label < items[j].Label
	})
	return items
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

// collectArtifacts copies files matching task artifact config from the worktree into the artifact dir.
func (tm *TaskManager) collectArtifacts(meta *RunMeta, worktreePath string) ([]ArtifactRef, error) {
	taskCfg, ok := tm.config.TaskConfig(meta.TaskLabel)
	if !ok || len(taskCfg.Artifacts) == 0 {
		return nil, nil
	}

	artifactDir := tm.history.ArtifactDir(meta.RunID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, err
	}

	var refs []ArtifactRef
	for _, rule := range taskCfg.Artifacts {
		matches, err := collectArtifactMatches(worktreePath, rule.Path, rule.Format)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			continue
		}
		if rule.Format == "file" {
			refs = append(refs, collectArtifactFiles(artifactDir, worktreePath, matches)...)
			continue
		}
		ruleRefs, err := tm.collectArtifactZip(meta, artifactDir, worktreePath, matches, rule)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ruleRefs...)
	}
	if len(refs) == 0 {
		return nil, nil
	}
	return refs, nil
}

type artifactMatch struct {
	Source string
	Dest   string
}

func collectArtifactMatches(worktreePath string, pattern string, format string) ([]artifactMatch, error) {
	seen := map[string]struct{}{}
	var matches []artifactMatch
	expanded, err := expandArtifactPattern(worktreePath, pattern, format)
	if err != nil {
		return nil, err
	}
	for _, match := range expanded {
		if _, ok := seen[match.Dest]; ok {
			continue
		}
		seen[match.Dest] = struct{}{}
		matches = append(matches, match)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Dest == matches[j].Dest {
			return matches[i].Source < matches[j].Source
		}
		return matches[i].Dest < matches[j].Dest
	})
	return matches, nil
}

func expandArtifactPattern(worktreePath string, pattern string, format string) ([]artifactMatch, error) {
	cleanPattern := filepath.Clean(filepath.FromSlash(pattern))
	if cleanPattern == "." || cleanPattern == string(filepath.Separator) {
		return nil, nil
	}
	if !strings.ContainsAny(pattern, "*?[") {
		fullPath := filepath.Join(worktreePath, cleanPattern)
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		if info.IsDir() {
			if format == "file" {
				return nil, fmt.Errorf("artifact path %q matched directory %q; format=file requires explicit files", pattern, filepath.ToSlash(cleanPattern))
			}
			return collectArtifactDir(worktreePath, fullPath, cleanPattern)
		}
		return []artifactMatch{{
			Source: filepath.ToSlash(cleanPattern),
			Dest:   filepath.ToSlash(cleanPattern),
		}}, nil
	}

	found, err := filepath.Glob(filepath.Join(worktreePath, cleanPattern))
	if err != nil {
		return nil, nil
	}
	sort.Strings(found)
	destRoot := artifactPatternDestRoot(pattern)
	destBasePath := worktreePath
	if destRoot != "" {
		destBasePath = filepath.Join(worktreePath, destRoot)
	}
	var matches []artifactMatch
	for _, path := range found {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		relPath, err := filepath.Rel(worktreePath, path)
		if err != nil {
			continue
		}
		relPath = filepath.Clean(relPath)
		destPath, err := filepath.Rel(destBasePath, path)
		if err != nil {
			continue
		}
		destPath = filepath.Clean(destPath)
		if info.IsDir() {
			if format == "file" {
				return nil, fmt.Errorf("artifact path %q matched directory %q; format=file requires explicit files", pattern, filepath.ToSlash(relPath))
			}
			dirMatches, err := collectArtifactDir(worktreePath, path, destPath)
			if err != nil {
				return nil, err
			}
			matches = append(matches, dirMatches...)
			continue
		}
		matches = append(matches, artifactMatch{
			Source: filepath.ToSlash(relPath),
			Dest:   filepath.ToSlash(destPath),
		})
	}
	return matches, nil
}

func collectArtifactDir(worktreePath string, dirPath string, destRoot string) ([]artifactMatch, error) {
	var matches []artifactMatch
	err := filepath.WalkDir(dirPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relSource, err := filepath.Rel(worktreePath, path)
		if err != nil {
			return nil
		}
		relWithinDir, err := filepath.Rel(dirPath, path)
		if err != nil {
			return nil
		}
		dest := filepath.Join(destRoot, relWithinDir)
		matches = append(matches, artifactMatch{
			Source: filepath.ToSlash(filepath.Clean(relSource)),
			Dest:   filepath.ToSlash(filepath.Clean(dest)),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Dest == matches[j].Dest {
			return matches[i].Source < matches[j].Source
		}
		return matches[i].Dest < matches[j].Dest
	})
	return matches, nil
}

func artifactPatternDestRoot(pattern string) string {
	parts := strings.Split(filepath.ToSlash(pattern), "/")
	var prefix []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.ContainsAny(part, "*?[") {
			break
		}
		prefix = append(prefix, part)
	}
	return filepath.FromSlash(strings.Join(prefix, "/"))
}

func collectArtifactFiles(artifactDir string, worktreePath string, matches []artifactMatch) []ArtifactRef {
	var refs []ArtifactRef
	for _, match := range matches {
		src := filepath.Join(worktreePath, filepath.FromSlash(match.Source))
		dest := filepath.Join(artifactDir, filepath.FromSlash(match.Dest))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			continue
		}
		if err := copyFile(src, dest); err != nil {
			continue
		}
		refs = append(refs, ArtifactRef{
			Source: match.Source,
			Dest:   match.Dest,
			Format: "file",
		})
	}
	return refs
}

func (tm *TaskManager) collectArtifactZip(meta *RunMeta, artifactDir string, worktreePath string, matches []artifactMatch, rule uiconfig.ArtifactRuleConfig) ([]ArtifactRef, error) {
	archiveName := tm.resolveArtifactArchiveName(meta, worktreePath, rule)
	archivePath := filepath.Join(artifactDir, archiveName)
	file, err := os.Create(archivePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	for _, match := range matches {
		entry, err := writer.Create(match.Dest)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		in, err := os.Open(filepath.Join(worktreePath, filepath.FromSlash(match.Source)))
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		_, copyErr := io.Copy(entry, in)
		closeErr := in.Close()
		if copyErr != nil {
			_ = writer.Close()
			return nil, copyErr
		}
		if closeErr != nil {
			_ = writer.Close()
			return nil, closeErr
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	return []ArtifactRef{{
		Source: fmt.Sprintf("%d files", len(matches)),
		Dest:   archiveName,
		Format: "zip",
	}}, nil
}

func (tm *TaskManager) resolveArtifactArchiveName(meta *RunMeta, worktreePath string, rule uiconfig.ArtifactRuleConfig) string {
	template := rule.NameTemplate
	if strings.TrimSpace(template) == "" {
		template = uiconfig.DefaultArtifactArchive
	}

	fullHash := strings.TrimSpace(meta.CommitHash)
	if fullHash == "" {
		if commitHash, err := git.CurrentCommitHash(worktreePath); err == nil {
			fullHash = strings.TrimSpace(commitHash)
		}
	}
	hash := shortCommitHash(fullHash)
	longHash := sanitizeArtifactFilenamePart(fullHash)
	if longHash == "unknown" {
		hash = "unknown"
	}

	replacements := strings.NewReplacer(
		"{yyyymmdd}", meta.StartTime.UTC().Format("20060102"),
		"{hhmmss}", meta.StartTime.UTC().Format("150405"),
		"{yyyymmddhhmmss}", meta.StartTime.UTC().Format("20060102150405"),
		"{hash}", hash,
		"{longhash}", longHash,
		"{branch}", sanitizeArtifactFilenamePart(meta.Branch),
	)
	name := replacements.Replace(template)
	name = filepath.Base(filepath.Clean(name))
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		return uiconfig.DefaultArtifactArchive
	}
	return sanitizeArtifactArchiveName(name)
}

func sanitizeArtifactFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isSafe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if isSafe {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "unknown"
	}
	return result
}

func sanitizeArtifactArchiveName(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	base = sanitizeArtifactFilenamePart(base)
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return base
	}
	if strings.HasPrefix(ext, ".") {
		return base + ext
	}
	return base + "." + ext
}

func shortCommitHash(value string) string {
	value = sanitizeArtifactFilenamePart(value)
	if value == "unknown" {
		return value
	}
	if len(value) <= 7 {
		return value
	}
	return value[:7]
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
