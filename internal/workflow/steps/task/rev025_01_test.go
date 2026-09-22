package task_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/forms/drafts"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/formcontinuity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
)

var (
	rev025Key         = []byte("0123456789abcdef0123456789abcdef")
	rev025Now         = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	rev025Principal   = "principal:worker-1"
	rev025Assistant   = "principal:interpreter-1"
	rev025Answers     = []byte(`{"hours":8}`)
	rev025InstanceID  = uuid.MustParse("60000000-0000-0000-0000-000000000001")
	rev025WorkItemID  = uuid.MustParse("60000000-0000-0000-0000-000000000002")
	rev025ClaimID     = uuid.MustParse("60000000-0000-0000-0000-000000000003")
	rev025NodeFixture = steptask.CompiledTaskNode{
		WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "review_task", WorkType: "promotion.review",
		OutputSchema:        workflow.SchemaRef{SchemaID: "hcmnext.task.promotion_review.Output", Version: 1, ProtobufFullName: "hcmnext.task.promotion_review.Output"},
		FormDefinition:      steptask.VersionedRef{Ref: "form.promotion.review", Version: 1},
		AccessibilityPolicy: steptask.VersionedRef{Ref: "policy.accessibility.default", Version: 1},
		AccommodationPolicy: steptask.VersionedRef{Ref: "policy.accommodation.default", Version: 1},
	}
)

