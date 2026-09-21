package hcmctl

import (
	"context"
	"flag"
	"fmt"
	"strings"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
)

// parsedCommand is one invocation's fully-parsed intent: the global flags
// plus a closure that calls exactly one AdminService method with the
// subcommand's own flags and formats the typed response. The closure is the
// entire "business" surface of this package, and it never does anything
// but call the generated client and format its result - it decides nothing
// about an HCM domain rule.
type parsedCommand struct {
	global        globalFlags
	run           func(ctx context.Context, client adminv1.AdminServiceClient) (string, error)
	runOnboarding func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error)
}

// newSubFlagSet builds a subcommand's own flag set, sharing
// flag.ContinueOnError with the global set so a parse failure returns an
// error rather than calling os.Exit from inside a library.
func newSubFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

// parseArgs parses global flags, the subcommand name, and the subcommand's
// own flags, in that order (matching cmd/migrate's convention of a leading
// positional subcommand).
func parseArgs(args []string) (parsedCommand, error) {
	gfs, g := newGlobalFlagSet()
	if err := gfs.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	rest := gfs.Args()
	if len(rest) == 0 {
		return parsedCommand{}, fmt.Errorf("hcmctl: a subcommand is required")
	}
	name, subArgs := rest[0], rest[1:]

	switch name {
	case "list-intents":
		return parseListIntents(*g, subArgs)
	case "release-manifest":
		return parseReleaseManifest(*g, subArgs)
	case "list-capabilities":
		return parseListCapabilities(*g, subArgs)
	case "explain-transaction":
		return parseExplainTransaction(*g, subArgs)
	case "worker-state":
		return parseWorkerState(*g, subArgs)
	case "instance":
		return parseInstance(*g, subArgs)
	case "onboarding":
		return parseOnboarding(*g, subArgs)
	case "explorer":
		return parseExplorer(*g, subArgs)
	default:
		return parsedCommand{}, fmt.Errorf("hcmctl: unknown subcommand %q", name)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseListIntents(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("list-intents")
	pageSize := sub.Int("page-size", 0, "page size (0 means server default)")
	cursor := sub.String("cursor", "", "opaque page cursor")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
		req := &adminv1.ListIntentsRequest{}
		if *pageSize > 0 || *cursor != "" {
			req.Page = &commonv1.PageRequest{PageSize: int32(*pageSize), Cursor: *cursor}
		}
		resp, err := client.ListIntents(ctx, req)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "intents: %d\n", len(resp.GetIntents()))
		for _, in := range resp.GetIntents() {
			fmt.Fprintf(&b, "  %s\n", in.GetIntentId())
		}
		writeEvidence(&b, resp.GetEvidenceRef())
		return b.String(), nil
	}}, nil
}

func parseReleaseManifest(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("release-manifest")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
		resp, err := client.GetReleaseManifest(ctx, &adminv1.GetReleaseManifestRequest{})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "manifest_digest: %s\n", resp.GetManifestDigest())
		fmt.Fprintf(&b, "endpoints: %d  capabilities: %d  intent_definitions: %d\n",
			len(resp.GetEndpoints()), len(resp.GetCapabilities()), len(resp.GetIntentDefinitions()))
		for _, e := range resp.GetEndpoints() {
			fmt.Fprintf(&b, "  %-60s %s\n", e.GetEndpointId(), e.GetDisposition())
		}
		writeEvidence(&b, resp.GetEvidenceRef())
		return b.String(), nil
	}}, nil
}

func parseListCapabilities(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("list-capabilities")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
		resp, err := client.ListCapabilityProfiles(ctx, &adminv1.ListCapabilityProfilesRequest{})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, c := range resp.GetCapabilities() {
			fmt.Fprintf(&b, "%-40s v%-3d %-24s %-10s %s\n", c.GetCapabilityId(), c.GetVersion(), c.GetEffectClass(), c.GetStatus(), c.GetDigest())
		}
		writeEvidence(&b, resp.GetEvidenceRef())
		return b.String(), nil
	}}, nil
}

