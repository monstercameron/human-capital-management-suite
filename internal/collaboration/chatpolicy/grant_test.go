package chatpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func conversationGrantTerms() ConversationGrantTerms {
	return ConversationGrantTerms{
		ConversationID: "conversation-1", HostTenant: "acme", ConsumerTenant: "vendor",
		Scope: "conversation", Classification: "internal", Residency: "US",
		ExpiresAt: policyAt.Add(time.Hour),
	}
}

func TestConversationGrantRequiresBoundedTerms(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ConversationGrantTerms)
	}{
		{"unbounded expiry", func(v *ConversationGrantTerms) { v.ExpiresAt = time.Time{} }},
		{"wrong scope", func(v *ConversationGrantTerms) { v.Scope = "tenant" }},
		{"missing classification", func(v *ConversationGrantTerms) { v.Classification = "" }},
		{"missing residency", func(v *ConversationGrantTerms) { v.Residency = "" }},
		{"same company", func(v *ConversationGrantTerms) { v.ConsumerTenant = v.HostTenant }},
		{"expired", func(v *ConversationGrantTerms) { v.ExpiresAt = policyAt }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terms := conversationGrantTerms()
			tt.mutate(&terms)
			if err := terms.Validate(policyAt); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Validate error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestConversationGrantConsentBindsExactTerms(t *testing.T) {
	terms := conversationGrantTerms()
	proposal, err := ProposeConversationGrant("grant-1", terms, 1, policyAt)
	if err != nil {
		t.Fatalf("ProposeConversationGrant: %v", err)
	}
	if ConversationGrantCurrent(proposal, terms, policyAt) {
		t.Fatal("host proposal admitted before consumer consent")
	}
	if _, err := AcceptConversationGrant(proposal, "other", policyAt); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("wrong consumer acceptance error = %v", err)
	}
	accepted, err := AcceptConversationGrant(proposal, terms.ConsumerTenant, policyAt)
	if err != nil {
		t.Fatalf("AcceptConversationGrant: %v", err)
	}
	if !ConversationGrantCurrent(accepted, terms, policyAt) {
		t.Fatal("accepted bilateral grant did not match the proposed terms")
	}
	changed := terms
	changed.Residency = "EU"
	if ConversationGrantCurrent(accepted, changed, policyAt) {
		t.Fatal("grant accepted after residency terms changed")
	}
	changed = terms
	changed.ExpiresAt = terms.ExpiresAt.Add(time.Second)
	if ConversationGrantCurrent(accepted, changed, policyAt) {
		t.Fatal("grant accepted after expiry terms changed")
	}
}

func TestTodo_CHAT_012_Golden(t *testing.T) {
	terms := conversationGrantTerms()
	proposal, err := ProposeConversationGrant("grant-1", terms, 1, policyAt)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := AcceptConversationGrant(proposal, terms.ConsumerTenant, policyAt)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := json.MarshalIndent(accepted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	path := filepath.Join("testdata", "chat_012_accepted_grant.golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if !bytes.Equal(actual, want) {
		t.Fatalf("accepted grant changed from golden %s\n--- got ---\n%s\n--- want ---\n%s", path, actual, want)
	}
}
