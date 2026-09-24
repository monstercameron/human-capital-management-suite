package cache

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_REV_032_02(t *testing.T) {
	store := New[string](Config{MaxEntries: 8, TTL: time.Minute})
	readerA, err := NewTenantReadCache[string](store, "tenant-a", "policy-1", "content-1", "people")
	if err != nil {
		t.Fatal(err)
	}
	readerB, err := NewTenantReadCache[string](store, "tenant-b", "policy-1", "content-1", "people")
	if err != nil {
		t.Fatal(err)
	}
	loads := 0
	load := func() (string, error) { loads++; return "tenant-a record", nil }
	allow := func(string) bool { return true }
	if got, err := readerA.GetOrLoad("person-7", load, allow); err != nil || got != "tenant-a record" {
		t.Fatalf("first tenant read=%q, %v", got, err)
	}
	if got, err := readerA.GetOrLoad("person-7", load, allow); err != nil || got != "tenant-a record" || loads != 1 {
		t.Fatalf("cached tenant read=%q, %v, loads=%d", got, err, loads)
	}
	if got, err := readerB.GetOrLoad("person-7", func() (string, error) { return "tenant-b record", nil }, allow); err != nil || got != "tenant-b record" {
		t.Fatalf("other tenant read=%q, %v", got, err)
	}
	if got, ok := store.Get(mustKey(t, "tenant-a", "person-7"), allow); !ok || got != "tenant-a record" {
		t.Fatalf("tenant-a cache value=%q, %v", got, ok)
	}
}

func TestTodo_REV_032_02_Integration(t *testing.T) {
	store := New[string](Config{MaxEntries: 8, TTL: time.Minute})
	reader, err := NewTenantReadCache[string](store, "tenant-safe", "policy-9", "revision-4", "payroll")
	if err != nil {
		t.Fatal(err)
	}
	loadCount := 0
	loader := func() (string, error) { loadCount++; return "authorized payroll view", nil }
	authorize := func(value string) bool { return value == "authorized payroll view" }
	if _, err := reader.GetOrLoad("employee-1", loader, authorize); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetOrLoad("employee-1", loader, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if loadCount != 2 {
		t.Fatalf("denied cached value should be evicted and recomputed; loader calls=%d", loadCount)
	}
	if _, err := reader.GetOrLoad("employee-1", loader, nil); !errors.Is(err, ErrKeyInvalid) {
		t.Fatalf("missing authorization error=%v, want ErrKeyInvalid", err)
	}
}

func TestTodo_REV_032_02_Security(t *testing.T) {
	store := New[string](Config{MaxEntries: 8, TTL: time.Minute})
	owner, err := NewTenantReadCache[string](store, "tenant-owner", "policy-1", "content-1", "payroll")
	if err != nil {
		t.Fatal(err)
	}
	otherTenant, err := NewTenantReadCache[string](store, "tenant-other", "policy-1", "content-1", "payroll")
	if err != nil {
		t.Fatal(err)
	}
	ownerValue, err := owner.GetOrLoad("employee-8", func() (string, error) { return "owner-only", nil }, func(string) bool { return true })
	if err != nil || ownerValue != "owner-only" {
		t.Fatalf("owner load=%q, %v", ownerValue, err)
	}
	loads := 0
	otherValue, err := otherTenant.GetOrLoad("employee-8", func() (string, error) { loads++; return "other-tenant", nil }, func(string) bool { return true })
	if err != nil || otherValue != "other-tenant" || loads != 1 {
		t.Fatalf("cross-tenant read=%q, %v, authoritative loads=%d", otherValue, err, loads)
	}
}

func mustKey(t *testing.T, tenant, id string) Key {
	t.Helper()
	key, err := NewKey(tenant, "policy-1", "content-1", "people", id)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
