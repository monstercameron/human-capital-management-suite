package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const testPurpose = "agent.answer"

func TestTodo_AGENT_040(t *testing.T) {
	t.Run("pinned item is admitted and read while its owner source remains current", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("m1", KindMemory)
		if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
			t.Fatal(err)
		}
		got, err := f.manager.Read(context.Background(), testActor(), "raw", item.ID)
		if err != nil || string(got.Payload) != string(item.Payload) {
			t.Fatalf("Read() = (%+v, %v), want original item", got, err)
		}
		if got.SourceID != item.SourceID || got.SourceVersion != item.SourceVersion || got.SourceDigest != item.SourceDigest || got.Purpose != testPurpose || got.DataClass != "EMPLOYEE" || got.RetentionVersion != "r7" || got.TTL != 24*time.Hour || !slices.Equal(got.Invalidators, item.Invalidators) {
			t.Fatalf("stored provenance was not preserved: %+v", got)
		}
	})
	t.Run("source revocation tombstones and purges raw and derived copies", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("m2", KindCache)
		if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
			t.Fatal(err)
		}
		if err := f.manager.RevokeSource(context.Background(), testActor(), testPin(), "document permission removed"); err != nil {
			t.Fatal(err)
		}
		for _, store := range f.stores {
			if _, err := store.Get(context.Background(), item.TenantID, item.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("copy survived in %s: %v", store.Name(), err)
			}
			if !store.invalid[scopeKey(item.TenantID, "SOURCE", SourceTarget(item.SourceOwner, item.SourceID))] {
				t.Fatalf("source tombstone missing in %s", store.Name())
			}
			if len(store.receipts) == 0 || store.receipts[0].ActorID != "user-a" || store.receipts[0].Operation != OperationRevoke || store.receipts[0].TenantID != item.TenantID {
				t.Fatalf("invalidation receipt lost actor or tenant evidence in %s: %+v", store.Name(), store.receipts)
			}
		}
		if _, err := f.manager.Read(context.Background(), testActor(), "search", item.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Read() after purge error = %v, want not found", err)
		}
	})
	t.Run("TTL hides an item and does not substitute for disposition", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("expired", KindMemory)
		if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
			t.Fatal(err)
		}
		*f.clock = item.CreatedAt.Add(item.TTL)
		if _, err := f.manager.Read(context.Background(), testActor(), "raw", item.ID); !errors.Is(err, ErrExpired) {
			t.Fatalf("expired Read() error = %v", err)
		}
		f.disposition.decision = DispositionDecision{Resolved: true, CanDelete: false, Reason: "minimum retention active"}
		if err := f.manager.Sweep(context.Background(), testActor()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.stores[0].Get(context.Background(), item.TenantID, item.ID); err != nil {
			t.Fatalf("TTL alone physically deleted a retained item: %v", err)
		}
	})
}

