package tasks

import "testing"

func TestNewCargoTasksSetDefaultMatcher(t *testing.T) {
	t.Parallel()

	tasks := NewCargoTasks("")
	if got, want := string(tasks[0].ProblemMatcher), `["$cargo","$cargo-panic"]`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
}
