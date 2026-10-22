package application

import (
	"context"
	"io/fs"
	"net/url"
	"reflect"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/pressly/goose/v3"
)

// Issue #58: composeChat rebuilt composedChat from the integrate2 wave in
// both of its returns (stream enabled and stream disabled) without carrying
// wave.locations / wave.locationPictures, so serve passed a nil surface to
// OverlayChatmap and every location call answered 503 location_unavailable.
// This drives the real composeChat over an isolated pgtest store (never a
// live database) with in-memory location ports and asserts both surfaces
// survive each return with their composed identity intact.
func TestComposeChatPreservesLocationSurfaces(t *testing.T) {
	ctx := context.Background()
	chatDB, coreDB := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chatDB.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	databaseURL := func(raw, schema string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	core, err := pgxadapter.NewPool(ctx, databaseURL(coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()

	grant := &integrate2LocationGrant{allowed: true}
	sites := chatcore.FixtureLocationSites{
		Sites:          []chatcore.LocationSite{{ID: "site-1", Label: "Head office", Address: "1 Main Street"}},
		AllowedSubject: "alice", AllowedTenant: "host",
	}
	lookup := chatcore.FixtureAddressLookup{Places: []chatcore.LocationPlace{{Label: "Head office", Address: "1 Main Street"}}}
	usage := &integrate2LocationUsage{}
	composition := ChatComposition{LocationGrant: grant, LocationSites: sites, LocationLookup: lookup, LocationUsage: usage}

	for _, tc := range []struct {
		name       string
		cursorKey  string
		wantStream bool
	}{
		{"stream enabled", "chat-location-composition-stream-key", true},
		{"stream disabled", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: tc.cursorKey}, time.Now, chatAdminFacts{role: "employee"}, core, nil, composition)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.close()

			// Prove this subtest really exercised the intended return: the
			// enabled branch installs the stream runtime on the service and
			// the extension admission, the disabled branch leaves both nil.
			service, ok := runtime.service.(*streamingChatService)
			if !ok {
				t.Fatalf("composed service = %T, want *streamingChatService", runtime.service)
			}
			if (service.runtime != nil) != tc.wantStream || (runtime.extensions.Admission != nil) != tc.wantStream {
				t.Fatalf("stream branch mismatch: service runtime set=%t, extension admission set=%t, want stream=%t", service.runtime != nil, runtime.extensions.Admission != nil, tc.wantStream)
			}

			if runtime.locations == nil {
				t.Fatal("composed chat dropped locations: serve would pass nil to OverlayChatmap (503 location_unavailable)")
			}
			if runtime.locationPictures == nil {
				t.Fatal("composed chat dropped locationPictures: serve would pass nil to OverlayChatmap (503 map_unavailable)")
			}
			location, ok := runtime.locations.(*integrate2Location)
			if !ok || location.LocationService == nil {
				t.Fatalf("locations = %T, want the composed *integrate2Location with its LocationService", runtime.locations)
			}
			pictures, ok := runtime.locationPictures.(*ChatmapPictureCache)
			if !ok {
				t.Fatalf("locationPictures = %T, want the composed *ChatmapPictureCache", runtime.locationPictures)
			}
			// Identity: pictures read through the very LocationService the
			// location surface writes through, and that service still holds
			// the exact in-memory ports this composition supplied — not
			// placeholders rebuilt by the final composedChat literal.
			if pictures.Locations != location.LocationService {
				t.Fatal("locationPictures.Locations is not identical to locations.LocationService")
			}
			if location.Grant != grant {
				t.Fatal("locations lost the supplied LocationGrant identity")
			}
			if location.Usage != usage {
				t.Fatal("locations lost the supplied LocationUsage identity")
			}
			if pictures.Usage != ChatmapUsagePort(usage) {
				t.Fatal("locationPictures lost the supplied LocationUsage identity")
			}
			if !reflect.DeepEqual(location.Sites, chatcore.LocationSitePort(sites)) {
				t.Fatalf("locations Sites = %#v, want the supplied fixture %#v", location.Sites, sites)
			}
			if !reflect.DeepEqual(location.Lookup, chatcore.AddressLookup(lookup)) {
				t.Fatalf("locations Lookup = %#v, want the supplied fixture %#v", location.Lookup, lookup)
			}
			if location.Routes == nil || location.Repo == nil || location.Chat == nil {
				t.Fatal("composed location surface is a placeholder: routes, repository or chat reader missing")
			}
		})
	}
}
