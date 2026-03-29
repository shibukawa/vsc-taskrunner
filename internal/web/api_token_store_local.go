package web

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type LocalAPITokenStore struct {
	path string
	mu   sync.Mutex
}

func NewLocalAPITokenStore(path string) *LocalAPITokenStore {
	return &LocalAPITokenStore{path: path}
}

func (s *LocalAPITokenStore) ReadAll(ctx context.Context) ([]*APITokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readAllLocked()
}

func (s *LocalAPITokenStore) UpdateAll(ctx context.Context, fn func([]*APITokenRecord) ([]*APITokenRecord, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.readAllLocked()
	if err != nil {
		return err
	}
	updated, err := fn(records)
	if err != nil {
		return err
	}
	return s.writeAllLocked(updated)
}

func (s *LocalAPITokenStore) readAllLocked() ([]*APITokenRecord, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []*APITokenRecord{}, nil
		}
		return nil, fmt.Errorf("read api token store %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return []*APITokenRecord{}, nil
	}
	var records []*APITokenRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse api token store %s: %w", s.path, err)
	}
	if records == nil {
		records = []*APITokenRecord{}
	}
	return records, nil
}

func (s *LocalAPITokenStore) writeAllLocked(records []*APITokenRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create api token store dir %s: %w", filepath.Dir(s.path), err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal api token store %s: %w", s.path, err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write api token store %s: %w", s.path, err)
	}
	return nil
}
