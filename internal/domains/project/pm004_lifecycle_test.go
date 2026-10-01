package project

import (
	"errors"
	"testing"
)

func TestTodo_PM_004(t *testing.T) {
	p, err := NewProject("project-pm004", "tenant-a", "owner-a", "Operations", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := p.TransitionProject(LifecycleSuspended, 1, "operator-a", RoleOperator, "incident response")
	if err != nil {
		t.Fatal(err)
	}
	if suspended.State != LifecycleSuspended || suspended.CanWrite() || suspended.Revision != 2 || len(suspended.History) != 1 || suspended.History[0].Reason != "incident response" {
		t.Fatalf("suspended project = %+v", suspended)
	}
	if _, err := NewTask("task-blocked", suspended, "Blocked during incident", "todo"); !errors.Is(err, ErrWriteUnavailable) {
		t.Fatalf("suspended project admitted task write: %v", err)
	}
	active, err := suspended.TransitionProject(LifecycleActive, 2, "owner-a", RoleOwner, "")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := active.TransitionProject(LifecycleArchived, 3, "owner-a", RoleOwner, "")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := archived.TransitionProject(LifecycleActive, 4, "owner-a", RoleOwner, "")
	if err != nil {
		t.Fatal(err)
	}
	if restored.State != LifecycleActive || restored.Revision != 5 || len(restored.History) != 4 {
		t.Fatalf("restored lifecycle = %+v", restored)
	}
}

func TestTodo_PM_004_Security(t *testing.T) {
	p, err := NewProject("project-pm004-security", "tenant-a", "owner-a", "Operations", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, actor string
		role        ActorRole
		reason      string
	}{
		{name: "owner is not scoped operator", actor: "owner-a", role: RoleOwner, reason: "incident"},
		{name: "operator must give reason", actor: "operator-a", role: RoleOperator},
		{name: "operator cannot archive", actor: "operator-a", role: RoleOperator, reason: "incident"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := LifecycleSuspended
			reason := tc.reason
			if tc.name == "operator cannot archive" {
				target = LifecycleArchived
				reason = ""
			}
			_, err := p.TransitionProject(target, 1, tc.actor, tc.role, reason)
			if !errors.Is(err, ErrNotAuthorized) {
				t.Fatalf("transition error = %v, want %v", err, ErrNotAuthorized)
			}
		})
	}
}
