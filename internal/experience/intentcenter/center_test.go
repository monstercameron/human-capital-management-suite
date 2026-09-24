package intentcenter

import (
	"bytes"
	"encoding/json"
	"testing"
)

func centerAuthority() Authority {
	return Authority{PrincipalID: "actor-1", TenantID: "tenant-1", Revision: "auth-7", CanView: true, Capabilities: []string{"intent.inspect", "approval.decide"}, CanViewRestrictedEvidence: false}
}
func centerRecord() SourceRecord {
	return SourceRecord{Reference: Reference{IntentID: "intent-1", RelationshipID: "rel-1", ProposalID: "proposal-1", WorkItemID: "work-1", ApprovalID: "approval-1"}, Kind: "approval", Title: "Approve legal name change", State: PartialLifecycle{Intent: "in_progress", Work: "waiting", Approval: "pending", Delivery: "not_started", External: "not_applicable"}, Owner: Owner{PrincipalID: "actor-1"}, UpdatedAt: "2026-09-03T11:00:00.000Z", Actions: []SourceAction{{ID: "approve", Capability: "approval.decide", Label: "Approve"}}, Evidence: map[string]any{"proposal": "proposal-1"}}
}
func centerInput() Input {
	return Input{Now: "2026-09-03T12:00:00.000Z", Authority: centerAuthority(), Records: []SourceRecord{centerRecord()}, Timeline: []TimelineSource{{Reference: Reference{IntentID: "intent-1", ApprovalID: "approval-1"}, EventID: "event-1", OccurredAt: "2026-09-03T10:00:00.000Z", Type: "approval.created", Summary: "Approval requested", Evidence: map[string]any{"secret": "hidden"}, RestrictedEvidence: true}}}
}

func TestTodo_REV_018_02_Golden(t *testing.T) {
	p := Project(centerInput())
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(p); err != nil {
		t.Fatal(err)
	}
	encoded := bytes.TrimSuffix(output.Bytes(), []byte("\n"))
	const want = `{"authorityRevision":"auth-7","generatedAt":"2026-09-03T12:00:00.000Z","drafts":[],"tasks":[],"approvals":[{"intentId":"intent-1","relationshipId":"rel-1","proposalId":"proposal-1","workItemId":"work-1","approvalId":"approval-1","refs":{"intentId":"intent-1","relationshipId":"rel-1","proposalId":"proposal-1","workItemId":"work-1","approvalId":"approval-1"},"kind":"approval","title":"Approve legal name change","state":{"intent":"in_progress","work":"waiting","approval":"pending","delivery":"not_started","external":"not_applicable"},"stateLabel":"in_progress/waiting/pending/not_started/not_applicable","stale":false,"evidence":{"proposal":"proposal-1"},"actions":[{"id":"approve","capability":"approval.decide","label":"Approve","enabled":true,"refs":{"intentId":"intent-1","relationshipId":"rel-1","proposalId":"proposal-1","workItemId":"work-1","approvalId":"approval-1"}}],"deepLink":{"href":"/intent-center/intent-1?intent=intent-1&auth=auth-7&relationship=rel-1&proposal=proposal-1&workItem=work-1&approval=approval-1","intentId":"intent-1","refs":{"intentId":"intent-1","relationshipId":"rel-1","proposalId":"proposal-1","workItemId":"work-1","approvalId":"approval-1"},"authorityRevision":"auth-7","requiresReauthorization":false},"owner":{"principalId":"actor-1"},"updatedAt":"2026-09-03T11:00:00.000Z"}],"messages":[],"timeline":[{"intentId":"intent-1","approvalId":"approval-1","eventId":"event-1","occurredAt":"2026-09-03T10:00:00.000Z","type":"approval.created","summary":"Approval requested","evidence":{"redacted":true,"reason":"restricted"}}]}`
	if string(encoded) != want {
		t.Fatalf("projection bytes differ\n got: %s\nwant: %s", encoded, want)
	}
}

