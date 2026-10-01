package agentmodel

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type routeRecorder struct {
	records []RouteRecord
	err     error
}

func (r *routeRecorder) RecordRoute(_ context.Context, record RouteRecord) error {
	if r.err != nil {
		return r.err
	}
	copy := record
	copy.Eligibility = append([]Eligibility(nil), record.Eligibility...)
	copy.Attempted = append([]ModelSelection(nil), record.Attempted...)
	r.records = append(r.records, copy)
	return nil
}

func routeFixture(t *testing.T) (*Router, RouteRequest, *routeRecorder) {
	t.Helper()
	primary := ModelProfile{
		ID: "hosted-standard-v3", Identity: ModelIdentity{ProviderID: "provider-a", ModelID: "model-x", Version: "2026-09"},
		Regions: []string{"us-east"}, DataClasses: []string{"INTERNAL", "CONFIDENTIAL"}, TaskProfileIDs: []string{"answer-v1"},
		MaxLatency: 2 * time.Second, MaxCostMicros: 900, ExpectedCostMicros: 700,
		SemanticsDigest: "semantics-1", OutputSchemaDigest: "output-1", ToolSchemaDigest: "tools-1",
		Evaluation: ModelEvaluation{AgentVersionDigest: "agent-v4", SuiteDigest: "eval-44", Passed: true},
	}
	fallback := ModelProfile{
		ID: "private-standard-v2", Identity: ModelIdentity{ProviderID: "provider-b", ModelID: "model-y", Version: "2.1"},
		Regions: []string{"us-east"}, DataClasses: []string{"INTERNAL", "CONFIDENTIAL"}, TaskProfileIDs: []string{"answer-v1"},
		MaxLatency: 1500 * time.Millisecond, MaxCostMicros: 900, ExpectedCostMicros: 800,
		SemanticsDigest: "semantics-1", OutputSchemaDigest: "output-1", ToolSchemaDigest: "tools-1",
		Evaluation: ModelEvaluation{AgentVersionDigest: "agent-v4", SuiteDigest: "eval-45", Passed: true},
	}
	primary.ProfileDigest = ModelProfileDigest(primary)
	fallback.ProfileDigest = ModelProfileDigest(fallback)
	primarySelection := ModelSelection{ProfileID: primary.ID, ProfileDigest: primary.ProfileDigest, Identity: primary.Identity}
	fallbackSelection := ModelSelection{ProfileID: fallback.ID, ProfileDigest: fallback.ProfileDigest, Identity: fallback.Identity}
	pin := ModelPin{
		AgentVersionDigest: "agent-v4", TaskProfileID: "answer-v1", Primary: primarySelection,
		Fallbacks: []ModelSelection{fallbackSelection}, SemanticsDigest: "semantics-1",
		OutputSchemaDigest: "output-1", ToolSchemaDigest: "tools-1",
	}
	task := TaskProfile{
		ID: "answer-v1", AgentVersionDigest: "agent-v4", Region: "us-east", DataClasses: []string{"INTERNAL"},
		MaxLatency: 2 * time.Second, MaxCostMicros: 1000, SemanticsDigest: "semantics-1",
		OutputSchemaDigest: "output-1", ToolSchemaDigest: "tools-1",
	}
	recorder := &routeRecorder{}
	router, err := NewRouter([]ModelProfile{primary, fallback}, recorder)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router, RouteRequest{TraceID: "trace-1", Pin: pin, Task: task, BudgetRemainingMicros: 1000}, recorder
}

func TestTodo_AGENT_024(t *testing.T) {
	router, request, recorder := routeFixture(t)
	got, err := router.Route(context.Background(), request)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if got.Selected != request.Pin.Primary || got.Fallback || got.Digest == "" {
		t.Fatalf("route = %+v, want pinned primary and digest", got)
	}
	if len(got.Eligibility) != 2 || !got.Eligibility[0].Eligible || !got.Eligibility[1].Eligible || got.Eligibility[0].ProfileDigest == "" || got.Eligibility[0].EvaluationSuiteDigest != "eval-44" {
		t.Fatalf("eligibility = %+v, want both evaluated pinned versions", got.Eligibility)
	}
	if len(recorder.records) != 1 || recorder.records[0].Selected.Identity.Version != "2026-09" {
		t.Fatalf("recorded routes = %+v, want exact provider model version", recorder.records)
	}
}

