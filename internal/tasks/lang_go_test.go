package tasks

import "testing"

func TestNewGoTasksIncludeCommonFlagsAndBench(t *testing.T) {
	t.Parallel()

	tasks := NewGoTasks("")
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
