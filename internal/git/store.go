package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vsc-taskrunner/internal/uiconfig"
)

type RepositoryStore interface {
	BasePath() string
	ListBranches(ctx context.Context) ([]Branch, error)
	Refresh(ctx context.Context) error
	FetchBranch(ctx context.Context, branch string) error
	ReadBranchMetadata(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error)
	ReadTasksJSON(ctx context.Context, branch string) ([]byte, error)
	PrepareRunWorkspace(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error)
	CleanupWorkspace(path string) error
	Maintenance(ctx context.Context) error
}

type BareRepositoryStore struct {
	source           string
	cachePath        string
	fetchDepth       int
	tasksSparsePaths []string
	auth             *remoteAuth
	mu               sync.Mutex
}

func NewBareRepositoryStore(source, cachePath string, fetchDepth int, tasksSparsePaths []string, authConfig uiconfig.RepositoryAuthConfig) (*BareRepositoryStore, error) {
	source = strings.TrimSpace(source)
	cachePath = strings.TrimSpace(cachePath)
	if source == "" {
		return nil, fmt.Errorf("repository source is required")
	}
	if cachePath == "" {
		return nil, fmt.Errorf("repository cache path is required")
	}
	if fetchDepth <= 0 {
		fetchDepth = 1
	}
	if len(tasksSparsePaths) == 0 {
		tasksSparsePaths = []string{".vscode"}
	}
	auth, err := newRemoteAuth(source, authConfig)
	if err != nil {
		return nil, err
	}
	return &BareRepositoryStore{
		source:           source,
		cachePath:        cachePath,
		fetchDepth:       fetchDepth,
		tasksSparsePaths: cloneStrings(tasksSparsePaths),
		auth:             auth,
	}, nil
}

func (s *BareRepositoryStore) BasePath() string {
	return s.cachePath
}

func (s *BareRepositoryStore) Maintenance(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureBareCache(ctx)
}

func (s *BareRepositoryStore) ListBranches(ctx context.Context) ([]Branch, error) {
	cmd, err := s.newRemoteCommand(ctx, ".", "ls-remote", "--heads", s.source)
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote --heads %s: %w\n%s", s.source, err, string(out))
	}

	var branches []Branch
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		ref := parts[1]
		if !strings.HasPrefix(ref, "refs/heads/") {
			continue
		}
		branches = append(branches, Branch{
			FullRef:    ref,
			ShortName:  strings.TrimPrefix(ref, "refs/heads/"),
			CommitHash: parts[0],
			CommitDate: time.Time{},
		})
	}
	sort.Slice(branches, func(i, j int) bool {
		return branches[i].ShortName < branches[j].ShortName
	})
	return branches, nil
}

func (s *BareRepositoryStore) Refresh(ctx context.Context) error {
	branches, err := s.ListBranches(ctx)
	if err != nil {
		return err
	}
	for _, branch := range branches {
		if err := s.FetchBranch(ctx, branch.ShortName); err != nil {
			return err
		}
	}
	return nil
}

func (s *BareRepositoryStore) FetchBranch(ctx context.Context, branch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetchBranchLocked(ctx, branch)
}

func (s *BareRepositoryStore) ReadBranchMetadata(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fetchBranchLocked(ctx, branch); err != nil {
		return "", time.Time{}, nil, err
	}
	commitHash, commitDate, err := s.readBranchCommitLocked(ctx, branch)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	var data []byte
	if strings.TrimSpace(filePath) != "" {
		data, err = s.readBranchFileLocked(ctx, branch, filePath)
		if err != nil {
			if err == os.ErrNotExist {
				return commitHash, commitDate, nil, err
			}
			return "", time.Time{}, nil, err
		}
	}
	return commitHash, commitDate, data, nil
}

