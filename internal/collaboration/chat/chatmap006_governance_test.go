package chat

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type chatmapCountry string

func (c chatmapCountry) LocationCountry(context.Context, Principal) string { return string(c) }

func TestTodo_CHATMAP_006(t *testing.T) {
	ctx := context.Background()
	// Locations are sensitive personal data and map onto a class the outbound
	// verifier, exports and agents already understand.
	if LocationClassification != "SENSITIVE_PERSONAL_DATA" || !dlp.DataClass(LocationDLPClass).Valid() || dlp.DataClass(LocationDLPClass) != dlp.ClassSpecialCategory {
		t.Fatal("location classification does not reach the data-loss classes")
	}
	// Retention: an administrator shortens it and cannot lengthen it.
	f := newLiveFixture()
	f.repo.admin = true
	short := DefaultLocationPolicy()
	short.MaxRetention = 30 * time.Minute
	if err := f.live.SetPolicy(ctx, f.alice, "tt", "", short); err != nil {
		t.Fatal(err)
	}
	long := DefaultLocationPolicy()
	long.MaxRetention = LocationHardMaxDuration + time.Minute
	if err := f.live.SetPolicy(ctx, f.alice, "tt", "", long); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("retention lengthened past the stated maximum", err)
	}
	keep := AttachLocationRequest{Principal: f.alice, TenantID: "tt", ConversationID: "c", PostID: "m", PostRevision: 1, Place: f.place(42.1)}
	v, err := f.svc.Attach(ctx, keep)
	if err != nil || v.ExpiresAt == nil || v.ExpiresAt.Sub(f.now) > 30*time.Minute {
		t.Fatal("a share kept with its message outlived the retention limit", v.ExpiresAt, err)
	}
	// The same request retried a moment later is the same share, not a conflict.
	f.now = f.now.Add(time.Second)
	if again, err := f.svc.Attach(ctx, keep); err != nil || again.ID != v.ID {
		t.Fatal("retry of a shortened share", err)
	}
	// A country can be off until an agreement exists, with the basis recorded.
	g := newLiveFixture()
	g.repo.admin = true
	g.svc.Country = chatmapCountry("de")
	if err := g.live.SetJurisdiction(ctx, g.alice, "tt", LocationJurisdiction{Country: "DE", Enabled: false, Basis: "works council agreement pending"}); err != nil {
		t.Fatal(err)
	}
	denied := AttachLocationRequest{Principal: g.alice, TenantID: "tt", ConversationID: "c", PostID: "m", PostRevision: 1, Place: g.place(42.1)}
	if _, err := g.svc.Attach(ctx, denied); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("sharing allowed where it is switched off", err)
	}
	g.svc.Country = chatmapCountry("us")
	if _, err := g.svc.Attach(ctx, denied); err != nil {
		t.Fatal("sharing refused in a country where it is on", err)
	}
	// A person sees only their own shares and can end each.
	f2 := newLiveFixture()
	_, k, err := f2.start(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	bob := Principal{TenantID: "tt", SubjectID: "bob"}
	if mine, err := f2.live.MyShares(ctx, bob); err != nil || len(mine) != 0 {
		t.Fatal("another person's shares listed", mine, err)
	}
	if mine, err := f2.live.MyShares(ctx, f2.alice); err != nil || len(mine) != 1 {
		t.Fatal("own shares not listed", mine, err)
	}
	if err = f2.live.Stop(ctx, f2.alice, k); err != nil {
		t.Fatal(err)
	}
	if mine, _ := f2.live.MyShares(ctx, f2.alice); len(mine) != 0 {
		t.Fatal("ended share still listed")
	}
}

// TestTodo_CHATMAP_006_Dependencies proves, in both directions, that Chat's
// location code and time capture do not read each other.
func TestTodo_CHATMAP_006_Dependencies(t *testing.T) {
	const chatPrefix = "github.com/monstercameron/human-capital-management-suite/internal/"
	chatSide := []string{".", "../../data/chatstore", "../../humanwork/chatui"}
	timeSide := []string{"../../domains/clock", "../../workflow/timeclock"}
	imports := func(dir string) map[string][]string {
		out := map[string][]string{}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && path != dir && dir == "." {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return nil
			}
			for _, spec := range file.Imports {
				out[path] = append(out[path], strings.Trim(spec.Path.Value, `"`))
			}
			return nil
		})
		return out
	}
	for _, dir := range timeSide {
		if _, err := os.Stat(dir); err != nil {
			t.Fatal("time capture package moved; update this test", dir)
		}
	}
	for _, dir := range chatSide {
		for path, list := range imports(dir) {
			for _, imp := range list {
				if imp == chatPrefix+"domains/clock" || strings.HasPrefix(imp, chatPrefix+"workflow/timeclock") {
					t.Fatal("chat reads time capture", path, imp)
				}
			}
		}
	}
	for _, dir := range timeSide {
		for path, list := range imports(dir) {
			for _, imp := range list {
				if strings.HasPrefix(imp, chatPrefix+"collaboration/chat") || strings.HasPrefix(imp, chatPrefix+"data/chatstore") || strings.HasPrefix(imp, chatPrefix+"humanwork/chatui") {
					t.Fatal("time capture reads chat locations", path, imp)
				}
			}
		}
	}
}

