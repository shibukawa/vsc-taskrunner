package web

import (
	"testing"
	"time"
)

func TestEvaluateScheduleDueInitialObservationDoesNotTrigger(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 1, 10, 0, 0, 0, time.Local)
	slot, triggered, state, err := evaluateScheduleDue("*/5 * * * *", now, ScheduleState{})
	if err != nil {
		t.Fatal(err)
	}
	if triggered {
		t.Fatal("expected first observation not to trigger")
	}
	if !slot.IsZero() {
		t.Fatalf("slot = %v, want zero", slot)
	}
	if !state.LastEvaluatedAt.Equal(now) {
		t.Fatalf("LastEvaluatedAt = %v, want %v", state.LastEvaluatedAt, now)
	}
}

func TestEvaluateScheduleDueDelayedHeartbeatTriggersLatestSlotOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 1, 10, 16, 0, 0, time.Local)
	state := ScheduleState{
		LastEvaluatedAt: time.Date(2026, time.April, 1, 10, 0, 0, 0, time.Local),
	}
	slot, triggered, nextState, err := evaluateScheduleDue("*/5 * * * *", now, state)
	if err != nil {
		t.Fatal(err)
	}
	if !triggered {
		t.Fatal("expected delayed heartbeat to trigger")
	}
	want := time.Date(2026, time.April, 1, 10, 15, 0, 0, time.Local)
	if !slot.Equal(want) {
		t.Fatalf("slot = %v, want %v", slot, want)
	}
	if !nextState.LastTriggeredSlot.Equal(want) {
		t.Fatalf("LastTriggeredSlot = %v, want %v", nextState.LastTriggeredSlot, want)
	}
	if !nextState.LastEvaluatedAt.Equal(now) {
		t.Fatalf("LastEvaluatedAt = %v, want %v", nextState.LastEvaluatedAt, now)
	}
	secondSlot, secondTriggered, _, err := evaluateScheduleDue("*/5 * * * *", now, nextState)
	if err != nil {
		t.Fatal(err)
	}
	if secondTriggered || !secondSlot.IsZero() {
		t.Fatalf("expected no duplicate trigger, got triggered=%v slot=%v", secondTriggered, secondSlot)
	}
}
