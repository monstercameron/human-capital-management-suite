package productui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
)

type i18n004PublisherStore struct {
	published int
	activated int
}

func (s *i18n004PublisherStore) Publish(context.Context, i18n.Scope, i18n.CatalogRevision) error {
	s.published++
	return nil
}
func (s *i18n004PublisherStore) Activate(context.Context, i18n.Scope, string, string) error {
	s.activated++
	return nil
}
func (*i18n004PublisherStore) Active(context.Context, i18n.Scope, string, time.Time) (i18n.CatalogRevision, error) {
	return i18n.CatalogRevision{}, i18n.ErrNoActiveRevision
}

type i18n004PublisherAuth struct{ err error }

func (a i18n004PublisherAuth) AuthorizeCatalogPublication(context.Context, i18n.Scope, string) error {
	return a.err
}

func i18n004Revision(at time.Time, key, text, meaning string) i18n.CatalogRevision {
	r := i18n.CatalogRevision{ID: "catalog-r2", PreviousRevision: "catalog-r1", Locale: "en-US", Version: "catalog-r2", CreatedAt: at, EffectiveFrom: at,
		Translations: []i18n.Translation{{Key: key, Text: text, MeaningID: meaning, Source: "reviewed-copy/" + key, Classification: "UI", EffectiveFrom: at}}}
	r.CanonicalDigest = i18n.DigestRevision(r)
	r.Digest = r.CanonicalDigest
	return r
}

func TestTodo_I18N_004(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	key := "page.home.title"
	revision := i18n004Revision(at, key, "Revised workspace", key)
	scope := i18n.Scope{Tenant: "tenant-one", Product: "workspace"}
	locale, err := ApplyActivatedCatalog(ResolveProductLocale("en-US"), scope, revision, at)
	if err != nil {
		t.Fatal(err)
	}
	got, err := locale.Resolve(key)
	if err != nil || got.Text != "Revised workspace" || got.CatalogVersion != locale.CatalogVersion {
		t.Fatalf("resolved activated catalog = %#v, %v", got, err)
	}
	if locale.CatalogRevision != revision.ID {
		t.Fatalf("catalog revision diagnostic = %q", locale.CatalogRevision)
	}
	english, err := locale.Resolve("page.home.subtitle")
	if err != nil || english.Text == "" {
		t.Fatalf("compiled last-known-good fallback = %#v, %v", english, err)
	}
	store := &i18n004PublisherStore{}
	if err := PublishActivatedCatalog(context.Background(), store, i18n004PublisherAuth{}, scope, "reviewer-1", revision); err != nil {
		t.Fatalf("authorized publication: %v", err)
	}
	if store.published != 1 || store.activated != 1 {
		t.Fatalf("publication/activation calls = %d/%d", store.published, store.activated)
	}
}

func TestTodo_I18N_004_Security(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	key := "page.home.title"
	revision := i18n004Revision(at, key, "Tenant copy", key)
	one, err := ApplyActivatedCatalog(ResolveProductLocale("en-US"), i18n.Scope{Tenant: "tenant-one", Product: "workspace"}, revision, at)
	if err != nil {
		t.Fatal(err)
	}
	two, err := ApplyActivatedCatalog(ResolveProductLocale("en-US"), i18n.Scope{Tenant: "tenant-two", Product: "workspace"}, revision, at)
	if err != nil {
		t.Fatal(err)
	}
	if one.CatalogVersion == two.CatalogVersion {
		t.Fatal("tenant scopes share an activated resolver version")
	}
	if leaked := ResolveProductLocale("en-US").Text(key); leaked == "Tenant copy" {
		t.Fatal("scoped text leaked into the build-time resolver")
	}
	badMeaning := i18n004Revision(at, key, "Changed semantics", "different.meaning")
	if _, err := ApplyActivatedCatalog(ResolveProductLocale("en-US"), i18n.Scope{Tenant: "tenant-one", Product: "workspace"}, badMeaning, at); !errors.Is(err, i18n.ErrMeaningChanged) {
		t.Fatalf("meaning mutation error = %v", err)
	}
	unknown := i18n004Revision(at, "unregistered.route", "forged route", "unregistered.route")
	if _, err := ApplyActivatedCatalog(ResolveProductLocale("en-US"), i18n.Scope{Tenant: "tenant-one", Product: "workspace"}, unknown, at); !errors.Is(err, i18n.ErrMeaningChanged) {
		t.Fatalf("unknown semantic key error = %v", err)
	}
	deniedStore := &i18n004PublisherStore{}
	denied := errors.New("publisher is not authorized")
	if err := PublishActivatedCatalog(context.Background(), deniedStore, i18n004PublisherAuth{err: denied}, i18n.Scope{Tenant: "tenant-one", Product: "workspace"}, "viewer-1", revision); !errors.Is(err, denied) {
		t.Fatalf("unauthorized publication error = %v", err)
	}
	if deniedStore.published != 0 || deniedStore.activated != 0 {
		t.Fatal("unauthorized publication mutated the catalog store")
	}
}

func TestTodo_I18N_004_Recovery(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	revision := i18n004Revision(at, "page.home.title", "Invalid legal copy", "page.home.title")
	revision.Translations[0].Classification = "LEGAL_TEXT"
	revision.CanonicalDigest = i18n.DigestRevision(revision)
	revision.Digest = revision.CanonicalDigest
	if _, err := ApplyActivatedCatalog(ResolveProductLocale("en-US"), i18n.Scope{Tenant: "tenant-one", Product: "workspace"}, revision, at); !errors.Is(err, i18n.ErrLegalReviewRequired) {
		t.Fatalf("unreviewed legal activation error = %v", err)
	}
	if got := ResolveProductLocale("en-US").CatalogVersion; got != productCatalogVersion {
		t.Fatalf("failed activation changed fallback catalog: %q", got)
	}
}
