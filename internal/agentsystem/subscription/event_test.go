package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENT_032(t *testing.T) {
	input := validInput()
	candidate, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Request.Source.Kind != agentrun.SourceEvent || candidate.Request.Principal.Mode != agentrun.ModeSponsored ||
		candidate.Request.Agent != input.Subscription.Agent || candidate.Request.CauseID != input.Event.CauseID {
		t.Fatalf("request omitted event authority pins: %+v", candidate.Request)
	}
	if len(candidate.Fields) != 1 || candidate.Fields[0].Name != "status" || candidate.Fields[0].Value != "approved" {
		t.Fatalf("projection fields = %+v, want only the subscribed audience-safe field", candidate.Fields)
	}
	if candidate.Request.Context.Digest == "" || candidate.Request.Source.Key == "" {
		t.Fatalf("candidate lacks a projection digest or stable source key: %+v", candidate.Request)
	}
	if _, err := Evaluate(input); err != nil {
		t.Fatalf("identical event replay changed admission candidate: %v", err)
	}
}

func TestTodo_AGENT_032_Security(t *testing.T) {
	tests := []struct {
		name   string
		change func(*EvaluateInput)
		want   error
	}{
		{name: "undeclared payload field", change: func(in *EvaluateInput) {
			in.Event.Fields = append(in.Event.Fields, ProjectedField{Name: "bonus", Value: "secret", Classification: "RESTRICTED", AudienceIDs: []string{"audience-a"}})
		}, want: ErrForbiddenField},
		{name: "stale subscription revision", change: func(in *EvaluateInput) { in.Subscription.CurrentRevision++ }, want: ErrInactive},
		{name: "revoked grant", change: func(in *EvaluateInput) { in.Subscription.GrantActive = false }, want: ErrRevoked},
		{name: "stale grant revision", change: func(in *EvaluateInput) { in.Subscription.CurrentGrantRevision++ }, want: ErrRevoked},
		{name: "revoked installation", change: func(in *EvaluateInput) { in.Subscription.InstallationActive = false }, want: ErrRevoked},
		{name: "wrong audience snapshot", change: func(in *EvaluateInput) { in.Event.Audience.SnapshotID = "audience-old" }, want: ErrAudience},
		{name: "wrong tenant", change: func(in *EvaluateInput) { in.Event.TenantID = "tenant-b" }, want: ErrAudience},
		{name: "self-trigger", change: func(in *EvaluateInput) { in.Event.OriginAgentIDs = []string{in.Subscription.Agent.AgentID} }, want: ErrLoop},
		{name: "cause depth ceiling", change: func(in *EvaluateInput) { in.Event.CauseDepth = in.Subscription.MaxCauseDepth }, want: ErrLoop},
		{name: "debounce", change: func(in *EvaluateInput) { in.LastAdmittedAt = in.At.Add(-time.Second) }, want: ErrDebounced},
		{name: "budget expansion", change: func(in *EvaluateInput) { in.Budget.MaxCostMicros = 2001 }, want: ErrBudget},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validInput()
			tc.change(&input)
			if _, err := Evaluate(input); !errors.Is(err, tc.want) {
				t.Fatalf("Evaluate error = %v, want %v", err, tc.want)
			}
		})
	}
	filtered, err := Evaluate(validInput())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range filtered.Fields {
		if field.Name == "salary" || field.Name == "private_note" {
			t.Fatalf("filtered projection leaked forbidden field %q", field.Name)
		}
	}
}

