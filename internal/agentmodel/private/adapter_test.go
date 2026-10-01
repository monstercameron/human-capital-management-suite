package private

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

type testRegistry struct {
	registration Registration
	err          error
	calls        atomic.Int32
}

func (r *testRegistry) ResolvePrivateEndpoint(_ context.Context, id string) (Registration, error) {
	r.calls.Add(1)
	if id != r.registration.ID {
		return Registration{}, errors.New("unknown endpoint")
	}
	return cloneRegistration(r.registration), r.err
}

type testTransport struct {
	client *http.Client
	err    error
	calls  atomic.Int32
}

func (v *testTransport) ClientForPrivateEndpoint(_ context.Context, reg Registration) (*http.Client, error) {
	v.calls.Add(1)
	if reg.ServiceIdentity == "" || reg.NetworkPathRef == "" {
		return nil, ErrEndpointTrust
	}
	if v.err != nil {
		return nil, v.err
	}
	return v.client, nil
}

func TestTodo_AGENT_023(t *testing.T) {
	var calls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			var probe struct {
				EndpointID      string `json:"endpoint_id"`
				RegistrationPin string `json:"registration_pin"`
			}
			if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
				t.Errorf("decode health request: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "endpoint_id": probe.EndpointID, "registration_pin": probe.RegistrationPin})
		case "/infer":
			var req agentmodel.ModelRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode inference request: %v", err)
			}
			json.NewEncoder(w).Encode(agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: "approved", Usage: agentmodel.ModelUsage{}, Finish: agentmodel.FinishComplete, Provider: testRegistration(server.URL).Identity})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	reg := testRegistration(server.URL)
	registry, verifier := &testRegistry{registration: reg}, &testTransport{client: server.Client()}
	adapter := newTestAdapter(t, reg, registry, verifier)
	result, err := adapter.Invoke(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Text != "approved" || adapter.Identity() != reg.Identity {
		t.Fatalf("result/identity = %+v / %+v", result, adapter.Identity())
	}
	if calls.Load() != 2 || registry.calls.Load() != 2 || verifier.calls.Load() != 1 {
		t.Fatalf("endpoint/registry/verifier calls = %d/%d/%d", calls.Load(), registry.calls.Load(), verifier.calls.Load())
	}
}

func TestTodo_AGENT_023_Security(t *testing.T) {
	for _, name := range []string{"registration drift", "service identity rejected", "processing policy mismatch"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				var probe struct {
					EndpointID      string `json:"endpoint_id"`
					RegistrationPin string `json:"registration_pin"`
				}
				if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
					t.Errorf("decode health: %v", err)
				}
				json.NewEncoder(w).Encode(map[string]string{"status": "ok", "endpoint_id": probe.EndpointID, "registration_pin": probe.RegistrationPin})
			}))
			defer server.Close()
			reg := testRegistration(server.URL)
			registry, verifier := &testRegistry{registration: reg}, &testTransport{client: server.Client()}
			adapter := newTestAdapter(t, reg, registry, verifier)
			req := testRequest()
			switch name {
			case "registration drift":
				registry.registration.Region = "eu-west"
			case "service identity rejected":
				verifier.err = ErrEndpointTrust
			case "processing policy mismatch":
				req.Processing.Residency = "eu"
			}
			_, err := adapter.Invoke(context.Background(), req)
			switch name {
			case "registration drift":
				if !errors.Is(err, ErrEndpointDrift) {
					t.Fatalf("Invoke error=%v", err)
				}
			case "service identity rejected":
				if !errors.Is(err, ErrEndpointTrust) {
					t.Fatalf("Invoke error=%v", err)
				}
			case "processing policy mismatch":
				if err == nil || calls.Load() != 0 {
					t.Fatalf("Invoke error/calls=%v/%d", err, calls.Load())
				}
			}
		})
	}
}

func TestTodo_AGENT_023_Conformance(t *testing.T) {
	reg := testRegistration("http://127.0.0.1:1234")
	registry := &testRegistry{registration: reg}
	verifier := &testTransport{client: http.DefaultClient}
	if _, err := New(context.Background(), Config{EndpointID: reg.ID, Pin: "caller-picked", Registry: registry, Transport: verifier}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("New arbitrary pin error=%v", err)
	}
	if _, err := New(context.Background(), Config{EndpointID: reg.ID, Pin: RegistrationPin(reg), Registry: registry, Transport: verifier}); err != nil {
		t.Fatalf("New loopback test endpoint: %v", err)
	}
	if _, err := New(context.Background(), Config{EndpointID: "caller-selected", Pin: RegistrationPin(reg), Registry: registry, Transport: verifier}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("New caller-selected endpoint error=%v", err)
	}
	reg.EndpointURL = "http://169.254.169.254/latest"
	registry.registration = reg
	if _, err := New(context.Background(), Config{EndpointID: reg.ID, Pin: RegistrationPin(reg), Registry: registry, Transport: verifier}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("New metadata endpoint error=%v", err)
	}
	reg.EndpointURL = "https://user:pass@model.example"
	registry.registration = reg
	if _, err := New(context.Background(), Config{EndpointID: reg.ID, Pin: RegistrationPin(reg), Registry: registry, Transport: verifier}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("New credential-bearing URL error=%v", err)
	}
}

func TestTodo_AGENT_023_Fault(t *testing.T) {
	reg := testRegistration("https://model.example")
	registry := &testRegistry{registration: reg}
	verifier := &testTransport{client: http.DefaultClient}
	adapter := newTestAdapter(t, reg, registry, verifier)
	registry.err = errors.New("registry unavailable")
	result, err := adapter.Invoke(context.Background(), testRequest())
	if err == nil || result.Failure == nil || result.Failure.Code != agentmodel.FailureUnavailable || verifier.calls.Load() != 0 {
		t.Fatalf("Invoke result/error/verifier=%+v/%v/%d", result, err, verifier.calls.Load())
	}
}

func newTestAdapter(t *testing.T, reg Registration, registry Registry, verifier EndpointTransport) *Adapter {
	t.Helper()
	adapter, err := New(context.Background(), Config{EndpointID: reg.ID, Pin: RegistrationPin(reg), Registry: registry, Transport: verifier})
	if err != nil {
		t.Fatalf("New() error=%v", err)
	}
	return adapter
}

func testRegistration(endpoint string) Registration {
	return Registration{ID: "private-a", Profile: "private-profile", EndpointURL: endpoint, NetworkPathRef: "egress:model-a:v1", ServiceIdentity: "spiffe://models/private-a", Identity: agentmodel.ModelIdentity{ProviderID: "private-a", ModelID: "model-a", Version: "rev-7"}, Region: "us-east", DataClasses: []string{"PUBLIC"}, Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied, HealthPath: "/health", InferencePath: "/infer", Capabilities: agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}, Features: []agentmodel.ModelFeature{agentmodel.FeatureStructuredJSON}, OutputModes: []agentmodel.OutputMode{agentmodel.OutputText, agentmodel.OutputJSON, agentmodel.OutputSchema}, MaxTools: 0}, Approved: true}
}

func testRequest() agentmodel.ModelRequest {
	return agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: "answer", ModelProfile: "private-profile", Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "question"}}, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: time.Now().Add(time.Minute), Limits: agentmodel.ModelLimits{MaxOutputTokens: 100}, TraceID: "trace-1", Processing: agentmodel.ProcessingPolicy{Residency: "us-east", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}}
}
