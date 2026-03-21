package tasks

import "testing"

func TestNewMavenTasksIncludeCleanAndLint(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasks := NewMavenTasks(workspace, "")
	if got, want := len(tasks), 4; got != want {
		t.Fatalf("task count = %d, want %d", got, want)
	}
	if got, want := string(tasks[0].ProblemMatcher), `["$maven","$maven-kotlin"]`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
	if got, want := taskArgs(tasks[2]), []string{"clean"}; !equalStrings(got, want) {
		t.Fatalf("maven-clean args = %v, want %v", got, want)
	}
	if got, want := taskArgs(tasks[3]), []string{"verify"}; !equalStrings(got, want) {
		t.Fatalf("maven-lint args = %v, want %v", got, want)
	}
}
