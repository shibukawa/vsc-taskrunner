package web

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type LocalScheduleStateStore struct {
	path     string
	lockPath string
}

func NewLocalScheduleStateStore(historyDir string) *LocalScheduleStateStore {
	path := filepath.Join(historyDir, scheduleStateObjectName)
	return &LocalScheduleStateStore{
		path:     path,
		lockPath: path + ".lock",
	}
}

func (s *LocalScheduleStateStore) ReadState(ctx context.Context) (*ScheduleStateIndex, error) {
	_ = ctx
	return s.readState()
}

func (s *LocalScheduleStateStore) UpdateState(ctx context.Context, fn func(*ScheduleStateIndex) error) error {
	_ = ctx
	if err := os.MkdirAll(filepath.Dir(s.lockPath), 0o755); err != nil {
		return fmt.Errorf("create schedule state lock dir: %w", err)
	}
	lockFile, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open schedule state lock: %w", err)
	}
	defer lockFile.Close()
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock schedule state: %w", err)
	}
	defer unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)

	state, err := s.readState()
	if err != nil {
		return err
	}
	if err := fn(state); err != nil {
		return err
	}
	return s.writeState(state)
}

func (s *LocalScheduleStateStore) readState() (*ScheduleStateIndex, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return newScheduleStateIndex(), nil
		}
		return nil, fmt.Errorf("read schedule state: %w", err)
	}
	if len(data) == 0 {
		return newScheduleStateIndex(), nil
	}
	var state ScheduleStateIndex
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse schedule state: %w", err)
	}
	if state.Items == nil {
		state.Items = make(map[string]ScheduleState)
	}
	return &state, nil
}

func (s *LocalScheduleStateStore) writeState(state *ScheduleStateIndex) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create schedule state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schedule state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "schedule-state-*.json")
	if err != nil {
		return fmt.Errorf("create temp schedule state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp schedule state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp schedule state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace schedule state: %w", err)
	}
	return nil
}
