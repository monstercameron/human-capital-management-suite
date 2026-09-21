package hcmctl

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
)

type fakeExplorerClient struct {
	adminv1.AdminServiceClient
	listed   *adminv1.ListLedgerEventsRequest
	verified *adminv1.GetChainVerificationRequest
	simmed   *adminv1.SimulateAuthorizationRequest
}

func (f *fakeExplorerClient) ListLedgerEvents(_ context.Context, in *adminv1.ListLedgerEventsRequest, _ ...grpc.CallOption) (*adminv1.ListLedgerEventsResponse, error) {
	f.listed = in
	return &adminv1.ListLedgerEventsResponse{
		Events: []*adminv1.LedgerEventProfile{
			{Sequence: 1, AssertionClass: "DOMAIN_FACT"},
			{Sequence: 2, AssertionClass: "DOMAIN_FACT", PayloadWithheld: true},
		},
		Digest: "sha256:stream",
	}, nil
}

func (f *fakeExplorerClient) GetChainVerification(_ context.Context, in *adminv1.GetChainVerificationRequest, _ ...grpc.CallOption) (*adminv1.GetChainVerificationResponse, error) {
	f.verified = in
	return &adminv1.GetChainVerificationResponse{
		StreamKey: in.GetStreamKey(), Verified: true,
		Head: &adminv1.ChainHeadProfile{StreamKey: in.GetStreamKey(), Sequence: 2, ChainHash: "abc", Algorithm: "sha256"},
	}, nil
}

func (f *fakeExplorerClient) SimulateAuthorization(_ context.Context, in *adminv1.SimulateAuthorizationRequest, _ ...grpc.CallOption) (*adminv1.SimulateAuthorizationResponse, error) {
	f.simmed = in
	return &adminv1.SimulateAuthorizationResponse{
		SubjectDisclosable: true, TenantEffect: "ALLOW", ScopeEffect: "ALLOW",
		FieldRulings: map[string]string{"compensation.base": "ALLOW"},
		MatchedRules: []string{"rule:admin-read"}, Digest: "sha256:sim",
	}, nil
}

func TestTodo_REV_037_01_Hcmctl(t *testing.T) {
	fake := &fakeExplorerClient{}
	ctx := context.Background()

	cmd, err := parseArgs([]string{"explorer", "-action", "list-events", "-tenant", "11111111-1111-1111-1111-111111111111", "-stream", "worker:1"})
	if err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if cmd.run == nil {
		t.Fatal("explorer list did not select the admin client path")
	}
	out, err := cmd.run(ctx, fake)
	if err != nil {
		t.Fatalf("run list: %v", err)
	}
	if !strings.Contains(out, "events: 2") || !strings.Contains(out, "payload_withheld=true") {
		t.Fatalf("list output missing evidence: %q", out)
	}

	cmd, err = parseArgs([]string{"explorer", "-action", "verify-chain", "-tenant", "11111111-1111-1111-1111-111111111111", "-stream", "worker:1"})
	if err != nil {
		t.Fatalf("parse verify: %v", err)
	}
	out, err = cmd.run(ctx, fake)
	if err != nil {
		t.Fatalf("run verify: %v", err)
	}
	if !strings.Contains(out, "verified: true") || !strings.Contains(out, "head: seq=2") {
		t.Fatalf("verify output missing evidence: %q", out)
	}

	cmd, err = parseArgs([]string{"explorer", "-action", "simulate-authz", "-subject-tenant", "11111111-1111-1111-1111-111111111111", "-subject-kind", "worker", "-subject-id", "worker:1", "-fields", "compensation.base"})
	if err != nil {
		t.Fatalf("parse simulate: %v", err)
	}
	out, err = cmd.run(ctx, fake)
	if err != nil {
		t.Fatalf("run simulate: %v", err)
	}
	if !strings.Contains(out, "subject_disclosable: true") || !strings.Contains(out, "rule:admin-read") {
		t.Fatalf("simulate output missing evidence: %q", out)
	}
	if len(fake.simmed.GetFields()) != 1 || fake.simmed.GetFields()[0] != "compensation.base" {
		t.Fatalf("simulate fields = %+v", fake.simmed.GetFields())
	}

	if _, err := parseArgs([]string{"explorer", "-action", "bogus"}); err == nil {
		t.Fatal("unknown explorer action parsed without an error")
	}
	if _, err := parseArgs([]string{"explorer", "-action", "list-events", "-tenant", "t"}); err == nil {
		t.Fatal("explorer list without -stream parsed without an error")
	}
}
