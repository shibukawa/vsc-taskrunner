package tasks

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
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

func TestRunnerDefaultOutputShowsStatusSummary(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"echo": {
				Label:        "echo",
				Type:         "shell",
				Command:      "printf",
				CommandToken: ResolvedToken{Value: "printf"},
				Args:         []string{"hello\n"},
				ArgTokens:    []ResolvedToken{{Value: "hello\n"}},
				Options:      ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
			},
		},
	}

	var stdout strings.Builder
	var stderr strings.Builder
	runner := NewRunnerWithOptions(catalog, &stdout, &stderr, RunnerOptions{OutputMode: OutputModeDefault, ColorMode: ColorModeNever})
	result, err := runner.Run("echo")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}
	if !strings.Contains(stdout.String(), "hello") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "🚀 run echo") {
		t.Fatalf("missing start summary: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "✅ OK echo") {
		t.Fatalf("missing finish summary: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("unexpected ANSI escape sequence: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "wall=") || !strings.Contains(stderr.String(), "user=") || !strings.Contains(stderr.String(), "sys=") {
		t.Fatalf("missing timing summary: %q", stderr.String())
	}
	if result.Tasks[0].WallTime < 0 || result.Tasks[0].UserTime < 0 || result.Tasks[0].SystemTime < 0 {
		t.Fatalf("unexpected task timing: %+v", result.Tasks[0])
	}
}

func TestRunnerQuietSuppressesStatusSummary(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"echo": {
				Label:        "echo",
				Type:         "shell",
				Command:      "printf",
				CommandToken: ResolvedToken{Value: "printf"},
				Args:         []string{"quiet\n"},
				ArgTokens:    []ResolvedToken{{Value: "quiet\n"}},
				Options:      ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
			},
		},
	}

	var stdout strings.Builder
	var stderr strings.Builder
	runner := NewRunnerWithOptions(catalog, &stdout, &stderr, RunnerOptions{OutputMode: OutputModeQuiet, ColorMode: ColorModeAlways})
	if _, err := runner.Run("echo"); err != nil {
		t.Fatal(err)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	if !strings.Contains(stdout.String(), "quiet") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunnerColorModesInjectEnvironment(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	newCatalog := func() *Catalog {
		return &Catalog{
			WorkspaceRoot: workspace,
			Tasks: map[string]ResolvedTask{
				"env": {
					Label:        "env",
					Type:         "process",
					Command:      "/bin/sh",
					CommandToken: ResolvedToken{Value: "/bin/sh"},
					Args:         []string{"-c", `printf '%s|%s|%s|%s' "$NO_COLOR" "$FORCE_COLOR" "$CLICOLOR" "$CLICOLOR_FORCE"`},
					ArgTokens:    []ResolvedToken{{Value: "-c"}, {Value: `printf '%s|%s|%s|%s' "$NO_COLOR" "$FORCE_COLOR" "$CLICOLOR" "$CLICOLOR_FORCE"`}},
					Options:      ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
				},
			},
		}
	}

	var noColorOut strings.Builder
	runner := NewRunnerWithOptions(newCatalog(), &noColorOut, io.Discard, RunnerOptions{OutputMode: OutputModeQuiet, ColorMode: ColorModeNever})
	if _, err := runner.Run("env"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(noColorOut.String()); got != "1|0|0|0" {
		t.Fatalf("no-color env = %q", got)
	}

	var forceColorOut strings.Builder
	runner = NewRunnerWithOptions(newCatalog(), &forceColorOut, io.Discard, RunnerOptions{OutputMode: OutputModeQuiet, ColorMode: ColorModeAlways})
	if _, err := runner.Run("env"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(forceColorOut.String()); got != "|1|1|1" {
		t.Fatalf("force-color env = %q", got)
	}
}

func TestRunnerForceColorAddsANSIToStatusSummary(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"echo": {
				Label:        "echo",
				Type:         "shell",
				Command:      "printf",
				CommandToken: ResolvedToken{Value: "printf"},
				Args:         []string{"color\n"},
				ArgTokens:    []ResolvedToken{{Value: "color\n"}},
				Options:      ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
			},
		},
	}

	var stdout strings.Builder
	var stderr strings.Builder
	runner := NewRunnerWithOptions(catalog, &stdout, &stderr, RunnerOptions{OutputMode: OutputModeDefault, ColorMode: ColorModeAlways})
	if _, err := runner.Run("echo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("expected ANSI escape sequence, got %q", stderr.String())
	}
}

func TestRunnerRecordsWallTimeMilliseconds(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	catalog := &Catalog{
		WorkspaceRoot: workspace,
		Tasks: map[string]ResolvedTask{
			"sleep": {
				Label:        "sleep",
				Type:         "shell",
				Command:      "sleep",
				CommandToken: ResolvedToken{Value: "sleep"},
				Args:         []string{"0.02"},
				ArgTokens:    []ResolvedToken{{Value: "0.02"}},
				Options:      ResolvedOptions{CWD: workspace, Shell: ShellRuntime{Executable: "/bin/sh", Args: []string{"-c"}, Family: "posix"}},
			},
		},
	}

	runner := NewRunnerWithOptions(catalog, io.Discard, io.Discard, RunnerOptions{OutputMode: OutputModeQuiet, ColorMode: ColorModeNever})
	result, err := runner.Run("sleep")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}
	if result.Tasks[0].WallTime < int64((20 * time.Millisecond).Milliseconds()) {
		t.Fatalf("wallTimeMs = %d", result.Tasks[0].WallTime)
	}
}
