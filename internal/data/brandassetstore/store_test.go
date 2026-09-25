package brandassetstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/brandasset"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestNewRequiresDatabase(t *testing.T) {
	if _, err := New(nil, nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
}

func TestValidateAssetBindsMediaDimensionsDigestAndRasterProxy(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 120, B: 180, A: 255})
		}
	}
	var original, proxy bytes.Buffer
	if err := png.Encode(&original, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&proxy, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original.Bytes())
	asset := brandasset.Asset{Name: "brand.png", MediaType: "image/png", Width: 32, Height: 24, Digest: hex.EncodeToString(digest[:]), Original: original.Bytes(), Proxy: proxy.Bytes(), ProxyType: "image/jpeg"}
	if err := validateAsset(asset); err != nil {
		t.Fatalf("valid raster asset rejected: %v", err)
	}

	for name, candidate := range map[string]brandasset.Asset{
		"svg media": func() brandasset.Asset {
			v := asset
			v.Name = "brand.svg"
			v.MediaType = "image/svg+xml"
			v.Original = []byte(`<svg width="32" height="24" onload="alert(1)"/>`)
			d := sha256.Sum256(v.Original)
			v.Digest = hex.EncodeToString(d[:])
			return v
		}(),
		"extension mismatch": func() brandasset.Asset { v := asset; v.Name = "brand.jpg"; return v }(),
		"media mismatch":     func() brandasset.Asset { v := asset; v.MediaType = "image/jpeg"; return v }(),
		"dimension mismatch": func() brandasset.Asset { v := asset; v.Width = 31; return v }(),
		"digest mismatch":    func() brandasset.Asset { v := asset; v.Digest = hex.EncodeToString(make([]byte, 32)); return v }(),
		"proxy mismatch":     func() brandasset.Asset { v := asset; v.ProxyType = "image/svg+xml"; return v }(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateAsset(candidate); !errors.Is(err, ErrInvalid) {
				t.Fatalf("validateAsset err=%v, want ErrInvalid", err)
			}
		})
	}
}

