package tasks

import "testing"

func TestNewTypeScriptTaskSetsBuildGroup(t *testing.T) {
	t.Parallel()

	task := NewTypeScriptTask("tsconfig.json", "build")
	group, ok := ParseTaskGroup(task.Group)
	if !ok || group.Kind != "build" {
		t.Fatalf("group = %+v, want build group", group)
	}
}
