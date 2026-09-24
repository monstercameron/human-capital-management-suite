package projection_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

func promotionEvents(tenant uuid.UUID, stream string, n int) []ledger.EventRecord {
	events := make([]ledger.EventRecord, n)
	for i := range events {
		events[i] = ledger.EventRecord{
			Tenant: tenant, StreamKey: stream, Sequence: int64(i + 1), EventID: uuid.New(),
			SchemaRef: projection.PromotionOutcomeSchemaRef, Digest: strings.Repeat(string(rune('a'+i)), 64),
			DigestAlgorithm: ledger.Algorithm,
		}
	}
	return events
}

func TestTodo_LEDGER_013(t *testing.T) {
	tenant := uuid.New()
	stream := "workflow:promotion"
	rebuilt, err := projection.RebuildPromotionOutcome(promotionEvents(tenant, stream, 3), tenant, stream)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt.SourceHead != 3 || rebuilt.RowCount != 3 || rebuilt.Digest == "" {
		t.Fatalf("report = %+v, want three rows and a digest", rebuilt)
	}
	if err := projection.ComparePromotionOutcome(rebuilt, rebuilt); err != nil {
		t.Fatalf("equal live projection diverged: %v", err)
	}
}

func TestTodo_LEDGER_013_Property(t *testing.T) {
	tenant := uuid.New()
	stream := "workflow:promotion"
	events := promotionEvents(tenant, stream, 2)
	first, err := projection.RebuildPromotionOutcome(events, tenant, stream)
	if err != nil {
		t.Fatal(err)
	}
	second, err := projection.RebuildPromotionOutcome([]ledger.EventRecord{events[1], events[0]}, tenant, stream)
	if err == nil || second.Digest != "" {
		t.Fatalf("reordered replay = report=%+v err=%v, want refusal", second, err)
	}
	if first.Digest == "" {
		t.Fatal("valid replay has no digest")
	}
}

func TestTodo_LEDGER_013_Golden(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	events := []ledger.EventRecord{{
		Tenant: tenant, StreamKey: "workflow:golden", Sequence: 1,
		EventID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		SchemaRef: projection.PromotionOutcomeSchemaRef, Digest: strings.Repeat("a", 64), DigestAlgorithm: ledger.Algorithm,
	}}
	report, err := projection.RebuildPromotionOutcome(events, tenant, "workflow:golden")
	if err != nil {
		t.Fatal(err)
	}
	if report.Digest != projection.DigestPromotionOutcomeRows(report.Rows) {
		t.Fatalf("digest %q does not reproduce from rows", report.Digest)
	}
}

func TestTodo_LEDGER_013_Race(t *testing.T) {
	tenant := uuid.New()
	stream := "workflow:promotion"
	events := promotionEvents(tenant, stream, 2)
	want, err := projection.RebuildPromotionOutcome(events, tenant, stream)
	if err != nil {
		t.Fatal(err)
	}
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := projection.RebuildPromotionOutcome(events, tenant, stream)
			if err != nil {
				errs[i] = err
				return
			}
			errs[i] = projection.ComparePromotionOutcomeDigests(got, want.RowCount, want.Digest)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("parallel replay %d: %v", i, err)
		}
	}
}

func TestTodo_LEDGER_013_Security(t *testing.T) {
	tenant := uuid.New()
	events := promotionEvents(tenant, "workflow:promotion", 1)
	events[0].Tenant = uuid.New()
	if _, err := projection.RebuildPromotionOutcome(events, tenant, "workflow:promotion"); err == nil {
		t.Fatal("foreign tenant replay was accepted")
	}
}

func TestTodo_LEDGER_013_Recovery(t *testing.T) {
	tenant := uuid.New()
	stream := "workflow:promotion"
	events := promotionEvents(tenant, stream, 1)
	rebuilt, err := projection.RebuildPromotionOutcome(events, tenant, stream)
	if err != nil {
		t.Fatal(err)
	}
	live := rebuilt
	live.Rows = append([]projection.PromotionOutcomeRow(nil), rebuilt.Rows...)
	live.Rows[0].EventDigest = strings.Repeat("f", 64)
	if err := projection.ComparePromotionOutcome(rebuilt, live); err == nil || !strings.Contains(err.Error(), stream+"@1") {
		t.Fatalf("divergence error = %v, want first row named", err)
	}
}

func TestTodo_LEDGER_013_Mutation(t *testing.T) {
	tenant := uuid.New()
	stream := "workflow:promotion"
	events := promotionEvents(tenant, stream, 1)
	events[0].SchemaRef = "hcmnext.workflow.Other/v1"
	if _, err := projection.RebuildPromotionOutcome(events, tenant, stream); err == nil {
		t.Fatal("wrong schema was accepted")
	}
}
