package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type rev031Verifier struct{ now time.Time }

func (v rev031Verifier) Verify(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
	spec := trust.PrincipalSpec{
		Tenant: values.TenantId("rev031-tenant"), SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "rev031-session", Purposes: []string{"operator_diagnostics"},
		IssuedAt: v.now.Add(-time.Minute), ExpiresAt: v.now.Add(time.Hour), CredentialDigest: "digest:" + credential.Token,
	}
	switch credential.Token {
	case "operator-token":
		spec.Subject, spec.Roles = "operator-one", []string{admin.OperatorRole}
	case "approver-token":
		spec.Subject, spec.Roles = "operator-two", []string{admin.OperatorRole}
	case "ordinary-token":
		spec.Subject = "ordinary-user"
	case "service-token":
		spec.Subject, spec.SubjectKind = "api", trust.SubjectKindService
	default:
		return nil, trust.ErrInvalidCredential
	}
	return trust.NewPrincipal(spec)
}

func rev031ControlFixture(t *testing.T, now time.Time) (*configBundleControl, http.Handler, configbundle.Scope, tenant.Placement) {
	t.Helper()
	seed1 := make([]byte, 32)
	seed2 := make([]byte, 32)
	for i := range seed1 {
		seed1[i], seed2[i] = byte(i+1), byte(i+101)
	}
	cfg := ServeConfig{
		ConfigBundleSigningSeed: base64.StdEncoding.EncodeToString(seed1),
		ConfigBundleReceiptSeed: base64.StdEncoding.EncodeToString(seed2),
	}
	control, err := newConfigBundleControl(cfg, func() time.Time { return now })
	if err != nil || control == nil {
		t.Fatalf("new config control: control=%v err=%v", control, err)
	}
	scope := configbundle.Scope{TenantID: "rev031-tenant", CellID: "cell-a"}
	placement := tenant.Placement{Tenant: scope.TenantID, Cell: scope.CellID, Region: "us-east", ResidencyProfile: "us", IsolationTier: "standard", Epoch: 1}
	transportConfig := transport.Config{Verifier: rev031Verifier{now: now}}
	handler := overlayConfigBundleControl(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }), transportConfig, control)
	return control, handler, scope, placement
}

func formatUintTest(value uint64) string { return strconv.FormatUint(value, 10) }