func (s *BareRepositoryStore) ReadTasksJSON(ctx context.Context, branch string) ([]byte, error) {
	_, _, data, err := s.ReadBranchMetadata(ctx, branch, filepath.ToSlash(filepath.Join(".vscode", "tasks.json")))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *BareRepositoryStore) PrepareRunWorkspace(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
	if s.isRemoteSource() {
		return s.prepareRemoteWorkspace(ctx, branch, workspacePath, sparsePaths)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prepareWorkspaceLocked(ctx, branch, workspacePath, sparsePaths)
}

func (s *BareRepositoryStore) CleanupWorkspace(path string) error {
	return os.RemoveAll(path)
}

func (s *BareRepositoryStore) ensureBareCache(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return fmt.Errorf("create repository cache parent dir %s: %w", filepath.Dir(s.cachePath), err)
	}
	if isExistingRepo(s.cachePath) {
		return ensureOriginMatches(s.cachePath, s.source)
	}
	if _, err := os.Stat(s.cachePath); err == nil {
		return fmt.Errorf("repository cache path exists but is not a git repository: %s", s.cachePath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat repository cache path %s: %w", s.cachePath, err)
	}
	cmd, err := s.newRemoteCommand(ctx, ".", "clone", "--bare", "--no-checkout", "--depth", fmt.Sprintf("%d", s.fetchDepth), s.source, s.cachePath)
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone --bare --no-checkout: %w\n%s", err, string(out))
	}
	return ensureOriginMatches(s.cachePath, s.source)
}

func (s *BareRepositoryStore) prepareWorkspaceLocked(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
	if err := s.fetchBranchLocked(ctx, branch); err != nil {
		return "", err
	}
	commit, err := s.resolveFetchedCommitLocked(ctx, branch)
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(workspacePath); err != nil {
		return "", fmt.Errorf("remove existing workspace %s: %w", workspacePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		return "", fmt.Errorf("create workspace parent dir %s: %w", filepath.Dir(workspacePath), err)
	}
	cloneArgs := []string{"clone", "--no-checkout", s.cachePath, workspacePath}
	if out, err := newContextCmd(ctx, ".", cloneArgs...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone workspace for %s: %w\n%s", branch, err, string(out))
	}
	if len(sparsePaths) > 0 {
		if out, err := newContextCmd(ctx, workspacePath, "sparse-checkout", "init", "--no-cone").CombinedOutput(); err != nil {
			return "", fmt.Errorf("git sparse-checkout init: %w\n%s", err, string(out))
		}
		args := append([]string{"sparse-checkout", "set"}, sparsePaths...)
		if out, err := newContextCmd(ctx, workspacePath, args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git sparse-checkout set: %w\n%s", err, string(out))
		}
	}
	if out, err := newContextCmd(ctx, workspacePath, "checkout", "--detach", commit).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git checkout %s: %w\n%s", commit, err, string(out))
	}
	return workspacePath, nil
}

func (s *BareRepositoryStore) prepareRemoteWorkspace(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
	if err := os.RemoveAll(workspacePath); err != nil {
		return "", fmt.Errorf("remove existing workspace %s: %w", workspacePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		return "", fmt.Errorf("create workspace parent dir %s: %w", filepath.Dir(workspacePath), err)
	}
	if out, err := newContextCmd(ctx, ".", "init", workspacePath).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init workspace for %s: %w\n%s", branch, err, string(out))
	}
	if out, err := newContextCmd(ctx, workspacePath, "remote", "add", "origin", s.source).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git remote add origin: %w\n%s", err, string(out))
	}
	if len(sparsePaths) > 0 {
		if out, err := newContextCmd(ctx, workspacePath, "sparse-checkout", "init", "--no-cone").CombinedOutput(); err != nil {
			return "", fmt.Errorf("git sparse-checkout init: %w\n%s", err, string(out))
		}
		args := append([]string{"sparse-checkout", "set"}, sparsePaths...)
		if out, err := newContextCmd(ctx, workspacePath, args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git sparse-checkout set: %w\n%s", err, string(out))
		}
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	fetchCmd, err := s.newRemoteCommand(ctx, workspacePath, "fetch", "--depth", fmt.Sprintf("%d", s.fetchDepth), "--prune", "origin", refspec)
	if err != nil {
		return "", err
	}
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git fetch origin %s: %w\n%s", branch, err, string(out))
	}
	if out, err := newContextCmd(ctx, workspacePath, "checkout", "--detach", "refs/remotes/origin/"+branch).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git checkout refs/remotes/origin/%s: %w\n%s", branch, err, string(out))
	}
	return workspacePath, nil
}