func TestTodo_AGENT_040_Security(t *testing.T) {
	t.Run("cross-tenant put never reaches a store", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("tenant", KindMemory)
		if err := f.manager.Put(context.Background(), Actor{TenantID: "tenant-b", PrincipalID: "user"}, item); !errors.Is(err, ErrDenied) {
			t.Fatalf("cross-tenant Put() error = %v", err)
		}
		if len(f.stores[0].items) != 0 {
			t.Fatal("cross-tenant item was persisted")
		}
	})
	t.Run("class audience purpose and retention cannot be widened", func(t *testing.T) {
		cases := []struct {
			name string
			edit func(*Item)
			want error
		}{
			{name: "unapproved class", edit: func(i *Item) { i.DataClass = "PAYROLL" }, want: ErrDenied},
			{name: "unapproved audience", edit: func(i *Item) { i.Audience = []string{"public-channel"} }, want: ErrDenied},
			{name: "unbounded ttl", edit: func(i *Item) { i.TTL = 25 * time.Hour }, want: ErrDenied},
			{name: "old retention policy", edit: func(i *Item) { i.RetentionVersion = "r6" }, want: ErrDenied},
			{name: "missing invalidator", edit: func(i *Item) { i.Invalidators = nil }, want: ErrInvalid},
			{name: "empty payload", edit: func(i *Item) { i.Payload = nil }, want: ErrInvalid},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				f := newFixture(t)
				item := testItem("bad-"+strings.ReplaceAll(tc.name, " ", "-"), KindMemory)
				tc.edit(&item)
				if err := f.manager.Put(context.Background(), testActor(), item); !errors.Is(err, tc.want) {
					t.Fatalf("Put() error=%v, want %v", err, tc.want)
				}
				if len(f.stores[0].items) != 0 {
					t.Fatal("invalid item reached persistence")
				}
			})
		}
	})
	t.Run("source changes deny reads and clean derived material", func(t *testing.T) {
		for _, field := range []string{"version", "digest", "audience", "invalidator", "revoked"} {
			t.Run(field, func(t *testing.T) {
				f := newFixture(t)
				item := testItem("stale-"+field, KindMemory)
				if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
					t.Fatal(err)
				}
				switch field {
				case "version":
					f.sources.decision.Version = "v2"
				case "digest":
					f.sources.decision.Digest = "sha256:other"
				case "audience":
					f.sources.decision.Audience = []string{"private-other"}
				case "invalidator":
					f.sources.decision.Invalidators = []string{"grant:new"}
				case "revoked":
					f.sources.decision.Current = false
				}
				if _, err := f.manager.Read(context.Background(), testActor(), "search", item.ID); !errors.Is(err, ErrStale) {
					t.Fatalf("stale Read() error=%v", err)
				}
				if _, err := f.stores[1].Get(context.Background(), item.TenantID, item.ID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("derived search cache survived definitive revocation: %v", err)
				}
			})
		}
	})
	t.Run("export fails as a unit on a cross-tenant row", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("foreign", KindMemory)
		foreign := item
		foreign.TenantID = "tenant-b"
		f.stores[1].foreignRows = []Item{cloneItem(foreign)}
		if out, err := f.manager.ExportTenant(context.Background(), testActor()); !errors.Is(err, ErrIncomplete) || out != nil {
			t.Fatalf("ExportTenant()=(%v,%v), want no partial export", out, err)
		}
	})
	t.Run("export fails as a unit when a store cannot prove completeness", func(t *testing.T) {
		f := newFixture(t)
		f.stores[1].incompleteList = true
		if out, err := f.manager.ExportTenant(context.Background(), testActor()); !errors.Is(err, ErrIncomplete) || out != nil {
			t.Fatalf("ExportTenant()=(%v,%v), want no partial export", out, err)
		}
	})
	t.Run("a persisted source tombstone prevents a new derived write", func(t *testing.T) {
		f := newFixture(t)
		if err := f.stores[1].RecordInvalidation(context.Background(), Invalidation{TenantID: "tenant-a", TargetKind: "SOURCE", TargetID: SourceTarget(testPin().Owner, "doc-1"), ActorID: "owner", Reason: "revoked elsewhere", Operation: OperationRevoke, OccurredAt: *f.clock}); err != nil {
			t.Fatal(err)
		}
		if err := f.manager.Put(context.Background(), testActor(), testItem("after-revoke", KindMemory)); !errors.Is(err, ErrStale) {
			t.Fatalf("Put() after persisted tombstone error=%v", err)
		}
		if len(f.stores[0].items) != 0 {
			t.Fatal("write after revocation reached another store")
		}
	})
}