func parseExplainTransaction(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("explain-transaction")
	txnID := sub.String("transaction-id", "", "transaction id (required)")
	sections := sub.String("sections", "", "comma-separated section tokens (empty means every section)")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	if *txnID == "" {
		return parsedCommand{}, fmt.Errorf("hcmctl: explain-transaction requires -transaction-id")
	}
	return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
		resp, err := client.ExplainTransaction(ctx, &adminv1.ExplainTransactionRequest{
			Transaction: &adminv1.TransactionRef{Id: *txnID},
			Sections:    splitCSV(*sections),
		})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "disclosed: %t  disclosure: %s  presence: %s\n", resp.GetDisclosed(), resp.GetDisclosure(), resp.GetPresence())
		if resp.GetWithheldReason() != "" {
			fmt.Fprintf(&b, "withheld_reason: %s\n", resp.GetWithheldReason())
		}
		for _, s := range resp.GetSections() {
			fmt.Fprintf(&b, "  %-16s %-10s entries=%d %s\n", s.GetSection(), s.GetAccess(), s.GetEntryCount(), s.GetDenialReason())
		}
		for _, n := range resp.GetNarrative() {
			fmt.Fprintf(&b, "  # %s\n", n)
		}
		writeEvidence(&b, resp.GetEvidenceRef())
		return b.String(), nil
	}}, nil
}

func parseWorkerState(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("worker-state")
	workerID := sub.String("worker-id", "", "worker id (required)")
	effectiveOn := sub.String("effective-on", "", "YYYY-MM-DD (empty means today)")
	fields := sub.String("fields", "", "comma-separated field tokens (empty means every field)")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	if *workerID == "" {
		return parsedCommand{}, fmt.Errorf("hcmctl: worker-state requires -worker-id")
	}
	return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
		resp, err := client.GetWorkerState(ctx, &adminv1.GetWorkerStateRequest{
			WorkerId:    *workerID,
			EffectiveOn: *effectiveOn,
			Fields:      splitCSV(*fields),
		})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "disclosed: %t  disclosure: %s  presence: %s\n", resp.GetDisclosed(), resp.GetDisclosure(), resp.GetPresence())
		if resp.GetWithheldReason() != "" {
			fmt.Fprintf(&b, "withheld_reason: %s\n", resp.GetWithheldReason())
		}
		for _, f := range resp.GetFields() {
			value := f.GetValue()
			if f.GetAccess() != "AUTHORIZED" {
				value = ""
			}
			fmt.Fprintf(&b, "  %-28s %-10s %-10s %s\n", f.GetField(), f.GetAccess(), f.GetPresence(), value)
		}
		writeEvidence(&b, resp.GetEvidenceRef())
		return b.String(), nil
	}}, nil
}

