package web

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type LocalRunStore struct {
	historyDir string
}

func NewLocalRunStore(historyDir string) *LocalRunStore {
	return &LocalRunStore{historyDir: historyDir}
}

func (s *LocalRunStore) RunDir(runID string) string {
	return filepath.Join(s.historyDir, runID)
}

func (s *LocalRunStore) LogPath(runID string) string {
	return filepath.Join(s.RunDir(runID), "stdout.log")
}

func (s *LocalRunStore) TaskLogPath(runID, taskLabel string) string {
	return filepath.Join(s.RunDir(runID), "tasks", sanitizeTaskLabel(taskLabel)+".log")
}

func (s *LocalRunStore) WorktreePath(runID string) string {
	return filepath.Join(s.RunDir(runID), "worktree")
}

func (s *LocalRunStore) ArtifactDir(runID string) string {
	return filepath.Join(s.RunDir(runID), "artifacts")
}

func (s *LocalRunStore) MetaPath(runID string) string {
	return filepath.Join(s.RunDir(runID), "meta.yaml")
}

func (s *LocalRunStore) WriteMeta(meta *RunMeta) error {
	if err := os.MkdirAll(s.RunDir(meta.RunID), 0o755); err != nil {
		return fmt.Errorf("create run dir %s: %w", meta.RunID, err)
	}
	data, err := yaml.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(s.MetaPath(meta.RunID), data, 0o644); err != nil {
		return fmt.Errorf("write meta %s: %w", meta.RunID, err)
	}
	return nil
}

func (s *LocalRunStore) ListMetas(ctx context.Context) ([]*RunMeta, error) {
	_ = ctx
	entries, err := os.ReadDir(s.historyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	metas := make([]*RunMeta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := s.ReadMeta(entry.Name())
		if err != nil {
			continue
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func (s *LocalRunStore) ReadMeta(runID string) (*RunMeta, error) {
	data, err := os.ReadFile(s.MetaPath(runID))
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", runID, err)
	}
	var meta RunMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", runID, err)
	}
	meta.ensureRunKey()
	return &meta, nil
}

func (s *LocalRunStore) ReadLog(runID string) ([]byte, error) {
	return os.ReadFile(s.LogPath(runID))
}

func (s *LocalRunStore) ReadTaskLog(runID, taskLabel string) ([]byte, error) {
	return os.ReadFile(s.TaskLogPath(runID, taskLabel))
}

func (s *LocalRunStore) TailLog(runID string, byteOffset int64) ([]byte, error) {
	f, err := os.Open(s.LogPath(runID))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if byteOffset > 0 {
		if _, err := f.Seek(byteOffset, 0); err != nil {
			return nil, err
		}
	}
	var buf []byte
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		buf = append(buf, scanner.Bytes()...)
		buf = append(buf, '\n')
	}
	return buf, scanner.Err()
}

func (s *LocalRunStore) ListWorktreeFiles(runID string) ([]string, error) {
	root := s.WorktreePath(runID)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, fmt.Errorf("worktree not present for run %s", runID)
	}
	var files []string
	err := filepath.WalkDir(root, func(currentPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			rel, relErr := filepath.Rel(root, currentPath)
			if relErr == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	return files, err
}

func (s *LocalRunStore) ReadWorktreeFile(runID, filePath string) ([]byte, error) {
	root := s.WorktreePath(runID)
	cleanPath, err := safePathWithinRoot(root, filePath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(cleanPath)
}

func (s *LocalRunStore) ListArtifactFiles(runID string) ([]string, error) {
	root := s.ArtifactDir(runID)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var files []string
	err := filepath.WalkDir(root, func(currentPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, currentPath)
		if relErr == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files, err
}

func (s *LocalRunStore) StatArtifactFile(runID, filePath string) (ArtifactFileInfo, error) {
	root := s.ArtifactDir(runID)
	cleanPath, err := safePathWithinRoot(root, filePath)
	if err != nil {
		return ArtifactFileInfo{}, err
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return ArtifactFileInfo{}, err
	}
	return ArtifactFileInfo{
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime(),
	}, nil
}

func (s *LocalRunStore) ReadArtifactFile(runID, filePath string) ([]byte, error) {
	root := s.ArtifactDir(runID)
	cleanPath, err := safePathWithinRoot(root, filePath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(cleanPath)
}

func (s *LocalRunStore) DeleteRun(runID string) error {
	return os.RemoveAll(s.RunDir(runID))
}

func (s *LocalRunStore) FinalizeRun(runID string) error {
	return nil
}

func safePathWithinRoot(root string, filePath string) (string, error) {
	cleanPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(filePath)))
	prefix := root + string(filepath.Separator)
	if cleanPath != root && !strings.HasPrefix(cleanPath, prefix) {
		return "", fmt.Errorf("path %q escapes root", filePath)
	}
	return cleanPath, nil
}
