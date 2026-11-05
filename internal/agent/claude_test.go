package agent

import (
	"testing"
	"time"

	"github.com/davidalecrim1/amx/internal/state"
)

var now = time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC)

func TestPromptStartsRunning(t *testing.T) {
	rec := &state.Record{State: state.Review}
	res := ApplyClaude("UserPromptSubmit", map[string]any{
		"prompt":     "fix   the\nwebhook",
		"session_id": "abc",
	}, rec, now)

	if !res.Changed {
		t.Fatal("expected a change")
	}
	if rec.State != state.Running {
		t.Fatalf("state = %q, want running", rec.State)
	}
	if rec.Agent != "claude" || rec.SessionID != "abc" {
		t.Fatalf("agent/session not recorded: %+v", rec)
	}
	if rec.Title != "fix the webhook" {
		t.Fatalf("title = %q", rec.Title)
	}
	ApplyClaude("UserPromptSubmit", map[string]any{"prompt": "another task"}, rec, now)
	if rec.Title != "fix the webhook" {
		t.Fatalf("title should keep the first prompt, got %q", rec.Title)
	}
	if !rec.UpdatedAt.Equal(now) {
		t.Fatalf("updated_at = %v", rec.UpdatedAt)
	}
}

func TestPermissionRequestBlocks(t *testing.T) {
	rec := &state.Record{State: state.Running}
	ApplyClaude("PermissionRequest", map[string]any{"tool_name": "Bash"}, rec, now)

	if rec.State != state.NeedsInput {
		t.Fatalf("state = %q, want needs_input", rec.State)
	}
	if rec.Detail != "Bash" {
		t.Fatalf("detail = %q", rec.Detail)
	}
}

func TestStopMarksDone(t *testing.T) {
	rec := &state.Record{State: state.Running}
	ApplyClaude("Stop", map[string]any{"background_tasks": []any{}}, rec, now)
	if rec.State != state.Review {
		t.Fatalf("state = %q, want review", rec.State)
	}
}

func TestStopStaysRunningWithBackgroundWork(t *testing.T) {
	rec := &state.Record{State: state.Running}
	ApplyClaude("Stop", map[string]any{
		"background_tasks": []any{map[string]any{"id": "task-1"}},
	}, rec, now)
	if rec.State != state.Running {
		t.Fatalf("state = %q, want running", rec.State)
	}
}

func TestNotificationFiltersTypes(t *testing.T) {
	rec := &state.Record{State: state.Running}
	res := ApplyClaude("Notification", map[string]any{"notification_type": "idle_prompt"}, rec, now)
	if res.Changed || rec.State != state.Running {
		t.Fatalf("idle_prompt should be ignored, got %q", rec.State)
	}

	ApplyClaude("Notification", map[string]any{
		"notification_type": "permission_prompt",
		"message":           "Claude needs your approval",
	}, rec, now)
	if rec.State != state.NeedsInput {
		t.Fatalf("state = %q, want needs_input", rec.State)
	}
	if rec.Detail != "Claude needs your approval" {
		t.Fatalf("detail = %q", rec.Detail)
	}
}

func TestStopFailureBlocks(t *testing.T) {
	rec := &state.Record{State: state.Running}
	ApplyClaude("StopFailure", map[string]any{}, rec, now)
	if rec.State != state.NeedsInput {
		t.Fatalf("state = %q, want needs_input", rec.State)
	}
}

func TestSessionEndDeletes(t *testing.T) {
	rec := &state.Record{State: state.Done}
	res := ApplyClaude("SessionEnd", map[string]any{}, rec, now)
	if !res.Delete {
		t.Fatal("expected delete")
	}
}

func TestUnknownEventIsIgnored(t *testing.T) {
	rec := &state.Record{State: state.Running, UpdatedAt: now.Add(-time.Minute), LastEvent: now.Add(-time.Minute)}
	res := ApplyClaude("PreCompact", map[string]any{}, rec, now)
	if res.Changed {
		t.Fatal("unknown event should not change state")
	}
}

func TestHeartbeatKeepsAge(t *testing.T) {
	t0 := now.Add(-10 * time.Minute)
	rec := &state.Record{State: state.Running, UpdatedAt: t0, LastEvent: t0}
	ApplyClaude("PreToolUse", map[string]any{}, rec, now)

	if !rec.UpdatedAt.Equal(t0) {
		t.Fatalf("heartbeat moved updated_at to %v", rec.UpdatedAt)
	}
	if !rec.LastEvent.Equal(now) {
		t.Fatalf("last_event = %v, want %v", rec.LastEvent, now)
	}
}