func TestTodo_AGENT_024_Property(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ModelProfile, *RouteRequest)
	}{
		{name: "region", mutate: func(profile *ModelProfile, _ *RouteRequest) { profile.Regions = []string{"eu-west"} }},
		{name: "data class", mutate: func(profile *ModelProfile, req *RouteRequest) { req.Task.DataClasses = []string{"RESTRICTED"} }},
		{name: "latency", mutate: func(profile *ModelProfile, _ *RouteRequest) { profile.MaxLatency = 3 * time.Second }},
		{name: "task cost", mutate: func(profile *ModelProfile, _ *RouteRequest) { profile.MaxCostMicros = 1100 }},
		{name: "budget", mutate: func(_ *ModelProfile, req *RouteRequest) { req.BudgetRemainingMicros = 699 }},
		{name: "evaluation", mutate: func(profile *ModelProfile, _ *RouteRequest) { profile.Evaluation.Passed = false }},
		{name: "semantics", mutate: func(profile *ModelProfile, _ *RouteRequest) { profile.ToolSchemaDigest = "tools-2" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, request, recorder := routeFixture(t)
			profile := router.profiles[request.Pin.Primary]
			oldSelection := request.Pin.Primary
			tc.mutate(&profile, &request)
			request.Pin.Primary = repinProfile(router, oldSelection, profile)
			_, err := router.Route(context.Background(), request)
			if !errors.Is(err, ErrNoEligibleModel) {
				t.Fatalf("Route() error = %v, want ErrNoEligibleModel", err)
			}
			if len(recorder.records) != 1 || recorder.records[0].Selected != (ModelSelection{}) || recorder.records[0].Denial == "" {
				t.Fatalf("denial record = %+v, want persisted fail-closed decision", recorder.records)
			}
		})
	}
}

func TestTodo_AGENT_024_Fault(t *testing.T) {
	for _, effect := range []EffectState{EffectCommitted, EffectUncertain} {
		t.Run(fmt.Sprintf("effect-%d", effect), func(t *testing.T) {
			router, request, recorder := routeFixture(t)
			request.Attempted = []ModelSelection{request.Pin.Primary}
			request.SameProviderRetriesExhausted = true
			request.PreviousFailure = ModelFailure{Code: FailureUnavailable, Retryable: true}
			request.Effect = effect
			_, err := router.Route(context.Background(), request)
			if !errors.Is(err, ErrFallbackDenied) {
				t.Fatalf("effect fallback error = %v, want ErrFallbackDenied", err)
			}
			if len(recorder.records) != 1 || recorder.records[0].Selected != (ModelSelection{}) || recorder.records[0].Effect != effect {
				t.Fatalf("effect decision = %+v, want recorded refusal", recorder.records)
			}
		})
	}

	router, request, recorder := routeFixture(t)
	request.Attempted = []ModelSelection{request.Pin.Primary}
	request.SameProviderRetriesExhausted = true
	request.PreviousFailure = ModelFailure{Code: FailureTimeout, Retryable: true}
	got, err := router.Route(context.Background(), request)
	if err != nil || got.Selected != request.Pin.Fallbacks[0] || !got.Fallback {
		t.Fatalf("eligible fallback = %+v, %v; want pinned evaluated fallback", got, err)
	}

	router, request, _ = routeFixture(t)
	request.Attempted = []ModelSelection{request.Pin.Primary}
	request.PreviousFailure = ModelFailure{Code: FailureUnavailable, Retryable: true}
	if _, err := router.Route(context.Background(), request); !errors.Is(err, ErrFallbackDenied) {
		t.Fatalf("fallback before retry exhaustion = %v, want ErrFallbackDenied", err)
	}

	router, request, _ = routeFixture(t)
	request.Attempted = []ModelSelection{request.Pin.Primary}
	request.SameProviderRetriesExhausted = true
	request.PreviousFailure = ModelFailure{Code: FailureInvalid, Retryable: true}
	if _, err := router.Route(context.Background(), request); !errors.Is(err, ErrFallbackDenied) {
		t.Fatalf("non-provider failure fallback error = %v, want ErrFallbackDenied", err)
	}

	router, request, recorder = routeFixture(t)
	recorder.err = errors.New("audit unavailable")
	_, err = router.Route(context.Background(), request)
	if !errors.Is(err, ErrRouteRecord) {
		t.Fatalf("record failure = %v, want ErrRouteRecord before dispatch", err)
	}
}

