package cli

import (
	"sort"
	"testing"

	"vsc-taskrunner/internal/tasks"
)

func TestAddSubcommandHandlersIncludesCoreCommands(t *testing.T) {
	t.Parallel()

	handlers := addSubcommandHandlers()
	want := []string{"detect", "go", "gradle", "grunt", "gulp", "jake", "maven", "npm", "rust", "swift", "typescript"}
	got := make([]string, 0, len(handlers))
	for name := range handlers {
		got = append(got, name)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("handler count = %d, want %d (%v)", len(got), len(want), got)
	}
	for index, name := range want {
		if got[index] != name {
			t.Fatalf("handler[%d] = %q, want %q", index, got[index], name)
		}
	}
}

func TestRunAddSubcommandReturnsFalseForUnknownCommand(t *testing.T) {
	t.Parallel()

	app := NewApp(nil, nil, nil)
	if _, ok := app.runAddSubcommand("unknown", nil); ok {
		t.Fatal("expected unknown add subcommand to be rejected")
	}
}

func TestAddTargetDefinitionsExposeTargetNames(t *testing.T) {
	t.Parallel()

	definitions := tasks.AddTargetDefinitions()
	if got, want := definitions["gradle"].TargetName, "java-gradle"; got != want {
		t.Fatalf("gradle targetName = %q, want %q", got, want)
	}
	if got, want := definitions["maven"].TargetName, "java-maven"; got != want {
		t.Fatalf("maven targetName = %q, want %q", got, want)
	}
}
