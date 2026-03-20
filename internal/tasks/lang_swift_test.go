package tasks

import "testing"

func TestNewSwiftTasksSetDefaultMatcher(t *testing.T) {
	t.Parallel()

	tasks := NewSwiftTasks("")
	if got, want := string(tasks[0].ProblemMatcher), `"$swift"`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
}