func TestTodo_UXAUDIT_022_Integration(t *testing.T) {
	db := pgtest.New(t)
	first, second := uuid.New(), uuid.New()
	for _, item := range []struct {
		id  uuid.UUID
		key string
	}{{first, "brand-one"}, {second, "brand-two"}} {
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$2,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, item.id, item.key)
	}
	mapper := func(key values.TenantId) uuid.UUID {
		switch key {
		case "brand-one":
			return first
		case "brand-two":
			return second
		default:
			return uuid.Nil
		}
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	store, err := New(conn, mapper)
	if err != nil {
		t.Fatal(err)
	}
	asset := storeTestAsset(t)
	ctx := context.Background()
	firstRevision, err := store.Save(ctx, "brand-one", 0, "admin-1", asset)
	if err != nil || firstRevision.Revision != 1 {
		t.Fatalf("Save revision 1 = %+v, %v", firstRevision, err)
	}
	if got, ok, err := store.Read(ctx, "brand-one", asset.Digest); err != nil || !ok || !bytes.Equal(got.Original, asset.Original) {
		t.Fatalf("same tenant Read = found:%v err:%v", ok, err)
	} else {
		digest := sha256.Sum256(got.Original)
		if hex.EncodeToString(digest[:]) != asset.Digest {
			t.Fatal("stored digest does not match served bytes")
		}
	}
	if _, ok, err := store.Read(ctx, "brand-two", asset.Digest); err != nil || ok {
		t.Fatalf("cross tenant Read found=%v err=%v", ok, err)
	}
	if rows, more, err := store.History(ctx, "brand-two", 0); err != nil || more || len(rows) != 0 {
		t.Fatalf("cross tenant History len=%d err=%v", len(rows), err)
	}
	if _, err := store.Save(ctx, "brand-one", 0, "admin-1", asset); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Save = %v, want ErrConflict", err)
	}
	removed, err := store.Remove(ctx, "brand-one", 1, "admin-1")
	if err != nil || !removed.Removed || removed.Revision != 2 {
		t.Fatalf("Remove = %+v, %v", removed, err)
	}
	if _, ok, err := store.Read(ctx, "brand-one", asset.Digest); err != nil || ok {
		t.Fatalf("removed Read found=%v err=%v", ok, err)
	}
	restored, err := store.Rollback(ctx, "brand-one", 2, 1, "admin-1")
	if err != nil || restored.Removed || restored.Revision != 3 {
		t.Fatalf("Rollback = %+v, %v", restored, err)
	}
	got, ok, err := store.Read(ctx, "brand-one", asset.Digest)
	if err != nil || !ok || !bytes.Equal(got.Original, asset.Original) {
		t.Fatalf("restored Read found=%v err=%v", ok, err)
	}
	secondConn := db.NewConn(t)
	if _, err := secondConn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume second app role: %v", err)
	}
	secondStore, err := New(secondConn, mapper)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, candidate := range []*Store{store, secondStore} {
		candidate := candidate
		go func() {
			_, saveErr := candidate.Save(ctx, "brand-one", 3, "admin-concurrent", asset)
			results <- saveErr
		}()
	}
	firstResult, secondResult := <-results, <-results
	successes, conflicts := 0, 0
	for _, result := range []error{firstResult, secondResult} {
		if result == nil {
			successes++
		} else if errors.Is(result, ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent Save error = %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent CAS results success=%d conflict=%d", successes, conflicts)
	}
	history, more, err := store.History(ctx, "brand-one", 0)
	if err != nil || more || len(history) != 4 || history[0].Revision != 4 || history[1].Revision != 3 || history[2].Revision != 2 || !history[2].Removed {
		t.Fatalf("History=%+v err=%v", history, err)
	}
	for revision := 4; revision < brandasset.HistoryPageSize+6; revision++ {
		if _, err := store.Save(ctx, "brand-one", revision, "admin-history", asset); err != nil {
			t.Fatalf("history fixture Save expected=%d: %v", revision, err)
		}
	}
	firstPage, hasMore, err := store.History(ctx, "brand-one", 0)
	if err != nil || !hasMore || len(firstPage) != brandasset.HistoryPageSize {
		t.Fatalf("first history page len=%d hasMore=%v err=%v", len(firstPage), hasMore, err)
	}
	secondPage, hasMore, err := store.History(ctx, "brand-one", firstPage[len(firstPage)-1].Revision)
	if err != nil || hasMore || len(secondPage) != 6 {
		t.Fatalf("second history page len=%d hasMore=%v err=%v", len(secondPage), hasMore, err)
	}
	if secondPage[0].Revision >= firstPage[len(firstPage)-1].Revision || secondPage[len(secondPage)-1].Revision != 1 {
		t.Fatalf("history cursor duplicated or skipped revisions: first=%d second=%d..%d", firstPage[len(firstPage)-1].Revision, secondPage[0].Revision, secondPage[len(secondPage)-1].Revision)
	}
}

func storeTestAsset(t *testing.T) brandasset.Asset {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 12, G: 90, B: 160, A: 255})
		}
	}
	var original, proxy bytes.Buffer
	if err := png.Encode(&original, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&proxy, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original.Bytes())
	return brandasset.Asset{Name: "logo.png", MediaType: "image/png", Width: 40, Height: 32, Digest: hex.EncodeToString(digest[:]), Original: original.Bytes(), Proxy: proxy.Bytes(), ProxyType: "image/jpeg"}
}

func TestBrandAssetStoreRejectsInvalidScopeAndActor(t *testing.T) {
	store := &Store{db: inertDB{}, tenantUUID: func(tenant values.TenantId) uuid.UUID {
		if tenant == values.TenantId(uuid.Nil.String()) {
			return uuid.Nil
		}
		return uuid.New()
	}}
	for _, test := range []struct{ tenant, actor string }{
		{"", "administrator"}, {uuid.Nil.String(), "administrator"}, {uuid.NewString(), " "}, {uuid.NewString(), " administrator"},
	} {
		if err := validate(store, test.tenant, test.actor); err == nil {
			t.Errorf("validate(%q,%q) succeeded", test.tenant, test.actor)
		}
	}
	if err := validate(store, "logical-tenant", "administrator"); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}
}

type inertDB struct{}

func (inertDB) Begin(context.Context) (dbport.Tx, error) { return nil, errors.New("unused") }
