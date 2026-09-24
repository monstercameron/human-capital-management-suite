package subscription

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func schemaFixture(t *testing.T, version int, fields []SchemaField) EventSchema {
	t.Helper()
	schema, err := NewEventSchema(EventWorkerChanged, version, fields)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestTodo_SUB_003(t *testing.T) {
	base := schemaFixture(t, 1, []SchemaField{{Name: "worker.id", Type: TypeRef, Classification: ClassificationInternal}, {Name: "worker.status", Type: TypeString, Classification: ClassificationInternal}})
	next := schemaFixture(t, 2, []SchemaField{{Name: "worker.id", Type: TypeRef, Classification: ClassificationInternal}, {Name: "worker.status", Type: TypeString, Classification: ClassificationInternal}, {Name: "worker.nickname", Type: TypeString, Classification: ClassificationPublic, Optional: true}})
	compatibility, err := CheckSchemaCompatibility(base, next)
	if err != nil || !compatibility.Compatible {
		t.Fatalf("compatibility=%+v err=%v", compatibility, err)
	}
	registry := NewSchemaRegistry()
	if err := registry.Register(base); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(next); err != nil {
		t.Fatal(err)
	}
	envelope := CanonicalEnvelope{Tenant: "tenant-a", Kind: EventWorkerChanged, SchemaVersion: 2, SubjectRefs: []string{"worker:42"}, EffectiveAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60)), KnownAt: time.Date(2026, 9, 5, 12, 1, 0, 0, time.UTC), PayloadDigest: "sha256:payload", ProvenanceRef: "evidence:event-1", Sequence: 7}
	bytes, err := envelope.CanonicalBytes()
	if err != nil || len(bytes) == 0 || envelope.Digest() == "" {
		t.Fatalf("bytes=%x digest=%q err=%v", bytes, envelope.Digest(), err)
	}
	if envelope.Explain() == "" {
		t.Fatal("missing envelope explanation")
	}
}

func TestTodo_SUB_003_Golden(t *testing.T) {
	envelope := CanonicalEnvelope{Tenant: "tenant-golden", Kind: EventWorkerChanged, SchemaVersion: 1, SubjectRefs: []string{"worker:7", "assignment:3"}, EffectiveAt: time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 9, 5, 16, 0, 1, 0, time.UTC), PayloadDigest: "sha256:fixture-payload", ProvenanceRef: "evidence:fixture", Sequence: 11}
	bytes, err := envelope.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	const wantBytes = "0724736368656d612e68636d6e6578742e646f6d61696e732e737562736372697074696f6e2e43616e6f6e6963616c456e76656c6f70650f24736368656d615f76657273696f6e01020674656e616e740d74656e616e742d676f6c64656e0a6576656e745f6b696e640e574f524b45525f4348414e4745440e736368656d615f76657273696f6e01020c7375626a6563745f7265662301040b7375626a6563745f7265660c61737369676e6d656e743a330b7375626a6563745f72656608776f726b65723a370c6566666563746976655f617414323032362d30392d30355431363a30303a30305a086b6e6f776e5f617414323032362d30392d30355431363a30303a30315a0e7061796c6f61645f646967657374167368613235363a666978747572652d7061796c6f61640e70726f76656e616e63655f7265661065766964656e63653a666978747572650873657175656e63650116"
	const wantDigest = "sha256:a0049ba6e046413a550e98105e4ffbd6862fb9341f4eb83c0dc0bd5dabf19890"
	if got := fmt.Sprintf("%x", bytes); got != wantBytes {
		t.Fatalf("golden bytes=%s want %s", got, wantBytes)
	}
	if got := envelope.Digest(); got != wantDigest {
		t.Logf("golden bytes=%x", bytes)
		t.Fatalf("golden digest=%q want %q", got, wantDigest)
	}
}

func TestTodo_SUB_003_Race(t *testing.T) {
	schema := schemaFixture(t, 1, []SchemaField{{Name: "worker.id", Type: TypeRef, Classification: ClassificationInternal}})
	copySchema := schema
	copySchema.Fields = append([]SchemaField(nil), schema.Fields...)
	copySchema.Fields[0].Name = "worker.changed"
	if err := copySchema.Verify(); err == nil {
		t.Fatal("mutated schema unexpectedly verified")
	}
	const workers = 12
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- schema.Verify() }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent schema verification: %v", err)
		}
	}
	if schema.Fields[0].Name != "worker.id" {
		t.Fatalf("schema fields were aliased: %+v", schema.Fields)
	}
}

func TestTodo_SUB_003_Integration(t *testing.T) {
	base := schemaFixture(t, 1, []SchemaField{{Name: "worker.id", Type: TypeRef, Classification: ClassificationInternal}})
	removed := schemaFixture(t, 2, []SchemaField{{Name: "worker.status", Type: TypeString, Classification: ClassificationInternal}})
	compatibility, err := CheckSchemaCompatibility(base, removed)
	if !errors.Is(err, ErrSchemaBreaking) || compatibility.BreakingField != "worker.id" {
		t.Fatalf("removed compatibility=%+v err=%v", compatibility, err)
	}
}

func TestTodo_SUB_003_Fault(t *testing.T) {
	if _, err := NewEventSchema(EventWorkerChanged, 1, []SchemaField{{Name: "*", Type: TypeString, Classification: ClassificationPublic}}); !errors.Is(err, ErrInvalidEventSchema) {
		t.Fatalf("wildcard field error=%v", err)
	}
	base := schemaFixture(t, 1, []SchemaField{{Name: "worker.id", Type: TypeRef, Classification: ClassificationInternal}})
	changed := schemaFixture(t, 2, []SchemaField{{Name: "worker.id", Type: TypeString, Classification: ClassificationInternal}})
	compatibility, err := CheckSchemaCompatibility(base, changed)
	if !errors.Is(err, ErrSchemaBreaking) || compatibility.BreakingField != "worker.id" {
		t.Fatalf("type change compatibility=%+v err=%v", compatibility, err)
	}
}

func TestTodo_SUB_003_Security(t *testing.T) {
	envelope := CanonicalEnvelope{Tenant: "tenant-a", Kind: EventWorkerChanged, SchemaVersion: 1, SubjectRefs: []string{"worker:7"}, EffectiveAt: time.Unix(1, 0), KnownAt: time.Unix(2, 0), PayloadDigest: "sha256:payload", ProvenanceRef: "evidence:1", Sequence: 1}
	if _, err := envelope.CanonicalBytes(); err != nil {
		t.Fatal(err)
	}
	envelope.PayloadDigest = "raw-secret"
	if envelope.Digest() == "" {
		t.Fatal("digest missing")
	}
}

func FuzzTodo_SUB_003(f *testing.F) {
	f.Add("tenant-a", "worker:1", "sha256:payload", "evidence:1")
	f.Fuzz(func(t *testing.T, tenant, subject, payloadDigest, provenance string) {
		envelope := CanonicalEnvelope{Tenant: tenant, Kind: EventWorkerChanged, SchemaVersion: 1, SubjectRefs: []string{subject}, EffectiveAt: time.Unix(1, 0), KnownAt: time.Unix(2, 0), PayloadDigest: payloadDigest, ProvenanceRef: provenance, Sequence: 1}
		bytes, err := envelope.CanonicalBytes()
		if err != nil {
			if envelope.Digest() != "" {
				t.Fatal("invalid envelope produced a digest")
			}
			return
		}
		if len(bytes) == 0 || envelope.Digest() == "" {
			t.Fatal("valid envelope did not produce canonical bytes and digest")
		}
	})
}
