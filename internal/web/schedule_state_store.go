package web

import (
	"context"
	"time"
)

const scheduleStateObjectName = "schedule-state.json"

type ScheduleState struct {
	LastEvaluatedAt   time.Time `json:"lastEvaluatedAt,omitempty"`
	LastTriggeredSlot time.Time `json:"lastTriggeredSlot,omitempty"`
}

type ScheduleStateIndex struct {
	Items map[string]ScheduleState `json:"items"`
}

func newScheduleStateIndex() *ScheduleStateIndex {
	return &ScheduleStateIndex{Items: make(map[string]ScheduleState)}
}

type ScheduleStateStore interface {
	ReadState(ctx context.Context) (*ScheduleStateIndex, error)
	UpdateState(ctx context.Context, fn func(*ScheduleStateIndex) error) error
}