func TestTodo_AGENT_040_Recovery(t *testing.T) {
	t.Run("failed durable tombstone keeps process-local read fence", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("no-tombstone", KindCache)
		if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
			t.Fatal(err)
		}
		f.stores[0].failTomb = errors.New("raw tombstone unavailable")
		f.stores[1].failTomb = errors.New("search tombstone unavailable")
		if err := f.manager.RevokeSource(context.Background(), testActor(), testPin(), "revoked"); !errors.Is(err, ErrIncomplete) {
			t.Fatalf("RevokeSource() error=%v, want incomplete tombstone", err)
		}
		if _, err := f.manager.Read(context.Background(), testActor(), "search", item.ID); !errors.Is(err, ErrStale) {
			t.Fatalf("read passed process-local revocation fence: %v", err)
		}
	})

	t.Run("partial write failure leaves a tombstone if rollback is incomplete", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("partial-write", KindMemory)
		f.stores[1].failPut = errors.New("search store offline")
		f.stores[0].failDelete = errors.New("raw store cleanup offline")
		if err := f.manager.Put(context.Background(), testActor(), item); !errors.Is(err, ErrIncomplete) {
			t.Fatalf("Put() error=%v, want incomplete rollback", err)
		}
		if _, err := f.manager.Read(context.Background(), testActor(), "raw", item.ID); !errors.Is(err, ErrStale) {
			t.Fatalf("partial copy was served after aborted write: %v", err)
		}
		if err := f.manager.Put(context.Background(), testActor(), item); !errors.Is(err, ErrStale) {
			t.Fatalf("aborted item id was reused: %v", err)
		}
	})

	t.Run("revoked held traces are hidden but retained until release", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("held-revoked", KindTrace)
		if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
			t.Fatal(err)
		}
		f.disposition.decision = DispositionDecision{Resolved: true, Held: true, CanDelete: true, Reason: "hold-42"}
		if err := f.manager.RevokeSource(context.Background(), testActor(), testPin(), "source access revoked"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.stores[1].Get(context.Background(), item.TenantID, item.ID); err != nil {
			t.Fatalf("held trace bytes were erased: %v", err)
		}
		if _, err := f.manager.Read(context.Background(), testActor(), "search", item.ID); !errors.Is(err, ErrStale) {
			t.Fatalf("revoked held trace remained readable: %v", err)
		}
		f.disposition.decision = DispositionDecision{Resolved: true, CanDelete: true, Reason: "hold released"}
		if err := f.manager.Sweep(context.Background(), testActor()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.stores[1].Get(context.Background(), item.TenantID, item.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("post-hold sweep retained disposable trace: %v", err)
		}
	})

	t.Run("tombstone survives failed purge and blocks residual cache", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("recover", KindCache)
		if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
			t.Fatal(err)
		}
		f.stores[1].failDelete = errors.New("index unavailable")
		if err := f.manager.RevokeSource(context.Background(), testActor(), testPin(), "source deleted"); !errors.Is(err, ErrIncomplete) {
			t.Fatalf("RevokeSource() error=%v, want incomplete purge", err)
		}
		if _, err := f.manager.Read(context.Background(), testActor(), "search", item.ID); !errors.Is(err, ErrStale) {
			t.Fatalf("residual copy served after failed purge: %v", err)
		}
		f.stores[1].failDelete = nil
		if err := f.manager.RevokeSource(context.Background(), testActor(), testPin(), "retry source deletion"); err != nil {
			t.Fatal(err)
		}
		if len(f.stores[1].items) != 0 {
			t.Fatal("retry did not clear residual derived copy")
		}
	})
	t.Run("failed inventory prevents destructive sweep", func(t *testing.T) {
		f := newFixture(t)
		item := testItem("unknown-copy", KindTrace)
		if err := f.stores[0].Put(context.Background(), item); err != nil {
			t.Fatal(err)
		}
		f.stores[1].failList = errors.New("index inventory unavailable")
		f.disposition.decision = DispositionDecision{Resolved: true, CanDelete: true, Reason: "eligible"}
		if err := f.manager.Sweep(context.Background(), testActor()); !errors.Is(err, ErrIncomplete) {
			t.Fatalf("Sweep() error=%v, want incomplete inventory", err)
		}
		if _, err := f.stores[0].Get(context.Background(), item.TenantID, item.ID); err != nil {
			t.Fatalf("sweep deleted incompletely inventoried item: %v", err)
		}
	})
	t.Run("unresolved and held disposition preserve traces", func(t *testing.T) {
		for _, decision := range []DispositionDecision{{}, {Resolved: true, Held: true, CanDelete: true, Reason: "hold-1"}} {
			f := newFixture(t)
			item := testItem("held", KindTrace)
			if err := f.stores[0].Put(context.Background(), item); err != nil {
				t.Fatal(err)
			}
			f.disposition.decision = decision
			err := f.manager.Sweep(context.Background(), testActor())
			if decision.Resolved && decision.Held {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrDisposition) {
				t.Fatalf("unresolved sweep error=%v", err)
			}
			if _, err := f.stores[0].Get(context.Background(), item.TenantID, item.ID); err != nil {
				t.Fatalf("held/unresolved trace erased: %v", err)
			}
		}
	})
}

