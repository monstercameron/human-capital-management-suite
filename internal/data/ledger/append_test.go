package ledger_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// TestTodo_LEDGER_002 proves the canonical single-stream append: the caller
// cannot supply a digest, sequence or recorded time; the transaction locks the
// head, allocates the next sequence, hashes the envelope and advances the head;
// and an exact replay returns the original receipt while the same key over
// different bytes is a typed conflict.
func TestTodo_LEDGER_002(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	t.Run("the caller cannot supply digest, sequence or recorded time", func(t *testing.T) {
		typ := reflect.TypeOf(ledger.AppendRequest{})
		for _, forbidden := range []string{"Digest", "DigestAlgorithm", "Sequence", "RecordedAt", "HeadDigest"} {
			if _, ok := typ.FieldByName(forbidden); ok {
				t.Fatalf("AppendRequest exposes %s; the ledger owns that field", forbidden)
			}
		}
	})

	t.Run("append allocates the sequence and advances the head", func(t *testing.T) {
		first := f.mustAppend(t, f.request(0))
		if first.Sequence != 1 {
			t.Fatalf("first append landed at sequence %d, want 1", first.Sequence)
		}
		if first.PreviousHead != 0 {
			t.Fatalf("first append reports previous head %d, want 0", first.PreviousHead)
		}
		if first.DigestAlgorithm != ledger.Algorithm || len(first.Digest) != 64 {
			t.Fatalf("receipt digest is %s:%s, want a sha256 hex digest", first.DigestAlgorithm, first.Digest)
		}
		if first.RecordedAt.IsZero() {
			t.Fatal("receipt has no recorded time")
		}

		head, headDigest := f.head(t)
		if head != 1 || headDigest != first.Digest {
			t.Fatalf("head is at %d with digest %s, want 1 with %s", head, headDigest, first.Digest)
		}

		second := f.mustAppend(t, f.request(1))
		if second.Sequence != 2 || second.PreviousHead != 1 {
			t.Fatalf("second append landed at %d after head %d, want 2 after 1", second.Sequence, second.PreviousHead)
		}
	})

	t.Run("a stale expected head is refused with both sequences", func(t *testing.T) {
		_, err := f.append(t, f.request(0))
		var stale ledger.ErrStaleStream
		if !errors.As(err, &stale) {
			t.Fatalf("appending against a stale head returned %v, want ErrStaleStream", err)
		}
		if stale.Expected != 0 || stale.Actual != 2 {
			t.Fatalf("conflict reports expected %d actual %d, want 0 and 2", stale.Expected, stale.Actual)
		}
		if stale.Code() != ledger.CodeStaleStream {
			t.Fatalf("conflict code is %s, want %s", stale.Code(), ledger.CodeStaleStream)
		}
	})

	t.Run("an exact replay returns the original receipt", func(t *testing.T) {
		req := f.request(2)
		original := f.mustAppend(t, req)

		replayed, err := f.append(t, req)
		if err != nil {
			t.Fatalf("exact replay was refused: %v", err)
		}
		if !replayed.Replayed {
			t.Fatal("replay receipt is not marked as a replay")
		}
		if replayed.EventID != original.EventID || replayed.Sequence != original.Sequence ||
			replayed.Digest != original.Digest {
			t.Fatalf("replay returned event %s at %d, want the original %s at %d",
				replayed.EventID, replayed.Sequence, original.EventID, original.Sequence)
		}

		head, _ := f.head(t)
		if head != original.Sequence {
			t.Fatalf("replay advanced the head to %d, want %d", head, original.Sequence)
		}
	})

	t.Run("the same key over different bytes is a conflict", func(t *testing.T) {
		req := f.request(3)
		req.IdempotencyKey = "reused-key"
		if _, err := f.append(t, req); err != nil {
			t.Fatalf("first append with the reused key: %v", err)
		}

		forged := f.request(4)
		forged.IdempotencyKey = "reused-key"
		forged.Payload = []byte("promotion-proposed-differently")
		_, err := f.append(t, forged)
		var conflict ledger.ErrIdempotencyConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("reusing an idempotency key over different bytes returned %v, want ErrIdempotencyConflict", err)
		}
		if conflict.RecordedDigest == conflict.RequestDigest || conflict.RecordedDigest == "" {
			t.Fatalf("conflict reports recorded %q and request %q", conflict.RecordedDigest, conflict.RequestDigest)
		}
	})

	t.Run("an unregistered stream is refused", func(t *testing.T) {
		req := f.request(0)
		req.StreamKey = "worker:never-registered"
		_, err := f.append(t, req)
		var missing ledger.ErrStreamNotFound
		if !errors.As(err, &missing) {
			t.Fatalf("appending to an unregistered stream returned %v, want ErrStreamNotFound", err)
		}
	})

	t.Run("an invalid assertion class is refused before any write", func(t *testing.T) {
		req := f.request(4)
		req.AssertionClass = "OPINION"
		_, err := f.append(t, req)
		var invalid ledger.ErrInvalidAssertionClass
		if !errors.As(err, &invalid) {
			t.Fatalf("an undeclared assertion class returned %v, want ErrInvalidAssertionClass", err)
		}
	})
}