// TestTodo_CHATMAP_006_NoReport asserts there is no way to list a person's
// shares across conversations other than the person's own list.
func TestTodo_CHATMAP_006_NoReport(t *testing.T) {
	allowed := map[string]bool{"Update": true, "Stop": true, "EndMine": true, "MyShares": true, "LiveMap": true, "Policy": true, "CanAdminister": true, "SetPolicy": true, "SetJurisdiction": true, "WorkspaceSettings": true}
	live := reflect.TypeOf(LiveLocationService{})
	for i := 0; i < live.NumMethod(); i++ {
		if !allowed[live.Method(i).Name] {
			t.Fatal("unexpected live location interface", live.Method(i).Name)
		}
	}
	// The only method returning shares without a conversation takes just the
	// caller's principal: no subject, no tenant, no conversation to widen.
	method, _ := live.MethodByName("MyShares")
	if method.Type.NumIn() != 3 || method.Type.In(2) != reflect.TypeOf(Principal{}) {
		t.Fatal("MyShares takes something other than the caller")
	}
	for _, repo := range []reflect.Type{reflect.TypeOf((*LocationRepository)(nil)).Elem(), reflect.TypeOf((*LiveLocationRepository)(nil)).Elem()} {
		for i := 0; i < repo.NumMethod(); i++ {
			name := strings.ToLower(repo.Method(i).Name)
			if strings.Contains(name, "byperson") || strings.Contains(name, "bysubject") || strings.Contains(name, "history") || strings.Contains(name, "route") {
				t.Fatal("repository can list by person or keep a route", repo.Method(i).Name)
			}
		}
	}
}

func TestTodo_CHATMAP_006_Golden(t *testing.T) {
	const golden = "testdata/chatmap006_governance.golden"
	ctx := context.Background()
	var b strings.Builder
	scenario := func(name string, arrange func(*liveFixture)) {
		f := newLiveFixture()
		arrange(f)
		pol, err := f.svc.effectivePolicy(ctx, f.alice, "tt", "c")
		fmt.Fprintf(&b, "%s: sharing=%t live=%t exact=%t maxLive=%s maxRetention=%s err=%v\n", name, pol.SharingEnabled, pol.LiveEnabled, pol.ExactAllowed, pol.MaxLive, pol.MaxRetention, err)
	}
	scenario("workspace default", func(*liveFixture) {})
	scenario("channel narrows", func(f *liveFixture) {
		f.repo.channel = &LocationPolicy{SharingEnabled: true, LiveEnabled: false, ExactAllowed: false, MaxLive: time.Hour, MaxRetention: 2 * time.Hour}
	})
	scenario("channel cannot widen", func(f *liveFixture) {
		f.repo.policy = LocationPolicy{SharingEnabled: true, ExactAllowed: false, MaxLive: 15 * time.Minute, MaxRetention: time.Hour}
		f.repo.channel = &LocationPolicy{SharingEnabled: true, LiveEnabled: true, ExactAllowed: true, MaxLive: 8 * time.Hour, MaxRetention: 8 * time.Hour}
	})
	scenario("country switched off", func(f *liveFixture) {
		f.svc.Country = chatmapCountry("DE")
		f.repo.jur = map[string]LocationJurisdiction{"DE": {Country: "DE", Enabled: false, Basis: "agreement pending"}}
	})
	scenario("workspace-wide rule off", func(f *liveFixture) {
		f.repo.jur = map[string]LocationJurisdiction{"*": {Country: "*", Enabled: false, Basis: "not yet agreed"}}
	})
	keys := []string{}
	for _, key := range []string{LiveEndedStopped, LiveEndedExpired, LiveEndedLeft, LiveEndedSignOut, LiveEndedPolicy} {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Fprintf(&b, "end reasons: %s\nclassification: %s -> %s\nhard maximum: %s\n", strings.Join(keys, ","), LocationClassification, LocationDLPClass, LocationHardMaxDuration)
	got := b.String()
	if os.Getenv("CHATMAP_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatal("governance matrix changed:\n" + got)
	}
}
