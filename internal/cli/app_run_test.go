package cli

import (
	"bytes"
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
