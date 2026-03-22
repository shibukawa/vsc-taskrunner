package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewGoTasksIncludeCommonFlagsAndBench(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasks := NewGoTasks(workspace, "")
	if len(tasks) != 5 {
		t.Fatalf("task count = %d, want 5", len(tasks))
	}
	if got, want := tasks[0].Label, "go-build"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	if got, want := taskArgs(tasks[0]), []string{"build", "-trimpath", "-ldflags=-s -w", "./..."}; !equalStrings(got, want) {
		t.Fatalf("go-build args = %v, want %v", got, want)
	}
	if got, want := taskArgs(tasks[1]), []string{"test", "-v", "./..."}; !equalStrings(got, want) {
		t.Fatalf("go-test args = %v, want %v", got, want)
	}
	if got, want := taskArgs(tasks[2]), []string{"test", "-run=^$", "-bench=.", "-benchmem", "./..."}; !equalStrings(got, want) {
		t.Fatalf("go-bench args = %v, want %v", got, want)
	}
	if got, want := taskArgs(tasks[3]), []string{"test", "-coverprofile=coverage.out", "./..."}; !equalStrings(got, want) {
		t.Fatalf("go-cover args = %v, want %v", got, want)
	}
	if got, want := tasks[4].Type, "shell"; got != want {
		t.Fatalf("go-lint type = %q, want %q", got, want)
	}
	if got, want := tasks[4].Command.Value, "gofmt -l -w . && go vet ./..."; got != want {
		t.Fatalf("go-lint command = %q, want %q", got, want)
	}
	if got, want := string(tasks[4].ProblemMatcher), `"$go"`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
}

func TestNewGoTasksAddsCommandBuildDependencies(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	for _, commandName := range []string{"runtask", "worker"} {
		commandDir := filepath.Join(workspace, "cmd", commandName)
		if err := os.MkdirAll(commandDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(commandDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tasks := NewGoTasks(workspace, "")
	if len(tasks) != 7 {
		t.Fatalf("task count = %d, want 7", len(tasks))
	}
	byLabel := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		byLabel[task.Label] = task
	}
	rootBuild := byLabel["go-build"]
	if got, want := rootBuild.DependsOrder, "parallel"; got != want {
		t.Fatalf("dependsOrder = %q, want %q", got, want)
	}
	if got, want := rootBuild.Dependencies.Labels(), []string{"go-build-runtask", "go-build-worker"}; !equalStrings(got, want) {
		t.Fatalf("dependsOn = %v, want %v", got, want)
	}
	if got, want := taskArgs(byLabel["go-build-runtask"]), []string{"build", "-trimpath", "-ldflags=-s -w", "-o", "./bin/runtask", "./cmd/runtask"}; !equalStrings(got, want) {
		t.Fatalf("go-build-runtask args = %v, want %v", got, want)
	}
	if got, want := taskArgs(byLabel["go-build-worker"]), []string{"build", "-trimpath", "-ldflags=-s -w", "-o", "./bin/worker", "./cmd/worker"}; !equalStrings(got, want) {
		t.Fatalf("go-build-worker args = %v, want %v", got, want)
	}
	if got, want := byLabel["go-build-runtask"].Options.CWD, "${workspaceFolder}"; got != want {
		t.Fatalf("go-build-runtask cwd = %q, want %q", got, want)
	}
	if got, want := byLabel["go-build-worker"].Options.CWD, "${workspaceFolder}"; got != want {
		t.Fatalf("go-build-worker cwd = %q, want %q", got, want)
	}
	if group, ok := ParseTaskGroup(byLabel["go-build-runtask"].Group); ok {
		t.Fatalf("go-build-runtask group = %+v, want no group", group)
	}
}

func taskArgs(task Task) []string {
	result := make([]string, 0, len(task.Args))
	for _, arg := range task.Args {
		result = append(result, arg.Value)
	}
	return result
}

func equalStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
