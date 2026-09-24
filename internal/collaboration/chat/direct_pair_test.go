package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodo_CHAT_015(t *testing.T) {
	participants := []MemberRef{
		{TenantID: "tenant-a", SubjectID: "alice"},
		{TenantID: "tenant-a", SubjectID: "bob"},
	}
	got, err := DirectPairConversationID("tenant-a", participants)
	if err != nil {
		t.Fatalf("DirectPairConversationID() error = %v", err)
	}
	if !isUUID(got) {
		t.Fatalf("DirectPairConversationID() = %q, want UUID", got)
	}
}

func TestTodo_CHAT_015_Property(t *testing.T) {
	first := []MemberRef{{TenantID: "tenant-a", SubjectID: "alice"}, {TenantID: "tenant-b", SubjectID: "bob"}}
	second := []MemberRef{first[1], first[0]}
	forward, err := DirectPairConversationID("tenant-a", first)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := DirectPairConversationID("tenant-a", second)
	if err != nil {
		t.Fatal(err)
	}
	if forward != reversed {
		t.Fatalf("reversing participant order changed pair identity: %s != %s", forward, reversed)
	}

	otherTenant, err := DirectPairConversationID("tenant-b", first)
	if err != nil {
		t.Fatal(err)
	}
	if otherTenant == forward {
		t.Fatal("the same pair in another host tenant reused the conversation identity")
	}

	sameSubjectDifferentHome, err := DirectPairConversationID("tenant-a", []MemberRef{{TenantID: "tenant-a", SubjectID: "alice"}, {TenantID: "tenant-c", SubjectID: "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	if sameSubjectDifferentHome == forward {
		t.Fatal("home tenant did not contribute to participant identity")
	}
}

func TestTodo_CHAT_015_Security(t *testing.T) {
	for name, participants := range map[string][]MemberRef{
		"third participant": {
			{TenantID: "tenant-a", SubjectID: "alice"},
			{TenantID: "tenant-a", SubjectID: "bob"},
			{TenantID: "tenant-a", SubjectID: "carol"},
		},
		"duplicate identity": {
			{TenantID: "tenant-a", SubjectID: "alice"},
			{TenantID: "tenant-a", SubjectID: "alice"},
		},
		"missing subject": {
			{TenantID: "tenant-a", SubjectID: "alice"},
			{TenantID: "tenant-a"},
		},
		"missing home tenant": {
			{TenantID: "tenant-a", SubjectID: "alice"},
			{SubjectID: "bob"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DirectPairConversationID("tenant-a", participants)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("DirectPairConversationID() error = %v, want ErrInvalidArgument", err)
			}
			if got != "" {
				t.Fatalf("invalid pair returned reusable identity %q", got)
			}
		})
	}
	if id, err := DirectPairConversationID(" ", []MemberRef{{TenantID: "tenant-a", SubjectID: "alice"}, {TenantID: "tenant-a", SubjectID: "bob"}}); !errors.Is(err, ErrInvalidArgument) || id != "" {
		t.Fatalf("blank tenant produced id=%q, err=%v; want empty id and ErrInvalidArgument", id, err)
	}
}

func TestTodo_CHAT_015_Golden(t *testing.T) {
	got, err := DirectPairConversationID("tenant-a", []MemberRef{
		{TenantID: "tenant-a", SubjectID: "alice"},
		{TenantID: "tenant-a", SubjectID: "bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "chat_015_direct_pair_id.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("stable direct pair id changed: got %s, want %s", got, strings.TrimSpace(string(want)))
	}
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdef", r) {
				return false
			}
		}
	}
	return true
}