func TestMemory_ExportAndRevocation_AcrossTestStores(t *testing.T) {
	f := newFixture(t)
	for _, item := range []Item{testItem("memory", KindMemory), testItem("prompt", KindPrompt), testItem("tool", KindToolResult), testItem("trace", KindTrace)} {
		if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
			t.Fatal(err)
		}
	}
	export, err := f.manager.ExportTenant(context.Background(), testActor())
	if err != nil {
		t.Fatal(err)
	}
	if len(export) != 4 {
		t.Fatalf("export has %d records, want 4 classes", len(export))
	}
	kinds := map[Kind]bool{}
	for _, item := range export {
		kinds[item.Kind] = true
	}
	for _, kind := range []Kind{KindMemory, KindPrompt, KindToolResult, KindTrace} {
		if !kinds[kind] {
			t.Errorf("export omitted %s", kind)
		}
	}
	if err := f.manager.RevokeSource(context.Background(), testActor(), testPin(), "document removed"); err != nil {
		t.Fatal(err)
	}
	for _, store := range f.stores {
		if rows := store.itemsFor("tenant-a"); len(rows) != 0 {
			t.Errorf("%s retained %d copies after revocation", store.Name(), len(rows))
		}
	}
}

func TestTodo_AGENT_040_Golden(t *testing.T) {
	got, err := MarshalExport([]Item{testItem("z-item", KindPrompt), testItem("a-item", KindToolResult)})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "agent040_export.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Fatalf("export differs from golden\n got: %s\nwant: %s", got, want)
	}
}

type fixture struct {
	manager     *Manager
	stores      []*testStore
	sources     *testSources
	disposition *testDisposition
	clock       *time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	stores := []*testStore{{name: "raw", items: map[string]Item{}, invalid: map[string]bool{}}, {name: "search", items: map[string]Item{}, invalid: map[string]bool{}}}
	sources := &testSources{decision: SourceDecision{Current: true, Version: "v1", Digest: "sha256:source", DataClass: "EMPLOYEE", Audience: []string{"private:owner"}, Invalidators: []string{"grant:17"}}}
	disposition := &testDisposition{decision: DispositionDecision{Resolved: true, CanDelete: true, Reason: "owner policy permits deletion"}}
	f := fixture{stores: stores, sources: sources, disposition: disposition, clock: &now}
	manager, err := NewManager(Config{Policies: testPolicies{policy: Policy{TenantID: "tenant-a", OwnerID: "owner-a", Purpose: testPurpose, Version: "p7", RetentionPolicyID: "retention-a", RetentionVersion: "r7", MaxTTL: 24 * time.Hour, AllowedClasses: []string{"EMPLOYEE"}, AllowedAudiences: []string{"private:owner", "private:agent"}}}, Sources: sources, Authorizer: testAuthorizer{}, Disposition: disposition, Stores: []CopyStore{stores[0], stores[1]}}, func() time.Time { return *f.clock })
	if err != nil {
		t.Fatal(err)
	}
	f.manager = manager
	return f
}
func testItem(id string, kind Kind) Item {
	return Item{ID: id, TenantID: "tenant-a", OwnerID: "owner-a", Kind: kind, SourceOwner: "documents", SourceID: "doc-1", SourceVersion: "v1", SourceDigest: "sha256:source", Audience: []string{"private:owner"}, Purpose: testPurpose, DataClass: "EMPLOYEE", RetentionPolicyID: "retention-a", RetentionVersion: "r7", CreatedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), TTL: 24 * time.Hour, Invalidators: []string{"grant:17"}, Payload: []byte("derived confidential data")}
}
func testPin() SourcePin {
	return SourcePin{TenantID: "tenant-a", Owner: "documents", ID: "doc-1", Version: "v1", Digest: "sha256:source", Audience: []string{"private:owner"}, Invalidators: []string{"grant:17"}}
}
func testActor() Actor { return Actor{TenantID: "tenant-a", PrincipalID: "user-a"} }

type testPolicies struct{ policy Policy }

func (p testPolicies) ResolveMemoryPolicy(_ context.Context, tenant, owner, purpose string) (Policy, error) {
	if p.policy.TenantID != tenant || p.policy.OwnerID != owner || p.policy.Purpose != purpose {
		return Policy{}, ErrDenied
	}
	return p.policy, nil
}