func rev031Request(t *testing.T, handler http.Handler, path, token string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	if token != "" {
		req.Header.Set(transport.AuthorizationMetadataKey, "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func rev031Activate(t *testing.T, handler http.Handler, placement tenant.Placement, epoch uint64, revision uint64) configbundle.DesiredStateRecord {
	t.Helper()
	object := configregistry.ConfigurationObject{
		Kind: configregistry.KindRule, ID: "rule", Revision: uint32(revision),
		Scope:     configbundle.Scope{TenantID: placement.Tenant, CellID: placement.Cell},
		SchemaRef: "rule/v1", Body: []byte(`{"enabled":true,"revision":` + formatUintTest(revision) + `}`),
	}
	published := rev031Request(t, handler, configBundleControlPath+"objects", "operator-token", publishObjectRequest{Object: object}, nil)
	if published.Code != http.StatusOK {
		t.Fatalf("publish object revision %d: status=%d body=%s", revision, published.Code, published.Body.String())
	}
	var publication struct {
		ObjectRef configregistry.ObjectRef `json:"object_ref"`
	}
	if err := json.Unmarshal(published.Body.Bytes(), &publication); err != nil || publication.ObjectRef.ID == "" {
		t.Fatalf("decode publication reference: ref=%+v err=%v", publication.ObjectRef, err)
	}
	response := rev031Request(t, handler, configBundleControlPath+"activate", "operator-token", activateBundleRequest{
		ObjectRef: publication.ObjectRef,
		BundleID:  "rev031-bundle-" + formatUintTest(revision), MinimumRuntimeVersion: "go1.0.0",
		Placement: placement, Epoch: epoch,
	}, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("activate revision %d: status=%d body=%s", revision, response.Code, response.Body.String())
	}
	var result struct {
		DesiredState configbundle.DesiredStateRecord `json:"desired_state"`
		Activation   configbundle.ActivationReceipt  `json:"activation_receipt"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode activation response: %v", err)
	}
	if result.DesiredState.Digest == "" || result.Activation.Epoch != epoch || result.Activation.BundleDigest == "" {
		t.Fatalf("activation response omitted distribution or receipt: %+v", result)
	}
	return result.DesiredState
}

func TestTodo_REV_031_01_Integration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	_, handler, scope, placement := rev031ControlFixture(t, now)
	first := rev031Activate(t, handler, placement, 1, 1)
	second := rev031Activate(t, handler, placement, 2, 2)
	if first.Epoch != 1 || second.Epoch != 2 || first.Digest == second.Digest {
		t.Fatalf("versioned desired states are not distinct: first=%+v second=%+v", first, second)
	}

	ack := rev031Request(t, handler, configBundleControlPath+"ack", "service-token", acknowledgeApplicationRequest{
		Receipt: configbundle.ApplicationReceipt{
			TenantID: scope.TenantID, CellID: scope.CellID, Service: "api", Build: "build-1",
			DesiredDigest: second.Bundle.Bundle.Digest, AppliedDigest: second.Bundle.Bundle.Digest, Epoch: second.Epoch,
			Validation: configbundle.ValidationValid,
		},
	}, nil)
	if ack.Code != http.StatusOK || !strings.Contains(ack.Body.String(), `"receipt"`) {
		t.Fatalf("service acknowledgement: status=%d body=%s", ack.Code, ack.Body.String())
	}
	watermark := rev031Request(t, handler, configBundleControlPath+"watermark", "operator-token", adoptionWatermarkRequest{
		TenantID: scope.TenantID, CellID: scope.CellID, DesiredDigest: second.Bundle.Bundle.Digest, Epoch: second.Epoch, Required: []string{"api"},
	}, nil)
	if watermark.Code != http.StatusOK || !strings.Contains(watermark.Body.String(), `"status":"ADOPTED"`) {
		t.Fatalf("adoption watermark: status=%d body=%s", watermark.Code, watermark.Body.String())
	}

	rollback := rev031Request(t, handler, configBundleControlPath+"rollback", "operator-token", rollbackBundleRequest{
		Tenant: scope.TenantID, CellID: scope.CellID, PriorDigest: first.Bundle.Digest, Epoch: 3,
	}, nil)
	if rollback.Code != http.StatusOK || !strings.Contains(rollback.Body.String(), `"epoch":3`) || !strings.Contains(rollback.Body.String(), first.Bundle.Digest) {
		t.Fatalf("rollback through operator surface: status=%d body=%s", rollback.Code, rollback.Body.String())
	}

	kill := rev031Request(t, handler, configBundleControlPath+"kill-switch", "operator-token", killSwitchRequest{
		SwitchID: "incident-17", Target: configbundle.KillSwitchTarget{TenantID: scope.TenantID, Service: "api"},
		Priority: 10, Reason: "incident containment", IncidentRef: "incident/17", EvidenceRef: "evidence/17",
		IssuedAt: now, ExpiresAt: now.Add(time.Hour), PropagationSLO: time.Minute,
	}, map[string]string{"X-Approver-Authorization": "Bearer approver-token"})
	if kill.Code != http.StatusOK || !strings.Contains(kill.Body.String(), `"Status":"APPLIED"`) {
		t.Fatalf("kill-switch publish and apply: status=%d body=%s", kill.Code, kill.Body.String())
	}
	evaluation := rev031Request(t, handler, configBundleControlPath+"kill-switch/evaluate", "operator-token", configbundle.KillSwitchTarget{TenantID: scope.TenantID, Service: "api"}, nil)
	if evaluation.Code != http.StatusOK || !strings.Contains(evaluation.Body.String(), `"Disabled":true`) {
		t.Fatalf("kill-switch evaluation: status=%d body=%s", evaluation.Code, evaluation.Body.String())
	}
	serviceExecution := rev031Request(t, handler, "/hcmnext.test.v1.Service/Execute", "service-token", nil, nil)
	if serviceExecution.Code != http.StatusServiceUnavailable {
		t.Fatalf("active kill switch did not stop service execution: status=%d body=%s", serviceExecution.Code, serviceExecution.Body.String())
	}
}

func TestTodo_REV_031_01_Fault(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	control, handler, _, placement := rev031ControlFixture(t, now)
	first := rev031Activate(t, handler, placement, 1, 1)
	replay := rev031Request(t, handler, configBundleControlPath+"activate", "operator-token", activateBundleRequest{
		Object: configregistry.ConfigurationObject{
			Kind: configregistry.KindRule, ID: "rule", Revision: 3,
			Scope:     configbundle.Scope{TenantID: placement.Tenant, CellID: placement.Cell},
			SchemaRef: "rule/v1", Body: []byte(`{"enabled":false}`),
		},
		BundleID: "replayed", MinimumRuntimeVersion: "go1.0.0", Placement: placement, Epoch: 1,
	}, nil)
	if replay.Code != http.StatusConflict || !strings.Contains(replay.Body.String(), "EPOCH_CONFLICT") {
		t.Fatalf("same-epoch changed bundle: status=%d body=%s", replay.Code, replay.Body.String())
	}
	if first.Epoch != 1 {
		t.Fatal("fixture did not establish initial active epoch")
	}
	failedCandidate := configregistry.ConfigurationObject{
		Kind: configregistry.KindRule, ID: "uncompilable", Revision: 1,
		Scope:     configbundle.Scope{TenantID: placement.Tenant, CellID: placement.Cell},
		SchemaRef: "rule/v1", Body: []byte(`{"dependencies":[`),
	}
	failed := rev031Request(t, handler, configBundleControlPath+"activate", "operator-token", activateBundleRequest{
		Object: failedCandidate, BundleID: "invalid-candidate", MinimumRuntimeVersion: "go1.0.0", Placement: placement, Epoch: 2,
	}, nil)
	if failed.Code != http.StatusBadRequest {
		t.Fatalf("uncompilable prepared candidate status=%d body=%s", failed.Code, failed.Body.String())
	}
	if _, found, err := control.registry.GetObject(failedCandidate.Ref()); err != nil || found {
		t.Fatalf("failed compilation published candidate: found=%t err=%v", found, err)
	}
	if history, err := control.registry.ListActivations(failedCandidate.Scope, failedCandidate.Kind, failedCandidate.ID); err != nil || len(history) != 0 {
		t.Fatalf("failed compilation committed activation: history=%+v err=%v", history, err)
	}
	malformedReq := httptest.NewRequest(http.MethodPost, configBundleControlPath+"activate", strings.NewReader(`{"unknown":true}`))
	malformedReq.Header.Set(transport.AuthorizationMetadataKey, "Bearer operator-token")
	malformedResponse := httptest.NewRecorder()
	handler.ServeHTTP(malformedResponse, malformedReq)
	if malformedResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field status=%d body=%s", malformedResponse.Code, malformedResponse.Body.String())
	}

	invalidPlacement := placement
	invalidPlacement.Cell = "cell-other"
	wrongScope := rev031Request(t, handler, configBundleControlPath+"activate", "operator-token", activateBundleRequest{
		Object: configregistry.ConfigurationObject{
			Kind: configregistry.KindRule, ID: "wrong", Revision: 1,
			Scope:     configbundle.Scope{TenantID: placement.Tenant, CellID: placement.Cell},
			SchemaRef: "rule/v1", Body: []byte(`{"enabled":true}`),
		}, BundleID: "wrong-placement", MinimumRuntimeVersion: "go1.0.0", Placement: invalidPlacement, Epoch: 2,
	}, nil)
	if wrongScope.Code != http.StatusBadRequest {
		t.Fatalf("object/placement scope mismatch status=%d body=%s", wrongScope.Code, wrongScope.Body.String())
	}

}

func TestTodo_REV_031_01_Security(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	_, handler, scope, _ := rev031ControlFixture(t, now)
	for _, tc := range []struct {
		name   string
		token  string
		status int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "ordinary user", token: "ordinary-token", status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := rev031Request(t, handler, configBundleControlPath+"objects", tc.token, publishObjectRequest{Object: configregistry.ConfigurationObject{
				Kind: configregistry.KindRule, ID: tc.name, Revision: 1, Scope: scope, SchemaRef: "rule/v1", Body: []byte(`{"enabled":true}`),
			}}, nil)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), tc.status)
			}
		})
	}
	body := killSwitchRequest{SwitchID: "secure-switch", Target: configbundle.KillSwitchTarget{TenantID: scope.TenantID}, Priority: 1,
		Reason: "contain", IncidentRef: "incident/1", EvidenceRef: "evidence/1", IssuedAt: now, ExpiresAt: now.Add(time.Hour), PropagationSLO: time.Minute}
	for _, approver := range []string{"", "operator-token"} {
		response := rev031Request(t, handler, configBundleControlPath+"kill-switch", "operator-token", body,
			map[string]string{"X-Approver-Authorization": "Bearer " + approver})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid distinct approver %q: status=%d body=%s", approver, response.Code, response.Body.String())
		}
	}
}

func TestTodo_REV_031_01_Recovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	control, handler, _, placement := rev031ControlFixture(t, now)
	desired := rev031Activate(t, handler, placement, 1, 1)
	scope := configbundle.Scope{TenantID: placement.Tenant, CellID: placement.Cell}
	receiver := control.receiver(scope, placement)
	before, ok := receiver.Snapshot()
	if !ok || before.DesiredDigest != desired.Digest {
		t.Fatalf("receiver did not retain accepted bundle: snapshot=%+v ok=%t", before, ok)
	}
	malformed := configbundle.DesiredStateRecord{TenantID: placement.Tenant, CellID: placement.Cell, Epoch: 2}
	if _, err := receiver.Receive(malformed); err == nil {
		t.Fatal("receiver accepted malformed recovery record")
	}
	after, ok := receiver.Snapshot()
	if !ok || after.DesiredDigest != before.DesiredDigest || after.Epoch != before.Epoch {
		t.Fatalf("rejected record damaged last-known-good snapshot: before=%+v after=%+v", before, after)
	}
	// The composed receiver workload rebuilds its local applied snapshot from
	// the distributor on startup, then remains alive until its owner cancels it.
	delete(control.receivers, scope)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- control.runReceiver(ctx, time.Millisecond) }()
	deadline := time.After(time.Second)
	for {
		control.mu.Lock()
		if restored, present := control.receivers[scope]; present {
			snapshot, applied := restored.Snapshot()
			if applied && snapshot.DesiredDigest == desired.Digest {
				control.mu.Unlock()
				break
			}
		}
		control.mu.Unlock()
		select {
		case <-deadline:
			cancel()
			t.Fatal("receiver lifecycle did not reconcile desired state")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatalf("receiver lifecycle stopped with error: %v", err)
	}
}

func TestTodo_REV_031_01_ConfigRejectsOneSigningSeed(t *testing.T) {
	seed := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := newConfigBundleControl(ServeConfig{ConfigBundleSigningSeed: seed}, time.Now); err == nil {
		t.Fatal("config control accepted only one of its two signing seeds")
	}
}
