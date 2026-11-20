package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3793Port struct {
	installs  []chatapps.Installation
	listErr   error
	authErr   error
	members   map[string]bool
	proposals []chatapps.Proposal
	proposeFn func(chatapps.Proposal) (chatapps.ProposalReceipt, error)
	scopes    []string
}

func (p *issue3793Port) ListInstallations(_ context.Context, _ chat.Principal, conversation string) ([]chatapps.Installation, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	var out []chatapps.Installation
	for _, i := range p.installs {
		if i.Conversation == conversation {
			out = append(out, i)
		}
	}
	return out, nil
}
func (p *issue3793Port) ProposeIntent(_ context.Context, _ chat.Principal, _ string, proposal chatapps.Proposal) (chatapps.ProposalReceipt, error) {
	p.proposals = append(p.proposals, proposal)
	if p.proposeFn != nil {
		return p.proposeFn(proposal)
	}
	return chatapps.ProposalReceipt{IntentID: "intent-1", Status: "DRAFT"}, nil
}
func (p *issue3793Port) AuthorizeRequestCommand(_ context.Context, _ chat.Principal, _, _, scope string) error {
	p.scopes = append(p.scopes, scope)
	return p.authErr
}
func (p *issue3793Port) ActiveMembers(context.Context, chat.Principal, string) (map[string]bool, error) {
	return p.members, nil
}

func issue3793Install(status chatapps.Status, granted bool) chatapps.Installation {
	i := chatapps.Installation{
		ID: "tenant-a:room:hr-assistant", Tenant: "tenant-a", Conversation: "room", AppID: "hr-assistant", Version: 1, Status: status,
		Manifest: chatapps.Manifest{AppID: "hr-assistant", Version: 1, Scopes: []string{"requests:leave"},
			Commands: []chatapps.Command{{Name: "leave", Scope: "requests:leave"}},
			Agent:    &chatapps.AgentManifest{DisplayName: "HR Assistant"}},
	}
	if granted {
		i.GrantedScopes = []string{"requests:leave"}
	}
	return i
}

func issue3793Fixture(t *testing.T) (*ChatIntentRequests, *issue3793Port, *trust.Principal) {
	t.Helper()
	human := issue3795Person(t, "person", trust.SubjectKindHuman)
	install := issue3793Install(chatapps.Active, true)
	install.Tenant = human.Tenant().String()
	port := &issue3793Port{installs: []chatapps.Installation{install}, members: map[string]bool{"person": true, "boss": true}}
	return &ChatIntentRequests{Port: port, Now: time.Now}, port, human
}

func issue3793Values() map[string]string {
	return map[string]string{"employee": "person", "starts": "2026-12-01", "ends": "2026-12-05", "reason": "Family trip", "approver": "boss"}
}

func issue3793Own(human *trust.Principal, status chatapps.Status, granted bool) []chatapps.Installation {
	i := issue3793Install(status, granted)
	i.Tenant = human.Tenant().String()
	return []chatapps.Installation{i}
}

func TestIssue3793_AvailabilityNamesWhyACommandIsMissing(t *testing.T) {
	s, port, human := issue3793Fixture(t)
	got, err := s.Availability(context.Background(), human, "room")
	if err != nil || len(got) != 1 || !got[0].Available || got[0].Command != "leave" {
		t.Fatalf("installed and granted: %+v %v", got, err)
	}
	noCommand := issue3793Own(human, chatapps.Active, true)
	noCommand[0].Manifest.Commands = nil
	cases := []struct {
		name   string
		mutate func()
		want   string
	}{
		{"no installation", func() { port.installs = nil }, ChatIntentRequestNotInstalled},
		{"agent without the command", func() { port.installs = noCommand }, ChatIntentRequestNotInstalled},
		{"suspended", func() { port.installs = issue3793Own(human, chatapps.Suspended, true) }, ChatIntentRequestPaused},
		{"revoked", func() { port.installs = issue3793Own(human, chatapps.Revoked, true) }, ChatIntentRequestPaused},
		{"scope not granted", func() { port.installs = issue3793Own(human, chatapps.Active, false) }, ChatIntentRequestNotGranted},
		{"person lacks authority", func() {
			port.installs = issue3793Own(human, chatapps.Active, true)
			port.authErr = chat.ErrPermissionDenied
		}, ChatIntentRequestNotAllowed},
	}
	for _, c := range cases {
		port.authErr = nil
		c.mutate()
		got, err := s.Availability(context.Background(), human, "room")
		if err != nil || got[0].Available || got[0].Reason != c.want {
			t.Fatalf("%s: want reason %q, got %+v %v", c.name, c.want, got, err)
		}
	}
}

