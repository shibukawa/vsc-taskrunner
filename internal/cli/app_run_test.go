package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppRunRejectsConflictingColorFlags(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)

	if exitCode := app.Run([]string{"run", "--no-color", "--force-color", "build"}); exitCode != 2 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cannot use --no-color and --force-color together") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestAppRunDryRunSkipsInputsOutsideSelectedTaskGraph(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".vscode", "tasks.json"), []byte(`{
  "version": "2.0.0",
  "inputs": [
    { "id": "unused", "type": "promptString", "description": "unused value" }
  ],
  "tasks": [
    {
      "label": "build",
      "type": "process",
      "command": "echo",
      "args": ["ok"]
    },
    {
      "label": "other",
      "type": "process",
      "command": "echo",
      "args": ["${input:unused}"]
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"run", "--dry-run", "build"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "unused value") {
		t.Fatalf("unexpected prompt in stdout: %q", stdout.String())
	}
}

func TestAppRunDryRunRedactsSecretLikeNames(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".vscode", "tasks.json"), []byte(`{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "deploy",
      "type": "process",
      "command": "aws",
      "args": ["deploy", "--token=${env:AWS_SECRET_ACCESS_KEY}", "--safe=${env:MONKEY}"]
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	app.env = []string{"AWS_SECRET_ACCESS_KEY=secret", "MONKEY=banana"}

	if exitCode := app.Run([]string{"run", "--dry-run", "deploy"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("dry-run leaked secret: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--token=***") {
		t.Fatalf("dry-run missing redaction: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--safe=banana") {
		t.Fatalf("dry-run should keep safe value: %q", stdout.String())
	}
}
