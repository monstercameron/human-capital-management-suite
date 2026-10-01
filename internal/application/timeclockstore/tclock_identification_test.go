package timeclockstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type identificationPolicyStub struct{}

func (identificationPolicyStub) IdentificationPolicy(context.Context, string, string, string) (clock.RateLimitPolicy, error) {
	return clock.RateLimitPolicy{MaxAttempts: 3}, nil
}

type tokenIssuerStub struct{}

func (tokenIssuerStub) IssueDeviceWorkerToken(context.Context, clockservice.DeviceWorkerTokenClaims) (clockservice.DeviceWorkerToken, error) {
	return clockservice.DeviceWorkerToken{Value: "signed"}, nil
}

type credentialLookupStub struct{}

func (credentialLookupStub) LookupCredential(context.Context, string, string, string, string) (clockservice.CredentialRecord, error) {
	return clockservice.CredentialRecord{WorkerID: "worker", ExternalID: "badge", State: "ACTIVE"}, nil
}

type supervisorVerifierStub struct{}

func (supervisorVerifierStub) VerifySupervisorCredential(context.Context, string, string, string) (*trust.Principal, error) {
	return nil, nil
}

func TestIdentificationAdapter_DelegatesInjectedDependencies(t *testing.T) {
	a := IdentificationAdapter{Policy: identificationPolicyStub{}, Token: tokenIssuerStub{}, Lookup: credentialLookupStub{}, Supervisor: supervisorVerifierStub{}}
	ctx := context.Background()
	if policy, err := a.IdentificationPolicy(ctx, "tenant", "site", "device"); err != nil || policy.MaxAttempts != 3 {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
	if token, err := a.IssueDeviceWorkerToken(ctx, clockservice.DeviceWorkerTokenClaims{}); err != nil || token.Value != "signed" {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	if credential, err := a.LookupCredential(ctx, "tenant", "worker", "BADGE", "badge"); err != nil || credential.ExternalID != "badge" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
	if principal, err := a.VerifySupervisorCredential(ctx, "tenant", "device", "ref"); err != nil || principal != nil {
		t.Fatalf("principal=%v err=%v", principal, err)
	}
}

func TestIdentificationAdapter_FailsClosedWithoutInjectedDependencies(t *testing.T) {
	a := IdentificationAdapter{}
	ctx := context.Background()
	if _, err := a.IdentificationPolicy(ctx, "", "", ""); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := a.IssueDeviceWorkerToken(ctx, clockservice.DeviceWorkerTokenClaims{}); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := a.LookupCredential(ctx, "", "", "", ""); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := a.VerifySupervisorCredential(ctx, "", "", ""); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
}
