package list

import (
	"testing"
	"time"
)

var now = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
var secret = "test-secret-32-bytes-long-1234"

func allowed() map[string]bool {
	return map[string]bool{"name": true, "title": true, "email": false, "salary": false}
}

func items() []Item {
	return []Item{
		{ID: "a1", Data: map[string]any{"name": "alice", "title": "eng", "salary": 100}},
		{ID: "b2", Data: map[string]any{"name": "bob", "title": "mgr", "email": "bob@example.com"}},
		{ID: "c3", Data: map[string]any{"name": "carol", "title": "dir"}},
		{ID: "d4", Data: map[string]any{"name": "dave", "title": "vp"}},
		{ID: "e5", Data: map[string]any{"name": "eve", "title": "eng"}},
	}
}

func TestEndpointListCursorAndFieldMaskCannotBroadenScopeOrRevealExistence(t *testing.T) {
	t.Run("stable ordering and bounded pages", func(t *testing.T) {
		req := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", PageSize: 2, FieldMask: []string{"name", "title"}}
		resp, err := ListItems(items(), req, secret, now, allowed())
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].ID != "a1" || resp.Items[1].ID != "b2" {
			t.Fatalf("page1 ids %v", resp.Items)
		}
		if resp.NextCursor == "" {
			t.Fatal("expected next cursor")
		}
		req2 := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", PageSize: 2, Cursor: resp.NextCursor, FieldMask: []string{"name"}}
		resp2, err := ListItems(items(), req2, secret, now, allowed())
		if err != nil {
			t.Fatalf("page2: %v", err)
		}
		if len(resp2.Items) != 2 || resp2.Items[0].ID != "c3" {
			t.Fatalf("page2 ids %v", resp2.Items)
		}
		if resp.Items[0].ID == resp2.Items[0].ID {
			t.Fatal("duplicate entries across pages")
		}
	})
	t.Run("cursor tampering denied", func(t *testing.T) {
		req := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", PageSize: 2, FieldMask: []string{"name"}}
		resp, _ := ListItems(items(), req, secret, now, allowed())
		bad := resp.NextCursor + "x"
		req2 := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", Cursor: bad, FieldMask: []string{"name"}}
		if _, err := ListItems(items(), req2, secret, now, allowed()); err == nil {
			t.Fatal("tampered cursor should be rejected")
		}
	})
	t.Run("cursor replay by another principal denied", func(t *testing.T) {
		req := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", PageSize: 2, FieldMask: []string{"name"}}
		resp, _ := ListItems(items(), req, secret, now, allowed())
		req2 := ListRequest{Principal: "user2", Tenant: "t1", Filter: "active", Cursor: resp.NextCursor, FieldMask: []string{"name"}}
		if _, err := ListItems(items(), req2, secret, now, allowed()); err == nil {
			t.Fatal("cross principal cursor should be rejected")
		}
	})
	t.Run("cursor tenant mismatch denied", func(t *testing.T) {
		req := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", PageSize: 2, FieldMask: []string{"name"}}
		resp, _ := ListItems(items(), req, secret, now, allowed())
		req2 := ListRequest{Principal: "user1", Tenant: "t2", Filter: "active", Cursor: resp.NextCursor, FieldMask: []string{"name"}}
		if _, err := ListItems(items(), req2, secret, now, allowed()); err == nil {
			t.Fatal("cross tenant cursor should be rejected")
		}
	})
	t.Run("cursor filter mismatch denied", func(t *testing.T) {
		req := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", PageSize: 2, FieldMask: []string{"name"}}
		resp, _ := ListItems(items(), req, secret, now, allowed())
		req2 := ListRequest{Principal: "user1", Tenant: "t1", Filter: "other", Cursor: resp.NextCursor, FieldMask: []string{"name"}}
		if _, err := ListItems(items(), req2, secret, now, allowed()); err == nil {
			t.Fatal("filter mismatch should be rejected")
		}
	})
	t.Run("cursor expiry", func(t *testing.T) {
		req := ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", PageSize: 2, FieldMask: []string{"name"}}
		resp, _ := ListItems(items(), req, secret, now, allowed())
		if _, err := ListItems(items(), ListRequest{Principal: "user1", Tenant: "t1", Filter: "active", Cursor: resp.NextCursor, FieldMask: []string{"name"}}, secret, now.Add(CursorTTL*2), allowed()); err == nil {
			t.Fatal("expired cursor should be rejected")
		}
	})
	t.Run("page size unbounded denied", func(t *testing.T) {
		if _, err := ListItems(items(), ListRequest{Principal: "u", Tenant: "t", PageSize: 1000}, secret, now, allowed()); err == nil {
			t.Fatal("unbounded page size should be rejected")
		}
		if _, err := ListItems(items(), ListRequest{Principal: "u", Tenant: "t", PageSize: 0}, secret, now, allowed()); err != nil {
			t.Fatalf("default page size should be allowed: %v", err)
		}
	})
	t.Run("field mask restricts", func(t *testing.T) {
		req := ListRequest{Principal: "user1", Tenant: "t1", PageSize: 10, FieldMask: []string{"name", "salary"}}
		resp, err := ListItems(items(), req, secret, now, allowed())
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, it := range resp.Items {
			if _, ok := it.Data["salary"]; ok {
				t.Fatal("salary should be redacted")
			}
			if _, ok := it.Data["name"]; !ok {
				t.Fatal("name should be present")
			}
		}
		if resp.RedactedCount == 0 {
			t.Fatal("expected redacted count")
		}
	})
	t.Run("display name not identity", func(t *testing.T) {
		rn, err := ParseResourceName("tenants/t1/workers/a1")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if rn.ID == "alice" {
			t.Fatal("display name used as identity")
		}
		if _, err := ParseResourceName("workers/a1"); err == nil {
			t.Fatal("invalid resource name should fail")
		}
	})
	t.Run("uniform non-disclosing error", func(t *testing.T) {
		e1 := UniformError().Error()
		e2 := UniformError().Error()
		if e1 != e2 {
			t.Fatal("errors should be uniform")
		}
		if e1 != "not found" {
			t.Fatalf("want not found got %s", e1)
		}
	})
	t.Run("no page drift", func(t *testing.T) {
		all := items()
		req := ListRequest{Principal: "u", Tenant: "t", Filter: "f", PageSize: 1, FieldMask: []string{"name"}}
		seen := map[string]bool{}
		cursor := ""
		for i := 0; i < len(all); i++ {
			req.Cursor = cursor
			resp, err := ListItems(all, req, secret, now, map[string]bool{"name": true})
			if err != nil {
				t.Fatalf("page %d: %v", i, err)
			}
			if len(resp.Items) != 1 {
				t.Fatalf("page %d len %d", i, len(resp.Items))
			}
			id := resp.Items[0].ID
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
			cursor = resp.NextCursor
			if i == len(all)-1 && cursor != "" {
				t.Fatal("last page should have no cursor")
			}
		}
		if len(seen) != len(all) {
			t.Fatalf("seen %d want %d", len(seen), len(all))
		}
	})
}

