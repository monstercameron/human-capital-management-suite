package hcmctl

import (
	"context"
	"fmt"
	"strings"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
)

func parseExplorer(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("explorer")
	action := sub.String("action", "", "explorer action: list-events|verify-chain|simulate-authz")
	tenant := sub.String("tenant", "", "ledger tenant UUID")
	stream := sub.String("stream", "", "ledger stream key")
	subjectTenant := sub.String("subject-tenant", "", "simulation subject tenant UUID")
	subjectKind := sub.String("subject-kind", "", "simulation subject entity kind")
	subjectID := sub.String("subject-id", "", "simulation subject id")
	purpose := sub.String("purpose", "", "simulation purpose of use")
	fields := sub.String("fields", "", "comma-separated simulation fields")
	policyVersion := sub.String("policy-version", "", "policy version label the simulation is compared against")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	switch *action {
	case "list-events", "verify-chain":
		if strings.TrimSpace(*tenant) == "" || strings.TrimSpace(*stream) == "" {
			return parsedCommand{}, fmt.Errorf("hcmctl: explorer -action=%s requires -tenant and -stream", *action)
		}
		return parseExplorerStream(g, *action, *tenant, *stream)
	case "simulate-authz":
		if strings.TrimSpace(*subjectTenant) == "" || strings.TrimSpace(*subjectKind) == "" || strings.TrimSpace(*subjectID) == "" {
			return parsedCommand{}, fmt.Errorf("hcmctl: explorer -action=simulate-authz requires -subject-tenant, -subject-kind and -subject-id")
		}
		return parseExplorerSimulate(g, *subjectTenant, *subjectKind, *subjectID, *purpose, splitCSV(*fields), *policyVersion)
	default:
		return parsedCommand{}, fmt.Errorf("hcmctl: unknown explorer action %q (want list-events|verify-chain|simulate-authz)", *action)
	}
}

func parseExplorerStream(g globalFlags, action, tenant, stream string) (parsedCommand, error) {
	switch action {
	case "list-events":
		return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
			resp, err := client.ListLedgerEvents(ctx, &adminv1.ListLedgerEventsRequest{TenantId: tenant, StreamKey: stream})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "stream: %s\nevents: %d\ndigest: %s\n", stream, len(resp.GetEvents()), resp.GetDigest())
			for _, ev := range resp.GetEvents() {
				fmt.Fprintf(&b, "  %-6d %-16s payload_withheld=%v subject_withheld=%v\n",
					ev.GetSequence(), ev.GetAssertionClass(), ev.GetPayloadWithheld(), ev.GetSubjectWithheld())
			}
			return b.String(), nil
		}}, nil
	case "verify-chain":
		return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
			resp, err := client.GetChainVerification(ctx, &adminv1.GetChainVerificationRequest{TenantId: tenant, StreamKey: stream})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "stream: %s\nverified: %v\nempty: %v\n", resp.GetStreamKey(), resp.GetVerified(), resp.GetEmpty())
			if resp.GetHead() != nil {
				fmt.Fprintf(&b, "head: seq=%d hash=%s algo=%s\n", resp.GetHead().GetSequence(), resp.GetHead().GetChainHash(), resp.GetHead().GetAlgorithm())
			}
			if resp.GetBrokenReason() != "" {
				fmt.Fprintf(&b, "broken: seq=%s reason=%s\n", resp.GetBrokenSequence(), resp.GetBrokenReason())
			}
			return b.String(), nil
		}}, nil
	default:
		return parsedCommand{}, fmt.Errorf("hcmctl: unknown explorer stream action %q", action)
	}
}

func parseExplorerSimulate(g globalFlags, subjectTenant, subjectKind, subjectID, purpose string, fields []string, policyVersion string) (parsedCommand, error) {
	return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
		resp, err := client.SimulateAuthorization(ctx, &adminv1.SimulateAuthorizationRequest{
			SubjectTenantId: subjectTenant,
			SubjectKind:     subjectKind,
			SubjectId:       subjectID,
			Purpose:         purpose,
			Fields:          fields,
			PolicyVersion:   policyVersion,
		})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "subject_disclosable: %v\ndenial_reason: %s\ntenant_effect: %s\nscope_effect: %s\n",
			resp.GetSubjectDisclosable(), resp.GetDenialReason(), resp.GetTenantEffect(), resp.GetScopeEffect())
		fmt.Fprintf(&b, "policy_version_match: %v\ndigest: %s\nexplanation: %s\n",
			resp.GetPolicyVersionMatch(), resp.GetDigest(), resp.GetExplanation())
		for _, rule := range resp.GetMatchedRules() {
			fmt.Fprintf(&b, "  rule: %s\n", rule)
		}
		return b.String(), nil
	}}, nil
}
