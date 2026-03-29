package web

import (
	"bytes"
	"fmt"
	"os/exec"
)

// runGitCmd is a low-level helper used within the web package to execute git
// commands without importing internal/git (avoiding potential import cycles).
func runGitCmd(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stderr.Bytes(), fmt.Errorf("%w", err)
	}
	return stdout.Bytes(), nil
}
