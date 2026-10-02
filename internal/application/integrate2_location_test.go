package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type integrate2LocationReader struct {
	*integrate2ReaderFixture
	*integrate2RouteFixture
}

func (f *integrate2LocationReader) ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error) {
	return chat.Conversation{}, nil, nil
}

type integrate2LocationGrant struct {
	allowed bool
	calls   int
}

func (f *integrate2LocationGrant) AllowsLocation(context.Context, chat.Principal, string, string) bool {
	f.calls++
	return f.allowed
}

type integrate2LocationUsage struct{}

func (integrate2LocationUsage) RecordLocationUsage(context.Context, string, string, string, string) error {
	return nil
}

func TestIntegrate2LocationNeedsGovernedPorts(t *testing.T) {
	reader := &integrate2LocationReader{integrate2ReaderFixture: &integrate2ReaderFixture{}, integrate2RouteFixture: &integrate2RouteFixture{}}
	repo := &chatstore.Store{}
	grant := &integrate2LocationGrant{}
	input := ChatComposition{LocationGrant: grant, LocationSites: chat.FixtureLocationSites{}, LocationLookup: chat.FixtureAddressLookup{}, LocationUsage: integrate2LocationUsage{}}
	for _, missing := range []string{"grant", "sites", "lookup", "usage"} {
		partial := input
		switch missing {
		case "grant":
			partial.LocationGrant = nil
		case "sites":
			partial.LocationSites = nil
		case "lookup":
			partial.LocationLookup = nil
		case "usage":
			partial.LocationUsage = nil
		}
		if surface, pictures := integrate2ComposeLocations(reader, repo, partial, time.Now); surface != nil || pictures != nil {
			t.Fatalf("missing %s enabled locations", missing)
		}
	}
	var absent *integrate2LocationGrant
	partial := input
	partial.LocationGrant = absent
	if surface, _ := integrate2ComposeLocations(reader, repo, partial, time.Now); surface != nil {
		t.Fatal("typed nil grant enabled locations")
	}
	surface, pictures := integrate2ComposeLocations(reader, repo, input, time.Now)
	if surface == nil || pictures == nil {
		t.Fatal("complete location ports were not composed")
	}
	location := surface.(*integrate2Location)
	cache := pictures.(*ChatmapPictureCache)
	if location.Grant != grant || !reflect.DeepEqual(location.Sites, input.LocationSites) || !reflect.DeepEqual(location.Lookup, input.LocationLookup) || location.Usage != input.LocationUsage || cache.Locations != location.LocationService || cache.Usage != input.LocationUsage {
		t.Fatal("location ports were replaced by placeholders")
	}
	if _, err := surface.Attach(t.Context(), chat.AttachLocationRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "alice"}, TenantID: "tenant", ConversationID: "room"}); !errors.Is(err, chat.ErrPermissionDenied) || grant.calls != 1 || reader.integrate2RouteFixture.calls != 0 {
		t.Fatal("human attach bypassed current location grant", err, grant.calls, reader.integrate2RouteFixture.calls)
	}
}
