package productquery_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func actionTestEnvelope() productquery.Envelope {
	return productquery.Envelope{
		ContractVersion: productquery.Version(),
		Tenant:          values.TenantId("acme"),
		Purpose:         authz.PurposeCompensationReview,
		Projection: productquery.Projection{
			Name: "worker_summary", DefinitionVersion: "worker-summary.v3",
			SchemaVersion: "schema.v5", SourceSequence: 12, Watermark: 10,
			ObservedAt: testNow.Add(-time.Minute), MaxAge: 5 * time.Minute,
		},
		Freshness: productquery.FreshnessCurrent,
		Rows: []productquery.Row{{
			Subject: subject("acme", "00000000-0000-4000-8000-000000000001"),
			Fields: []productquery.Field{
				{ID: authz.FieldWorkerNumber, Disposition: authz.EffectAllow, State: productquery.ValuePresent, Value: "W-1"},
				{ID: authz.FieldBaseSalary, Disposition: authz.EffectRedacted, State: productquery.ValueRedacted},
			},
		}},
	}
}

// TestTodo_ALIGN_021 proves a product response projects exactly the
// semantic actions currently available: row views for disclosed values, an
// authoritative refetch for a stale projection, and nothing for an empty
// response.
func TestTodo_ALIGN_021(t *testing.T) {
	env := actionTestEnvelope()
	set, err := productquery.AvailableActions(env, testNow)
	if err != nil {
		t.Fatalf("AvailableActions: %v", err)
	}
	if err := set.VerifyActions(); err != nil {
		t.Fatalf("VerifyActions: %v", err)
	}
	if len(set.Actions) != 1 || set.Actions[0].ID != productquery.ActionViewRow {
		t.Fatalf("actions = %+v, want exactly one row view", set.Actions)
	}
	if set.Actions[0].Subject != subject("acme", "00000000-0000-4000-8000-000000000001").String() {
		t.Fatalf("view subject = %q", set.Actions[0].Subject)
	}
	// A stale projection offers only an authoritative refetch.
	stale := env
	stale.Freshness = productquery.FreshnessStale
	staleSet, err := productquery.AvailableActions(stale, testNow.Add(time.Hour))
	if err != nil {
		t.Fatalf("AvailableActions(stale): %v", err)
	}
	if len(staleSet.Actions) != 1 || staleSet.Actions[0].ID != productquery.ActionRefetch {
		t.Fatalf("stale actions = %+v, want exactly one refetch", staleSet.Actions)
	}
	if staleSet.Actions[0].Projection != "worker_summary" {
		t.Fatalf("refetch projection = %q", staleSet.Actions[0].Projection)
	}
	// An empty (fully unauthorized) response offers nothing.
	empty := env
	empty.Rows = nil
	emptySet, err := productquery.AvailableActions(empty, testNow)
	if err != nil {
		t.Fatalf("AvailableActions(empty): %v", err)
	}
	if len(emptySet.Actions) != 0 {
		t.Fatalf("empty actions = %+v, want none", emptySet.Actions)
	}
}

