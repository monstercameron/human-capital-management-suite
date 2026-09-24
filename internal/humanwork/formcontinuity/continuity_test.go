package formcontinuity

import (
	"errors"
	"testing"
	"time"
)

func validRequest() ContinuityRequest {
	return ContinuityRequest{
		FormID: "form:leave-1", TaskVersion: "v7", CanonicalChannel: ChannelWeb,
		AlternateChannel: ChannelPhone, OriginalDeadline: time.Date(2026, 9, 4, 17, 0, 0, 0, time.UTC), Now: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		Identity:         Identity{PrincipalID: "principal:worker-1", Assurance: "IAL2", AssistantID: "principal:interpreter-1"},
		Authority:        Authority{DecisionRight: "submit.leave", Scope: "worker:worker-1", AuthorityRef: "authority:leave-2026"},
		Attribution:      Attribution{RespondentID: "principal:worker-1", AssistantID: "principal:interpreter-1", TranscriberID: "principal:interpreter-1", ReadBackBy: "principal:worker-1"},
		Privacy:          Privacy{Purpose: "leave-request", Compartment: "hr:restricted", RedactionRule: "leave:v2"},
		Validation:       Validation{FormRevision: "leave:v3", SchemaDigest: "sha256:schema", ProofDigest: "sha256:proof"},
		TranscriptionRef: "transcription:1", ReadBackConfirmed: true, EvidenceDigest: "sha256:evidence", ResponseDigest: "sha256:response",
	}
}

func TestTodo_FORM_006(t *testing.T) {
	r, err := Establish(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != OutcomeSafe || !r.OriginalDeadline.Equal(validRequest().OriginalDeadline) {
		t.Fatalf("continuity changed outcome/deadline: %#v", r)
	}
	if r.RespondentID != "principal:worker-1" || r.AuthorityRef != "authority:leave-2026" {
		t.Fatalf("authority or identity was not retained: %#v", r)
	}
}

func TestTodo_FORM_006_Golden(t *testing.T) {
	r, err := Establish(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := ContinuityRecord{Outcome: OutcomeSafe, FormID: "form:leave-1", TaskVersion: "v7", Channel: ChannelPhone, OriginalDeadline: validRequest().OriginalDeadline, RespondentID: "principal:worker-1", AssistantID: "principal:interpreter-1", AuthorityRef: "authority:leave-2026", Purpose: "leave-request", Compartment: "hr:restricted", FormRevision: "leave:v3", SchemaDigest: "sha256:schema", TranscriptionRef: "transcription:1", ReadBackConfirmed: true, EvidenceDigest: "sha256:evidence", ResponseDigest: "sha256:response"}
	if r != want {
		t.Fatalf("record changed: got %#v want %#v", r, want)
	}
}

func TestTodo_FORM_006_Fault(t *testing.T) {
	for name, mutate := range map[string]func(*ContinuityRequest){"expired": func(r *ContinuityRequest) { r.Now = r.OriginalDeadline }, "missing-readback": func(r *ContinuityRequest) { r.ReadBackConfirmed = false }, "identity-drift": func(r *ContinuityRequest) { r.Attribution.RespondentID = "principal:other" }} {
		t.Run(name, func(t *testing.T) {
			_, err := Establish(func() ContinuityRequest { r := validRequest(); mutate(&r); return r }())
			if err == nil {
				t.Fatal("expected refusal")
			}
		})
	}
}

func TestTodo_FORM_006_Security(t *testing.T) {
	r := validRequest()
	r.Identity.AssistantID = "principal:assistant"
	r.Attribution.AssistantID = "principal:assistant"
	r.Attribution.RespondentID = "principal:assistant"
	_, err := Establish(r)
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("assistant must not become respondent, got %v", err)
	}
}

func TestTodo_FORM_006_Browser(t *testing.T) { // browser/manual matrix shares the same server contract.
	for _, c := range []Channel{ChannelRTL, ChannelAccessible, ChannelInPerson, ChannelPostal} {
		r := validRequest()
		r.AlternateChannel = c
		if c == ChannelRTL {
			r.TranscriptionRef = ""
			r.ReadBackConfirmed = false
		}
		if c == ChannelAccessible {
			r.AccommodationRef = "accommodation:large-print"
		}
		if _, err := Establish(r); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
	}
}

func TestTodo_FORM_006_Integration(t *testing.T) {
	in := validRequest()
	record, err := Route(in)
	if err != nil {
		t.Fatal(err)
	}
	if record.Outcome != OutcomeSafe || record.Channel != ChannelPhone || record.Deadline() != in.OriginalDeadline || record.AuthorityRef != in.Authority.AuthorityRef || record.RespondentID != in.Identity.PrincipalID {
		t.Fatalf("alternate route did not preserve canonical contract: %+v", record)
	}
}

func TestTodo_FORM_006_Mutation(t *testing.T) {
	in := validRequest()
	record, err := Establish(in)
	if err != nil {
		t.Fatal(err)
	}
	in.AlternateChannel = ChannelPostal
	in.OriginalDeadline = in.OriginalDeadline.Add(24 * time.Hour)
	in.Identity.PrincipalID = "principal:changed"
	in.Validation.SchemaDigest = "sha256:changed"
	if record.Channel != ChannelPhone || record.OriginalDeadline != validRequest().OriginalDeadline || record.RespondentID != "principal:worker-1" || record.SchemaDigest != "sha256:schema" {
		t.Fatalf("established record changed with caller input: %+v", record)
	}
}

func FuzzTodo_FORM_006(f *testing.F) {
	f.Add("principal:worker-1", "authority:leave-2026")
	f.Fuzz(func(t *testing.T, principal, authority string) {
		r := validRequest()
		r.Identity.PrincipalID, r.Attribution.RespondentID, r.Authority.AuthorityRef = principal, principal, authority
		_, _ = Establish(r)
	})
}