// TestTodo_LEDGER_002_Race proves that independent transactions racing at the
// same expected head cannot both allocate sequence one.
func TestTodo_LEDGER_002_Race(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		conn := f.db.NewConn(t)
		req := f.request(0)
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
			t.Fatalf("losing concurrent append returned %v, want stale head 0/1", err)
		}
		stale++
	}
	if accepted != 1 || stale != 1 {
		t.Fatalf("concurrent append accepted=%d stale=%d, want one of each", accepted, stale)
	}
	if got := f.sequences(t); len(got) != 1 || got[0] != 1 {
		t.Fatalf("sequences after concurrent append are %v, want [1]", got)
	}
}

// TestTodo_LEDGER_002_Recovery proves a failed append leaves nothing behind: the
// head does not move and no sequence is consumed.
func TestTodo_LEDGER_002_Recovery(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustAppend(t, f.request(0))

	// A doomed append inside its own transaction.
	doomed := f.request(1)
	doomed.SchemaRef = "never.registered@1"
	if _, err := f.append(t, doomed); err == nil {
		t.Fatal("an event citing an unregistered payload schema was appended")
	}

	head, _ := f.head(t)
	if head != 1 {
		t.Fatalf("head is at %d after a failed append, want 1", head)
	}
	if got := f.sequences(t); len(got) != 1 || got[0] != 1 {
		t.Fatalf("stream holds sequences %v after a failed append, want [1]", got)
	}

	// The stream is still usable at the head the failure left behind.
	if receipt := f.mustAppend(t, f.request(1)); receipt.Sequence != 2 {
		t.Fatalf("the next append landed at %d, want 2", receipt.Sequence)
	}
}

// TestTodo_LEDGER_002_Golden pins the canonical digest of a fixed envelope so an
// accidental change to the profile is visible.
func TestTodo_LEDGER_002_Golden(t *testing.T) {
	t.Parallel()

	algorithm, digest, length, err := ledger.SHA256Digester{}.Digest([]byte("promotion-proposed"), schemaRef)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if algorithm != "sha256" {
		t.Fatalf("algorithm is %q, want sha256", algorithm)
	}
	if length != len("promotion-proposed") {
		t.Fatalf("canonical length is %d, want %d", length, len("promotion-proposed"))
	}
	// Recompute the preimage independently: profile, schema reference and payload,
	// each prefixed with its length so the encoding is unambiguous.
	h := sha256.New()
	for _, field := range [][]byte{
		[]byte(ledger.DigestProfile),
		[]byte(schemaRef),
		[]byte("promotion-proposed"),
	} {
		var prefix [8]byte
		binary.BigEndian.PutUint64(prefix[:], uint64(len(field)))
		h.Write(prefix[:])
		h.Write(field)
	}
	if want := hex.EncodeToString(h.Sum(nil)); digest != want {
		t.Fatalf("canonical digest is %s, want %s", digest, want)
	}

	// The same bytes under a different schema reference are a different fact.
	_, other, _, err := ledger.SHA256Digester{}.Digest([]byte("promotion-proposed"), "other.schema@1")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if other == digest {
		t.Fatal("the schema reference is not bound into the canonical digest")
	}
}

