package agentegress

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

type dispatchLeaseConsumer struct {
	uses int
	err  error
	used map[string]bool
}

type dispatchSourceVerifier struct{ reject bool }

func (v dispatchSourceVerifier) VerifySourceClassification(_ context.Context, req SourceClassificationRequest) error {
	if v.reject || req.Tenant != "tenant-a" || req.Purpose != "agent.lookup" || req.FieldName != "model.message.0" || req.SourceClass != "hcm-context" || req.DataClass != trustdlp.ClassPublic || len(req.Provenance) == 0 {
		return ErrRefused
	}
	return nil
}

type dispatchIdentitylessAdapter struct{ calls int }

func (a *dispatchIdentitylessAdapter) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}, OutputModes: []agentmodel.OutputMode{agentmodel.OutputText}}
}

func (a *dispatchIdentitylessAdapter) Invoke(context.Context, agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	a.calls++
	return agentmodel.ModelResult{}, nil
}

type dispatchCustodyPort struct{ now time.Time }

func (p dispatchCustodyPort) IssueLease(_ custody.Context, handle custody.Handle, operation custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{ID: "custody-lease-1", Handle: handle, Operation: operation, ExpiresAt: p.now.Add(ttl)}, nil
}

func (c *dispatchLeaseConsumer) Use(presented lease.CredentialLease, _ string, _ custody.Operation) (lease.Evidence, error) {
	c.uses++
	if c.used == nil {
		c.used = make(map[string]bool)
	}
	if c.used[presented.ID] {
		return lease.Evidence{Outcome: "denied"}, lease.ErrAlreadyUsed
	}
	if c.err == nil {
		c.used[presented.ID] = true
	}
	return lease.Evidence{Outcome: "granted"}, c.err
}

type dispatchAdapter struct {
	calls  int
	err    error
	result agentmodel.ModelResult
}

func (a *dispatchAdapter) Identity() agentmodel.ModelIdentity {
	return agentmodel.ModelIdentity{ProviderID: "provider-a", ModelID: "model-a", Version: "v1"}
}

func (a *dispatchAdapter) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}, OutputModes: []agentmodel.OutputMode{agentmodel.OutputText}}
}

func (a *dispatchAdapter) Invoke(_ context.Context, _ agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	a.calls++
	return a.result, a.err
}

func providerDispatchFixture(t *testing.T) (*ProviderDispatcher, *dispatchLeaseConsumer, *dispatchAdapter, ProviderDispatchRequest) {
	t.Helper()
	terms := ProviderTerms{
		ModelProfile: "model.eu", ProviderID: "provider-a", ModelID: "model-a", ModelVersion: "v1",
		ContractRef: "contract:model-a:v1", EgressGrantRef: "grant:model-eu:v1", Approved: true, Encryption: true,
		AllowedRegions: []string{"eu-west"}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic},
		Retention: RetentionPolicy{Mode: RetentionNone}, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied,
		SourceRules: []ProviderSourceRule{{Class: "hcm-context", Classes: []trustdlp.DataClass{trustdlp.ClassPublic}}},
	}
	leases := &dispatchLeaseConsumer{}
	dispatcher, err := NewProviderDispatcher(egressEvaluator(t), leases, dispatchSourceVerifier{}, []ProviderTerms{terms})
	if err != nil {
		t.Fatal(err)
	}
	result := agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: "ready", Finish: agentmodel.FinishComplete, Usage: agentmodel.ModelUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5, CostMicros: 1}, Provider: agentmodel.ModelIdentity{ProviderID: "provider-a", ModelID: "model-a", Version: "v1"}}
	adapter := &dispatchAdapter{result: result}
	field := egressField("model.message.0", "Summarize the approved context.", trustdlp.ClassPublic)
	request := ProviderDispatchRequest{
		Model: agentmodel.ModelRequest{
			ContractVersion: agentmodel.ContractVersion, TaskProfile: "summarize", ModelProfile: "model.eu",
			Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: field.Value.(string)}},
			Output:   agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: egressNow.Add(time.Minute),
			Limits: agentmodel.ModelLimits{MaxInputTokens: 100, MaxOutputTokens: 100, MaxCostMicros: 1000}, TraceID: "trace-1",
			Processing: agentmodel.ProcessingPolicy{Residency: "eu-west", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied},
		},
		Outbound: OutboundRequest{
			TaskID: "task-1", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup",
			Profile: Profile{ID: "model.eu", Kind: TargetModel, AllowedRegions: []string{"eu-west"}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, Retention: RetentionPolicy{Mode: RetentionNone}},
			Region:  "eu-west", DeclaredFields: []string{"model.message.0"}, Fields: []Field{field},
			Task: TaskPolicy{AllowedRegions: []string{"eu-west"}, AllowedResultClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, ResultRetention: time.Hour}, Now: egressNow,
		},
		FieldSources: map[string]string{"model.message.0": "hcm-context"},
		Lease:        lease.CredentialLease{ID: "lease-1", Handle: custody.Handle{ID: "provider-key-ref", Kind: custody.Secret, Version: "v1", Tenant: "tenant-a", Region: "eu-west"}, Workload: "agent-worker", Tenant: "tenant-a", Purpose: "agent.lookup", Destination: "model.eu", Operation: custody.Decrypt},
	}
	return dispatcher, leases, adapter, request
}

