package git

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Branch represents a git branch (local or remote).
type Branch struct {
	// FullRef is the full ref name, e.g. refs/heads/main or refs/remotes/origin/main.
	FullRef string `json:"fullRef"`
	// ShortName is the human-readable name, e.g. main or origin/main.
	ShortName string `json:"shortName"`
	// IsRemote is true for refs/remotes/* branches.
	IsRemote bool `json:"isRemote"`
	// CommitHash is the full SHA of the tip commit.
	CommitHash string `json:"commitHash"`
	// CommitDate is the author date of the tip commit.
	CommitDate time.Time `json:"commitDate"`
}

// ReadHeadCommit returns the checked-out HEAD commit hash and commit date for a local worktree.
func ReadHeadCommit(repoRoot string) (string, time.Time, error) {
	out, err := runGit(repoRoot, "show", "-s", "--format=%H\t%cI", "HEAD")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read HEAD commit: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 {
		return "", time.Time{}, fmt.Errorf("read HEAD commit: unexpected output %q", strings.TrimSpace(string(out)))
	}
	commitDate, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read HEAD commit date: %w", err)
	}
	return strings.TrimSpace(parts[0]), commitDate, nil
}

// ListBranches returns all local and remote branches for the given repo root.
func ListBranches(repoRoot string) ([]Branch, error) {
	// format: <refname>\t<objectname>\t<authordate:iso-strict>
	const format = "%(refname)\t%(objectname)\t%(creatordate:iso-strict)"
	out, err := runGit(repoRoot, "for-each-ref",
		"--format="+format,
		"refs/heads",
		"refs/remotes",
	)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}

	var branches []Branch
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		fullRef := parts[0]
		commitHash := parts[1]
		dateStr := parts[2]

		isRemote := strings.HasPrefix(fullRef, "refs/remotes/")

		// Skip refs/remotes/origin/HEAD symbolic ref entries
		shortName := refToShortName(fullRef)
		if isRemote && strings.HasSuffix(shortName, "/HEAD") {
			continue
		}

		var commitDate time.Time
		if dateStr != "" {
			commitDate, _ = time.Parse(time.RFC3339, dateStr)
		}

		branches = append(branches, Branch{
			FullRef:    fullRef,
			ShortName:  shortName,
			IsRemote:   isRemote,
			CommitHash: commitHash,
			CommitDate: commitDate,
		})
	}

	return branches, nil
}

// Fetch runs `git fetch --all --prune` in the given repo with a 30-second timeout.
func Fetch(repoRoot string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := newContextCmd(ctx, repoRoot, "fetch", "--all", "--prune")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %w\n%s", err, string(out))
	}
	return nil
}

// FetchBranch fetches only the requested branch from origin.
func FetchBranch(repoRoot, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	cmd := newContextCmd(ctx, repoRoot, "fetch", "--prune", "origin", refspec)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch origin %s: %w\n%s", branch, err, string(out))
	}
	return nil
}

// SyncLocalBranchesFromOrigin creates or fast-forwards local branches to match
// refs/remotes/origin/* so the UI can refer to short local branch names.
func SyncLocalBranchesFromOrigin(repoRoot string) error {
	out, err := runGit(repoRoot, "for-each-ref", "--format=%(refname:strip=3)", "refs/remotes/origin")
	if err != nil {
		return fmt.Errorf("list remote branches: %w", err)
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || name == "HEAD" {
			continue
		}
		remoteRef := "refs/remotes/origin/" + name
		if _, err := runGit(repoRoot, "update-ref", "refs/heads/"+name, remoteRef); err != nil {
			return fmt.Errorf("sync local branch %s: %w", name, err)
		}
	}
	return nil
}

// SyncLocalBranchFromOrigin creates or fast-forwards one local branch from the
// corresponding origin/<branch> remote-tracking ref.
func SyncLocalBranchFromOrigin(repoRoot, branch string) error {
	remoteRef := "refs/remotes/origin/" + branch
	if _, err := runGit(repoRoot, "update-ref", "refs/heads/"+branch, remoteRef); err != nil {
		return fmt.Errorf("sync local branch %s: %w", branch, err)
	}
	return nil
}

func refToShortName(ref string) string {
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		return strings.TrimPrefix(ref, "refs/heads/")
	case strings.HasPrefix(ref, "refs/remotes/"):
		return strings.TrimPrefix(ref, "refs/remotes/")
	default:
		return ref
	}
}