func TestIssue3793_AnotherConversationsOrTenantsInstallIsNotUsed_Security(t *testing.T) {
	s, port, human := issue3793Fixture(t)
	other := issue3793Own(human, chatapps.Active, true)[0]
	other.Conversation = "elsewhere"
	port.installs = []chatapps.Installation{other}
	if got, _ := s.Availability(context.Background(), human, "room"); got[0].Available {
		t.Fatalf("an installation of another room must not take a request in: %+v", got)
	}
	foreign := issue3793Install(chatapps.Active, true)
	foreign.Tenant = "tenant-z"
	port.installs = []chatapps.Installation{foreign}
	if got, _ := s.Availability(context.Background(), human, "room"); got[0].Available {
		t.Fatalf("an installation of another tenant must not take a request in: %+v", got)
	}
}

func TestIssue3793_PreviewChecksEverythingAndWritesNothing(t *testing.T) {
	s, port, human := issue3793Fixture(t)
	preview, err := s.Preview(context.Background(), human, "room", "leave", issue3793Values())
	if err != nil || preview.Via != "HR Assistant" || preview.Values["reason"] != "Family trip" || preview.Command != "leave" {
		t.Fatalf("preview = %+v %v", preview, err)
	}
	if len(port.proposals) != 0 {
		t.Fatal("a preview must not propose anything")
	}
	bad := issue3793Values()
	bad["ends"], bad["employee"] = "2026-11-01", "stranger"
	_, err = s.Preview(context.Background(), human, "room", "leave", bad)
	var invalid *ChatIntentRequestInvalidError
	if !errors.As(err, &invalid) || invalid.Fields["employee"] != chat.FormErrUnknownUser {
		t.Fatalf("invalid values must be named by field: %v", err)
	}
	bad["employee"] = "person"
	_, err = s.Preview(context.Background(), human, "room", "leave", bad)
	if !errors.As(err, &invalid) || invalid.Fields["ends"] != chat.IntentRequestErrBadRange {
		t.Fatalf("an end before the start must be named: %v", err)
	}
	port.installs = nil
	var gone *ChatIntentRequestNotAvailableError
	if _, err = s.Preview(context.Background(), human, "room", "leave", issue3793Values()); !errors.As(err, &gone) || gone.Reason != ChatIntentRequestNotInstalled {
		t.Fatalf("no installation must be an unavailable state: %v", err)
	}
}