func parseInstance(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("instance")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	if sub.NArg() != 1 {
		return parsedCommand{}, fmt.Errorf("hcmctl: instance requires exactly one positional argument, the instance id")
	}
	instanceID := sub.Arg(0)
	return parsedCommand{global: g, run: func(ctx context.Context, client adminv1.AdminServiceClient) (string, error) {
		resp, err := client.GetWorkflowInstance(ctx, &adminv1.GetWorkflowInstanceRequest{InstanceId: instanceID})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "disclosed: %t  complete: %t\n", resp.GetDisclosed(), resp.GetComplete())
		if resp.GetDenialReason() != "" {
			fmt.Fprintf(&b, "denial_reason: %s\n", resp.GetDenialReason())
		}
		if !resp.GetDisclosed() {
			writeEvidence(&b, resp.GetEvidenceRef())
			return b.String(), nil
		}

		def := resp.GetDefinition()
		fmt.Fprintf(&b, "definition: workflow_id=%s version=%d plan_hash=%s\n",
			def.GetWorkflowId(), def.GetWorkflowVersion(), def.GetCompiledPlanHash())

		inst := resp.GetInstance()
		lc := inst.GetLifecycle()
		fmt.Fprintf(&b, "instance: id=%s status=%s mode=%s version=%d\n",
			inst.GetInstanceId(), inst.GetRuntimeStatus(), inst.GetExecutionMode(), inst.GetInstanceVersion())
		fmt.Fprintf(&b, "  lifecycle: request=%s execution=%s business=%s consistency=%s obligation=%s\n",
			lc.GetRequestState(), lc.GetExecutionState(), lc.GetBusinessState(), lc.GetConsistencyState(), lc.GetObligationState())
		fmt.Fprintf(&b, "  %s\n", renderRef("input_ref", inst.GetInputRef()))
		fmt.Fprintf(&b, "  %s\n", renderRef("effective_context_ref", inst.GetEffectiveContextRef()))
		fmt.Fprintf(&b, "  %s\n", renderRef("last_checkpoint_ref", inst.GetLastCheckpointRef()))

		fmt.Fprintf(&b, "frontier: %d node(s)\n", len(resp.GetFrontier()))
		for _, f := range resp.GetFrontier() {
			fmt.Fprintf(&b, "  %-24s attempt_recorded=%t attempt=%d status=%s\n",
				f.GetNodeId(), f.GetAttemptRecorded(), f.GetAttempt(), f.GetStatus())
		}

		fmt.Fprintf(&b, "nodes: %d\n", len(resp.GetNodes()))
		for _, n := range resp.GetNodes() {
			fmt.Fprintf(&b, "  %-24s attempt=%d step=%-16s status=%-10s current=%t\n",
				n.GetNodeId(), n.GetAttempt(), n.GetStepType(), n.GetStatus(), n.GetCurrent())
			gov := n.GetGovernance()
			fmt.Fprintf(&b, "    %s\n", renderRef("proposal_ref", gov.GetProposalRef()))
			fmt.Fprintf(&b, "    %s\n", renderRef("baseline_ref", gov.GetBaselineRef()))
			fmt.Fprintf(&b, "    %s\n", renderRef("policy_ref", gov.GetPolicyRef()))
			fmt.Fprintf(&b, "    %s\n", renderRef("capability_execution_id", n.GetConnector().GetCapabilityExecutionId()))
			fmt.Fprintf(&b, "    %s\n", renderRef("trace_id", n.GetTrace().GetTraceId()))
		}

		fmt.Fprintf(&b, "work_items: disclosed=%t count=%d\n", resp.GetWorkItemsDisclosed(), len(resp.GetWorkItems()))
		for _, w := range resp.GetWorkItems() {
			fmt.Fprintf(&b, "  %-36s kind=%-8s status=%-12s node=%s\n",
				w.GetWorkItemId(), w.GetKind(), w.GetStatus(), w.GetNodeId())
			fmt.Fprintf(&b, "    %s\n", renderRef("approval_requirement_ref", w.GetApprovalRequirementRef()))
			fmt.Fprintf(&b, "    transitions_recorded=%t (%d)\n", w.GetTransitionsRecorded(), len(w.GetTransitions()))
			for _, t := range w.GetTransitions() {
				fmt.Fprintf(&b, "      %s -> %-12s by=%-20s %s\n", t.GetFromStatus(), t.GetToStatus(), t.GetActorPrincipalId(),
					renderRef("evidence_ref", t.GetEvidenceRef()))
			}
		}

		if len(resp.GetRedactions()) > 0 {
			fmt.Fprintf(&b, "redactions: %s\n", strings.Join(resp.GetRedactions(), ", "))
		}
		if len(resp.GetGaps()) > 0 {
			fmt.Fprintf(&b, "gaps: %s\n", strings.Join(resp.GetGaps(), ", "))
		}
		writeEvidence(&b, resp.GetEvidenceRef())
		return b.String(), nil
	}}, nil
}

// renderRef formats one RefValue as "label: VALUE" for a disclosed value,
// "label: REDACTED (reason)" for a withheld one, "label: GAP (unrecorded)"
// for a reference the projection expected and did not receive, and
// "label: (absent)" for one correctly recorded as empty -- ADMIN-008's whole
// point: an operator reading this line can always tell which of the three it
// is, never just "blank."
func renderRef(label string, r *adminv1.RefValue) string {
	switch r.GetState() {
	case "VALUE":
		return fmt.Sprintf("%s: %s", label, r.GetValue())
	case "REDACTED":
		return fmt.Sprintf("%s: REDACTED (%s)", label, r.GetReason())
	default:
		if r.GetGapKind() == "UNRECORDED" {
			return fmt.Sprintf("%s: GAP (unrecorded)", label)
		}
		return fmt.Sprintf("%s: (absent)", label)
	}
}

// writeEvidence appends the standard evidence line every subcommand prints
// on success (ADMIN-001: "evidence IDs printed on every call").
func writeEvidence(b *strings.Builder, ref *commonv1.EvidenceRef) {
	if ref == nil {
		fmt.Fprintln(b, "evidence: (none)")
		return
	}
	fmt.Fprintf(b, "evidence: %s (%s)\n", ref.GetEvidenceId(), ref.GetEvidenceKind())
}