func TestTodo_AGENT_032_Fault(t *testing.T) {
	tests := []struct {
		name   string
		change func(*EvaluateInput)
		want   error
	}{
		{name: "missing tenant", change: func(in *EvaluateInput) { in.Subscription.TenantID = "" }, want: ErrInvalid},
		{name: "invalid deadline", change: func(in *EvaluateInput) { in.Deadline = in.At }, want: ErrInvalid},
		{name: "unknown event class", change: func(in *EvaluateInput) { in.Event.Kind = "UNREGISTERED" }, want: ErrInvalid},
		{name: "no visible subscribed fields", change: func(in *EvaluateInput) { in.Event.Fields = nil }, want: ErrProjectionInvalid},
		{name: "subscription names unknown field", change: func(in *EvaluateInput) {
			in.Subscription.FieldsByKind["INTENT_COMPLETED"] = append(in.Subscription.FieldsByKind["INTENT_COMPLETED"], "unknown")
		}, want: ErrProjectionInvalid},
		{name: "duplicate projection field", change: func(in *EvaluateInput) { in.Event.Fields = append(in.Event.Fields, in.Event.Fields[0]) }, want: ErrProjectionInvalid},
		{name: "schema class tampered", change: func(in *EvaluateInput) { in.Event.Fields[0].Classification = "RESTRICTED" }, want: ErrForbiddenField},
		{name: "invalid classification", change: func(in *EvaluateInput) { in.Subscription.MaximumClassification = "SECRET" }, want: ErrInvalid},
		{name: "future admission history", change: func(in *EvaluateInput) { in.LastAdmittedAt = in.At.Add(time.Second) }, want: ErrDebounced},
		{name: "future event", change: func(in *EvaluateInput) { in.Event.OccurredAt = in.At.Add(time.Second) }, want: ErrInvalid},
		{name: "oversized event projection", change: func(in *EvaluateInput) { in.Event.Fields = make([]ProjectedField, 129) }, want: ErrProjectionInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validInput()
			tc.change(&input)
			if _, err := Evaluate(input); !errors.Is(err, tc.want) {
				t.Fatalf("Evaluate error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_AGENT_032_Integration(t *testing.T) {
	input := validInput()
	candidate, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	authority := &eventAdmissionAuthority{}
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{
		Authority: authority, Store: agentrun.NewMemoryAdmissionStore(), Now: func() time.Time { return input.At },
	})
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := service.Admit(context.Background(), candidate.Request)
	if err != nil || !created || first.Decision != agentrun.DecisionAccepted {
		t.Fatalf("first admission = (%+v,%t,%v), want accepted creation", first, created, err)
	}
	second, created, err := service.Admit(context.Background(), candidate.Request)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate admission = (%+v,%t,%v), want original durable decision", second, created, err)
	}
	changed := input
	changed.Event.Fields = append([]ProjectedField(nil), input.Event.Fields...)
	changed.Event.Fields[0].Value = "rejected"
	changedCandidate, err := Evaluate(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Admit(context.Background(), changedCandidate.Request); !errors.Is(err, agentrun.ErrSourceConflict) {
		t.Fatalf("changed event replay error = %v, want source conflict", err)
	}
}

func TestTodo_AGENT_032_Golden(t *testing.T) {
	input := validInput()
	forward, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Event.Fields[0], input.Event.Fields[1] = input.Event.Fields[1], input.Event.Fields[0]
	reversed, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if forward.Request.Source.Key != reversed.Request.Source.Key || forward.Request.Context.Digest != reversed.Request.Context.Digest {
		t.Fatalf("projection order changed identity: forward=(%q,%q), reverse=(%q,%q)", forward.Request.Source.Key, forward.Request.Context.Digest, reversed.Request.Source.Key, reversed.Request.Context.Digest)
	}
	const wantKey = "domain-event:a907bc62f12a6f2d4bb9bb1d3de0d38a8ea4944fd1ce9d025b558b07a24e110c"
	const wantDigest = "sha256:e9cf870be83f9f22b49c7308980007ccb9efc37a509630f8c3283ee67042ccef"
	if forward.Request.Source.Key != wantKey || forward.Request.Context.Digest != wantDigest {
		t.Fatalf("candidate golden = (%q,%q), want (%q,%q)", forward.Request.Source.Key, forward.Request.Context.Digest, wantKey, wantDigest)
	}
}

type eventAdmissionAuthority struct{}

func (a *eventAdmissionAuthority) VerifyAdmission(_ context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if request.Source.Kind != agentrun.SourceEvent || request.Principal.Mode != agentrun.ModeSponsored {
		return agentrun.AuthoritySnapshot{}, errors.New("unexpected event admission request")
	}
	return agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context,
		BudgetCeiling: agentrun.Budget{MaxCostMicros: 3000, MaxInputTokens: 4000, MaxOutputTokens: 1000},
		GrantRef:      "grant:current", PolicyDigest: digest("policy")}, nil
}

func validInput() EvaluateInput {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	audience := agentrun.AudienceScope{ID: "audience-a", SnapshotID: "audience-revision-7", Digest: digest("audience")}
	return EvaluateInput{
		Subscription: Subscription{
			ID: "subscription-4", TenantID: "tenant-a", Revision: 3, CurrentRevision: 3, State: "ACTIVE",
			Agent:          agentrun.VersionRef{AgentID: "agent-3", Version: "2", Digest: digest("agent-version")},
			InstallationID: "installation-5", InstallationRevision: 2, CurrentInstallRevision: 2,
			InstallationActive: true, GrantActive: true, Purpose: "domain-event-review", SponsorID: "sponsor-8",
			GrantRevision: 5, CurrentGrantRevision: 5,
			AgentPrincipalID: "principal-agent-3", Audience: audience, EventKinds: []string{"INTENT_COMPLETED"},
			FieldsByKind:          map[string][]string{"INTENT_COMPLETED": {"status", "private_note"}},
			MaximumClassification: "CONFIDENTIAL", Debounce: 5 * time.Second, MaxCauseDepth: 4,
			BudgetCeiling: agentrun.Budget{MaxCostMicros: 2000, MaxInputTokens: 4000, MaxOutputTokens: 1000},
		},
		Event: EventProjection{
			ID: "event-42", TenantID: "tenant-a", LegalEntity: "entity-a", Kind: "INTENT_COMPLETED", SchemaVersion: "v1",
			OccurredAt: at.Add(-time.Minute), CauseID: "cause-9", CauseDepth: 2,
			Audience: audience,
			Schema:   []FieldRule{{Name: "status", Classification: "PUBLIC"}, {Name: "private_note", Classification: "CONFIDENTIAL"}, {Name: "salary", Classification: "RESTRICTED"}},
			Fields: []ProjectedField{
				{Name: "status", Value: "approved", Classification: "PUBLIC", AudienceIDs: []string{"audience-a"}},
				{Name: "private_note", Value: "do not disclose", Classification: "CONFIDENTIAL", AudienceIDs: []string{"audience-other"}},
				{Name: "salary", Value: "100000", Classification: "RESTRICTED", AudienceIDs: []string{"audience-a"}},
			},
		},
		At: at, LastAdmittedAt: at.Add(-time.Minute), Deadline: at.Add(time.Minute),
		Budget: agentrun.Budget{MaxCostMicros: 1000, MaxInputTokens: 2000, MaxOutputTokens: 500},
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
