package connectivity_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
)

const (
	testTenant       = "5e3f1c2b-0000-4000-8000-000000000001"
	goodCredential   = "secretref://harborcare/workday/client"
	testConnectionID = "conn-harborcare-workday"
)

var draftedAt = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

func evidenceAt(reason string, offset time.Duration) connectivity.TransitionEvidence {
	return connectivity.TransitionEvidence{
		Reason:      reason,
		ActorRef:    "user:ops@harborcare",
		EvidenceRef: "evd:" + reason,
		OccurredAt:  draftedAt.Add(offset),
	}
}

func mustCredential(t *testing.T, raw string) connectivity.CredentialRef {
	t.Helper()
	ref, err := connectivity.ParseCredentialRef(raw)
	if err != nil {
		t.Fatalf("parse credential %q: %v", raw, err)
	}
	return ref
}

func validSpec(t *testing.T) connectivity.ConnectionSpec {
	t.Helper()
	def := validDefinition()
	return connectivity.ConnectionSpec{
		ConnectionID:     testConnectionID,
		TenantID:         testTenant,
		OrgID:            "org-harborcare-us",
		SystemID:         "sys-workday-prod",
		Environment:      connectivity.EnvironmentProduction,
		Residency:        "us-east",
		ConnectorID:      def.ConnectorID,
		ConnectorVersion: def.Version,
		AuthMode:         connectivity.AuthOAuth2ClientCredentials,
		CredentialRef:    mustCredential(t, goodCredential),
		Scopes:           []string{"worker.read", "position.read", "compensation.read"},
		EndpointPolicy: connectivity.EndpointPolicy{
			AllowedHosts:  []string{"api.workday.example"},
			RequireTLS:    true,
			EgressProfile: "cell-egress/us-east",
		},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...),
		Bounds:       def.Bounds,
		CreatedAt:    draftedAt,
	}
}

func publishedDefinition(t *testing.T) connectivity.Publication {
	t.Helper()
	registry := connectivity.NewRegistry()
	pub, err := registry.Publish(validDefinition(), meta())
	if err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	return pub
}

func mustConnection(t *testing.T) *connectivity.ConnectorConnection {
	t.Helper()
	conn, err := connectivity.NewConnection(publishedDefinition(t), validSpec(t))
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}
	return conn
}

func TestConnectorConnection_OrgID_SystemID_AuthMode(t *testing.T) {
	conn := mustConnection(t)

	if got := conn.OrgID(); got != "org-harborcare-us" {
		t.Fatalf("OrgID() = %q, want org-harborcare-us", got)
	}
	if got := conn.SystemID(); got != "sys-workday-prod" {
		t.Fatalf("SystemID() = %q, want sys-workday-prod", got)
	}
	if got := conn.AuthMode(); got != connectivity.AuthOAuth2ClientCredentials {
		t.Fatalf("AuthMode() = %q, want %q", got, connectivity.AuthOAuth2ClientCredentials)
	}
}

