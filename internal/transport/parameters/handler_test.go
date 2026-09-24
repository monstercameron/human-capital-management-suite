package parameters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type parameterServiceStub struct {
	resolution config.ParameterValueResolution
	revision   config.ParameterValueRevision
	consumer   config.Consumer
	change     config.ParameterValueChange
	readCalls  int
	writeCalls int
}

func (s *parameterServiceStub) ResolveDeclared(_ context.Context, key string, consumer config.Consumer) (config.ParameterValueResolution, error) {
	s.readCalls++
	s.consumer = consumer
	s.resolution.Key = key
	return s.resolution, nil
}

func (s *parameterServiceStub) Append(_ context.Context, change config.ParameterValueChange) (config.ParameterValueRevision, error) {
	s.writeCalls++
	s.change = change
	return s.revision, nil
}

func principalForParameters(t *testing.T, roles ...string) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "subject-a", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-a", Roles: roles, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "parameters-test-session",
		IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "parameters-test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_WF_DATA_037_AuthenticatedReadUsesFixedDeclaredConsumer(t *testing.T) {
	service := &parameterServiceStub{resolution: config.ParameterValueResolution{
		Environment: config.EnvironmentSandbox, Found: true, Value: "USD", DefinitionVersion: "4",
	}}
	h := NewHandler(service)
	req := httptest.NewRequest(http.MethodGet, CollectionPath+"payroll.currency", nil)
	req = req.WithContext(trust.WithPrincipal(req.Context(), principalForParameters(t, "hcm_admin")))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusOK || service.readCalls != 1 || service.consumer != readConsumer {
		t.Fatalf("status=%d calls=%d consumer=%+v body=%s", response.Code, service.readCalls, service.consumer, response.Body.String())
	}
	var body resolutionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Environment != "SANDBOX" || body.Value != "USD" {
		t.Fatalf("read response=%+v err=%v", body, err)
	}
}

func TestTodo_WF_DATA_037_AuthenticatedAppendOmitsCallerAuthority(t *testing.T) {
	service := &parameterServiceStub{revision: config.ParameterValueRevision{
		Key: "payroll.currency", Scope: config.ParameterScope{Kind: config.ScopeTenant, ID: "tenant-a"},
		Environment: config.EnvironmentProduction, Revision: 1, Value: "USD", Author: "subject-a",
	}}
	h := NewHandler(service)
	req := httptest.NewRequest(http.MethodPost, CollectionPath+"payroll.currency/revisions", strings.NewReader(`{"scope":{"kind":"TENANT","id":"tenant-a"},"value":"USD","expected_revision":0,"reason":"finance setup","locked":false}`))
	req = req.WithContext(trust.WithPrincipal(req.Context(), principalForParameters(t, "hcm_admin")))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusCreated || service.writeCalls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, service.writeCalls, response.Body.String())
	}
	if service.change.Key != "payroll.currency" || service.change.Author != "" || service.change.Environment != "" || service.change.Path != nil || service.change.ValueType.Kind != "" {
		t.Fatalf("transport supplied or failed to derive authority fields: %+v", service.change)
	}
}

func TestTodo_WF_DATA_037_ParameterAPIRequiresAuthenticatedAdmin(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"anonymous", context.Background(), http.StatusUnauthorized},
		{"non admin", trust.WithPrincipal(context.Background(), principalForParameters(t, "manager")), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &parameterServiceStub{}
			req := httptest.NewRequest(http.MethodGet, CollectionPath+"payroll.currency", nil).WithContext(tc.ctx)
			response := httptest.NewRecorder()
			NewHandler(service).ServeHTTP(response, req)
			if response.Code != tc.want || service.readCalls != 0 {
				t.Fatalf("status=%d calls=%d", response.Code, service.readCalls)
			}
		})
	}
}

func TestTodo_WF_DATA_037_ParameterWriteRejectsCallerEnvironmentAndAuthor(t *testing.T) {
	service := &parameterServiceStub{}
	req := httptest.NewRequest(http.MethodPost, CollectionPath+"payroll.currency/revisions", strings.NewReader(`{"scope":{"kind":"TENANT","id":"tenant-a"},"value":"USD","expected_revision":0,"reason":"finance setup","environment":"SANDBOX","author":"forged","locked":false}`))
	req = req.WithContext(trust.WithPrincipal(req.Context(), principalForParameters(t, "hcm_admin")))
	response := httptest.NewRecorder()
	NewHandler(service).ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || service.writeCalls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, service.writeCalls)
	}
}
