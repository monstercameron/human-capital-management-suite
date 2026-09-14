package jit

import (
	"errors"
	"testing"
	"time"
)

func storedGrant() Stored {
	issued := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	return Stored{ID: "jit-1", Principal: "operator:ana", Tenant: "tenant-1", Role: RoleIncidentResponder, TicketRef: "INC-1",
		Justification: "outage", Capabilities: []string{"WORKFLOW_PAUSE"}, Purpose: "incident", Approver: "approver:lead",
		IssuedAt: issued, ExpiresAt: issued.Add(4 * time.Hour)}
}

func TestRestoreKeepsRecordedWindowAndRules(t *testing.T) {
	s := storedGrant()
	g, err := Restore(s)
	if err != nil {
		t.Fatal(err)
	}
	if !g.IssuedAt.Equal(s.IssuedAt) || !g.ExpiresAt.Equal(s.ExpiresAt) || !g.IsActive(s.IssuedAt.Add(time.Hour)) || g.IsActive(s.ExpiresAt) {
		t.Fatalf("restored window = %s..%s", g.IssuedAt, g.ExpiresAt)
	}
	if err := g.Use("pause", s.IssuedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	revoked := s
	revoked.Revoked = true
	rg, err := Restore(revoked)
	if err != nil || rg.IsActive(s.IssuedAt.Add(time.Minute)) {
		t.Fatalf("revoked restore = %v active=%v", err, rg.IsActive(s.IssuedAt.Add(time.Minute)))
	}
	for name, tc := range map[string]struct {
		mutate func(*Stored)
		want   error
	}{
		"no id":          {func(s *Stored) { s.ID = " " }, ErrInvalidRequest},
		"unknown role":   {func(s *Stored) { s.Role = "ROOT" }, ErrUnknownRole},
		"no ticket":      {func(s *Stored) { s.TicketRef = "" }, ErrInvalidRequest},
		"no capability":  {func(s *Stored) { s.Capabilities = nil }, ErrInvalidRequest},
		"no approver":    {func(s *Stored) { s.Approver = "" }, ErrInvalidRequest},
		"self approved":  {func(s *Stored) { s.Approver = "OPERATOR:ANA" }, ErrApproverIsRequester},
		"widened window": {func(s *Stored) { s.ExpiresAt = s.IssuedAt.Add(9 * time.Hour) }, ErrTTLExceedsMax},
		"no window":      {func(s *Stored) { s.ExpiresAt = s.IssuedAt }, ErrTTLRequired},
		"no issue":       {func(s *Stored) { s.IssuedAt = time.Time{} }, ErrTTLRequired},
	} {
		st := storedGrant()
		tc.mutate(&st)
		if _, err := Restore(st); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}
