package tasks

import "testing"

func TestNewMavenTasksSetDefaultMatcher(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasks := NewMavenTasks(workspace, "")
	if got, want := string(tasks[0].ProblemMatcher), `["$maven","$maven-kotlin"]`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
}
