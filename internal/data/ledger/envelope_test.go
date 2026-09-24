package ledger_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// storedEvent is one persisted envelope, read back for inspection.
type storedEvent struct {
	assertionClass   string
	authorityRef     *string
	sourceRef        string
	schemaRef        string
	payload          []byte
	artifactRef      *string
	canonicalLength  int
	digest           string
	digestAlgorithm  string
	occurredAt       time.Time
	effectiveAt      time.Time
	recordedAt       time.Time
	correlationID    uuid.UUID
	causationID      *uuid.UUID
	correctsStream   *string
	correctsSequence *int64
}

func (f fixture) read(t *testing.T, sequence int64) storedEvent {
	t.Helper()
	var e storedEvent
	if err := f.db.QueryRow(context.Background(), `
		SELECT assertion_class, authority_ref, source_ref, schema_ref, payload, artifact_ref,
		       canonical_length, digest, digest_algorithm, occurred_at, effective_at, recorded_at,
		       correlation_id, causation_id, corrects_stream_key, corrects_sequence
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		f.tenant, streamKey, sequence).Scan(
		&e.assertionClass, &e.authorityRef, &e.sourceRef, &e.schemaRef, &e.payload, &e.artifactRef,
		&e.canonicalLength, &e.digest, &e.digestAlgorithm, &e.occurredAt, &e.effectiveAt, &e.recordedAt,
		&e.correlationID, &e.causationID, &e.correctsStream, &e.correctsSequence); err != nil {
		t.Fatalf("read event %d: %v", sequence, err)
	}
	return e
}

// TestTodo_DATA_003 proves the canonical ledger envelope is persisted whole: the
// digest is computed by the ledger over the exact payload and schema, the
// assertion class is explicit, and tenant, stream, sequence, correlation,
// causation, the three times and the schema reference are all recorded.
func TestTodo_DATA_003(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	causation := uuid.New()
	req := f.request(0)
	req.CausationID = causation
	receipt := f.mustAppend(t, req)

	stored := f.read(t, receipt.Sequence)

	if stored.assertionClass != string(ledger.TransactionFact) {
		t.Fatalf("assertion class is %q, want %q", stored.assertionClass, ledger.TransactionFact)
	}
	if stored.sourceRef != req.SourceRef || stored.schemaRef != req.SchemaRef {
		t.Fatalf("provenance is %s/%s, want %s/%s", stored.sourceRef, stored.schemaRef, req.SourceRef, req.SchemaRef)
	}
	if string(stored.payload) != string(req.Payload) {
		t.Fatalf("payload is %q, want %q", stored.payload, req.Payload)
	}
	if stored.canonicalLength != len(req.Payload) {
		t.Fatalf("canonical length is %d, want %d", stored.canonicalLength, len(req.Payload))
	}
	if stored.correlationID != req.CorrelationID {
		t.Fatalf("correlation is %s, want %s", stored.correlationID, req.CorrelationID)
	}
	if stored.causationID == nil || *stored.causationID != causation {
		t.Fatalf("causation is %v, want %s", stored.causationID, causation)
	}
	if !stored.occurredAt.Equal(req.OccurredAt) || !stored.effectiveAt.Equal(req.EffectiveAt) {
		t.Fatalf("times are occurred=%s effective=%s, want %s and %s",
			stored.occurredAt, stored.effectiveAt, req.OccurredAt, req.EffectiveAt)
	}
	if !stored.recordedAt.After(stored.occurredAt) {
		t.Fatalf("recorded_at %s is not after occurred_at %s", stored.recordedAt, stored.occurredAt)
	}

	// The digest is the ledger's, computed over the exact bytes and schema.
	_, want, _, err := ledger.SHA256Digester{}.Digest(req.Payload, req.SchemaRef)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if stored.digest != want || stored.digestAlgorithm != ledger.Algorithm {
		t.Fatalf("stored digest is %s:%s, want %s:%s",
			stored.digestAlgorithm, stored.digest, ledger.Algorithm, want)
	}
	if receipt.Digest != want {
		t.Fatalf("receipt digest is %s, want %s", receipt.Digest, want)
	}

	// Altering one byte of the payload is a different fact.
	altered := f.request(1)
	altered.Payload = []byte("promotion-proposeD")
	alteredReceipt := f.mustAppend(t, altered)
	if alteredReceipt.Digest == receipt.Digest {
		t.Fatal("altering the payload did not change the canonical digest")
	}
}

// TestTodo_DATA_003_Security proves the digest cannot be supplied or bypassed
// and that an injected digester is the only way to change it.
func TestTodo_DATA_003_Security(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// A caller-controlled digester is the seam, and it is explicit.
	appender := ledger.New(ledger.WithDigester(constantDigester{}))
	var receipt ledger.AppendReceipt
	err := f.inTxErr(f.db.Conn, func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = appender.Append(context.Background(), tx, f.request(0))
		return appendErr
	})
	if err != nil {
		t.Fatalf("append with an injected digester: %v", err)
	}
	if receipt.Digest != constantDigest {
		t.Fatalf("receipt digest is %s, want the injected %s", receipt.Digest, constantDigest)
	}

	// A digester that fails stops the append; nothing is written.
	failing := ledger.New(ledger.WithDigester(failingDigester{}))
	err = f.inTxErr(f.db.Conn, func(tx dbport.Tx) error {
		_, appendErr := failing.Append(context.Background(), tx, f.request(1))
		return appendErr
	})
	if err == nil {
		t.Fatal("an append whose digest could not be computed succeeded")
	}
	if got := f.sequences(t); len(got) != 1 {
		t.Fatalf("stream holds %v after a failed digest, want one event", got)
	}
}

// TestTodo_DATA_003_Golden pins the receipt a fixed request produces.
func TestTodo_DATA_003_Golden(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	clock := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	appender := ledger.New(ledger.WithClock(func() time.Time { return clock }))

	req := f.request(0)
	req.IdempotencyKey = "golden-key"

	var receipt ledger.AppendReceipt
	if err := f.inTxErr(f.db.Conn, func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = appender.Append(context.Background(), tx, req)
		return appendErr
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if receipt.Sequence != 1 || receipt.PreviousHead != 0 {
		t.Fatalf("receipt is sequence %d after head %d, want 1 after 0", receipt.Sequence, receipt.PreviousHead)
	}
	if !receipt.RecordedAt.Equal(clock) {
		t.Fatalf("receipt recorded at %s, want the injected %s", receipt.RecordedAt, clock)
	}
	if receipt.CanonicalLength != len(req.Payload) {
		t.Fatalf("receipt canonical length is %d, want %d", receipt.CanonicalLength, len(req.Payload))
	}
	if receipt.Tenant != f.tenant || receipt.StreamKey != streamKey {
		t.Fatalf("receipt names %s/%s, want %s/%s", receipt.Tenant, receipt.StreamKey, f.tenant, streamKey)
	}
	if receipt.Replayed {
		t.Fatal("a first append reported itself as a replay")
	}
}

// TestTodo_LEDGER_004 proves assertion authority and typed payload references:
// an external observation cannot become a domain fact without an authority
// assignment, a correction must name its target, and large bytes must be an
// artifact reference.
func TestTodo_LEDGER_004(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	t.Run("a domain fact needs an authority assignment", func(t *testing.T) {
		// The observation itself is fine: it says only that a source reported it.
		observation := f.request(0)
		observation.AssertionClass = ledger.ExternalObservation
		observation.Authority = authorityRef
		f.mustAppend(t, observation)

		// The same content asserted as a domain fact without any authority is not.
		promoted := f.request(1)
		promoted.AssertionClass = ledger.DomainFact
		_, err := f.append(t, promoted)
		var unassigned ledger.ErrAuthorityNotAssigned
		if !errors.As(err, &unassigned) {
			t.Fatalf("a domain fact without an authority returned %v, want ErrAuthorityNotAssigned", err)
		}

		// Nor is it with an authority that was never assigned.
		promoted.Authority = "authority:invented"
		_, err = f.append(t, promoted)
		if !errors.As(err, &unassigned) {
			t.Fatalf("a domain fact citing an unassigned authority returned %v, want ErrAuthorityNotAssigned", err)
		}
		if unassigned.AuthorityRef != "authority:invented" {
			t.Fatalf("the conflict names authority %q", unassigned.AuthorityRef)
		}

		// With the assignment in place it is accepted.
		promoted.Authority = authorityRef
		f.mustAppend(t, promoted)
	})

	t.Run("authority is bounded by its effective interval", func(t *testing.T) {
		// This authority governs only up to 2026-02-01, exclusive.
		f.db.Exec(t, `
			INSERT INTO authority_assignment (
				tenant_id, authority_ref, authority_kind, domain_scope, effective_from, effective_to)
			VALUES ($1, 'authority:expired', 'EXTERNAL_SYSTEM', 'workforce.compensation',
				timestamptz '2026-01-01T00:00:00Z', timestamptz '2026-02-01T00:00:00Z')`,
			f.tenant)

		expired := f.request(2)
		expired.AssertionClass = ledger.DomainFact
		expired.Authority = "authority:expired"
		expired.EffectiveAt = effectiveAt // 2026-03-01, outside the interval
		_, err := f.append(t, expired)
		var unassigned ledger.ErrAuthorityNotAssigned
		if !errors.As(err, &unassigned) {
			t.Fatalf("a domain fact outside its authority interval returned %v, want ErrAuthorityNotAssigned", err)
		}

		// The half-open boundary: the instant the assignment ends is not covered.
		expired.EffectiveAt = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
		if _, err := f.append(t, expired); !errors.As(err, &unassigned) {
			t.Fatalf("the closing instant of a half-open authority interval was covered: %v", err)
		}

		// One microsecond earlier is inside it.
		expired.EffectiveAt = time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
		f.mustAppend(t, expired)
	})

	t.Run("a correction names the assertion it corrects", func(t *testing.T) {
		correction := f.request(3)
		correction.AssertionClass = ledger.Correction
		_, err := f.append(t, correction)
		var target ledger.ErrCorrectionTarget
		if !errors.As(err, &target) {
			t.Fatalf("a correction without a target returned %v, want ErrCorrectionTarget", err)
		}

		correction.Corrects = &ledger.EventRef{StreamKey: streamKey, Sequence: 0}
		if _, err := f.append(t, correction); !errors.As(err, &target) {
			t.Fatalf("a correction naming sequence 0 returned %v, want ErrCorrectionTarget", err)
		}

		correction.Corrects = &ledger.EventRef{StreamKey: streamKey, Sequence: 1}
		receipt := f.mustAppend(t, correction)
		stored := f.read(t, receipt.Sequence)
		if stored.correctsStream == nil || *stored.correctsStream != streamKey ||
			stored.correctsSequence == nil || *stored.correctsSequence != 1 {
			t.Fatalf("the correction records target %v/%v", stored.correctsStream, stored.correctsSequence)
		}

		// A claim is never silently promoted by carrying a correction target.
		claim := f.request(4)
		claim.AssertionClass = ledger.Claim
		claim.Corrects = &ledger.EventRef{StreamKey: streamKey, Sequence: 1}
		if _, err := f.append(t, claim); !errors.As(err, &target) {
			t.Fatalf("a claim carrying a correction target returned %v, want ErrCorrectionTarget", err)
		}
	})

	t.Run("large payloads must be artifact references", func(t *testing.T) {
		oversized := f.request(4)
		oversized.Payload = make([]byte, ledger.MaxInlinePayloadBytes+1)
		_, err := f.append(t, oversized)
		var tooLarge ledger.ErrPayloadTooLarge
		if !errors.As(err, &tooLarge) {
			t.Fatalf("an oversized payload returned %v, want ErrPayloadTooLarge", err)
		}
		if tooLarge.Limit != ledger.MaxInlinePayloadBytes {
			t.Fatalf("the limit reported is %d, want %d", tooLarge.Limit, ledger.MaxInlinePayloadBytes)
		}

		referenced := f.request(4)
		referenced.Payload = nil
		referenced.ArtifactRef = "artifact://sha256/" + strings.Repeat("a", 64)
		receipt := f.mustAppend(t, referenced)
		stored := f.read(t, receipt.Sequence)
		if stored.payload != nil {
			t.Fatal("an artifact-referenced event stored inline bytes")
		}
		if stored.artifactRef == nil || *stored.artifactRef != referenced.ArtifactRef {
			t.Fatalf("the event records artifact %v, want %q", stored.artifactRef, referenced.ArtifactRef)
		}
	})

	t.Run("payload and artifact reference are exclusive", func(t *testing.T) {
		both := f.request(5)
		both.ArtifactRef = "artifact://sha256/" + strings.Repeat("b", 64)
		var reference ledger.ErrPayloadReference
		if _, err := f.append(t, both); !errors.As(err, &reference) {
			t.Fatalf("an event with both a payload and an artifact returned %v, want ErrPayloadReference", err)
		}

		neither := f.request(5)
		neither.Payload = nil
		if _, err := f.append(t, neither); !errors.As(err, &reference) {
			t.Fatalf("an event with neither returned %v, want ErrPayloadReference", err)
		}
	})
}

// TestTodo_LEDGER_004_Property drives every assertion class through the append
// path and checks that only the authority-bearing classes demand an assignment.
func TestTodo_LEDGER_004_Property(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	classes := []ledger.AssertionClass{
		ledger.TransactionFact, ledger.DomainFact, ledger.ExternalObservation,
		ledger.Claim, ledger.Correction,
	}
	var head int64
	for _, class := range classes {
		req := f.request(head)
		req.AssertionClass = class
		if class.RequiresAuthority() {
			req.Authority = authorityRef
		}
		if class == ledger.Correction {
			req.Corrects = &ledger.EventRef{StreamKey: streamKey, Sequence: 1}
		}

		// Without its authority, an authority-bearing class is refused.
		if class.RequiresAuthority() {
			bare := req
			bare.Authority = ""
			var unassigned ledger.ErrAuthorityNotAssigned
			if _, err := f.append(t, bare); !errors.As(err, &unassigned) {
				t.Fatalf("%s without an authority returned %v, want ErrAuthorityNotAssigned", class, err)
			}
		}

		receipt := f.mustAppend(t, req)
		head = receipt.Sequence

		stored := f.read(t, receipt.Sequence)
		if stored.assertionClass != string(class) {
			t.Fatalf("event %d records class %q, want %q", receipt.Sequence, stored.assertionClass, class)
		}
		if class.RequiresAuthority() && (stored.authorityRef == nil || *stored.authorityRef != authorityRef) {
			t.Fatalf("%s recorded authority %v, want %q", class, stored.authorityRef, authorityRef)
		}
		if !class.RequiresAuthority() && stored.authorityRef != nil {
			t.Fatalf("%s recorded authority %q; recording is not authority", class, *stored.authorityRef)
		}
	}
}

// TestTodo_LEDGER_004_Security proves an unknown assertion class never reaches
// the database, and that authority never leaks across tenants.
func TestTodo_LEDGER_004_Security(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	for _, class := range []string{"", "domain_fact", "DOMAIN FACT", "OPINION", "UNSPECIFIED"} {
		req := f.request(0)
		req.AssertionClass = ledger.AssertionClass(class)
		var invalid ledger.ErrInvalidAssertionClass
		if _, err := f.append(t, req); !errors.As(err, &invalid) {
			t.Fatalf("assertion class %q returned %v, want ErrInvalidAssertionClass", class, err)
		}
	}

	// Another tenant's authority assignment does not cover this tenant.
	other := uuid.New()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'other', 'cell-local', 'Other', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, other)
	f.db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, 'authority:foreign', 'EXTERNAL_SYSTEM', 'workforce',
			timestamptz '2026-01-01T00:00:00Z')`, other)

	req := f.request(0)
	req.AssertionClass = ledger.DomainFact
	req.Authority = "authority:foreign"
	var unassigned ledger.ErrAuthorityNotAssigned
	if _, err := f.append(t, req); !errors.As(err, &unassigned) {
		t.Fatalf("borrowing another tenant's authority returned %v, want ErrAuthorityNotAssigned", err)
	}
}

