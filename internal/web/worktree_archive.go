package web

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

const (
	worktreeArchiveObjectName = "worktree.zip"
	worktreeIgnoreFileName    = ".runtaskignore"
)

func listVisibleWorktreeFiles(root string) ([]string, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, fmt.Errorf("worktree not present")
	}
	matcher, err := loadWorktreeIgnore(root)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(currentPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, skip, err := shouldSkipWorktreePath(root, currentPath, entry, matcher)
		if err != nil {
			return err
		}
		if skip {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && rel != "" {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func readVisibleWorktreeFile(root string, filePath string) ([]byte, error) {
	cleanPath, err := safePathWithinRoot(root, filePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, err
	}
	matcher, err := loadWorktreeIgnore(root)
	if err != nil {
		return nil, err
	}
	rel, skip, err := shouldSkipWorktreePath(root, cleanPath, dirEntryFromFileInfo{info: info}, matcher)
	if err != nil {
		return nil, err
	}
	if skip || rel == "" || info.IsDir() {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(cleanPath)
}

func createWorktreeArchiveTemp(root string) (*os.File, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	matcher, err := loadWorktreeIgnore(root)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "runtask-worktree-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create worktree archive temp file: %w", err)
	}
	cleanup := func(cause error) (*os.File, error) {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, cause
	}
	archive := zip.NewWriter(tmp)
	err = filepath.WalkDir(root, func(currentPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, skip, err := shouldSkipWorktreePath(root, currentPath, entry, matcher)
		if err != nil {
			return err
		}
		if skip {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || rel == "" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = rel
		header.Method = zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(currentPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(writer, file); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return cleanup(fmt.Errorf("walk worktree for archive: %w", err))
	}
	if err := archive.Close(); err != nil {
		return cleanup(fmt.Errorf("close worktree archive: %w", err))
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		return cleanup(fmt.Errorf("rewind worktree archive: %w", err))
	}
	return tmp, nil
}

func listWorktreeArchiveFiles(data []byte) ([]string, error) {
	archive, err := openWorktreeArchive(data)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name, err := normalizeWorktreeArchivePath(file.Name)
		if err != nil {
			return nil, err
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}

func readWorktreeArchiveFile(data []byte, filePath string) ([]byte, error) {
	target, err := normalizeWorktreeArchivePath(filePath)
	if err != nil {
		return nil, err
	}
	archive, err := openWorktreeArchive(data)
	if err != nil {
		return nil, err
	}
	for _, file := range archive.File {
		name, err := normalizeWorktreeArchivePath(file.Name)
		if err != nil {
			return nil, err
		}
		if name != target || file.FileInfo().IsDir() {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open worktree archive entry %s: %w", target, err)
		}
		defer reader.Close()
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("read worktree archive entry %s: %w", target, err)
		}
		return content, nil
	}
	return nil, os.ErrNotExist
}

func openWorktreeArchive(data []byte) (*zip.Reader, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open worktree archive: %w", err)
	}
	return archive, nil
}

func loadWorktreeIgnore(root string) (*ignore.GitIgnore, error) {
	ignorePath := filepath.Join(root, worktreeIgnoreFileName)
	matcher, err := ignore.CompileIgnoreFile(ignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load %s: %w", worktreeIgnoreFileName, err)
	}
	return matcher, nil
}

func shouldSkipWorktreePath(root string, currentPath string, entry os.DirEntry, matcher *ignore.GitIgnore) (string, bool, error) {
	rel, err := filepath.Rel(root, currentPath)
	if err != nil {
		return "", false, err
	}
	if rel == "." {
		return "", false, nil
	}
	rel = filepath.ToSlash(rel)
	if isGitMetadataPath(rel) {
		return rel, true, nil
	}
	if matcher == nil {
		return rel, false, nil
	}
	if matcher.MatchesPath(rel) {
		return rel, true, nil
	}
	if entry.IsDir() && matcher.MatchesPath(rel+"/") {
		return rel, true, nil
	}
	return rel, false, nil
}

func normalizeWorktreeArchivePath(filePath string) (string, error) {
	filePath = strings.ReplaceAll(filePath, "\\", "/")
	cleanPath := path.Clean(strings.TrimPrefix(filePath, "/"))
	if cleanPath == "." || cleanPath == "" || strings.HasPrefix(cleanPath, "../") || cleanPath == ".." {
		return "", fmt.Errorf("invalid worktree path %q", filePath)
	}
	return cleanPath, nil
}

func isGitMetadataPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == ".git" {
			return true
		}
	}
	return false
}

type dirEntryFromFileInfo struct {
	info os.FileInfo
}

func (d dirEntryFromFileInfo) Name() string               { return d.info.Name() }
func (d dirEntryFromFileInfo) IsDir() bool                { return d.info.IsDir() }
func (d dirEntryFromFileInfo) Type() os.FileMode          { return d.info.Mode().Type() }
func (d dirEntryFromFileInfo) Info() (os.FileInfo, error) { return d.info, nil }
