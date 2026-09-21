package app

import (
	"strings"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// UXLIVE-003's RED was measured on the running server: a proposal that
// carried a picker-issued target position rendered the whole 299-character
// encoded revision reference as the Position row's Proposed value, which
// pushed the current-versus-proposed table to 3096px inside an 881px
// wrapper and carried the Change column off-screen.
//
// The encoded reference is the right thing to bind durably and the wrong
// thing to print: it names a position the reader cannot read. The journey
// summary now decodes it to the position it names, exactly as
// journeySubjects already does for the intent's material subject.

const uxlive003PositionID = "c1a6b1fe-574b-5e6b-a1b0-c4961da9d302"

func uxlive003Ref(t *testing.T) string {
	t.Helper()
	entity := values.EntityRef{Tenant: fixtures.Tenant, Kind: position.KindPosition, Id: uxlive003PositionID}
	revision, err := values.NewOpaqueRevision("aggregate.job_position."+uxlive003PositionID, []byte("uxlive-003-target-position"))
	if err != nil {
		t.Fatalf("mint revision: %v", err)
	}
	encoded, err := position.EncodeRevisionRef(entity, revision)
	if err != nil {
		t.Fatalf("encode revision ref: %v", err)
	}
	return encoded.String()
}

func uxlive003Summary(t *testing.T, targetPositionID string) string {
	t.Helper()
	proposal := journeyProposalFixture()
	proposal.TargetPositionID = targetPositionID
	baseline, err := journeyBaseline(proposal, journeyCorpusSubject(t))
	if err != nil {
		t.Fatalf("journeyBaseline: %v", err)
	}
	wire, err := journeyRequestPayload(proposal, "omar-reyes", journeyCurrent{
		name: "Omar", jobCode: "OPS-HRBP2", grade: "P2",
		orgUnit: "people-ops", positionID: "POS-HRBP-204", payZone: "US-EAST",
	}, baseline)
	if err != nil {
		t.Fatalf("journeyRequestPayload: %v", err)
	}
	summary, err := journeySummaryFromProto(&intentsv1.IntentInstance{
		IntentId: "01a0b189-e04c-7265-8926-909531dd6530",
		TenantId: string(fixtures.Tenant),
		Request:  &intentsv1.TypedPayload{ProtobufWireBytes: wire},
	})
	if err != nil {
		t.Fatalf("journeySummaryFromProto: %v", err)
	}
	return summary.Target.PositionID
}

// TestTodo_UXLIVE_003 is the primary red/green test: the journey summary
// must name the position, never its encoded revision reference.
func TestTodo_UXLIVE_003(t *testing.T) {
	ref := uxlive003Ref(t)
	if len(ref) < 100 || !strings.Contains(ref, ".") {
		t.Fatalf("fixture is not a realistic encoded reference: %q", ref)
	}

	got := uxlive003Summary(t, ref)
	if got == ref {
		t.Fatalf("journey summary still carries the encoded reference as the position: %q", got)
	}
	if got != uxlive003PositionID {
		t.Fatalf("position = %q, want the position it names %q", got, uxlive003PositionID)
	}
}

// TestTodo_UXLIVE_003_Golden pins the shape a reader sees: a value short
// enough to sit in a table cell, and a value the decoder does not
// understand left exactly as the intent carried it rather than replaced.
func TestTodo_UXLIVE_003_Golden(t *testing.T) {
	cases := []struct {
		name  string
		given string
		want  string
	}{
		{name: "encoded reference", given: uxlive003Ref(t), want: uxlive003PositionID},
		{name: "plain position code", given: "POS-HRBP-301", want: "POS-HRBP-301"},
		{name: "undecodable", given: "not-a-reference.at-all", want: "not-a-reference.at-all"},
	}
	for _, c := range cases {
		got := uxlive003Summary(t, c.given)
		if got != c.want {
			t.Fatalf("%s: position = %q, want %q", c.name, got, c.want)
		}
		if len(got) > 64 {
			t.Fatalf("%s: position value is %d characters, too long for a table cell", c.name, len(got))
		}
	}
}

// TestTodo_UXLIVE_003_Security proves the decode never widens what the page
// discloses: it names exactly the position the intent's own governed
// subject names, and carries no tenant or revision material out of the
// reference.
func TestTodo_UXLIVE_003_Security(t *testing.T) {
	ref := uxlive003Ref(t)
	shown := uxlive003Summary(t, ref)

	var subjectPosition string
	for _, subject := range journeySubjects("omar-reyes", ref) {
		if subject.GetSubjectKind() == "POSITION" {
			subjectPosition = subject.GetSubjectId()
		}
	}
	if subjectPosition != uxlive003PositionID {
		t.Fatalf("subject position = %q, want %q", subjectPosition, uxlive003PositionID)
	}
	if shown != subjectPosition {
		t.Fatalf("displayed position %q differs from the governed subject %q", shown, subjectPosition)
	}
	for _, leaked := range []string{string(fixtures.Tenant), "aggregate.job_position", "uxlive-003-target-position"} {
		if strings.Contains(shown, leaked) {
			t.Fatalf("displayed position leaks %q: %q", leaked, shown)
		}
	}
}