func TestIssue3793_SubmitProposesATypedIntentAndAnswersWithoutImplyingApproval_Integration(t *testing.T) {
	s, port, human := issue3793Fixture(t)
	receipt, err := s.Submit(context.Background(), human, "room", "leave", issue3793Values(), "submit-0001-aaaa")
	if err != nil || receipt.IntentID != "intent-1" || receipt.Command != "leave" {
		t.Fatalf("receipt = %+v %v", receipt, err)
	}
	if !strings.HasPrefix(receipt.Href, "/workspace/app/journeys?journey=intent-1") {
		t.Fatalf("receipt must link the intent: %q", receipt.Href)
	}
	if len(port.proposals) != 1 {
		t.Fatalf("proposals = %d", len(port.proposals))
	}
	got := port.proposals[0]
	if got.AgentInstallation != "tenant-a:room:hr-assistant" || got.IntentType != "hcmnext.workforce.leave_request" || got.SchemaID == "" || got.SchemaVersion != 1 {
		t.Fatalf("proposal must name the owning installation and the typed intent: %+v", got)
	}
	if len(got.Subjects) != 1 || got.Subjects[0].ID != "person" || got.Subjects[0].Kind != "EMPLOYMENT" {
		t.Fatalf("subjects = %+v", got.Subjects)
	}
	if got.RequestPayload["starts"] != "2026-12-01" || got.RequestPayload["reason"] != "Family trip" || got.Arguments["approver"] != "boss" {
		t.Fatalf("payload = %+v args = %+v", got.RequestPayload, got.Arguments)
	}
	if len(port.scopes) == 0 || port.scopes[len(port.scopes)-1] != "requests:leave" {
		t.Fatalf("authority for the command must be rechecked at submit: %v", port.scopes)
	}
}

func TestIssue3793_SubmissionKeyIsBoundToOneExplicitSubmission(t *testing.T) {
	s, port, human := issue3793Fixture(t)
	for _, key := range []string{"submit-0001-aaaa", "submit-0001-aaaa", "submit-0002-bbbb"} {
		if _, err := s.Submit(context.Background(), human, "room", "leave", issue3793Values(), key); err != nil {
			t.Fatal(err)
		}
	}
	a, b, c := port.proposals[0].IdempotencyKey, port.proposals[1].IdempotencyKey, port.proposals[2].IdempotencyKey
	if a != b {
		t.Fatalf("a retry of the same submission must carry the same key: %q %q", a, b)
	}
	if a == c {
		t.Fatalf("a deliberately different request must carry a different key")
	}
	if strings.Contains(a, "submit-0001") || strings.Contains(a, "room") || strings.Contains(a, "person") {
		t.Fatalf("the kernel key must not echo its inputs: %q", a)
	}
	// The same key replayed with different values is a different request.
	changed := issue3793Values()
	changed["reason"] = "Something else"
	if _, err := s.Submit(context.Background(), human, "room", "leave", changed, "submit-0001-aaaa"); err != nil {
		t.Fatal(err)
	}
	if port.proposals[3].IdempotencyKey == a {
		t.Fatal("a replayed key with different values must not recover the first request")
	}
	port.proposals = port.proposals[:3]
	// The same key from another person never collides.
	other := issue3795Person(t, "boss", trust.SubjectKindHuman)
	if _, err := s.Submit(context.Background(), other, "room", "leave", issue3793Values(), "submit-0001-aaaa"); err != nil {
		t.Fatal(err)
	}
	if port.proposals[3].IdempotencyKey == a {
		t.Fatal("a submission key must be bound to the person who made it")
	}
}

