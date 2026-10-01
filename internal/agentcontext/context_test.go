package agentcontext

import (
	"context"
	"errors"
	"testing"
	"time"
)

type resolverFunc func(context.Context, ResolveRequest) (Snapshot, error)

func (f resolverFunc) ResolveAgentContext(ctx context.Context, request ResolveRequest) (Snapshot, error) {
	return f(ctx, request)
}

type recheckerFunc func(context.Context, RecheckRequest) (RecheckDecision, error)

func (f recheckerFunc) RecheckAgentContext(ctx context.Context, request RecheckRequest) (RecheckDecision, error) {
	return f(ctx, request)
}

func TestTodo_AGENT_017(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	input := validSnapshot(now)
	var lateCalls int
	builder, err := NewBuilder(resolverFunc(func(context.Context, ResolveRequest) (Snapshot, error) { return input, nil }), recheckerFunc(func(_ context.Context, request RecheckRequest) (RecheckDecision, error) {
		lateCalls++
		return RecheckDecision{Allowed: true, Audience: request.Audience, AuthZVersion: request.AuthZVersion, LegalPolicyVersion: request.LegalPolicyVersion, Revocations: request.Revocations, Sources: request.Sources}, nil
	}), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	effective, err := builder.Build(context.Background(), ResolveRequest{InvocationID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	input.SourceGrants[0] = "mutated"
	if effective.Snapshot.SourceGrants[0] != "source:read" {
		t.Fatalf("owner snapshot mutation leaked into context: %#v", effective.Snapshot.SourceGrants)
	}
	if err := builder.Recheck(context.Background(), effective); err != nil {
		t.Fatalf("valid context recheck: %v", err)
	}
	if lateCalls != 1 {
		t.Fatalf("late recheck calls=%d, want 1", lateCalls)
	}
}

func TestTodo_AGENT_017_Security(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	builder, err := NewBuilder(resolverFunc(func(context.Context, ResolveRequest) (Snapshot, error) { return validSnapshot(now), nil }), recheckerFunc(func(context.Context, RecheckRequest) (RecheckDecision, error) {
		t.Fatal("late recheck must not run when chat asserts authority")
		return RecheckDecision{}, nil
	}), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range []map[string]string{{"tenant_id": "tenant-attacker"}, {"Role": "admin"}, {"roles": "owner"}, {"tenantId": ""}, {"user-role": ""}} {
		if _, err := builder.Build(context.Background(), ResolveRequest{InvocationID: "inv-1", ChatClaims: claim}); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("claim %#v err=%v, want invalid request", claim, err)
		}
	}
	validBuilder, err := NewBuilder(resolverFunc(func(context.Context, ResolveRequest) (Snapshot, error) { return validSnapshot(now), nil }), recheckerFunc(func(context.Context, RecheckRequest) (RecheckDecision, error) {
		t.Fatal("modified context must be refused before owner recheck")
		return RecheckDecision{}, nil
	}), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	effective, err := validBuilder.Build(context.Background(), ResolveRequest{InvocationID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	effective.Snapshot.OutputAudience = "tenant:everyone"
	if err := validBuilder.Recheck(context.Background(), effective); !errors.Is(err, ErrRecheckDenied) {
		t.Fatalf("mutated output audience err=%v, want ErrRecheckDenied", err)
	}
}

func TestTodo_AGENT_017_Fault(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	stale := validSnapshot(now)
	stale.Sources[0].FreshUntil = now
	builder, err := NewBuilder(resolverFunc(func(context.Context, ResolveRequest) (Snapshot, error) { return stale, nil }), recheckerFunc(func(context.Context, RecheckRequest) (RecheckDecision, error) { return RecheckDecision{}, nil }), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(context.Background(), ResolveRequest{InvocationID: "inv-1"}); !errors.Is(err, ErrStaleSource) {
		t.Fatalf("stale source err=%v, want ErrStaleSource", err)
	}

	changedPolicy := validSnapshot(now)
	builder, err = NewBuilder(resolverFunc(func(context.Context, ResolveRequest) (Snapshot, error) { return changedPolicy, nil }), recheckerFunc(func(context.Context, RecheckRequest) (RecheckDecision, error) {
		return RecheckDecision{Allowed: true, Audience: "user:user-1", AuthZVersion: "authz-v9", LegalPolicyVersion: "legal-v2", Revocations: []string{"grant:17"}, Sources: sourcePins(changedPolicy)}, nil
	}), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	effective, err := builder.Build(context.Background(), ResolveRequest{InvocationID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Recheck(context.Background(), effective); !errors.Is(err, ErrRecheckDenied) {
		t.Fatalf("changed owner policy err=%v, want ErrRecheckDenied", err)
	}
}

func TestTodo_AGENT_017_Golden(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	snapshot := validSnapshot(now)
	builder, err := NewBuilder(resolverFunc(func(context.Context, ResolveRequest) (Snapshot, error) { return snapshot, nil }), recheckerFunc(func(context.Context, RecheckRequest) (RecheckDecision, error) { return RecheckDecision{}, nil }), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	got, err := builder.Build(context.Background(), ResolveRequest{InvocationID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != "sha256:822f7ed0ffb1d9e30c70f1d325ce4ca24f7300e54b072455f8f0fbeba369a5dd" {
		t.Fatalf("context digest=%s", got.Digest)
	}
	if got.Snapshot.Facts[0].Source.Kind != CanonicalHCMFact || got.Snapshot.Facts[1].Source.Kind != UserClaim || got.Snapshot.Facts[2].Source.Kind != ModelInference {
		t.Fatalf("fact classes collapsed: %#v", got.Snapshot.Facts)
	}
}

func TestTodo_AGENT_017_RejectsBlankAuthorizationEvidence(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{name: "blank source grant", mutate: func(s *Snapshot) { s.SourceGrants = []string{" "} }, want: ErrInvalidRequest},
		{name: "blank capability grant", mutate: func(s *Snapshot) { s.CapabilityGrants = []string{"capability:read", ""} }, want: ErrInvalidRequest},
		{name: "blank revocation token", mutate: func(s *Snapshot) { s.RevocationTokens = []string{"grant:17", "\t"} }, want: ErrInvalidRequest},
		{name: "blank source audience", mutate: func(s *Snapshot) { s.Sources[0].Audience = []string{"user:user-1", " "} }, want: ErrStaleSource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := validSnapshot(now)
			tc.mutate(&snapshot)
			builder, err := NewBuilder(resolverFunc(func(context.Context, ResolveRequest) (Snapshot, error) { return snapshot, nil }), recheckerFunc(func(context.Context, RecheckRequest) (RecheckDecision, error) { return RecheckDecision{}, nil }), func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			_, err = builder.Build(context.Background(), ResolveRequest{InvocationID: "inv-1"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Build error = %v, want %v", err, tc.want)
			}
		})
	}
}

func validSnapshot(now time.Time) Snapshot {
	return Snapshot{
		TenantID: "tenant-a", OrganizationID: "org-1", AgentID: "agent-1", AgentVersion: "v3", InstallationID: "install-1",
		TriggerID: "trigger-1", InvokerID: "user-1", Purpose: "people.lookup", SubjectID: "person-1", ResourceID: "profile-1",
		ConversationID: "conversation-1", OutputAudience: "user:user-1", TemporalMode: "CURRENT", Principals: []Principal{{ID: "user-1", Kind: "HUMAN"}, {ID: "agent-1", Kind: "AGENT"}},
		SourceGrants: []string{"source:read"}, CapabilityGrants: []string{"people.lookup"}, AuthZVersion: "authz-v8", LegalPolicyVersion: "legal-v2",
		Classification: "CONFIDENTIAL", TimeZone: "America/New_York", Locale: "en-US", Autonomy: "READ_ONLY", Risk: "LOW", BudgetID: "budget-1", CorrelationID: "corr-1",
		Sources:          []Source{{Owner: "people", ID: "profile-1", Version: "7", Kind: CanonicalHCMFact, Classification: "CONFIDENTIAL", Audience: []string{"user:user-1"}, ObservedAt: now.Add(-time.Minute), FreshUntil: now.Add(time.Minute), RevocationToken: "people:epoch:12"}},
		Facts:            []Fact{{Key: "department", Value: "Engineering", Source: factSource("people", "profile-1", "7", CanonicalHCMFact, now)}, {Key: "claim", Value: "prefers concise answers", Source: factSource("chat", "post-1", "1", UserClaim, now)}, {Key: "inference", Value: "may want team data", Source: factSource("agentmodel", "response-1", "1", ModelInference, now)}},
		RevocationTokens: []string{"grant:17"},
	}
}

func factSource(owner, id, version string, kind FactKind, now time.Time) Source {
	return Source{Owner: owner, ID: id, Version: version, Kind: kind, Classification: "CONFIDENTIAL", Audience: []string{"user:user-1"}, ObservedAt: now.Add(-time.Minute), FreshUntil: now.Add(time.Minute), RevocationToken: owner + ":epoch:1"}
}