func TestTodo_REV_018_02_Conformance(t *testing.T) {
	in := centerInput()
	in.InspectIntentID = "intent-1"
	p := ProjectIntentCenter(in)
	item := p.Approvals[0]
	if item.Refs.IntentID != "intent-1" || item.Refs.RelationshipID != "rel-1" || item.Refs.ProposalID != "proposal-1" || item.Refs.WorkItemID != "work-1" || item.Refs.ApprovalID != "approval-1" {
		t.Fatalf("refs lost: %+v", item.Refs)
	}
	if p.Inspector == nil || p.Inspector.IntentID != "intent-1" || item.StateLabel != "in_progress/waiting/pending/not_started/not_applicable" {
		t.Fatalf("projection lifecycle/inspector: %+v", p)
	}
	if !IsSafeAction(item.Actions[0], centerAuthority()) {
		t.Fatal("authorized action was not safe")
	}
	if got, ok := ResolveDeepLink(*item.DeepLink, centerAuthority()); !ok || got.RequiresReauthorization {
		t.Fatalf("current deep link rejected: %+v %v", got, ok)
	}
	if _, ok := ResolveDeepLink(*item.DeepLink, Authority{CanView: true, Revision: "auth-8"}); ok {
		t.Fatal("stale authority revision accepted")
	}
	if _, ok := ResolveDeepLink(*item.DeepLink, Authority{CanView: false, Revision: "auth-7"}); ok {
		t.Fatal("unauthorized deep link accepted")
	}
	without := centerAuthority()
	without.Capabilities = []string{"intent.inspect"}
	if IsSafeAction(item.Actions[0], without) {
		t.Fatal("action survived current capability loss")
	}
	in.Authority = Authority{PrincipalID: "actor-1", TenantID: "tenant-1", Revision: "auth-7", CanView: false, CanViewRestrictedEvidence: true}
	in.Records[0].RestrictedEvidence = true
	denied := Project(in)
	if len(denied.Approvals) != 1 || !denied.Approvals[0].Stale || denied.Approvals[0].DeepLink != nil || denied.Approvals[0].Evidence != (RedactedValue{Redacted: true, Reason: "not_authorized"}) {
		t.Fatalf("authority denial projection: %+v", denied.Approvals)
	}
	for _, action := range denied.Approvals[0].Actions {
		if action.Enabled || action.Reason != "unauthorized" {
			t.Fatalf("authority denial left action enabled: %+v", action)
		}
	}
}

func TestTodo_REV_018_02_Security(t *testing.T) {
	in := centerInput()
	in.Records = append(in.Records, centerRecord())
	in.Records[0].RestrictedEvidence = true
	in.Authority.CanView = false
	p := Project(in)
	if len(p.Approvals) != 1 {
		t.Fatalf("hidden restricted record remained visible: %d", len(p.Approvals))
	}
	if len(p.Timeline) != 0 {
		t.Fatalf("restricted timeline remained visible: %d", len(p.Timeline))
	}
	in.Authority.CanView = true
	p = Project(in)
	if p.Approvals[0].Evidence != (RedactedValue{Redacted: true, Reason: "restricted"}) || p.Timeline[0].Evidence != (RedactedValue{Redacted: true, Reason: "restricted"}) {
		t.Fatalf("restricted evidence disclosed: %+v %+v", p.Approvals[0].Evidence, p.Timeline[0].Evidence)
	}
	in.Authority.CanViewRestrictedEvidence = true
	p = Project(in)
	if p.Approvals[0].Evidence == nil || p.Timeline[0].Evidence == nil {
		t.Fatal("permitted evidence was removed")
	}
	in.Records = []SourceRecord{centerRecord()}
	in.Records[0].State.Intent = "completed"
	p = Project(in)
	if p.Approvals[0].Actions[0].Enabled || p.Approvals[0].Actions[0].Reason != "terminal" {
		t.Fatalf("terminal action enabled: %+v", p.Approvals[0].Actions[0])
	}
	in.Records[0].State.Intent = "unknown"
	in.Records[0].Actions = []SourceAction{{ID: "retry", Capability: "intent.retry", Label: "Retry"}}
	in.Authority.Capabilities = []string{"intent.retry"}
	p = Project(in)
	if p.Approvals[0].Actions[0].Enabled || p.Approvals[0].Actions[0].Reason != "stale" {
		t.Fatalf("ambiguous retry enabled: %+v", p.Approvals[0].Actions[0])
	}
}
