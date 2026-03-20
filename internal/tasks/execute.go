package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var ErrTaskFailed = errors.New("task failed")

type RunResult struct {
	Task     string    `json:"task"`
	ExitCode int       `json:"exitCode"`
	Tasks    []TaskRun `json:"tasks"`
	Problems []Problem `json:"problems,omitempty"`
	Failed   bool      `json:"failed"`
}

type TaskRun struct {
	Label    string    `json:"label"`
	ExitCode int       `json:"exitCode"`
	Command  string    `json:"command"`
	Args     []string  `json:"args,omitempty"`
	CWD      string    `json:"cwd"`
	Problems []Problem `json:"problems,omitempty"`
}

type Runner struct {
	catalog *Catalog
	stdout  io.Writer
	stderr  io.Writer
	mu      sync.Mutex
	runs    []TaskRun
	states  map[string]*taskState
}

type taskState struct {
	done     chan struct{}
	exitCode int
	err      error
}

func NewRunner(catalog *Catalog, stdout io.Writer, stderr io.Writer) *Runner {
	return &Runner{
		catalog: catalog,
		stdout:  stdout,
		stderr:  stderr,
		states:  make(map[string]*taskState, len(catalog.Tasks)),
	}
}

func (r *Runner) Run(taskLabel string) (RunResult, error) {
	if _, ok := r.catalog.Tasks[taskLabel]; !ok {
		return RunResult{}, fmt.Errorf("task not found: %s", taskLabel)
	}

	exitCode, err := r.runTask(context.Background(), taskLabel, map[string]bool{})
	result := RunResult{
		Task:     taskLabel,
		ExitCode: exitCode,
		Tasks:    append([]TaskRun(nil), r.runs...),
		Problems: r.collectProblems(),
		Failed:   err != nil,
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func (r *Runner) runTask(ctx context.Context, label string, stack map[string]bool) (int, error) {
	if stack[label] {
		return 1, fmt.Errorf("circular dependency detected at %s", label)
	}

	r.mu.Lock()
	if state, ok := r.states[label]; ok {
		r.mu.Unlock()
		<-state.done
		return state.exitCode, state.err
	}
	state := &taskState{done: make(chan struct{})}
	r.states[label] = state
	r.mu.Unlock()

	task := r.catalog.Tasks[label]
	childStack := cloneBoolMap(stack)
	childStack[label] = true

	if dependencyExitCode, err := r.runDependencies(ctx, task, childStack); err != nil {
		state.exitCode = dependencyExitCode
		state.err = err
		close(state.done)
		return dependencyExitCode, err
	}

	exitCode, err := r.executeTask(ctx, task)
	state.exitCode = exitCode
	state.err = err
	close(state.done)
	if err != nil {
		return exitCode, err
	}
	return 0, nil
}

func (r *Runner) runDependencies(ctx context.Context, task ResolvedTask, stack map[string]bool) (int, error) {
	if len(task.DependsOn) == 0 {
		return 0, nil
	}

	if task.DependsOrder == "sequence" {
		for _, dep := range task.DependsOn {
			exitCode, err := r.runTask(ctx, dep, cloneBoolMap(stack))
			if err != nil {
				return exitCode, err
			}
		}
		return 0, nil
	}

	var wg sync.WaitGroup
	type dependencyError struct {
		exitCode int
		err      error
	}
	errCh := make(chan dependencyError, len(task.DependsOn))
	for _, dep := range task.DependsOn {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			exitCode, err := r.runTask(ctx, name, cloneBoolMap(stack))
			if err != nil {
				errCh <- dependencyError{exitCode: exitCode, err: err}
			}
		}(dep)
	}
	wg.Wait()
	close(errCh)
	for item := range errCh {
		if item.err != nil {
			return item.exitCode, item.err
		}
	}
	return 0, nil
}

func (r *Runner) executeTask(ctx context.Context, task ResolvedTask) (int, error) {
	if task.Command == "" {
		return 0, nil
	}

	var cmd *exec.Cmd
	switch task.Type {
	case "shell":
		commandLine := renderShellCommand(task)
		args := append([]string(nil), task.Options.Shell.Args...)
		args = append(args, commandLine)
		cmd = exec.CommandContext(ctx, task.Options.Shell.Executable, args...)
	default:
		cmd = exec.CommandContext(ctx, task.Command, task.Args...)
	}

	cmd.Dir = task.Options.CWD
	cmd.Env = mergeProcessEnv(os.Environ(), task.Options.Env)

	collector, err := newProblemCollector(task.Label, task.ProblemMatcher, r.catalog.WorkspaceRoot)
	if err != nil {
		return 1, err
	}
	outputWriter := newMatchedWriter(r.stdout, collector)
	cmd.Stdout = outputWriter
	cmd.Stderr = outputWriter

	r.record(TaskRun{
		Label:    task.Label,
		Command:  task.Command,
		Args:     append([]string(nil), task.Args...),
		CWD:      task.Options.CWD,
		ExitCode: 0,
	})

	if err := cmd.Run(); err != nil {
		outputWriter.Close()
		problems := collector.Close()
		r.attachProblems(task.Label, problems)
		exitCode := exitCode(err)
		r.updateExitCode(task.Label, exitCode)
		return exitCode, fmt.Errorf("%w: %s", ErrTaskFailed, task.Label)
	}
	outputWriter.Close()
	problems := collector.Close()
	r.attachProblems(task.Label, problems)
	r.updateExitCode(task.Label, 0)
	return 0, nil
}

func (r *Runner) record(run TaskRun) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, run)
}

