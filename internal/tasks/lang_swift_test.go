package tasks

import "testing"

func TestNewSwiftTasksSetDefaultMatcher(t *testing.T) {
	t.Parallel()

	tasks := NewSwiftTasks("")
	if got, want := len(tasks), 4; got != want {
		t.Fatalf("task count = %d, want %d", got, want)
	}
	if got, want := string(tasks[0].ProblemMatcher), `"$swift"`; got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
	if got, want := taskArgs(tasks[2]), []string{"package", "clean"}; !equalStrings(got, want) {
		t.Fatalf("swift-clean args = %v, want %v", got, want)
	}
	if got, want := taskArgs(tasks[3]), []string{"run"}; !equalStrings(got, want) {
		t.Fatalf("swift-run args = %v, want %v", got, want)
	}
}