// TestTodo_LEDGER_002_Integration walks a whole transaction: an intent, a
// proposal revision, a ledger append, a projection checkpoint and an outbox
// record all commit together, and none of it involves an external call.
func TestTodo_LEDGER_002_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)

	intentID := uuid.New()
	var receipt ledger.AppendReceipt

	f.inTx(t, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO intent_instance (
				tenant_id, intent_id, definition_ref, definition_version, request_digest,
				idempotency_key, request_state, execution_state, business_state,
				consistency_state, obligation_state, created_at, last_transition_at)
			VALUES ($1, $2, 'promotion', 1, $3, $4, 'SUBMITTED', 'SCHEDULED', 'NOT_STARTED',
				'PENDING_OBSERVATION', 'PENDING', $5, $5)`,
			f.tenant, intentID,
			"0000000000000000000000000000000000000000000000000000000000000001",
			uuid.NewString(), effectiveAt); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO proposal_revision (
				tenant_id, intent_id, revision, proposal_digest, material_digest,
				schema_ref, payload, produced_by, produced_at)
			VALUES ($1, $2, 1, $3, $3, $4, $5, 'planner', $6)`,
			f.tenant, intentID,
			"0000000000000000000000000000000000000000000000000000000000000002",
			schemaRef, []byte("proposal"), effectiveAt); err != nil {
			return err
		}

		var err error
		receipt, err = ledger.Append(ctx, tx, f.request(0))
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO projection_checkpoint (
				tenant_id, projection_name, stream_key, last_applied_sequence,
				last_applied_digest, status)
			VALUES ($1, 'worker-current', $2, $3, $4, 'CURRENT')`,
			f.tenant, streamKey, receipt.Sequence, receipt.Digest); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO outbox (
				tenant_id, outbox_id, effect_identity, ordering_key, schema_ref, payload)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			f.tenant, uuid.New(), "notify:"+receipt.EventID.String(), streamKey,
			schemaRef, []byte("effect"))
		return err
	})

	var checkpoint int64
	if err := f.db.QueryRow(ctx, `
		SELECT last_applied_sequence FROM projection_checkpoint
		WHERE tenant_id = $1 AND projection_name = 'worker-current' AND stream_key = $2`,
		f.tenant, streamKey).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if checkpoint != receipt.Sequence {
		t.Fatalf("checkpoint is at %d, want the appended sequence %d", checkpoint, receipt.Sequence)
	}

	var pending int
	if err := f.db.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE tenant_id = $1 AND status = 'PENDING'`,
		f.tenant).Scan(&pending); err != nil {
		t.Fatalf("count outbox records: %v", err)
	}
	if pending != 1 {
		t.Fatalf("outbox holds %d pending records, want 1", pending)
	}
}

// FuzzTodo_LEDGER_002 drives the canonical digest over arbitrary payloads and
// schema references. The digest must be total, deterministic, and unambiguous
// across the boundary between the two fields.
func FuzzTodo_LEDGER_002(f *testing.F) {
	f.Add([]byte("promotion-proposed"), "hcmnext.intents.v1.BusinessIntent@1")
	f.Add([]byte{}, "a")
	f.Add([]byte("ab"), "c")
	f.Add([]byte("a"), "bc")

	digester := ledger.SHA256Digester{}
	f.Fuzz(func(t *testing.T, payload []byte, schemaRef string) {
		if schemaRef == "" {
			if _, _, _, err := digester.Digest(payload, schemaRef); err == nil {
				t.Fatal("an empty schema reference produced a digest")
			}
			return
		}
		algorithm, digest, length, err := digester.Digest(payload, schemaRef)
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if algorithm != ledger.Algorithm || len(digest) != 64 {
			t.Fatalf("digest %s:%s is malformed", algorithm, digest)
		}
		if length != len(payload) {
			t.Fatalf("canonical length %d, want %d", length, len(payload))
		}
		_, again, _, err := digester.Digest(payload, schemaRef)
		if err != nil || again != digest {
			t.Fatalf("digest is not deterministic: %s then %s (%v)", digest, again, err)
		}
	})
}

// TestTodo_LEDGER_002_Mutation proves each guard is load bearing: removing any
// required field from a request is refused rather than silently defaulted.
func TestTodo_LEDGER_002_Mutation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	mutations := map[string]func(*ledger.AppendRequest){
		"tenant":          func(r *ledger.AppendRequest) { r.Tenant = uuid.Nil },
		"stream key":      func(r *ledger.AppendRequest) { r.StreamKey = "" },
		"source":          func(r *ledger.AppendRequest) { r.SourceRef = "" },
		"schema":          func(r *ledger.AppendRequest) { r.SchemaRef = "" },
		"idempotency key": func(r *ledger.AppendRequest) { r.IdempotencyKey = "" },
		"correlation":     func(r *ledger.AppendRequest) { r.CorrelationID = uuid.Nil },
		"occurred at":     func(r *ledger.AppendRequest) { r.OccurredAt = time.Time{} },
		"effective at":    func(r *ledger.AppendRequest) { r.EffectiveAt = time.Time{} },
		"expected head":   func(r *ledger.AppendRequest) { r.ExpectedHead = -1 },
		"payload":         func(r *ledger.AppendRequest) { r.Payload = nil },
	}
	for name, mutate := range mutations {
		req := f.request(0)
		mutate(&req)
		if _, err := f.append(t, req); err == nil {
			t.Fatalf("an append without a valid %s was accepted", name)
		}
	}

	if got := f.sequences(t); len(got) != 0 {
		t.Fatalf("rejected appends wrote sequences %v", got)
	}
}
