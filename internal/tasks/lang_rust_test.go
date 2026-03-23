package tasks

import "testing"

func TestNewCargoTasksSetDefaultMatcher(t *testing.T) {
	t.Parallel()

	tasks := NewCargoTasks("")
	if got, want := len(tasks), 4; got != want {
		t.Fatalf("task count = %d, want %d", got, want)
	}
	if got, want := string(tasks[0].ProblemMatcher), `["$cargo","$cargo-panic"]`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
	if got, want := taskArgs(tasks[2]), []string{"check"}; !equalStrings(got, want) {
		t.Fatalf("cargo-check args = %v, want %v", got, want)
	}
	if got, want := taskArgs(tasks[3]), []string{"bench"}; !equalStrings(got, want) {
		t.Fatalf("cargo-bench args = %v, want %v", got, want)
	}
}