func TestTodo_INTG_017_ConnectionCoverage(t *testing.T) {
	t.Run("constructor rejects invalid tenant scope and bounds", func(t *testing.T) {
		pub := publishedDefinition(t)
		cases := []struct {
			name   string
			mutate func(*connectivity.ConnectionSpec)
			want   error
		}{
			{name: "missing tenant", mutate: func(s *connectivity.ConnectionSpec) { s.TenantID = "" }, want: connectivity.ErrInvalid},
			{name: "missing organization", mutate: func(s *connectivity.ConnectionSpec) { s.OrgID = "" }, want: connectivity.ErrInvalid},
			{name: "missing external system", mutate: func(s *connectivity.ConnectionSpec) { s.SystemID = "" }, want: connectivity.ErrInvalid},
			{name: "unpublished auth mode", mutate: func(s *connectivity.ConnectionSpec) { s.AuthMode = connectivity.AuthAPIKey }, want: connectivity.ErrCapabilityBroadened},
			{name: "missing scopes", mutate: func(s *connectivity.ConnectionSpec) { s.Scopes = nil }, want: connectivity.ErrInvalid},
			{name: "missing creation time", mutate: func(s *connectivity.ConnectionSpec) { s.CreatedAt = time.Time{} }, want: connectivity.ErrInvalid},
			{name: "missing capabilities", mutate: func(s *connectivity.ConnectionSpec) { s.Capabilities = nil }, want: connectivity.ErrInvalid},
			{name: "empty allowed host", mutate: func(s *connectivity.ConnectionSpec) { s.EndpointPolicy.AllowedHosts = []string{""} }, want: connectivity.ErrInvalid},
			{name: "non-positive page size", mutate: func(s *connectivity.ConnectionSpec) { s.Bounds.MaxPageSize = 0 }, want: connectivity.ErrInvalid},
			{name: "non-positive pages per run", mutate: func(s *connectivity.ConnectionSpec) { s.Bounds.MaxPagesPerRun = 0 }, want: connectivity.ErrInvalid},
			{name: "non-positive records per run", mutate: func(s *connectivity.ConnectionSpec) { s.Bounds.MaxRecordsPerRun = 0 }, want: connectivity.ErrInvalid},
			{name: "non-positive record bytes", mutate: func(s *connectivity.ConnectionSpec) { s.Bounds.MaxRecordBytes = 0 }, want: connectivity.ErrInvalid},
			{name: "negative request interval", mutate: func(s *connectivity.ConnectionSpec) { s.Bounds.MinRequestInterval = -time.Nanosecond }, want: connectivity.ErrInvalid},
			{name: "less than one page per run", mutate: func(s *connectivity.ConnectionSpec) {
				s.Bounds.MaxRecordsPerRun = s.Bounds.MaxPageSize - 1
			}, want: connectivity.ErrInvalid},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				spec := validSpec(t)
				tc.mutate(&spec)
				conn, err := connectivity.NewConnection(pub, spec)
				if conn != nil {
					t.Fatalf("NewConnection returned a connection in state %s", conn.State())
				}
				if !errors.Is(err, tc.want) {
					t.Fatalf("NewConnection returned %v, want %v", err, tc.want)
				}
			})
		}
	})

	t.Run("metadata remains authoritative through valid lifecycle transitions", func(t *testing.T) {
		pub := publishedDefinition(t)
		spec := validSpec(t)
		conn, err := connectivity.NewConnection(pub, spec)
		if err != nil {
			t.Fatalf("NewConnection: %v", err)
		}
		wantCapabilities := append([]connectivity.Capability(nil), spec.Capabilities...)
		wantScopes := []string{"compensation.read", "position.read", "worker.read"}
		assertMetadata := func(stage string) {
			t.Helper()
			if conn.ID() != spec.ConnectionID || conn.TenantID() != spec.TenantID || conn.OrgID() != spec.OrgID || conn.SystemID() != spec.SystemID {
				t.Fatalf("%s: connection identity changed: id=%q tenant=%q org=%q system=%q", stage, conn.ID(), conn.TenantID(), conn.OrgID(), conn.SystemID())
			}
			if conn.ConnectorID() != pub.Definition.ConnectorID || conn.ConnectorVersion() != pub.Definition.Version {
				t.Fatalf("%s: connector authority changed: %s@%s", stage, conn.ConnectorID(), conn.ConnectorVersion())
			}
			if conn.Environment() != spec.Environment || conn.Residency() != spec.Residency || conn.AuthMode() != spec.AuthMode {
				t.Fatalf("%s: deployment or auth metadata changed: env=%s residency=%q auth=%s", stage, conn.Environment(), conn.Residency(), conn.AuthMode())
			}
			if conn.CredentialRef().String() != spec.CredentialRef.String() || strings.Join(conn.Scopes(), ",") != strings.Join(wantScopes, ",") {
				t.Fatalf("%s: credential or scopes changed: credential=%q scopes=%v", stage, conn.CredentialRef(), conn.Scopes())
			}
			policy := conn.EndpointPolicy()
			if len(policy.AllowedHosts) != len(spec.EndpointPolicy.AllowedHosts) || policy.AllowedHosts[0] != spec.EndpointPolicy.AllowedHosts[0] || policy.RequireTLS != spec.EndpointPolicy.RequireTLS || policy.EgressProfile != spec.EndpointPolicy.EgressProfile {
				t.Fatalf("%s: endpoint policy changed: %+v", stage, policy)
			}
			if conn.Bounds() != spec.Bounds {
				t.Fatalf("%s: bounds changed: %+v", stage, conn.Bounds())
			}
			gotCapabilities := conn.Capabilities()
			if len(gotCapabilities) != len(wantCapabilities) {
				t.Fatalf("%s: capabilities changed: %v", stage, gotCapabilities)
			}
			for i := range gotCapabilities {
				if gotCapabilities[i] != wantCapabilities[i] {
					t.Fatalf("%s: capability %d changed: %v", stage, i, gotCapabilities[i])
				}
			}
			if !conn.Supports(connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}) {
				t.Fatalf("%s: worker read capability disappeared", stage)
			}
			if conn.Supports(connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationWrite}) {
				t.Fatalf("%s: unsupported worker write capability appeared", stage)
			}
		}

		assertMetadata("DRAFT")
		steps := []struct {
			name string
			to   connectivity.LifecycleState
		}{
			{name: "validating", to: connectivity.StateValidating},
			{name: "ready", to: connectivity.StateReady},
			{name: "active", to: connectivity.StateActive},
			{name: "degraded", to: connectivity.StateDegraded},
			{name: "active again", to: connectivity.StateActive},
			{name: "suspended", to: connectivity.StateSuspended},
			{name: "ready after suspension", to: connectivity.StateReady},
			{name: "active after re-enable", to: connectivity.StateActive},
		}
		for i, step := range steps {
			t.Run(step.name, func(t *testing.T) {
				if err := conn.Transition(step.to, evidenceAt(step.name, time.Duration(i+1)*time.Minute)); err != nil {
					t.Fatalf("Transition(%s): %v", step.to, err)
				}
				assertMetadata(step.name)
			})
		}

		t.Run("unknown state is rejected without mutation", func(t *testing.T) {
			beforeVersion := conn.StateVersion()
			beforeHistory := len(conn.History())
			err := conn.Transition(connectivity.LifecycleState("UNKNOWN"), evidenceAt("bad-state", time.Hour))
			if !errors.Is(err, connectivity.ErrInvalid) {
				t.Fatalf("Transition(UNKNOWN) returned %v, want ErrInvalid", err)
			}
			if conn.State() != connectivity.StateActive || conn.StateVersion() != beforeVersion || len(conn.History()) != beforeHistory {
				t.Fatalf("rejected unknown state mutated lifecycle: state=%s version=%d history=%d", conn.State(), conn.StateVersion(), len(conn.History()))
			}
			assertMetadata("rejected unknown state")
		})
	})

	t.Run("zero credential reference remains empty", func(t *testing.T) {
		if got := (connectivity.CredentialRef{}).String(); got != "" {
			t.Fatalf("zero credential reference rendered as %q", got)
		}
		if _, err := connectivity.ParseCredentialRef("secretref://harborcare/workday/client!"); !errors.Is(err, connectivity.ErrCredential) {
			t.Fatalf("invalid credential path character returned %v, want ErrCredential", err)
		}
	})
}

