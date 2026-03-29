package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureRepository resolves a repository source into a local git directory.
// Remote sources are cloned into localPath on first use. Local sources are used as-is.
func EnsureRepository(ctx context.Context, source, localPath string) (string, error) {
	source = strings.TrimSpace(source)
	localPath = strings.TrimSpace(localPath)
	if source == "" {
		return "", fmt.Errorf("repository source is required")
	}
	if isRemoteSource(source) {
		if localPath == "" {
			return "", fmt.Errorf("repository local path is required for remote sources")
		}
		if err := ensureRemoteClone(ctx, source, localPath); err != nil {
			return "", err
		}
		if err := SyncLocalBranchesFromOrigin(localPath); err != nil {
			return "", err
		}
		return localPath, nil
	}
	if err := validateRepository(source); err != nil {
		return "", err
	}
	return source, nil
}

func ensureRemoteClone(ctx context.Context, source, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("create repository parent dir %s: %w", filepath.Dir(localPath), err)
	}
	if isExistingRepo(localPath) {
		return ensureOriginMatches(localPath, source)
	}
	if _, err := os.Stat(localPath); err == nil {
		return fmt.Errorf("repository path exists but is not a git repository: %s", localPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat repository path %s: %w", localPath, err)
	}
	cmd := newContextCmd(ctx, ".", "clone", "--no-checkout", source, localPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone --no-checkout: %w\n%s", err, string(out))
	}
	return ensureOriginMatches(localPath, source)
}

func ensureOriginMatches(repoRoot, source string) error {
	out, err := runGit(repoRoot, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("resolve git origin for %s: %w", repoRoot, err)
	}
	origin := strings.TrimSpace(string(out))
	if origin != source {
		return fmt.Errorf("git origin mismatch for %s: got %q, want %q", repoRoot, origin, source)
	}
	return nil
}

func validateRepository(path string) error {
	cmd := newContextCmd(context.Background(), path, "rev-parse", "--git-dir")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("not a git repository: %s\n%s", path, strings.TrimSpace(string(out)))
	}
	return nil
}

func isExistingRepo(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	cmd := newContextCmd(context.Background(), path, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func isRemoteSource(source string) bool {
	return strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "file://") ||
		strings.HasPrefix(source, "ssh://") ||
		strings.HasPrefix(source, "git@")
}