// FuzzTodo_LEDGER_004 exercises arbitrary assertion labels through validation
// and verifies that each declared class has the expected authority requirement.
func FuzzTodo_LEDGER_004(fz *testing.F) {
	for _, class := range []string{"TRANSACTION_FACT", "DOMAIN_FACT", "EXTERNAL_OBSERVATION", "CLAIM", "CORRECTION", "OPINION", "domain_fact", ""} {
		fz.Add(class)
	}
	fz.Fuzz(func(t *testing.T, label string) {
		f := newFixture(t)
		f.mustAppend(t, f.request(0)) // correction seeds an exact valid target
		req := f.request(1)
		req.AssertionClass = ledger.AssertionClass(label)
		if req.AssertionClass == ledger.Correction {
			req.Corrects = &ledger.EventRef{StreamKey: streamKey, Sequence: 1}
		}
		if !req.AssertionClass.Valid() {
			var invalid ledger.ErrInvalidAssertionClass
			if _, err := f.append(t, req); !errors.As(err, &invalid) {
				t.Fatalf("undeclared assertion label %q returned %v, want ErrInvalidAssertionClass", label, err)
			}
			if got := f.sequences(t); len(got) != 1 {
				t.Fatalf("invalid assertion label %q changed sequences to %v", label, got)
			}
			return
		}
		if req.AssertionClass.RequiresAuthority() {
			withoutAuthority := req
			withoutAuthority.Authority = ""
			var unassigned ledger.ErrAuthorityNotAssigned
			if _, err := f.append(t, withoutAuthority); !errors.As(err, &unassigned) {
				t.Fatalf("%s without authority returned %v, want ErrAuthorityNotAssigned", label, err)
			}
			req.Authority = authorityRef
		}
		receipt := f.mustAppend(t, req)
		stored := f.read(t, receipt.Sequence)
		if stored.assertionClass != label {
			t.Fatalf("stored assertion class %q, want %q", stored.assertionClass, label)
		}
		if req.AssertionClass.RequiresAuthority() && (stored.authorityRef == nil || *stored.authorityRef != authorityRef) {
			t.Fatalf("stored authority %v, want %q", stored.authorityRef, authorityRef)
		}
	})
}

