package roleaccessstore

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

// TestTodo_RBAC_RT_021 is the PRIMARY matrix entry: the revision builder
// shapes one ledger row per role change with JSON before/after images, and
// refuses unknown kinds, missing identity and unencodable payloads.
func TestTodo_RBAC_RT_021(t *testing.T) {
	revisionID, priorID := uuid.New(), uuid.New()
	before := roleaccess.PagePermission{Version: 1, RoleID: "manager", PageID: "journeys", View: true}
	after := roleaccess.PagePermission{Version: 2, RoleID: "manager", PageID: "journeys", View: true, Update: true}

	entry, err := NewRevision(revisionID, "system:test", RevisionPagePermission, "manager", "", "journeys", "", before, after, &priorID, "Expanded page access for the regional review team")
	if err != nil {
		t.Fatalf("NewRevision: %v", err)
	}
	if entry.Kind != RevisionPagePermission || entry.ActorRef != "system:test" || entry.PageID != "journeys" {
		t.Fatalf("entry identity = %+v, want kind/page/actor carried", entry)
	}
	if entry.Reason != "Expanded page access for the regional review team" {
		t.Fatalf("reason = %q, want the supplied audit rationale", entry.Reason)
	}
	if entry.PriorRevision == nil || *entry.PriorRevision != priorID {
		t.Fatal("entry dropped the prior revision link")
	}
	var decodedBefore, decodedAfter roleaccess.PagePermission
	if err := json.Unmarshal(entry.Before, &decodedBefore); err != nil || decodedBefore != before {
		t.Fatalf("before image = %s (err=%v), want %+v", entry.Before, err, before)
	}
	if err := json.Unmarshal(entry.After, &decodedAfter); err != nil || decodedAfter != after {
		t.Fatalf("after image = %s (err=%v), want %+v", entry.After, err, after)
	}

	genesis, err := NewRevision(uuid.New(), "system:test", RevisionRole, "manager", "", "", "", nil, roleaccess.Role{ID: "manager", Active: true}, nil, "Initial role creation for regional review")
	if err != nil {
		t.Fatalf("genesis NewRevision: %v", err)
	}
	if genesis.Before != nil || genesis.PriorRevision != nil || len(genesis.After) == 0 {
		t.Fatal("genesis entry must carry only the after image")
	}

	for _, kind := range []ChangeKind{RevisionRole, RevisionAssignment, RevisionVisibility, RevisionPagePermission, RevisionFeaturePermission} {
		if _, err := NewRevision(uuid.New(), "system:test", kind, "manager", "", "", "", nil, nil, nil, "Permission correction requested by the administrator"); err != nil {
			t.Fatalf("NewRevision(%s): %v", kind, err)
		}
	}

	for name, build := range map[string]func() (RevisionEntry, error){
		"unknown kind": func() (RevisionEntry, error) {
			return NewRevision(uuid.New(), "system:test", "MERGE", "manager", "", "", "", nil, nil, nil)
		},
		"missing revision id": func() (RevisionEntry, error) {
			return NewRevision(uuid.Nil, "system:test", RevisionRole, "manager", "", "", "", nil, nil, nil)
		},
		"missing actor": func() (RevisionEntry, error) {
			return NewRevision(uuid.New(), "  ", RevisionRole, "manager", "", "", "", nil, nil, nil)
		},
		"missing role": func() (RevisionEntry, error) {
			return NewRevision(uuid.New(), "system:test", RevisionRole, "", "", "", "", nil, nil, nil)
		},
		"missing reason": func() (RevisionEntry, error) {
			return NewRevision(uuid.New(), "system:test", RevisionRole, "manager", "", "", "", nil, nil, nil)
		},
		"multiline reason": func() (RevisionEntry, error) {
			return NewRevision(uuid.New(), "system:test", RevisionRole, "manager", "", "", "", nil, nil, nil, "changed\nwithout rationale")
		},
		"unencodable image": func() (RevisionEntry, error) {
			return NewRevision(uuid.New(), "system:test", RevisionRole, "manager", "", "", "", func() {}, nil, nil)
		},
	} {
		if _, err := build(); !errors.Is(err, ErrInvalidRevision) {
			t.Fatalf("%s: err = %v, want ErrInvalidRevision", name, err)
		}
	}
}