func (r *Runner) updateExitCode(label string, code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.runs {
		if r.runs[index].Label == label {
			r.runs[index].ExitCode = code
		}
	}
}

func (r *Runner) attachProblems(label string, problems []Problem) {
	if len(problems) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.runs {
		if r.runs[index].Label == label {
			r.runs[index].Problems = append(r.runs[index].Problems, problems...)
		}
	}
}

func (r *Runner) collectProblems() []Problem {
	r.mu.Lock()
	defer r.mu.Unlock()
	var problems []Problem
	for _, run := range r.runs {
		problems = append(problems, run.Problems...)
	}
	return problems
}

func renderShellCommand(task ResolvedTask) string {
	if len(task.Args) == 0 {
		return task.Command
	}

	parts := make([]string, 0, len(task.ArgTokens)+1)
	parts = append(parts, quoteTokenForShell(task.Options.Shell.Family, task.CommandToken, true))
	for _, arg := range task.ArgTokens {
		parts = append(parts, quoteTokenForShell(task.Options.Shell.Family, arg, false))
	}
	return strings.Join(parts, " ")
}

func quoteTokenForShell(family string, token ResolvedToken, isCommand bool) string {
	style := token.Quoting
	if style == "" {
		if isCommand || needsQuoting(family, token.Value) {
			style = "strong"
		}
	}
	if style == "" {
		return token.Value
	}
	return quoteForShell(family, token.Value, style)
}

func quoteForShell(family string, value string, style string) string {
	switch family {
	case "cmd":
		switch style {
		case "escape":
			return escapeCMD(value)
		default:
			quoted := strings.ReplaceAll(value, `"`, `""`)
			return fmt.Sprintf(`"%s"`, quoted)
		}
	case "powershell":
		switch style {
		case "weak":
			quoted := strings.NewReplacer("`", "``", `"`, "`\"", "$", "`$").Replace(value)
			return fmt.Sprintf(`"%s"`, quoted)
		case "escape":
			return escapePowerShell(value)
		default:
			quoted := strings.ReplaceAll(value, `'`, `''`)
			return fmt.Sprintf("'%s'", quoted)
		}
	default:
		switch style {
		case "weak":
			quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(value)
			return fmt.Sprintf(`"%s"`, quoted)
		case "escape":
			return escapePOSIX(value)
		default:
			quoted := strings.ReplaceAll(value, `'`, `"'"'"'`)
			return fmt.Sprintf("'%s'", quoted)
		}
	}
}

func needsQuoting(family string, value string) bool {
	switch family {
	case "cmd":
		return strings.ContainsAny(value, " \t\n\"^&|<>()%!")
	case "powershell":
		return strings.ContainsAny(value, " \t\n'\"`$&|;<>()[]{}")
	default:
		return strings.ContainsAny(value, " \t\n'\"`$&;|<>()[]{}*!?#~")
	}
}

func escapePOSIX(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if strings.ContainsRune(" \t\n\\'\"$&;|<>()[]{}*!?#~", r) {
			builder.WriteByte('\\')
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func escapePowerShell(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if strings.ContainsRune(" `\t\n'\"`$&|;<>()[]{}", r) {
			builder.WriteByte('`')
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func escapeCMD(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if strings.ContainsRune(" ^&|<>()%!\t\n\"", r) {
			builder.WriteByte('^')
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func mergeProcessEnv(base []string, overlay map[string]string) []string {
	if len(overlay) == 0 {
		return base
	}
	items := envMap(base)
	for key, value := range overlay {
		items[key] = value
	}
	result := make([]string, 0, len(items))
	for key, value := range items {
		result = append(result, key+"="+value)
	}
	return result
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

type matchedWriter struct {
	mu        sync.Mutex
	underlier io.Writer
	collector *problemCollector
	buffer    strings.Builder
}

func newMatchedWriter(underlier io.Writer, collector *problemCollector) *matchedWriter {
	return &matchedWriter{underlier: underlier, collector: collector}
}

func (w *matchedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.underlier.Write(data); err != nil {
		return 0, err
	}
	w.buffer.Write(data)
	for {
		text := w.buffer.String()
		index := strings.IndexByte(text, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimRight(text[:index], "\r")
		w.collector.ProcessLine(line)
		w.buffer.Reset()
		w.buffer.WriteString(text[index+1:])
	}
	return len(data), nil
}

func (w *matchedWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buffer.Len() == 0 {
		return
	}
	w.collector.ProcessLine(strings.TrimRight(w.buffer.String(), "\r"))
	w.buffer.Reset()
}