func TestTodo_ENDPOINT_005(t *testing.T) {
	items := items()
	request := ListRequest{Principal: "u", Tenant: "t", Filter: "f", PageSize: 1, FieldMask: []string{"name"}}
	response, err := ListItems(items, request, secret, now, allowed())
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].ID == "" {
		t.Fatalf("first page omitted visible item: %+v", response.Items)
	}
	if response.NextCursor == "" {
		t.Fatal("bounded first page omitted continuation cursor")
	}
	if response.Items[0].Data["name"] == nil {
		t.Fatal("requested name field was omitted")
	}
}

func TestTodo_ENDPOINT_005_Golden(t *testing.T) {
	rn, _ := ParseResourceName("tenants/t1/workers/a1")
	if rn.String() != "tenants/t1/workers/a1" {
		t.Fatalf("resource string %s", rn.String())
	}
	d1 := DigestList(items())
	d2 := DigestList(items())
	if d1 != d2 {
		t.Fatal("digest not deterministic")
	}
	if len(d1) != 64 {
		t.Fatalf("digest len %d", len(d1))
	}
	cp := CursorPayload{Principal: "p", Tenant: "t", Filter: "f", Watermark: "w", Version: 1, ExpiresAt: now.Add(CursorTTL).Unix(), Nonce: "n"}
	tok, _ := EncodeCursor(cp, secret)
	cp2, err := DecodeCursor(tok, secret, now, "p", "t", "f")
	if err != nil || cp2.Watermark != "w" {
		t.Fatalf("cursor golden failed %v", err)
	}
}

func TestTodo_ENDPOINT_005_Property(t *testing.T) {
	for i := 0; i < 20; i++ {
		cp := CursorPayload{Principal: "p", Tenant: "t", Filter: "f", Watermark: "a1", Version: 1, ExpiresAt: now.Add(CursorTTL).Unix(), Nonce: "n"}
		tok, _ := EncodeCursor(cp, secret)
		if _, err := DecodeCursor(tok, secret, now, "p", "t", "f"); err != nil {
			t.Fatalf("property decode %v", err)
		}
	}
}

func FuzzTodo_ENDPOINT_005(f *testing.F) {
	f.Add("tenants/t1/workers/a1", "p", "t", "f", 10)
	f.Fuzz(func(t *testing.T, name, principal, tenant, filter string, sz int) {
		_, _ = ParseResourceName(name)
		_, _ = NormalizePageSize(sz)
		cp := CursorPayload{Principal: principal, Tenant: tenant, Filter: filter, Watermark: "w", Version: 1, ExpiresAt: now.Add(CursorTTL).Unix(), Nonce: "x"}
		tok, _ := EncodeCursor(cp, secret)
		_, _ = DecodeCursor(tok, secret, now, principal, tenant, filter)
	})
}

func TestTodo_ENDPOINT_005_Integration(t *testing.T) {
	all := items()
	req := ListRequest{Principal: "u", Tenant: "t", Filter: "f", PageSize: 2, FieldMask: []string{"name"}}
	resp, err := ListItems(all, req, secret, now, allowed())
	if err != nil {
		t.Fatalf("integration list: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("integration count %d", len(resp.Items))
	}
}

func TestTodo_ENDPOINT_005_Security(t *testing.T) {
	if _, err := DecodeCursor("bad.token", secret, now, "p", "t", "f"); err == nil {
		t.Fatal("bad token should fail")
	}
	cp := CursorPayload{Principal: "p", Tenant: "t", Filter: "f", Watermark: "w", Version: 1, ExpiresAt: now.Unix() - 1, Nonce: "n"}
	tok, _ := EncodeCursor(cp, secret)
	if _, err := DecodeCursor(tok, secret, now, "p", "t", "f"); err == nil {
		t.Fatal("expired should fail")
	}
}

func TestTodo_ENDPOINT_005_Conformance(t *testing.T) {
	d1 := DigestList([]Item{{ID: "b"}, {ID: "a"}})
	d2 := DigestList([]Item{{ID: "a"}, {ID: "b"}})
	if d1 != d2 {
		t.Fatal("digest should be order independent")
	}
}

func BenchmarkTodo_ENDPOINT_005(b *testing.B) {
	all := items()
	req := ListRequest{Principal: "u", Tenant: "t", Filter: "f", PageSize: 20, FieldMask: []string{"name"}}
	for i := 0; i < b.N; i++ {
		_, _ = ListItems(all, req, secret, now, allowed())
	}
}
