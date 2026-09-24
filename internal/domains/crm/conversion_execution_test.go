package crm

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type conversionAuthorityFixture struct {
	err   error
	calls int
}

func (a *conversionAuthorityFixture) AuthorizeProspectConversion(_ context.Context, _ CandidateConversionProposal) error {
	a.calls++
	return a.err
}

type conversionWriterFixture struct {
	writes []ConversionWrite
	result ConversionResult
	err    error
}

func (w *conversionWriterFixture) CommitProspectConversion(_ context.Context, in ConversionWrite) (ConversionResult, error) {
	w.writes = append(w.writes, in)
	return w.result, w.err
}

func TestTodo_REV_046_01(t *testing.T) {
	p := validProspect(t)
	at := conversionAt(t, "2026-01-03T00:00:00Z")
	proposal, _, err := PrepareProspectConversion(p, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), CRM005IntentType, CRM005IntentVersion, at)
	if err != nil {
		t.Fatal(err)
	}
	authority := &conversionAuthorityFixture{}
	writer := &conversionWriterFixture{result: ConversionResult{Candidate: proposal.ProposedCandidate, Application: proposal.ProposedApplication, IdentityLink: proposal.ProposedIdentityLink}}
	fence := ConversionCommitFence{ProspectRevision: p.Revision, Token: "fence-1"}
	first, err := ExecuteProspectConversion(context.Background(), proposal, authority, writer, fence, "conversion-1")
	if err != nil || first.Replayed || authority.calls != 1 || len(writer.writes) != 1 {
		t.Fatalf("first conversion=%+v authority=%d writes=%d err=%v", first, authority.calls, len(writer.writes), err)
	}
	replay := writer.result
	replay.Replayed = true
	writer.result = replay
	second, err := ExecuteProspectConversion(context.Background(), proposal, authority, writer, fence, "conversion-1")
	if err != nil || !second.Replayed || len(writer.writes) != 2 || writer.writes[0].IdempotencyKey != writer.writes[1].IdempotencyKey {
		t.Fatalf("replay=%+v writes=%d err=%v", second, len(writer.writes), err)
	}
}

func TestTodo_REV_046_01_Security(t *testing.T) {
	p := validProspect(t)
	proposal, _, err := PrepareProspectConversion(p, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), CRM005IntentType, CRM005IntentVersion, conversionAt(t, "2026-01-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	authority := &conversionAuthorityFixture{err: errors.New("denied")}
	writer := &conversionWriterFixture{}
	if _, err := ExecuteProspectConversion(context.Background(), proposal, authority, writer, ConversionCommitFence{ProspectRevision: p.Revision, Token: "fence-1"}, "key"); !errors.Is(err, ErrCRM005Rejected) || authority.calls != 1 || len(writer.writes) != 0 {
		t.Fatalf("denied authorization err=%v authority=%d writes=%d", err, authority.calls, len(writer.writes))
	}
	authority.err = nil
	staleRevision, revisionErr := values.NewSequenceRevision("crm", 999)
	if revisionErr != nil {
		t.Fatal(revisionErr)
	}
	badFence := ConversionCommitFence{ProspectRevision: staleRevision, Token: "fence-2"}
	if _, err := ExecuteProspectConversion(context.Background(), proposal, authority, writer, badFence, "key"); !errors.Is(err, ErrCRM005Rejected) || len(writer.writes) != 0 {
		t.Fatalf("stale fence err=%v writes=%d", err, len(writer.writes))
	}
	altered := proposal
	altered.Source.Campaign = "forged"
	if _, err := ExecuteProspectConversion(context.Background(), altered, authority, writer, ConversionCommitFence{ProspectRevision: p.Revision, Token: "fence-1"}, "key"); !errors.Is(err, ErrCRM005Rejected) || len(writer.writes) != 0 {
		t.Fatalf("altered proposal err=%v writes=%d", err, len(writer.writes))
	}
	var typedNilAuthority *conversionAuthorityFixture
	if _, err := ExecuteProspectConversion(context.Background(), proposal, typedNilAuthority, writer, ConversionCommitFence{ProspectRevision: p.Revision, Token: "fence-1"}, "key"); !errors.Is(err, ErrCRM005Rejected) || len(writer.writes) != 0 {
		t.Fatalf("typed nil authority err=%v writes=%d", err, len(writer.writes))
	}
}

func TestTodo_REV_046_01_Mutation(t *testing.T) {
	p := validProspect(t)
	proposal, _, err := PrepareProspectConversion(p, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), CRM005IntentType, CRM005IntentVersion, conversionAt(t, "2026-01-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	proposal.Prospect.Attribution.Campaign = "forged"
	authority := &conversionAuthorityFixture{}
	writer := &conversionWriterFixture{}
	if _, err := ExecuteProspectConversion(context.Background(), proposal, authority, writer, ConversionCommitFence{ProspectRevision: p.Revision, Token: "fence"}, "key"); !errors.Is(err, ErrCRM005Rejected) || len(writer.writes) != 0 {
		t.Fatalf("mutated prospect accepted: err=%v writes=%d", err, len(writer.writes))
	}
}
