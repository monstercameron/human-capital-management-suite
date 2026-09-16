package promotionterminal

// WF-RUN-034: the material rules the resolver applies before it touches the
// database -- which effective start a revision names, which manager an
// approval pinned, and which outbox legs its approved material authorizes.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustLocalDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

// TestEffectiveStartAcceptsBothApprovedIntervalKinds proves the commit's
// business instant is the same day-aligned coordinate whether the approved
// revision carries a local-date interval (the typed propose path) or an
// instant interval (the served journey path), and that a revision naming
// neither is refused.
func TestEffectiveStartAcceptsBothApprovedIntervalKinds(t *testing.T) {
	want := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

	dated, err := values.NewOpenLocalDateInterval(mustLocalDate(t, "2026-10-15"), values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := EffectiveStart(dated); err != nil || !got.Equal(want) {
		t.Fatalf("local-date interval = %s, %v; want %s", got, err, want)
	}

	instant, err := values.NewOpenInstantInterval(values.NewInstant(time.Date(2026, 10, 15, 13, 45, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := EffectiveStart(instant); err != nil || !got.Equal(want) {
		t.Fatalf("instant interval = %s, %v; want the same day-aligned %s", got, err, want)
	}

	if _, err := EffectiveStart(values.EffectiveInterval{}); !errors.Is(err, ErrPlanBinding) {
		t.Fatalf("interval naming no start = %v, want ErrPlanBinding", err)
	}
}

// TestUnchangedAssertionAcceptsOnlyAPinnedPair proves an unchanged manager is
// read from an identical current/proposed assertion pair, and that a
// one-sided, disagreeing or duplicated assertion pins nothing.
func TestUnchangedAssertionAcceptsOnlyAPinnedPair(t *testing.T) {
	manager := uuid.NewString()
	assert := func(field, text string) intent.StateAssertion {
		return intent.StateAssertion{FieldPath: field, CanonicalText: text}
	}
	pinned := intent.ProposalRevision{
		CurrentState:  []intent.StateAssertion{assert(resolveManagerIDFieldPath, manager)},
		ProposedState: []intent.StateAssertion{assert(resolveManagerIDFieldPath, manager)},
	}
	if got := unchangedAssertion(pinned, resolveManagerIDFieldPath); got != manager {
		t.Fatalf("pinned manager = %q, want %s", got, manager)
	}
	for name, revision := range map[string]intent.ProposalRevision{
		"only proposed": {ProposedState: []intent.StateAssertion{assert(resolveManagerIDFieldPath, manager)}},
		"only current":  {CurrentState: []intent.StateAssertion{assert(resolveManagerIDFieldPath, manager)}},
		"disagreeing": {
			CurrentState:  []intent.StateAssertion{assert(resolveManagerIDFieldPath, manager)},
			ProposedState: []intent.StateAssertion{assert(resolveManagerIDFieldPath, uuid.NewString())},
		},
		"asserted twice": {
			CurrentState: []intent.StateAssertion{assert(resolveManagerIDFieldPath, manager), assert(resolveManagerIDFieldPath, manager)},
			ProposedState: []intent.StateAssertion{
				assert(resolveManagerIDFieldPath, manager), assert(resolveManagerIDFieldPath, manager),
			},
		},
		"nothing asserted": {},
	} {
		if got := unchangedAssertion(revision, resolveManagerIDFieldPath); got != "" {
			t.Fatalf("%s pinned %q, want nothing", name, got)
		}
	}
}

// TestMatchApprovedEffectsAuthorizesFromEffectsOrWrites proves the terminal
// may render its two outbox legs only when approval authorized them: by the
// declared effect kinds, or -- for a revision whose definition may declare no
// effect at all -- by the approved pay and placement writes themselves.
func TestMatchApprovedEffectsAuthorizesFromEffectsOrWrites(t *testing.T) {
	rendered := renderEffects(uuid.New())
	if len(rendered) != 2 {
		t.Fatalf("rendered effects = %d, want the payroll and IAM syncs", len(rendered))
	}
	for _, effect := range rendered {
		if !strings.Contains(strings.Join(EffectSchemaRefs(), " "), effect.SchemaRef) {
			t.Fatalf("effect %s names schema %q, which the composition never registers", effect.EffectID, effect.SchemaRef)
		}
	}

	declared := intent.ProposalRevision{Effects: []intent.PlannedEffect{
		{EffectID: "payroll", Kind: resolveCompensationRevisionKind},
		{EffectID: "iam", Kind: resolveAssignmentRevisionKind},
	}}
	if err := matchApprovedEffects(declared, rendered); err != nil {
		t.Fatalf("declared effect kinds: %v", err)
	}

	write := func(field string) intent.PlannedWrite { return intent.PlannedWrite{FieldPath: field} }
	written := intent.ProposalRevision{Writes: []intent.PlannedWrite{
		write(resolvePayFieldPath), write(resolvePlacementJobCodePath),
	}}
	if err := matchApprovedEffects(written, rendered); err != nil {
		t.Fatalf("approved pay and placement writes: %v", err)
	}
	byGrade := intent.ProposalRevision{Writes: []intent.PlannedWrite{
		write(resolvePayFieldPath), write(resolvePlacementGradePath),
	}}
	if err := matchApprovedEffects(byGrade, rendered); err != nil {
		t.Fatalf("approved pay and grade writes: %v", err)
	}

	for name, revision := range map[string]intent.ProposalRevision{
		"no pay change":       {Writes: []intent.PlannedWrite{write(resolvePlacementJobCodePath)}},
		"no placement change": {Writes: []intent.PlannedWrite{write(resolvePayFieldPath)}},
		"nothing approved":    {},
	} {
		if err := matchApprovedEffects(revision, rendered); !errors.Is(err, ErrPlanBinding) {
			t.Fatalf("%s = %v, want ErrPlanBinding", name, err)
		}
	}
	if err := matchApprovedEffects(declared, rendered[:1]); !errors.Is(err, ErrPlanBinding) {
		t.Fatalf("a single rendered effect = %v, want ErrPlanBinding", err)
	}
	var _ []domaincommit.ExternalEffect = rendered
}

// TestExecutionSubmissionIDNamesTheInstance proves the submission identity a
// commit attests when the served plan routed no human task is derived from
// the instance, so two runs never share one.
func TestExecutionSubmissionIDNamesTheInstance(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	if ExecutionSubmissionID(first) == ExecutionSubmissionID(second) {
		t.Fatal("two instances share one execution submission identity")
	}
	if got := ExecutionSubmissionID(first); !strings.HasSuffix(got, first.String()) {
		t.Fatalf("execution submission = %q, want it to name the instance", got)
	}
}