func TestTodo_AGENT_020(t *testing.T) {
	dispatcher, leases, adapter, request := providerDispatchFixture(t)
	result, err := dispatcher.Dispatch(context.Background(), request, adapter)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if adapter.calls != 1 || leases.uses != 1 || result.Model.Text != "ready" || result.Egress.Decision != DecisionAllow || result.LeaseID != "lease-1" || result.ContractRef != "contract:model-a:v1" || result.EgressGrantRef != "grant:model-eu:v1" || len(result.SourceClasses) != 1 || result.SourceClasses[0] != "hcm-context" {
		t.Fatalf("dispatch result=%+v calls=%d lease uses=%d", result, adapter.calls, leases.uses)
	}
}

func TestTodo_AGENT_020_Security(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ProviderDispatchRequest)
	}{
		{name: "region", mutate: func(r *ProviderDispatchRequest) {
			r.Outbound.Region = "us-east"
			r.Model.Processing.Residency = "us-east"
		}},
		{name: "retention", mutate: func(r *ProviderDispatchRequest) { r.Model.Processing.Retention = "BOUNDED:3600000000" }},
		{name: "training-use-unspecified", mutate: func(r *ProviderDispatchRequest) { r.Model.Processing.TrainingUse = agentmodel.UseUnspecified }},
		{name: "unknown-source", mutate: func(r *ProviderDispatchRequest) { r.FieldSources["model.message.0"] = "unregistered" }},
		{name: "missing-source", mutate: func(r *ProviderDispatchRequest) { delete(r.FieldSources, "model.message.0") }},
		{name: "message-not-classified", mutate: func(r *ProviderDispatchRequest) { r.Outbound.DeclaredFields = nil }},
		{name: "wrong-destination-lease", mutate: func(r *ProviderDispatchRequest) { r.Lease.Destination = "model.us" }},
		{name: "egress-policy", mutate: func(r *ProviderDispatchRequest) {
			r.Outbound.Purpose = "agent.unreviewed"
			r.Lease.Purpose = "agent.unreviewed"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dispatcher, leases, adapter, request := providerDispatchFixture(t)
			tc.mutate(&request)
			if _, err := dispatcher.Dispatch(context.Background(), request, adapter); err == nil {
				t.Fatal("Dispatch succeeded for an ineligible request")
			}
			if adapter.calls != 0 || leases.uses != 0 {
				t.Fatalf("denial caused provider calls=%d lease consumption=%d", adapter.calls, leases.uses)
			}
		})
	}
	t.Run("adapter without identity", func(t *testing.T) {
		dispatcher, leases, _, request := providerDispatchFixture(t)
		adapter := &dispatchIdentitylessAdapter{}
		if _, err := dispatcher.Dispatch(context.Background(), request, adapter); err == nil {
			t.Fatal("Dispatch succeeded without a pre-call provider identity")
		}
		if adapter.calls != 0 || leases.uses != 0 {
			t.Fatalf("identity denial caused provider calls=%d lease consumption=%d", adapter.calls, leases.uses)
		}
	})
	t.Run("provider training is more permissive than required", func(t *testing.T) {
		dispatcher, leases, adapter, request := providerDispatchFixture(t)
		terms := cloneProviderTerms(dispatcher.terms[request.Model.ModelProfile])
		terms.TrainingUse = agentmodel.UseAllowed
		dispatcher, err := NewProviderDispatcher(dispatcher.evaluator, leases, dispatchSourceVerifier{}, []ProviderTerms{terms})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dispatcher.Dispatch(context.Background(), request, adapter); err == nil {
			t.Fatal("provider with training use enabled received a no-training request")
		}
		if adapter.calls != 0 || leases.uses != 0 {
			t.Fatalf("training denial caused provider calls=%d lease consumption=%d", adapter.calls, leases.uses)
		}
	})
	t.Run("owner source verifier denies", func(t *testing.T) {
		dispatcher, leases, adapter, request := providerDispatchFixture(t)
		terms := cloneProviderTerms(dispatcher.terms[request.Model.ModelProfile])
		dispatcher, err := NewProviderDispatcher(dispatcher.evaluator, leases, dispatchSourceVerifier{reject: true}, []ProviderTerms{terms})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dispatcher.Dispatch(context.Background(), request, adapter); err == nil {
			t.Fatal("Dispatch succeeded after the authoritative source verifier denied")
		}
		if adapter.calls != 0 || leases.uses != 0 {
			t.Fatalf("source denial caused provider calls=%d lease consumption=%d", adapter.calls, leases.uses)
		}
	})
}