// TestTodo_LEDGER_004_Mutation proves callers cannot mutate persisted assertion
// provenance by changing the payload or authority after the append.
func TestTodo_LEDGER_004_Mutation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	req := f.request(0)
	req.AssertionClass = ledger.DomainFact
	req.Authority = authorityRef
	receipt := f.mustAppend(t, req)
	before := f.read(t, receipt.Sequence)
	if err := f.db.ExecErr(`UPDATE ledger_event SET payload = $1, authority_ref = 'authority:forged'
		WHERE tenant_id = $2 AND stream_key = $3 AND sequence = $4`, []byte("forged"), f.tenant, streamKey, receipt.Sequence); err == nil {
		t.Fatal("mutation of assertion payload and provenance succeeded")
	}
	after := f.read(t, receipt.Sequence)
	if string(before.payload) != string(after.payload) || before.authorityRef == nil || after.authorityRef == nil || *before.authorityRef != *after.authorityRef {
		t.Fatalf("assertion changed after refused mutation: before=%+v after=%+v", before, after)
	}
}

// TestTodo_LEDGER_004_Race races two authority-backed facts on one stream. The
// accepted event must retain its tenant's assignment and the head must advance
// exactly once.
func TestTodo_LEDGER_004_Race(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		conn := f.db.NewConn(t)
		req := f.request(0)
		req.AssertionClass = ledger.DomainFact
		req.Authority = authorityRef
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := f.inTxErr(conn, func(tx dbport.Tx) error {
				_, appendErr := ledger.Append(context.Background(), tx, req)
				return appendErr
			})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, stale := 0, 0
	for err := range results {
		if err == nil {
			accepted++
			continue
		}
		var conflict ledger.ErrStaleStream
		if !errors.As(err, &conflict) || conflict.Expected != 0 || conflict.Actual != 1 {
			t.Fatalf("losing authority-backed append returned %v, want stale head 0/1", err)
		}
		stale++
	}
	if accepted != 1 || stale != 1 {
		t.Fatalf("authority-backed race accepted=%d stale=%d, want one of each", accepted, stale)
	}
	events := f.sequences(t)
	if len(events) != 1 || events[0] != 1 {
		t.Fatalf("sequences after authority race are %v, want [1]", events)
	}
	stored := f.read(t, 1)
	if stored.authorityRef == nil || *stored.authorityRef != authorityRef || stored.assertionClass != string(ledger.DomainFact) {
		t.Fatalf("persisted event lost assertion provenance: %+v", stored)
	}
}

const constantDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type constantDigester struct{}

func (constantDigester) Digest(payload []byte, schemaRef string) (string, string, int, error) {
	return "sha256", constantDigest, len(payload), nil
}

type failingDigester struct{}

func (failingDigester) Digest([]byte, string) (string, string, int, error) {
	return "", "", 0, errors.New("canonicalization profile unavailable")
}