func (s *BareRepositoryStore) fetchBranchLocked(ctx context.Context, branch string) error {
	if err := s.ensureBareCache(ctx); err != nil {
		return err
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	cmd, err := s.newGitDirCommand(ctx, s.cachePath, "fetch", "--depth", fmt.Sprintf("%d", s.fetchDepth), "--prune", "origin", refspec)
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch origin %s: %w\n%s", branch, err, string(out))
	}
	return nil
}

func (s *BareRepositoryStore) resolveFetchedCommitLocked(ctx context.Context, branch string) (string, error) {
	ref := fmt.Sprintf("refs/remotes/origin/%s", branch)
	cmd, err := s.newGitDirCommand(ctx, s.cachePath, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w\n%s", ref, err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *BareRepositoryStore) readBranchCommitLocked(ctx context.Context, branch string) (string, time.Time, error) {
	ref := fmt.Sprintf("refs/remotes/origin/%s", branch)
	cmd, err := s.newGitDirCommand(ctx, s.cachePath, "show", "-s", "--format=%H\t%cI", ref)
	if err != nil {
		return "", time.Time{}, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("git show %s: %w\n%s", ref, err, string(out))
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 {
		return "", time.Time{}, fmt.Errorf("git show %s: unexpected output %q", ref, strings.TrimSpace(string(out)))
	}
	commitDate, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("git show %s: parse commit date: %w", ref, err)
	}
	return strings.TrimSpace(parts[0]), commitDate, nil
}

func (s *BareRepositoryStore) readBranchFileLocked(ctx context.Context, branch, filePath string) ([]byte, error) {
	ref := fmt.Sprintf("refs/remotes/origin/%s", branch)
	object := fmt.Sprintf("%s:%s", ref, filepath.ToSlash(filePath))
	cmd, err := s.newGitDirCommand(ctx, s.cachePath, "show", object)
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := string(out)
		if strings.Contains(text, "does not exist in") || strings.Contains(text, "exists on disk, but not in") {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("git show %s: %w\n%s", object, err, text)
	}
	return out, nil
}

func newGitDirCmd(ctx context.Context, gitDir string, args ...string) *execCmdAdapter {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "GIT_DIR="+gitDir)
	return &execCmdAdapter{cmd: cmd}
}

func (s *BareRepositoryStore) newRemoteCommand(ctx context.Context, dir string, args ...string) (*exec.Cmd, error) {
	cmd := newContextCmd(ctx, dir, args...)
	if s.auth == nil {
		return cmd, nil
	}
	if err := s.auth.validate(ctx); err != nil {
		return nil, err
	}
	if err := s.auth.applyToCommand(cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (s *BareRepositoryStore) newGitDirCommand(ctx context.Context, gitDir string, args ...string) (*execCmdAdapter, error) {
	cmd := newGitDirCmd(ctx, gitDir, args...)
	if s.auth == nil {
		return cmd, nil
	}
	if err := s.auth.validate(ctx); err != nil {
		return nil, err
	}
	if err := s.auth.applyToExecAdapter(cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}

type execCmdAdapter struct {
	cmd interface {
		CombinedOutput() ([]byte, error)
	}
}

func (a *remoteAuth) applyToExecAdapter(cmd *execCmdAdapter) error {
	wrapped, ok := cmd.cmd.(*exec.Cmd)
	if !ok {
		return fmt.Errorf("internal error: unsupported git command adapter")
	}
	return a.applyToCommand(wrapped)
}

func (c *execCmdAdapter) CombinedOutput() ([]byte, error) {
	return c.cmd.CombinedOutput()
}

func cloneStrings(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func (s *BareRepositoryStore) isRemoteSource() bool {
	source := strings.TrimSpace(s.source)
	return strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "file://") ||
		strings.HasPrefix(source, "ssh://") ||
		strings.HasPrefix(source, "git@")
}