func TestTodo_AGENT_020_Integration(t *testing.T) {
	dispatcher, _, adapter, request := providerDispatchFixture(t)
	now := egressNow
	manager, err := lease.NewManager(dispatchCustodyPort{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := manager.Mint(lease.Request{
		Handle: request.Lease.Handle, Workload: "agent-worker", Tenant: request.Outbound.Tenant,
		Purpose: request.Outbound.Purpose, Destination: request.Outbound.Profile.ID,
		Operation: custody.Decrypt, TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Lease = credential
	terms := cloneProviderTerms(dispatcher.terms[request.Model.ModelProfile])
	dispatcher, err = NewProviderDispatcher(dispatcher.evaluator, manager, dispatchSourceVerifier{}, []ProviderTerms{terms})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Dispatch(context.Background(), request, adapter); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Dispatch(context.Background(), request, adapter); !errors.Is(err, ErrRefused) {
		t.Fatalf("second use error=%v, want fail-closed lease denial", err)
	}
	evidence := manager.Events()
	if adapter.calls != 1 || len(evidence) != 3 || evidence[1].Kind != lease.EventUse || evidence[2].Outcome != "denied" {
		t.Fatalf("single-use integration calls=%d lease evidence=%+v", adapter.calls, evidence)
	}
}

func TestTodo_AGENT_020_Mutation(t *testing.T) {
	_, leases, adapter, request := providerDispatchFixture(t)
	terms := ProviderTerms{ModelProfile: "other", ProviderID: "provider-a", ModelID: "model-a", ModelVersion: "v1", ContractRef: "c", EgressGrantRef: "g", Approved: true, Encryption: true, AllowedRegions: []string{"eu-west"}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, Retention: RetentionPolicy{Mode: RetentionNone}, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied, SourceRules: []ProviderSourceRule{{Class: "hcm-context", Classes: []trustdlp.DataClass{trustdlp.ClassPublic}}}}
	terms.ModelProfile = "model.eu"
	dispatcher, err := NewProviderDispatcher(egressEvaluator(t), leases, dispatchSourceVerifier{}, []ProviderTerms{terms})
	if err != nil {
		t.Fatal(err)
	}
	terms.AllowedRegions[0] = "us-east"
	terms.SourceRules[0].Classes[0] = trustdlp.ClassPII
	if _, err := dispatcher.Dispatch(context.Background(), request, adapter); err != nil {
		t.Fatalf("mutating caller-owned construction data changed frozen dispatcher policy: %v", err)
	}
	if adapter.calls != 1 {
		t.Fatalf("provider call count=%d", adapter.calls)
	}
}

func TestTodo_AGENT_020_Golden(t *testing.T) {
	_, _, _, request := providerDispatchFixture(t)
	terms := ProviderTerms{ModelProfile: "model.eu", ProviderID: "provider-a", ModelID: "model-a", ModelVersion: "v1", ContractRef: "contract:model-a:v1", EgressGrantRef: "grant:model-eu:v1", Approved: true, Encryption: true, AllowedRegions: []string{"eu-west"}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, Retention: RetentionPolicy{Mode: RetentionNone}, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied, SourceRules: []ProviderSourceRule{{Class: "hcm-context", Classes: []trustdlp.DataClass{trustdlp.ClassPublic}}}}
	canonical := CanonicalProviderTerms(terms)
	want := "model.eu\x00provider-a\x00model-a\x00v1\x00contract:model-a:v1\x00grant:model-eu:v1\x00NONE\x000\x00denied\x00denied\x00eu-west\x00PUBLIC\x00hcm-context=PUBLIC\x00true\x00true\x00false\x00false"
	if canonical != want || canonical == request.Lease.ID || canonical == request.Model.Messages[0].Content {
		t.Fatalf("canonical terms = %q, want %q", canonical, want)
	}
	if !sameStrings([]string{"b", "a"}, []string{"a", "b"}) || sameClasses([]trustdlp.DataClass{trustdlp.ClassPublic}, []trustdlp.DataClass{trustdlp.ClassPII}) {
		t.Fatal("provider terms set comparison is not canonical")
	}
}

func TestTodo_AGENT_020_Fault(t *testing.T) {
	dispatcher, leases, adapter, request := providerDispatchFixture(t)
	adapter.err = errors.New("provider error included a credential-like raw value")
	adapter.result = agentmodel.ModelResult{
		ContractVersion: agentmodel.ContractVersion,
		Provider:        agentmodel.ModelIdentity{ProviderID: "provider-a", ModelID: "model-a", Version: "v1"},
		Failure:         &agentmodel.ModelFailure{Code: agentmodel.FailureUnavailable, Retryable: true, Message: "provider error included a credential-like raw value"},
	}
	result, err := dispatcher.Dispatch(context.Background(), request, adapter)
	var failure ProviderDispatchFailure
	if !errors.As(err, &failure) || failure.Code != agentmodel.FailureUnavailable || !failure.Retryable {
		t.Fatalf("failure=%v, want normalized unavailable failure", err)
	}
	if result.Model.Failure == nil || result.Model.Failure.Message != "" || strings.Contains(err.Error(), "credential-like") {
		t.Fatalf("provider error detail escaped sanitization: result=%+v err=%v", result, err)
	}
	if adapter.calls != 1 || leases.uses != 1 {
		t.Fatalf("provider fault path calls=%d lease uses=%d", adapter.calls, leases.uses)
	}
	t.Run("zero cost with token usage is never returned as free", func(t *testing.T) {
		dispatcher, leases, adapter, request := providerDispatchFixture(t)
		adapter.result.Usage.CostMicros = 0
		result, err := dispatcher.Dispatch(context.Background(), request, adapter)
		var refusal *Refusal
		if !errors.As(err, &refusal) || refusal.Code != RefusalProviderCost {
			t.Fatalf("zero-cost result error=%v, want reconciled-cost refusal", err)
		}
		if result.Model.ContractVersion != 0 || adapter.calls != 1 || leases.uses != 1 {
			t.Fatalf("zero-cost outcome leaked or counts are wrong: result=%+v calls=%d lease uses=%d", result, adapter.calls, leases.uses)
		}
	})
}
