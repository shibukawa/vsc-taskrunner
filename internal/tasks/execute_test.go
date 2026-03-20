package tasks

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestRunnerRejectsCircularDependencies(t *testing.T) {
	t.Parallel()

	catalog := &Catalog{
		Tasks: map[string]ResolvedTask{
			"a": {
				Label:        "a",
				DependsOn:    []string{"b"},
				DependsOrder: "sequence",
			},
			"b": {
				Label:        "b",
				DependsOn:    []string{"a"},
				DependsOrder: "sequence",
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	_, err := runner.Run("a")
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
}

func TestRunnerSkipsSharedDependencyReexecution(t *testing.T) {
	t.Parallel()

	catalog := &Catalog{
		Tasks: map[string]ResolvedTask{
			"root": {
				Label:        "root",
				DependsOn:    []string{"left", "right"},
				DependsOrder: "parallel",
			},
			"left": {
				Label:        "left",
				DependsOn:    []string{"shared"},
				DependsOrder: "sequence",
			},
			"right": {
				Label:        "right",
				DependsOn:    []string{"shared"},
				DependsOrder: "sequence",
			},
			"shared": {
				Label: "shared",
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("root")
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed {
		t.Fatal("expected success")
	}
	if len(result.Tasks) != 0 {
		t.Fatalf("expected no command executions, got %d", len(result.Tasks))
	}
	if len(runner.states) != 4 {
		t.Fatalf("expected 4 resolved task states, got %d", len(runner.states))
	}
}

func TestQuoteTokenForShellHonorsQuotingStyle(t *testing.T) {
	t.Parallel()

	if got, want := quoteTokenForShell("posix", ResolvedToken{Value: "folder with spaces", Quoting: "escape"}, false), `folder\ with\ spaces`; got != want {
		t.Fatalf("posix escape = %q, want %q", got, want)
	}
	if got, want := quoteTokenForShell("powershell", ResolvedToken{Value: "hello world", Quoting: "weak"}, false), `"hello world"`; got != want {
		t.Fatalf("powershell weak = %q, want %q", got, want)
	}
	if got, want := quoteTokenForShell("cmd", ResolvedToken{Value: `Program Files`, Quoting: "strong"}, false), `"Program Files"`; got != want {
		t.Fatalf("cmd strong = %q, want %q", got, want)
	}
}

func TestRunnerCollectsProblemsFromInlineMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	matcher, err := json.Marshal(map[string]any{
		"owner":        "go",
		"fileLocation": []string{"relative", workspace},
		"pattern": map[string]any{
			"regexp":  `^(.*):(\d+):(\d+):\s+(.*)$`,
			"file":    1,
			"line":    2,
			"column":  3,
			"message": 4,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"lint": {
				Label:          "lint",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"main.go:12:5: undefined: x\\n"},
				ArgTokens:      []ResolvedToken{{Value: "main.go:12:5: undefined: x\\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: matcher,
			},
		},
	}

	var output strings.Builder
	runner := NewRunner(catalog, &output, io.Discard)
	result, err := runner.Run("lint")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	problem := result.Problems[0]
	if problem.File != workspace+"/main.go" {
		t.Fatalf("file = %q", problem.File)
	}
	if problem.Line != 12 || problem.Column != 5 {
		t.Fatalf("location = %d:%d", problem.Line, problem.Column)
	}
	if !strings.Contains(problem.Message, "undefined: x") {
		t.Fatalf("message = %q", problem.Message)
	}
	if !strings.Contains(output.String(), "undefined: x") {
		t.Fatalf("stdout missing task output: %q", output.String())
	}
}

func TestRunnerCollectsBuiltinGoMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"lint": {
				Label:          "lint",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"pkg/main.go:3:1: build failed\\n"},
				ArgTokens:      []ResolvedToken{{Value: "pkg/main.go:3:1: build failed\\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$go"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("lint")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].File != workspace+"/pkg/main.go" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
}

func TestRunnerCollectsBuiltinESLintCompactMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"lint": {
				Label:          "lint",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"src/app.js: line 2, col 4, Error - Missing semicolon. (semi)\\n"},
				ArgTokens:      []ResolvedToken{{Value: "src/app.js: line 2, col 4, Error - Missing semicolon. (semi)\\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$eslint-compact"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("lint")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].Severity != "error" {
		t.Fatalf("severity = %q", result.Problems[0].Severity)
	}
}

func TestRunnerCollectsBuiltinJSHintStylishMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"lint": {
				Label:          "lint",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"src/app.js\\n  line 3 col 7 Missing 'use strict'. (W033)\\n"},
				ArgTokens:      []ResolvedToken{{Value: "src/app.js\\n  line 3 col 7 Missing 'use strict'. (W033)\\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$jshint-stylish"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("lint")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].Severity != "warning" {
		t.Fatalf("severity = %q", result.Problems[0].Severity)
	}
	if result.Problems[0].Code != "033" {
		t.Fatalf("code = %q", result.Problems[0].Code)
	}
}

func TestRunnerCollectsBuiltinMSCompileMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"/workspace/app.cs(5,10): error CS1001: Sample message\\n"},
				ArgTokens:      []ResolvedToken{{Value: "/workspace/app.cs(5,10): error CS1001: Sample message\\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$msCompile"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].Code != "CS1001" {
		t.Fatalf("code = %q", result.Problems[0].Code)
	}
}

func TestRunnerCollectsBuiltinCargoMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"error[E0425]: cannot find value `x` in this scope\n --> src/main.rs:2:5\n"},
				ArgTokens:      []ResolvedToken{{Value: "error[E0425]: cannot find value `x` in this scope\n --> src/main.rs:2:5\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$cargo"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].Code != "E0425" {
		t.Fatalf("code = %q", result.Problems[0].Code)
	}
	if result.Problems[0].File != workspace+"/src/main.rs" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
	if result.Problems[0].Severity != "error" {
		t.Fatalf("severity = %q", result.Problems[0].Severity)
	}
}

func TestRunnerCollectsBuiltinCargoPanicMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "process",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"thread 'main' panicked at src/main.rs:9:13: assertion failed: left == right\n"},
				ArgTokens:      []ResolvedToken{{Value: "thread 'main' panicked at src/main.rs:9:13: assertion failed: left == right\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$cargo-panic"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].File != workspace+"/src/main.rs" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
	if result.Problems[0].Line != 9 || result.Problems[0].Column != 13 {
		t.Fatalf("unexpected location: %d:%d", result.Problems[0].Line, result.Problems[0].Column)
	}
}

func TestRunnerCollectsBuiltinSwiftMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"Sources/App/main.swift:3:7: error: cannot find 'x' in scope\n"},
				ArgTokens:      []ResolvedToken{{Value: "Sources/App/main.swift:3:7: error: cannot find 'x' in scope\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$swift"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].File != workspace+"/Sources/App/main.swift" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
}

func TestRunnerCollectsBuiltinGradleMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"src/main/java/App.java:10: error JAVA1001: cannot find symbol\n"},
				ArgTokens:      []ResolvedToken{{Value: "src/main/java/App.java:10: error JAVA1001: cannot find symbol\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$gradle"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].File != workspace+"/src/main/java/App.java" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
	if result.Problems[0].Code != "JAVA1001" {
		t.Fatalf("code = %q", result.Problems[0].Code)
	}
}

func TestRunnerCollectsBuiltinGradleKotlinMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"e: file:///workspace/app/src/main/kotlin/App.kt: (4, 7): Unresolved reference: value\n"},
				ArgTokens:      []ResolvedToken{{Value: "e: file:///workspace/app/src/main/kotlin/App.kt: (4, 7): Unresolved reference: value\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$gradle-kotlin"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].File != "/workspace/app/src/main/kotlin/App.kt" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
	if result.Problems[0].Severity != "error" {
		t.Fatalf("severity = %q", result.Problems[0].Severity)
	}
}

func TestRunnerCollectsBuiltinMavenMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"[ERROR] /workspace/src/main/java/App.java:[10,5] COMP1001: cannot find symbol\n"},
				ArgTokens:      []ResolvedToken{{Value: "[ERROR] /workspace/src/main/java/App.java:[10,5] COMP1001: cannot find symbol\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$maven"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].File != "/workspace/src/main/java/App.java" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
	if result.Problems[0].Severity != "error" {
		t.Fatalf("severity = %q", result.Problems[0].Severity)
	}
	if result.Problems[0].Code != "COMP1001" {
		t.Fatalf("code = %q", result.Problems[0].Code)
	}
}

func TestRunnerCollectsBuiltinMavenKotlinMatcher(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"build": {
				Label:          "build",
				Type:           "shell",
				Command:        "printf",
				CommandToken:   ResolvedToken{Value: "printf"},
				Args:           []string{"[ERROR] file:///workspace/src/main/kotlin/App.kt: (8, 15) Unresolved reference: testValue\n"},
				ArgTokens:      []ResolvedToken{{Value: "[ERROR] file:///workspace/src/main/kotlin/App.kt: (8, 15) Unresolved reference: testValue\n"}},
				Options:        ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				ProblemMatcher: json.RawMessage(`"$maven-kotlin"`),
			},
		},
	}

	runner := NewRunner(catalog, io.Discard, io.Discard)
	result, err := runner.Run("build")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(result.Problems))
	}
	if result.Problems[0].File != "/workspace/src/main/kotlin/App.kt" {
		t.Fatalf("unexpected file: %q", result.Problems[0].File)
	}
	if result.Problems[0].Line != 8 || result.Problems[0].Column != 15 {
		t.Fatalf("unexpected location: %d:%d", result.Problems[0].Line, result.Problems[0].Column)
	}
}