func rev025Store(t *testing.T) *drafts.Store {
	t.Helper()
	s, err := drafts.NewStore(drafts.Config{Key: rev025Key, Now: func() time.Time { return rev025Now }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rev025SaveReq(revision uint64) drafts.SaveRequest {
	return drafts.SaveRequest{
		ID: "draft-leave-1", TenantID: "tenant-a", PrincipalID: rev025Principal,
		FormID: rev025NodeFixture.FormDefinition.Ref, FormVersion: "v1",
		ExpectedRevision: revision, Answers: rev025Answers, ExpiresAt: rev025Now.Add(time.Hour),
	}
}

func rev025ResumeReq(revision uint64) drafts.ResumeRequest {
	return drafts.ResumeRequest{
		ID: "draft-leave-1", TenantID: "tenant-a", PrincipalID: rev025Principal,
		FormID: rev025NodeFixture.FormDefinition.Ref, FormVersion: "v1", Revision: revision,
	}
}

func rev025ContinuityReq() formcontinuity.Request {
	return formcontinuity.Request{
		FormID: rev025NodeFixture.FormDefinition.Ref, TaskVersion: "v1",
		CanonicalChannel: formcontinuity.ChannelWeb, AlternateChannel: formcontinuity.ChannelPhone,
		OriginalDeadline: rev025Now.Add(48 * time.Hour), Now: rev025Now,
		Identity:         formcontinuity.Identity{PrincipalID: rev025Principal, Assurance: "IAL2", AssistantID: rev025Assistant},
		Authority:        formcontinuity.Authority{DecisionRight: "submit.leave", Scope: "worker:worker-1", AuthorityRef: "authority:leave-2026"},
		Attribution:      formcontinuity.Attribution{RespondentID: rev025Principal, AssistantID: rev025Assistant, TranscriberID: rev025Assistant, ReadBackBy: rev025Principal},
		Privacy:          formcontinuity.Privacy{Purpose: "leave-request", Compartment: "hr:restricted", RedactionRule: "leave:v2"},
		Validation:       formcontinuity.Validation{FormRevision: "leave:v3", SchemaDigest: "sha256:schema", ProofDigest: "sha256:proof"},
		TranscriptionRef: "transcription:1", ReadBackConfirmed: true,
		EvidenceDigest: "sha256:evidence", ResponseDigest: "sha256:response",
	}
}

func rev025BaseSpec() steptask.SubmissionSpec {
	return steptask.SubmissionSpec{
		WorkflowInstanceID: rev025InstanceID, NodeID: rev025NodeFixture.NodeID, WorkItemID: rev025WorkItemID, ItemVersion: 4,
		CompletedBy: rev025Principal, CandidateVia: humanwork.SourceDirect,
		ClaimID: rev025ClaimID, ClaimExpiresAt: values.NewInstant(rev025Now.Add(time.Hour)), SubmittedAt: values.NewInstant(rev025Now),
		OutputSchema: rev025NodeFixture.OutputSchema, CanonicalPayloadDigest: "sha256:" + strings.Repeat("9", 64),
		FormDefinition: rev025NodeFixture.FormDefinition, RenderContextDigest: "sha256:" + strings.Repeat("8", 64),
		ValidationEvidenceRef: "validation:reviewed", AccessibilityEvidenceRef: "ack:accessibility:reviewed",
		AccommodationEvidenceRef: "ack:accommodation:reviewed",
	}
}

var rev025AcceptDraft = func(_ string, _ []byte) error { return nil }

// TestTodo_REV_025_01 is the PRIMARY case: the served task intake boundary
// saves, interrupts, resumes and submits an encrypted form draft and
// establishes/routes an alternate-channel continuity record, then maps both
// typed records into a governed submission spec without moving decision
// authority to the assistant.
func TestTodo_REV_025_01(t *testing.T) {
	intake := steptask.NewFormIntake(rev025Store(t))

	saved, err := intake.SaveDraft(rev025SaveReq(0))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if saved.Revision != 1 || saved.PrincipalID != rev025Principal || saved.FormID != rev025NodeFixture.FormDefinition.Ref {
		t.Fatalf("saved draft = %#v, want revision 1 for %s on %s", saved, rev025Principal, rev025NodeFixture.FormDefinition.Ref)
	}
	sealed, err := intake.Drafts.Encrypted(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, rev025Answers) {
		t.Fatal("draft ciphertext contains plaintext answers")
	}

	// Interrupt: a fresh intake handle over the same store resumes the session.
	interrupted := steptask.NewFormIntake(intake.Drafts)
	resumed, err := interrupted.ResumeDraft(rev025ResumeReq(1))
	if err != nil {
		t.Fatalf("ResumeDraft: %v", err)
	}
	if resumed.Outcome != drafts.Current || !bytes.Equal(resumed.Answers, rev025Answers) {
		t.Fatalf("resumed = %#v, want CURRENT with intact answers", resumed)
	}

	updated, err := interrupted.SaveDraft(rev025SaveReq(1))
	if err != nil || updated.Revision != 2 {
		t.Fatalf("re-save = %#v, err %v, want revision 2", updated, err)
	}
	submitted, err := interrupted.SubmitDraft(rev025ResumeReq(2), rev025AcceptDraft)
	if err != nil {
		t.Fatalf("SubmitDraft: %v", err)
	}
	if submitted.Submission.DraftRevision != 2 || !submitted.Effects.IsZero() {
		t.Fatalf("draft submission = %#v, want revision 2 with zero effects", submitted)
	}
	if !bytes.Equal(submitted.Submission.Answers, rev025Answers) {
		t.Fatalf("draft submission answers = %q, want intact answers", submitted.Submission.Answers)
	}

	established, err := intake.EstablishAlternateRoute(rev025ContinuityReq())
	if err != nil {
		t.Fatalf("EstablishAlternateRoute: %v", err)
	}
	if established.Outcome != formcontinuity.OutcomeSafe {
		t.Fatalf("continuity outcome = %q, want SAFE_TO_CONTINUE", established.Outcome)
	}
	if !established.Deadline().Equal(rev025Now.Add(48 * time.Hour)) {
		t.Fatalf("continuity deadline moved: %v", established.Deadline())
	}
	if established.RespondentID != rev025Principal || established.AssistantID != rev025Assistant {
		t.Fatalf("continuity identity = respondent %q assistant %q, want respondent %q recorded with assistant as evidence",
			established.RespondentID, established.AssistantID, rev025Principal)
	}

	routed, err := intake.RouteAlternateChannel(rev025ContinuityReq())
	if err != nil {
		t.Fatalf("RouteAlternateChannel: %v", err)
	}
	if routed != established {
		t.Fatalf("route record = %#v, want establish record %#v", routed, established)
	}

	spec, err := steptask.AlternateSubmissionSpec(rev025BaseSpec(), updated, rev025Answers, established)
	if err != nil {
		t.Fatalf("AlternateSubmissionSpec: %v", err)
	}
	sum := sha256.Sum256(rev025Answers)
	if want := "sha256:" + hex.EncodeToString(sum[:]); spec.CanonicalPayloadDigest != want {
		t.Fatalf("payload digest = %q, want draft answers digest %q", spec.CanonicalPayloadDigest, want)
	}
	if spec.ValidationEvidenceRef != "sha256:evidence" || spec.AccommodationEvidenceRef != "transcription:1" {
		t.Fatalf("evidence refs = %q/%q, want continuity evidence/transcription", spec.ValidationEvidenceRef, spec.AccommodationEvidenceRef)
	}
	sub, err := steptask.NewSubmission(spec)
	if err != nil {
		t.Fatalf("NewSubmission from alternate spec: %v", err)
	}
	if err := sub.Verify(); err != nil {
		t.Fatalf("alternate submission verify: %v", err)
	}
	if sub.CompletedBy != rev025Principal {
		t.Fatalf("completed by = %q, want respondent %q: alternate route gained decision authority", sub.CompletedBy, rev025Principal)
	}

	// The assistant recorded on the continuity record can never complete.
	assisted := rev025BaseSpec()
	assisted.CompletedBy = rev025Assistant
	if _, err := steptask.AlternateSubmissionSpec(assisted, updated, rev025Answers, established); !errors.Is(err, steptask.ErrInvalidSubmission) {
		t.Fatalf("assistant completion error = %v, want ErrInvalidSubmission", err)
	}
	// A stranger cannot ride the respondent's draft and route either.
	foreign := rev025BaseSpec()
	foreign.CompletedBy = "principal:stranger"
	if _, err := steptask.AlternateSubmissionSpec(foreign, updated, rev025Answers, established); !errors.Is(err, steptask.ErrInvalidSubmission) {
		t.Fatalf("stranger completion error = %v, want ErrInvalidSubmission", err)
	}
	// Substituted answers do not match the sealed draft digest.
	if _, err := steptask.AlternateSubmissionSpec(rev025BaseSpec(), updated, []byte(`{"hours":9}`), established); !errors.Is(err, steptask.ErrBindingMismatch) {
		t.Fatalf("substituted answers error = %v, want ErrBindingMismatch", err)
	}
	// A draft for another form cannot back this node's submission.
	otherForm := updated
	otherForm.FormID = "form.other"
	if _, err := steptask.AlternateSubmissionSpec(rev025BaseSpec(), otherForm, rev025Answers, established); !errors.Is(err, steptask.ErrBindingMismatch) {
		t.Fatalf("foreign form error = %v, want ErrBindingMismatch", err)
	}
	// A blocked route produces no submission spec.
	blocked := established
	blocked.Outcome = formcontinuity.OutcomeBlocked
	if _, err := steptask.AlternateSubmissionSpec(rev025BaseSpec(), updated, rev025Answers, blocked); !errors.Is(err, steptask.ErrInvalidSubmission) {
		t.Fatalf("blocked route error = %v, want ErrInvalidSubmission", err)
	}
	// A self-service RTL route carries no accommodation evidence, so it stays
	// on the plain Submit path instead of the alternate-evidence mapping.
	rtlReq := rev025ContinuityReq()
	rtlReq.AlternateChannel = formcontinuity.ChannelRTL
	rtlReq.TranscriptionRef = ""
	rtlReq.ReadBackConfirmed = false
	rtl, err := intake.EstablishAlternateRoute(rtlReq)
	if err != nil {
		t.Fatalf("RTL establish: %v", err)
	}
	if _, err := steptask.AlternateSubmissionSpec(rev025BaseSpec(), updated, rev025Answers, rtl); !errors.Is(err, steptask.ErrInvalidSubmission) {
		t.Fatalf("RTL evidence error = %v, want ErrInvalidSubmission", err)
	}
	// No draft store, no draft path.
	bare := steptask.NewFormIntake(nil)
	if _, err := bare.SaveDraft(rev025SaveReq(0)); !errors.Is(err, steptask.ErrInvalidSubmission) {
		t.Fatalf("nil store save error = %v, want ErrInvalidSubmission", err)
	}
	if _, err := bare.ResumeDraft(rev025ResumeReq(1)); !errors.Is(err, steptask.ErrInvalidSubmission) {
		t.Fatalf("nil store resume error = %v, want ErrInvalidSubmission", err)
	}
	if _, err := bare.SubmitDraft(rev025ResumeReq(1), rev025AcceptDraft); !errors.Is(err, steptask.ErrInvalidSubmission) {
		t.Fatalf("nil store submit error = %v, want ErrInvalidSubmission", err)
	}
}

// TestTodo_REV_025_01_Integration drives the composed save-interrupt-resume
// plus accommodated-channel intake against the real encrypted draft store and
// the real continuity contract, then proves the resulting submission encodes
// and decodes with its digest intact.
func TestTodo_REV_025_01_Integration(t *testing.T) {
	store := rev025Store(t)
	intake := steptask.NewFormIntake(store)

	saved, err := intake.SaveDraft(rev025SaveReq(0))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	req := rev025ContinuityReq()
	req.AlternateChannel = formcontinuity.ChannelAccessible
	req.AccommodationRef = "accommodation:large-print"
	req.TranscriptionRef = ""
	req.ReadBackConfirmed = false
	rec, err := steptask.NewFormIntake(store).EstablishAlternateRoute(req)
	if err != nil {
		t.Fatalf("EstablishAlternateRoute: %v", err)
	}
	resumed, err := steptask.NewFormIntake(store).ResumeDraft(rev025ResumeReq(saved.Revision))
	if err != nil {
		t.Fatalf("ResumeDraft after interrupt: %v", err)
	}
	draftSub, err := steptask.NewFormIntake(store).SubmitDraft(rev025ResumeReq(saved.Revision), rev025AcceptDraft)
	if err != nil {
		t.Fatalf("SubmitDraft: %v", err)
	}
	if draftSub.Submission.DraftID != saved.ID {
		t.Fatalf("draft submission backs %q, want draft %q", draftSub.Submission.DraftID, saved.ID)
	}
	spec, err := steptask.AlternateSubmissionSpec(rev025BaseSpec(), resumed.Draft, resumed.Answers, rec)
	if err != nil {
		t.Fatalf("AlternateSubmissionSpec: %v", err)
	}
	if spec.AccommodationEvidenceRef != "accommodation:large-print" {
		t.Fatalf("accommodation evidence = %q, want the continuity accommodation ref", spec.AccommodationEvidenceRef)
	}
	sub, err := steptask.NewSubmission(spec)
	if err != nil {
		t.Fatalf("NewSubmission: %v", err)
	}
	body, err := steptask.EncodeSubmission(sub)
	if err != nil {
		t.Fatalf("EncodeSubmission: %v", err)
	}
	decoded, err := steptask.DecodeSubmission(body)
	if err != nil {
		t.Fatalf("DecodeSubmission: %v", err)
	}
	if decoded.Digest() != sub.Digest() {
		t.Fatalf("decoded digest = %q, want %q", decoded.Digest(), sub.Digest())
	}
	if decoded.CompletedBy != rev025Principal {
		t.Fatalf("decoded completed by = %q, want respondent %q", decoded.CompletedBy, rev025Principal)
	}
}

// TestTodo_REV_025_01_Golden pins the bytes the wired path produces: the
// continuity record for a fixed request and the encoded submission built from
// fixed draft evidence.
func TestTodo_REV_025_01_Golden(t *testing.T) {
	rec, err := steptask.NewFormIntake(rev025Store(t)).RouteAlternateChannel(rev025ContinuityReq())
	if err != nil {
		t.Fatalf("RouteAlternateChannel: %v", err)
	}
	wantRec := formcontinuity.Record{
		Outcome: formcontinuity.OutcomeSafe, FormID: "form.promotion.review", TaskVersion: "v1",
		Channel: formcontinuity.ChannelPhone, OriginalDeadline: rev025Now.Add(48 * time.Hour),
		RespondentID: rev025Principal, AssistantID: rev025Assistant, AuthorityRef: "authority:leave-2026",
		Purpose: "leave-request", Compartment: "hr:restricted",
		FormRevision: "leave:v3", SchemaDigest: "sha256:schema",
		TranscriptionRef: "transcription:1", ReadBackConfirmed: true,
		EvidenceDigest: "sha256:evidence", ResponseDigest: "sha256:response",
	}
	if rec != wantRec {
		t.Fatalf("continuity record = %#v, want %#v", rec, wantRec)
	}

	sum := sha256.Sum256(rev025Answers)
	draft := drafts.Draft{
		ID: "draft-leave-1", TenantID: "tenant-a", PrincipalID: rev025Principal,
		FormID: "form.promotion.review", FormVersion: "v1", Revision: 2,
		ExpiresAt: rev025Now.Add(time.Hour), AnswerDigest: sum,
	}
	spec, err := steptask.AlternateSubmissionSpec(rev025BaseSpec(), draft, rev025Answers, wantRec)
	if err != nil {
		t.Fatalf("AlternateSubmissionSpec: %v", err)
	}
	sub, err := steptask.NewSubmission(spec)
	if err != nil {
		t.Fatalf("NewSubmission: %v", err)
	}
	body, err := steptask.EncodeSubmission(sub)
	if err != nil {
		t.Fatalf("EncodeSubmission: %v", err)
	}
	const wantBody = `{"workflow_instance_id":"60000000-0000-0000-0000-000000000001","node_id":"review_task","work_item_id":"60000000-0000-0000-0000-000000000002","item_version":4,"completed_by":"principal:worker-1","candidate_via":"DIRECT","claim_id":"60000000-0000-0000-0000-000000000003","claim_expires_at":"2026-09-03T13:00:00Z","submitted_at":"2026-09-03T12:00:00Z","output_schema":{"schema_id":"hcmnext.task.promotion_review.Output","version":1,"protobuf_full_name":"hcmnext.task.promotion_review.Output"},"canonical_payload_digest":"sha256:d145b21faadd289b163aebf0cdce6f86acad7e8881e3c58beb24cdf65d20dabf","form_definition":{"Ref":"form.promotion.review","Version":1},"render_context_digest":"sha256:8888888888888888888888888888888888888888888888888888888888888888","validation_evidence_ref":"sha256:evidence","accessibility_evidence_ref":"ack:accessibility:reviewed","accommodation_evidence_ref":"transcription:1"}`
	if string(body) != wantBody {
		t.Fatalf("submission body = %s, want %s", body, wantBody)
	}
}