func TestTodo_AGENT_024_Security(t *testing.T) {
	router, request, recorder := routeFixture(t)
	profile := router.profiles[request.Pin.Fallbacks[0]]
	profile.ExpectedCostMicros++
	repinProfile(router, request.Pin.Fallbacks[0], profile)
	request.Attempted = []ModelSelection{request.Pin.Primary}
	request.SameProviderRetriesExhausted = true
	request.PreviousFailure = ModelFailure{Code: FailureUnavailable, Retryable: true}
	if _, err := router.Route(context.Background(), request); !errors.Is(err, ErrNoEligibleModel) {
		t.Fatalf("changed pinned profile error = %v, want ErrNoEligibleModel", err)
	}
	if recorder.records[0].Eligibility[1].Reason != "model version is not in the approved catalog" {
		t.Fatalf("stale profile pin reason = %q", recorder.records[0].Eligibility[1].Reason)
	}

	router, request, recorder = routeFixture(t)
	profile = router.profiles[request.Pin.Fallbacks[0]]
	profile.Regions = []string{"eu-west"}
	oldSelection := request.Pin.Fallbacks[0]
	request.Pin.Fallbacks[0] = repinProfile(router, oldSelection, profile)
	request.Attempted = []ModelSelection{request.Pin.Primary}
	request.SameProviderRetriesExhausted = true
	request.PreviousFailure = ModelFailure{Code: FailureUnavailable, Retryable: true}
	_, err := router.Route(context.Background(), request)
	if !errors.Is(err, ErrNoEligibleModel) {
		t.Fatalf("cross-region fallback error = %v, want ErrNoEligibleModel", err)
	}
	if len(recorder.records) != 1 || recorder.records[0].Eligibility[1].Eligible || recorder.records[0].Eligibility[1].Reason != "processing region is not eligible" {
		t.Fatalf("fallback eligibility record = %+v, want cross-region denial", recorder.records)
	}

	profile = router.profiles[request.Pin.Primary]
	profile.Evaluation.AgentVersionDigest = "other-agent-version"
	oldSelection = request.Pin.Primary
	request.Pin.Primary = repinProfile(router, oldSelection, profile)
	request.Attempted = nil
	request.SameProviderRetriesExhausted = false
	_, err = router.Route(context.Background(), request)
	if !errors.Is(err, ErrNoEligibleModel) {
		t.Fatalf("wrong-agent evaluation error = %v, want ErrNoEligibleModel", err)
	}
}

func repinProfile(router *Router, old ModelSelection, profile ModelProfile) ModelSelection {
	profile.ProfileDigest = ModelProfileDigest(profile)
	selection := ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	delete(router.profiles, old)
	router.profiles[selection] = profile
	return selection
}

func TestTodo_AGENT_024_Golden(t *testing.T) {
	router, request, recorder := routeFixture(t)
	first, err := router.Route(context.Background(), request)
	if err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	second, err := router.Route(context.Background(), request)
	if err != nil {
		t.Fatalf("second Route() error = %v", err)
	}
	const wantDigest = "67ecb93268510dca5054d4a34ed5506ffe2e21bdbe881c11cdef7b7d7c2669c8"
	if first.Digest != second.Digest || first.Digest != routeDigest(first) || first.Digest != wantDigest {
		t.Fatalf("route digests = %q, %q, want stable canonical digest %q", first.Digest, second.Digest, wantDigest)
	}
	if first.Selected.Identity != (ModelIdentity{ProviderID: "provider-a", ModelID: "model-x", Version: "2026-09"}) {
		t.Fatalf("selected identity = %+v, want exact provider/model/version", first.Selected.Identity)
	}
	profile := router.profiles[request.Pin.Primary]
	profile.Regions = []string{"us-west", "us-east"}
	reordered := profile
	reordered.Regions = []string{"us-east", "us-west"}
	profileDigest := ModelProfileDigest(profile)
	if profileDigest != ModelProfileDigest(reordered) {
		t.Fatalf("profile digest changed with set ordering, want canonical digest")
	}
	profile.ProfileDigest = profileDigest
	profile.MaxLatency++
	if ModelProfileDigest(profile) == profileDigest {
		t.Fatalf("profile digest did not change with policy content")
	}
	profile.ProfileDigest = ""
	if _, err := NewRouter([]ModelProfile{profile}, recorder); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("router accepted profile without pinned digest: %v", err)
	}
}
