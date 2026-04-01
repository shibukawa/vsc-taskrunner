package web

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type LocalIndexStore struct {
	path     string
	lockPath string
}

func NewLocalIndexStore(historyDir string) *LocalIndexStore {
	return &LocalIndexStore{
		path:     filepath.Join(historyDir, historyIndexObjectName),
		lockPath: filepath.Join(historyDir, historyIndexObjectName+".lock"),
	}
}

func (s *LocalIndexStore) ReadIndex(ctx context.Context) (*RunHistoryIndex, error) {
	_ = ctx
	return s.readIndex()
}

func (s *LocalIndexStore) UpdateIndex(ctx context.Context, fn func(*RunHistoryIndex) error) error {
	_ = ctx
	if err := os.MkdirAll(filepath.Dir(s.lockPath), 0o755); err != nil {
		return fmt.Errorf("create history index lock dir: %w", err)
	}
	return withExclusiveFileLock(s.lockPath, "open history index lock", "lock history index", func() error {
		index, err := s.readIndex()
		if err != nil {
			return err
		}
		if err := fn(index); err != nil {
			return err
		}
		return s.writeIndex(index)
	})
}

func (s *LocalIndexStore) readIndex() (*RunHistoryIndex, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return newRunHistoryIndex(), nil
		}
		return nil, fmt.Errorf("read history index: %w", err)
	}
	var index RunHistoryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse history index: %w", err)
	}
	if index.Groups == nil {
		index.Groups = make(map[string]*RunHistoryGroup)
	}
	return &index, nil
}

func (s *LocalIndexStore) writeIndex(index *RunHistoryIndex) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create history index dir: %w", err)
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal history index: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "history-index-*.json")
	if err != nil {
		return fmt.Errorf("create temp history index: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp history index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp history index: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace history index: %w", err)
	}
	return nil
}