func TestTodo_ALIGN_021_Property(t *testing.T) {
	env := actionTestEnvelope()
	first, err := productquery.AvailableActions(env, testNow)
	if err != nil {
		t.Fatal(err)
	}
	second, err := productquery.AvailableActions(env, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("action projection is not deterministic: %s != %s", first.Digest, second.Digest)
	}
	reversed := env
	reversed.Rows[0].Fields = []productquery.Field{env.Rows[0].Fields[1], env.Rows[0].Fields[0]}
	third, err := productquery.AvailableActions(reversed, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if third.Digest != first.Digest {
		t.Fatalf("field order changed action digest: %s != %s", third.Digest, first.Digest)
	}
}

func TestTodo_ALIGN_021_Golden(t *testing.T) {
	env := actionTestEnvelope()
	set, err := productquery.AvailableActions(env, testNow)
	if err != nil {
		t.Fatalf("AvailableActions: %v", err)
	}
	const wantDigest = "d320a173654ab9d91492268f0d7e0662b0c0edf17a91d3e42926fc368534d3ea"
	if set.Digest != wantDigest {
		t.Fatalf("action set digest=%q want=%q", set.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_021_Security(t *testing.T) {
	env := actionTestEnvelope()
	// A tampered freshness label yields no actions.
	tampered := env
	tampered.Freshness = productquery.FreshnessStale
	if _, err := productquery.AvailableActions(tampered, testNow); !errors.Is(err, productquery.ErrFreshnessMismatch) {
		t.Fatalf("AvailableActions(tampered) = %v, want ErrFreshnessMismatch", err)
	}
	// A cross-tenant envelope yields no actions.
	foreign := env
	foreign.Rows = []productquery.Row{{Subject: subject("other", "00000000-0000-4000-8000-000000000002")}}
	if _, err := productquery.AvailableActions(foreign, testNow); err == nil {
		t.Fatal("cross-tenant envelope projected actions")
	}
	// A metadata-only (fully redacted) row offers no view: there is nothing
	// to see, only presence the envelope already discloses.
	redactedOnly := env
	redactedOnly.Rows = []productquery.Row{{
		Subject: subject("acme", "00000000-0000-4000-8000-000000000001"),
		Fields: []productquery.Field{
			{ID: authz.FieldBaseSalary, Disposition: authz.EffectRedacted, State: productquery.ValueRedacted},
		},
	}}
	redactedSet, err := productquery.AvailableActions(redactedOnly, testNow)
	if err != nil {
		t.Fatalf("AvailableActions(redacted-only): %v", err)
	}
	if len(redactedSet.Actions) != 0 {
		t.Fatalf("redacted-only actions = %+v, want none", redactedSet.Actions)
	}
	// An unbound or unknown action fails verification.
	set, err := productquery.AvailableActions(env, testNow)
	if err != nil {
		t.Fatal(err)
	}
	forged := set
	forged.Actions = []productquery.SemanticAction{{ID: productquery.ActionViewRow, Subject: "acme:worker:ghost", EnvelopeDigest: "sha256:forged"}}
	if err := forged.VerifyActions(); err == nil {
		t.Fatal("unbound action verified")
	}
}

func TestTodo_ALIGN_021_Conformance(t *testing.T) {
	env := actionTestEnvelope()
	set, err := productquery.AvailableActions(env, testNow)
	if err != nil {
		t.Fatalf("AvailableActions: %v", err)
	}
	// Every projected view names a row the envelope discloses, and the set
	// digest covers the envelope it was projected from.
	rows := make(map[string]bool, len(env.Rows))
	for _, row := range env.Rows {
		rows[row.Subject.String()] = true
	}
	for _, action := range set.Actions {
		if action.ID == productquery.ActionViewRow && !rows[action.Subject] {
			t.Fatalf("view action names undisclosed subject %q", action.Subject)
		}
		if action.EnvelopeDigest != env.Digest() {
			t.Fatalf("action is not bound to this envelope: %+v", action)
		}
	}
	if set.Envelope != env.Digest() {
		t.Fatalf("action set envelope=%q want %q", set.Envelope, env.Digest())
	}
	// A live Project output projects a consistent action set at its own
	// observation instant.
	live := freshEnvelope(t)
	liveSet, err := productquery.AvailableActions(live, testNow)
	if err != nil {
		t.Fatalf("AvailableActions(live): %v", err)
	}
	if err := liveSet.VerifyActions(); err != nil {
		t.Fatalf("VerifyActions(live): %v", err)
	}
}

func FuzzTodo_ALIGN_021_Fuzz(f *testing.F) {
	f.Add(int64(0))
	f.Fuzz(func(t *testing.T, skewSeconds int64) {
		env := actionTestEnvelope()
		now := testNow.Add(time.Duration(skewSeconds%7200) * time.Second)
		first, firstErr := productquery.AvailableActions(env, now)
		second, secondErr := productquery.AvailableActions(env, now)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("projection is not deterministic: %v vs %v", firstErr, secondErr)
		}
		if firstErr == nil && first.Digest != second.Digest {
			t.Fatalf("action digest is not deterministic: %s != %s", first.Digest, second.Digest)
		}
	})
}
