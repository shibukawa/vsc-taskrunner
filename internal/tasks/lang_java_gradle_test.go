package tasks

import "testing"

func TestNewGradleTasksIncludeCleanAndLint(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasks := NewGradleTasks(workspace, "")
	if got, want := len(tasks), 4; got != want {
		t.Fatalf("task count = %d, want %d", got, want)
	}
	if got, want := string(tasks[0].ProblemMatcher), `["$gradle","$gradle-kotlin"]`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
	if got, want := taskArgs(tasks[2]), []string{"clean"}; !equalStrings(got, want) {
		t.Fatalf("gradle-clean args = %v, want %v", got, want)
	}
	if got, want := taskArgs(tasks[3]), []string{"check"}; !equalStrings(got, want) {
		t.Fatalf("gradle-lint args = %v, want %v", got, want)
	}
}
