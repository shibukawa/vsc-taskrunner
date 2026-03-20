package tasks

import "testing"

func TestNewGradleTasksSetDefaultMatcher(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasks := NewGradleTasks(workspace, "")
	if got, want := string(tasks[0].ProblemMatcher), `["$gradle","$gradle-kotlin"]`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
}