// enable walks a fresh connection to ACTIVE through the legal path.
func enable(t *testing.T, conn *connectivity.ConnectorConnection) {
	t.Helper()
	steps := []struct {
		to     connectivity.LifecycleState
		reason string
	}{
		{connectivity.StateValidating, "diagnose"},
		{connectivity.StateReady, "diagnosis-passed"},
		{connectivity.StateActive, "enable"},
	}
	for i, step := range steps {
		if err := conn.Transition(step.to, evidenceAt(step.reason, time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatalf("transition to %s: %v", step.to, err)
		}
	}
}

// TestTodo_INTG_002 is the INTG-002 primary test.
//
// RED: a connection with a wrong tenant/org/environment/residency/destination,
// a broadened capability, a failed validation or a revoked lifecycle becomes
// active.
// GREEN: DRAFT -> VALIDATING -> READY -> ACTIVE/DEGRADED/SUSPENDED/
// QUARANTINED/REVOKED records the secret reference, scopes, endpoint policy and
// evidence on every transition.
func TestTodo_INTG_002(t *testing.T) {
	t.Parallel()

	t.Run("a connection is drafted, never born active", func(t *testing.T) {
		t.Parallel()
		conn := mustConnection(t)
		if got := conn.State(); got != connectivity.StateDraft {
			t.Fatalf("new connection is %s, want DRAFT", got)
		}
		if conn.Usable() {
			t.Fatal("a draft connection reports itself usable")
		}
		if err := conn.RequireUsable(); !errors.Is(err, connectivity.ErrPermission) {
			t.Fatalf("RequireUsable on a draft returned %v", err)
		}
	})

	t.Run("the legality table is exactly the implementation", func(t *testing.T) {
		t.Parallel()
		expected := map[connectivity.LifecycleState][]connectivity.LifecycleState{
			connectivity.StateDraft: {connectivity.StateRevoked, connectivity.StateValidating},
			connectivity.StateValidating: {
				connectivity.StateDraft, connectivity.StateQuarantined,
				connectivity.StateReady, connectivity.StateRevoked,
			},
			connectivity.StateReady: {
				connectivity.StateActive, connectivity.StateQuarantined, connectivity.StateRevoked,
				connectivity.StateSuspended, connectivity.StateValidating,
			},
			connectivity.StateActive: {
				connectivity.StateDegraded, connectivity.StateQuarantined,
				connectivity.StateRevoked, connectivity.StateSuspended,
			},
			connectivity.StateDegraded: {
				connectivity.StateActive, connectivity.StateQuarantined,
				connectivity.StateRevoked, connectivity.StateSuspended,
			},
			connectivity.StateSuspended: {
				connectivity.StateQuarantined, connectivity.StateReady, connectivity.StateRevoked,
			},
			connectivity.StateQuarantined: {connectivity.StateRevoked, connectivity.StateValidating},
			connectivity.StateRevoked:     nil,
		}
		for _, from := range connectivity.LifecycleStates() {
			got := connectivity.LegalTransitions(from)
			want := expected[from]
			if len(got) != len(want) {
				t.Fatalf("%s reaches %v, want %v", from, got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("%s reaches %v, want %v", from, got, want)
				}
			}
			for _, to := range connectivity.LifecycleStates() {
				legal := false
				for _, allowed := range want {
					if allowed == to {
						legal = true
					}
				}
				if connectivity.IsLegalTransition(from, to) != legal {
					t.Fatalf("IsLegalTransition(%s, %s) = %t, table says %t",
						from, to, !legal, legal)
				}
			}
			if connectivity.IsLegalTransition(from, from) {
				t.Fatalf("%s is legal to itself; a self-transition would record evidence for nothing", from)
			}
		}
	})

	t.Run("the enabling path records evidence at every step", func(t *testing.T) {
		t.Parallel()
		conn := mustConnection(t)
		enable(t, conn)

		if got := conn.State(); got != connectivity.StateActive {
			t.Fatalf("connection is %s, want ACTIVE", got)
		}
		if !conn.Usable() {
			t.Fatal("an active connection reports itself unusable")
		}
		if got := conn.StateVersion(); got != 4 {
			t.Fatalf("state version is %d after three transitions, want 4", got)
		}

		history := conn.History()
		if len(history) != 3 {
			t.Fatalf("history has %d transitions, want 3", len(history))
		}
		want := []struct{ from, to connectivity.LifecycleState }{
			{connectivity.StateDraft, connectivity.StateValidating},
			{connectivity.StateValidating, connectivity.StateReady},
			{connectivity.StateReady, connectivity.StateActive},
		}
		for i, step := range history {
			if step.Sequence != uint64(i+1) {
				t.Fatalf("transition %d has sequence %d", i, step.Sequence)
			}
			if step.From != want[i].from || step.To != want[i].to {
				t.Fatalf("transition %d is %s -> %s, want %s -> %s",
					i, step.From, step.To, want[i].from, want[i].to)
			}
			if err := step.Evidence.Validate(); err != nil {
				t.Fatalf("transition %d carries invalid evidence: %v", i, err)
			}
		}

		// The connection still names the secret reference, scopes and egress
		// bound it was drafted with.
		if conn.CredentialRef().String() != goodCredential {
			t.Fatalf("credential reference is %q", conn.CredentialRef())
		}
		if scopes := conn.Scopes(); len(scopes) != 3 || scopes[0] != "compensation.read" {
			t.Fatalf("scopes are %v; expected them sorted and preserved", scopes)
		}
		if policy := conn.EndpointPolicy(); policy.EgressProfile != "cell-egress/us-east" || !policy.RequireTLS {
			t.Fatalf("endpoint policy was not preserved: %+v", policy)
		}
	})

	t.Run("every terminal-adjacent state is reachable and revoked is terminal", func(t *testing.T) {
		t.Parallel()
		for _, target := range []connectivity.LifecycleState{
			connectivity.StateDegraded, connectivity.StateSuspended,
			connectivity.StateQuarantined, connectivity.StateRevoked,
		} {
			conn := mustConnection(t)
			enable(t, conn)
			if err := conn.Transition(target, evidenceAt("move", 10*time.Minute)); err != nil {
				t.Fatalf("ACTIVE -> %s: %v", target, err)
			}
			if conn.State() != target {
				t.Fatalf("state is %s after moving to %s", conn.State(), target)
			}
			if target != connectivity.StateDegraded && conn.Usable() {
				t.Fatalf("%s reports itself usable", target)
			}
		}

		conn := mustConnection(t)
		enable(t, conn)
		if err := conn.Transition(connectivity.StateRevoked, evidenceAt("revoke", time.Hour)); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		for _, to := range connectivity.LifecycleStates() {
			err := conn.Transition(to, evidenceAt("resurrect", 2*time.Hour))
			if !errors.Is(err, connectivity.ErrIllegalTransition) {
				t.Fatalf("REVOKED -> %s returned %v, want ErrIllegalTransition", to, err)
			}
		}
		if conn.State() != connectivity.StateRevoked {
			t.Fatalf("revoked connection moved to %s", conn.State())
		}
	})

	t.Run("a transition without evidence is refused", func(t *testing.T) {
		t.Parallel()
		cases := map[string]connectivity.TransitionEvidence{
			"no reason":   {ActorRef: "a", EvidenceRef: "e", OccurredAt: draftedAt},
			"no actor":    {Reason: "r", EvidenceRef: "e", OccurredAt: draftedAt},
			"no evidence": {Reason: "r", ActorRef: "a", OccurredAt: draftedAt},
			"no time":     {Reason: "r", ActorRef: "a", EvidenceRef: "e"},
		}
		for name, ev := range cases {
			conn := mustConnection(t)
			if err := conn.Transition(connectivity.StateValidating, ev); !errors.Is(err, connectivity.ErrInvalid) {
				t.Fatalf("%s: transition returned %v, want ErrInvalid", name, err)
			}
			if conn.State() != connectivity.StateDraft || len(conn.History()) != 0 {
				t.Fatalf("%s: refused transition still changed state to %s", name, conn.State())
			}
		}
	})

	t.Run("a connection cannot broaden its definition", func(t *testing.T) {
		t.Parallel()
		pub := publishedDefinition(t)

		broadened := validSpec(t)
		broadened.Capabilities = append(broadened.Capabilities, connectivity.Capability{
			Object: connectivity.ObjectWorker, Operation: connectivity.OperationWrite,
		})
		if _, err := connectivity.NewConnection(pub, broadened); !errors.Is(err, connectivity.ErrCapabilityBroadened) {
			t.Fatalf("a write-claiming connection was created: %v", err)
		}

		looser := validSpec(t)
		looser.Bounds.MaxPageSize = pub.Definition.Bounds.MaxPageSize + 1
		if _, err := connectivity.NewConnection(pub, looser); !errors.Is(err, connectivity.ErrCapabilityBroadened) {
			t.Fatalf("a connection with looser bounds was created: %v", err)
		}

		otherAuth := validSpec(t)
		otherAuth.AuthMode = connectivity.AuthAPIKey
		if _, err := connectivity.NewConnection(pub, otherAuth); !errors.Is(err, connectivity.ErrCapabilityBroadened) {
			t.Fatalf("a connection with an unpublished auth mode was created: %v", err)
		}
	})

	t.Run("missing tenancy, environment, residency or destination is refused", func(t *testing.T) {
		t.Parallel()
		pub := publishedDefinition(t)
		cases := map[string]func(*connectivity.ConnectionSpec){
			"no connection id": func(s *connectivity.ConnectionSpec) { s.ConnectionID = "" },
			"no tenant":        func(s *connectivity.ConnectionSpec) { s.TenantID = "" },
			"no organization":  func(s *connectivity.ConnectionSpec) { s.OrgID = "" },
			"no system":        func(s *connectivity.ConnectionSpec) { s.SystemID = "" },
			"no environment":   func(s *connectivity.ConnectionSpec) { s.Environment = "" },
			"bad environment":  func(s *connectivity.ConnectionSpec) { s.Environment = "STAGING" },
			"no residency":     func(s *connectivity.ConnectionSpec) { s.Residency = "" },
			"no scopes":        func(s *connectivity.ConnectionSpec) { s.Scopes = nil },
			"no destination": func(s *connectivity.ConnectionSpec) {
				s.EndpointPolicy.AllowedHosts = nil
			},
			"plaintext egress": func(s *connectivity.ConnectionSpec) {
				s.EndpointPolicy.RequireTLS = false
			},
			"no egress profile": func(s *connectivity.ConnectionSpec) {
				s.EndpointPolicy.EgressProfile = ""
			},
			"wrong connector": func(s *connectivity.ConnectionSpec) { s.ConnectorID = "other.connector" },
			"wrong version": func(s *connectivity.ConnectionSpec) {
				s.ConnectorVersion = connectivity.Version{Major: 9}
			},
			"no credential": func(s *connectivity.ConnectionSpec) {
				s.CredentialRef = connectivity.CredentialRef{}
			},
		}
		for name, mutate := range cases {
			spec := validSpec(t)
			mutate(&spec)
			conn, err := connectivity.NewConnection(pub, spec)
			if err == nil {
				t.Fatalf("%s: connection was created and is %s", name, conn.State())
			}
		}
	})
}

// TestTodo_INTG_002_Security proves credential material never enters a
// connection, and that a connection's own rendering cannot leak one.
func TestTodo_INTG_002_Security(t *testing.T) {
	t.Parallel()

	t.Run("inline credential material is refused", func(t *testing.T) {
		t.Parallel()
		rejected := []string{
			"",
			"hunter2",
			"https://api.workday.example/token",
			"secretref://user:hunter2@harborcare/workday",
			"secretref://harborcare/workday?token=abcdef",
			"secretref://harborcare/workday#Bearer",
			"secretref://harborcare/workday/client_secret=shhh",
			"vault://harborcare//workday",
			"secretref://harborcare/" + strings.Repeat("A", 65),
			"secretref://",
			"basic://harborcare/workday",
			"secretref://harborcare/workday/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.abc",
		}
		for _, raw := range rejected {
			if _, err := connectivity.ParseCredentialRef(raw); !errors.Is(err, connectivity.ErrCredential) {
				t.Fatalf("ParseCredentialRef(%q) returned %v, want ErrCredential", raw, err)
			}
		}
		accepted := []string{
			"secretref://harborcare/workday/client",
			"vault://kv/harborcare/workday-oauth",
			"kms://us-east/harborcare.workday",
		}
		for _, raw := range accepted {
			ref, err := connectivity.ParseCredentialRef(raw)
			if err != nil {
				t.Fatalf("ParseCredentialRef(%q): %v", raw, err)
			}
			if ref.String() != raw {
				t.Fatalf("reference round-tripped to %q, want %q", ref.String(), raw)
			}
		}
	})

	t.Run("a connection renders its reference, never a secret", func(t *testing.T) {
		t.Parallel()
		conn := mustConnection(t)
		enable(t, conn)
		rendered := conn.String()
		if !strings.Contains(rendered, goodCredential) {
			t.Fatalf("rendering %q does not name the credential reference", rendered)
		}
		for _, forbidden := range []string{"hunter2", "Bearer", "password", "client_secret"} {
			if strings.Contains(rendered, forbidden) {
				t.Fatalf("rendering %q leaks %q", rendered, forbidden)
			}
		}
	})

	t.Run("a revoked connection is never usable again", func(t *testing.T) {
		t.Parallel()
		conn := mustConnection(t)
		enable(t, conn)
		if err := conn.Transition(connectivity.StateRevoked, evidenceAt("credential-compromised", time.Hour)); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if conn.Usable() {
			t.Fatal("a revoked connection reports itself usable")
		}
		if err := conn.RequireUsable(); !errors.Is(err, connectivity.ErrPermission) {
			t.Fatalf("RequireUsable on a revoked connection returned %v", err)
		}
	})
}

// TestTodo_INTG_002_Fault proves a failed diagnosis cannot slip into ACTIVE and
// that recovery follows the table rather than shortcutting it.
func TestTodo_INTG_002_Fault(t *testing.T) {
	t.Parallel()

	conn := mustConnection(t)
	if err := conn.Transition(connectivity.StateValidating, evidenceAt("diagnose", time.Minute)); err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	// Diagnosis failed: the only ways out are back to DRAFT, to QUARANTINED or
	// to REVOKED. ACTIVE is not among them.
	if err := conn.Transition(connectivity.StateActive, evidenceAt("force-enable", 2*time.Minute)); !errors.Is(err, connectivity.ErrIllegalTransition) {
		t.Fatalf("VALIDATING -> ACTIVE returned %v, want ErrIllegalTransition", err)
	}
	if err := conn.Transition(connectivity.StateDraft, evidenceAt("diagnosis-failed", 3*time.Minute)); err != nil {
		t.Fatalf("VALIDATING -> DRAFT: %v", err)
	}
	if conn.Usable() {
		t.Fatal("a connection whose diagnosis failed reports itself usable")
	}

	// A suspended connection resumes through READY, not straight to ACTIVE.
	resumed := mustConnection(t)
	enable(t, resumed)
	if err := resumed.Transition(connectivity.StateSuspended, evidenceAt("pause", 10*time.Minute)); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if err := resumed.Transition(connectivity.StateActive, evidenceAt("resume", 11*time.Minute)); !errors.Is(err, connectivity.ErrIllegalTransition) {
		t.Fatalf("SUSPENDED -> ACTIVE returned %v, want ErrIllegalTransition", err)
	}
	if err := resumed.Transition(connectivity.StateReady, evidenceAt("resume", 12*time.Minute)); err != nil {
		t.Fatalf("SUSPENDED -> READY: %v", err)
	}
	if err := resumed.Transition(connectivity.StateActive, evidenceAt("re-enable", 13*time.Minute)); err != nil {
		t.Fatalf("READY -> ACTIVE: %v", err)
	}
	if got := len(resumed.History()); got != 6 {
		t.Fatalf("history has %d transitions after suspend and resume, want 6", got)
	}
}

// TestTodo_INTG_002_Integration drives a real connector through a connection,
// proving the lifecycle actually gates reads rather than merely recording a
// state nobody consults.
func TestTodo_INTG_002_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	conn := mustConnection(t)

	read := func() error {
		if err := conn.RequireUsable(); err != nil {
			return err
		}
		snapshot, err := incumbent.Snapshot(ctx, connectivity.ObjectWorker)
		if err != nil {
			return err
		}
		_, err = incumbent.Read(ctx, connectivity.ReadRequest{
			Object: connectivity.ObjectWorker,
			Mode:   connectivity.ReadFull,
			Cursor: connectivity.StartCursor(snapshot),
			Limit:  conn.Bounds().MaxPageSize,
		})
		return err
	}

	if err := read(); !errors.Is(err, connectivity.ErrPermission) {
		t.Fatalf("reading through a DRAFT connection returned %v", err)
	}
	if calls := len(incumbent.Calls()); calls != 0 {
		t.Fatalf("a refused read still made %d external calls", calls)
	}

	enable(t, conn)
	if err := read(); err != nil {
		t.Fatalf("reading through an ACTIVE connection: %v", err)
	}
	if calls := len(incumbent.Calls()); calls == 0 {
		t.Fatal("an allowed read made no external call")
	}
	if n := incumbent.MutatingCalls(); n != 0 {
		t.Fatalf("the connection performed %d mutating calls", n)
	}

	if err := conn.Transition(connectivity.StateSuspended, evidenceAt("pause", time.Hour)); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	before := len(incumbent.Calls())
	if err := read(); !errors.Is(err, connectivity.ErrPermission) {
		t.Fatalf("reading through a SUSPENDED connection returned %v", err)
	}
	if after := len(incumbent.Calls()); after != before {
		t.Fatalf("a refused read made %d further external calls", after-before)
	}
}