type testSources struct {
	decision SourceDecision
	err      error
}

func (s *testSources) CheckMemorySource(context.Context, SourcePin, string) (SourceDecision, error) {
	return s.decision, s.err
}

type testAuthorizer struct{ deny Operation }

func (a testAuthorizer) AuthorizeMemory(_ context.Context, _ Actor, op Operation, _ Item) error {
	if op == a.deny {
		return ErrDenied
	}
	return nil
}

type testDisposition struct {
	decision DispositionDecision
	err      error
}

func (d *testDisposition) ResolveMemoryDisposition(context.Context, Item, time.Time) (DispositionDecision, error) {
	return d.decision, d.err
}

type testStore struct {
	name                                             string
	items                                            map[string]Item
	foreignRows                                      []Item
	invalid                                          map[string]bool
	receipts                                         []Invalidation
	failPut, failGet, failList, failDelete, failTomb error
	incompleteList                                   bool
}

func (s *testStore) Name() string { return s.name }
func (s *testStore) Put(_ context.Context, item Item) error {
	if s.failPut != nil {
		return s.failPut
	}
	if s.invalid[scopeKey(item.TenantID, "ITEM", item.ID)] || s.invalid[scopeKey(item.TenantID, "SOURCE", SourceTarget(item.SourceOwner, item.SourceID))] {
		return ErrStale
	}
	s.items[itemKey(item.TenantID, item.ID)] = cloneItem(item)
	return nil
}
func (s *testStore) Get(_ context.Context, tenant, id string) (Item, error) {
	if s.failGet != nil {
		return Item{}, s.failGet
	}
	item, ok := s.items[itemKey(tenant, id)]
	if !ok {
		return Item{}, ErrNotFound
	}
	return cloneItem(item), nil
}
func (s *testStore) ListTenant(_ context.Context, tenant string) (Inventory, error) {
	if s.failList != nil {
		return Inventory{}, s.failList
	}
	var out []Item
	for _, item := range s.items {
		if item.TenantID == tenant {
			out = append(out, cloneItem(item))
		}
	}
	for _, item := range s.foreignRows {
		out = append(out, cloneItem(item))
	}
	return testInventory(out, s.incompleteList), nil
}
func (s *testStore) ListSource(_ context.Context, tenant, owner, source string) (Inventory, error) {
	if s.failList != nil {
		return Inventory{}, s.failList
	}
	var out []Item
	for _, item := range s.items {
		if item.TenantID == tenant && item.SourceOwner == owner && item.SourceID == source {
			out = append(out, cloneItem(item))
		}
	}
	return testInventory(out, s.incompleteList), nil
}
func (s *testStore) ListItem(_ context.Context, tenant, id string) (Inventory, error) {
	if s.failList != nil {
		return Inventory{}, s.failList
	}
	item, ok := s.items[itemKey(tenant, id)]
	if !ok {
		return testInventory(nil, s.incompleteList), nil
	}
	return testInventory([]Item{cloneItem(item)}, s.incompleteList), nil
}
func (s *testStore) Delete(_ context.Context, tenant, id string) error {
	if s.failDelete != nil {
		return s.failDelete
	}
	delete(s.items, itemKey(tenant, id))
	return nil
}
func (s *testStore) RecordInvalidation(_ context.Context, receipt Invalidation) error {
	if s.failTomb != nil {
		return s.failTomb
	}
	s.invalid[scopeKey(receipt.TenantID, receipt.TargetKind, receipt.TargetID)] = true
	s.receipts = append(s.receipts, receipt)
	return nil
}
func (s *testStore) Invalidated(_ context.Context, tenant, kind, id string) (bool, error) {
	if s.failGet != nil {
		return false, s.failGet
	}
	return s.invalid[scopeKey(tenant, kind, id)], nil
}
func (s *testStore) itemsFor(tenant string) []Item {
	items, _ := s.ListTenant(context.Background(), tenant)
	return items.Items
}

func testInventory(items []Item, incomplete bool) Inventory {
	return Inventory{Items: items, Complete: !incomplete, Watermark: "snapshot-1"}
}
func itemKey(tenant, id string) string { return tenant + "\x00" + id }