func TestIssue3793_SubmitRefusesBeforeAnySideEffect_Security(t *testing.T) {
	ctx := context.Background()
	s, port, human := issue3793Fixture(t)
	if _, err := s.Submit(ctx, issue3795Person(t, "agent", trust.SubjectKindAgent), "room", "leave", issue3793Values(), "submit-0001-aaaa"); !errors.Is(err, ErrChatIntentRequestDenied) {
		t.Fatalf("an agent must not submit a person's request: %v", err)
	}
	if _, err := s.Submit(ctx, human, "room", "leave", issue3793Values(), "short"); !errors.Is(err, ErrChatIntentRequestInvalid) {
		t.Fatalf("a submission needs its own key: %v", err)
	}
	if _, err := s.Submit(ctx, human, "room", "fire", issue3793Values(), "submit-0001-aaaa"); !errors.Is(err, ErrChatIntentRequestNotFound) {
		t.Fatalf("an unknown command: %v", err)
	}
	port.listErr = chat.ErrPermissionDenied
	if _, err := s.Submit(ctx, human, "room", "leave", issue3793Values(), "submit-0001-aaaa"); !errors.Is(err, ErrChatIntentRequestNotFound) {
		t.Fatalf("a conversation the person cannot use must read as not found: %v", err)
	}
	port.listErr = nil
	port.authErr = chat.ErrPermissionDenied
	var gone *ChatIntentRequestNotAvailableError
	if _, err := s.Submit(ctx, human, "room", "leave", issue3793Values(), "submit-0001-aaaa"); !errors.As(err, &gone) || gone.Reason != ChatIntentRequestNotAllowed {
		t.Fatalf("a person without authority for the command: %v", err)
	}
	port.authErr = nil
	port.installs = issue3793Own(human, chatapps.Revoked, true)
	if _, err := s.Submit(ctx, human, "room", "leave", issue3793Values(), "submit-0001-aaaa"); !errors.As(err, &gone) || gone.Reason != ChatIntentRequestPaused {
		t.Fatalf("a revoked installation: %v", err)
	}
	port.installs = issue3793Own(human, chatapps.Active, true)
	bad := issue3793Values()
	bad["employee"] = "stranger"
	if _, err := s.Submit(ctx, human, "room", "leave", bad, "submit-0001-aaaa"); err == nil {
		t.Fatal("a person who is not in the conversation must be refused")
	}
	if len(port.proposals) != 0 {
		t.Fatalf("no refused request may reach the intent kernel: %d proposals", len(port.proposals))
	}
}

func TestIssue3793_KernelRefusalsReachThePersonAsShortErrors(t *testing.T) {
	ctx := context.Background()
	s, port, human := issue3793Fixture(t)
	port.proposeFn = func(chatapps.Proposal) (chatapps.ProposalReceipt, error) {
		return chatapps.ProposalReceipt{}, chatapps.ErrDenied
	}
	if _, err := s.Submit(ctx, human, "room", "leave", issue3793Values(), "submit-0001-aaaa"); !errors.Is(err, ErrChatIntentRequestDenied) {
		t.Fatalf("a refusal of the intent kernel: %v", err)
	}
	port.proposeFn = func(chatapps.Proposal) (chatapps.ProposalReceipt, error) {
		return chatapps.ProposalReceipt{}, errors.New("database exploded at host db-7")
	}
	_, err := s.Submit(ctx, human, "room", "leave", issue3793Values(), "submit-0001-aaaa")
	if !errors.Is(err, ErrChatIntentRequestUnavailable) || strings.Contains(err.Error(), "db-7") {
		t.Fatalf("a failure must not leak its cause: %v", err)
	}
	port.proposeFn = func(chatapps.Proposal) (chatapps.ProposalReceipt, error) { return chatapps.ProposalReceipt{}, nil }
	if _, err := s.Submit(ctx, human, "room", "leave", issue3793Values(), "submit-0001-aaaa"); !errors.Is(err, ErrChatIntentRequestUnavailable) {
		t.Fatalf("a receipt with no intent is not a receipt: %v", err)
	}
}

func TestIssue3793_UnconfiguredUseCaseFailsClosed(t *testing.T) {
	human := issue3795Person(t, "person", trust.SubjectKindHuman)
	var s *ChatIntentRequests
	if _, err := s.Availability(context.Background(), human, "room"); !errors.Is(err, ErrChatIntentRequestUnavailable) {
		t.Fatalf("nil use case: %v", err)
	}
	if NewChatIntentRequests(nil) != nil || NewChatIntentRequests(&ChatExtensions{}) != nil {
		t.Fatal("without extensions the routes stay off")
	}
	ext := &ChatExtensions{}
	if err := ext.AuthorizeRequestCommand(context.Background(), chat.Principal{}, "room", "app", "scope"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("no command authority wired must deny: %v", err)
	}
}