// FuzzTodo_INTG_002 fuzzes credential parsing and lifecycle transitions. No
// input may panic, produce a reference carrying inline material, or move a
// connection along an edge the legality table forbids.
func FuzzTodo_INTG_002(f *testing.F) {
	f.Add("secretref://harborcare/workday/client", uint8(0), uint8(3))
	f.Add("https://user:pass@example.com/token?x=1", uint8(7), uint8(7))
	f.Add("", uint8(255), uint8(255))
	f.Add("vault://a/b", uint8(1), uint8(2))

	states := connectivity.LifecycleStates()

	f.Fuzz(func(t *testing.T, raw string, fromIdx, toIdx uint8) {
		ref, err := connectivity.ParseCredentialRef(raw)
		if err != nil {
			if !errors.Is(err, connectivity.ErrCredential) {
				t.Fatalf("credential parse failed with a non-credential cause: %v", err)
			}
			if !ref.IsZero() {
				t.Fatal("a rejected credential produced a non-zero reference")
			}
		} else {
			rendered := ref.String()
			if rendered != raw {
				t.Fatalf("accepted reference rendered as %q, input was %q", rendered, raw)
			}
			if strings.ContainsAny(rendered[strings.Index(rendered, "://")+3:], "@?#=") {
				t.Fatalf("accepted reference %q carries inline material", rendered)
			}
		}

		from := states[int(fromIdx)%len(states)]
		to := states[int(toIdx)%len(states)]
		legal := connectivity.IsLegalTransition(from, to)
		if from == to && legal {
			t.Fatalf("%s -> %s reported legal", from, to)
		}
		if from == connectivity.StateRevoked && legal {
			t.Fatalf("REVOKED -> %s reported legal", to)
		}
		reachable := connectivity.LegalTransitions(from)
		found := false
		for _, r := range reachable {
			if r == to {
				found = true
			}
		}
		if found != legal {
			t.Fatalf("LegalTransitions(%s) and IsLegalTransition disagree about %s", from, to)
		}
	})
}
