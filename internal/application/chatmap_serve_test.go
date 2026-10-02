package application

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func chatmapHTTPContextFor(t *testing.T, subject string) context.Context {
	t.Helper()
	now := time.Now()
	actor, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tt"), Subject: subject, SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "chatmap-session-" + subject, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:chatmap-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), actor)
}

type chatmapStoreAdmin struct{ channelManager bool }

func (a chatmapStoreAdmin) IsLocationAdmin(_ context.Context, _ chat.Principal, _, conversation string) bool {
	return a.channelManager && conversation != ""
}

// The workspace's location settings belong to the workspace administrator, the
// way the other workspace Chat settings do; a channel's own manager keeps the
// channel's and gains nothing at workspace scope.
func TestTodo_CHATMAP_006_WorkspaceAdministrator(t *testing.T) {
	ctx := filterHTTPContext(t)
	alice := chat.Principal{TenantID: "tenant-a", SubjectID: "admin"}
	now := time.Now().UTC()
	for _, tc := range []struct {
		name         string
		roles        []string
		channelAdmin bool
		conversation string
		tenant       string
		want         bool
	}{
		{"an administrator at workspace scope", []string{"hcm_admin"}, false, "", "tenant-a", true},
		{"an employee at workspace scope", []string{"employee"}, false, "", "tenant-a", false},
		{"a channel manager at workspace scope", []string{"employee"}, true, "", "tenant-a", false},
		{"a channel manager in their channel", []string{"employee"}, true, "c", "tenant-a", true},
		{"an employee in a channel", []string{"employee"}, false, "c", "tenant-a", false},
		{"an administrator in a channel they can read", []string{"hcm_admin"}, false, "c", "tenant-a", true},
		{"an administrator asking about another workspace", []string{"hcm_admin"}, false, "", "tenant-b", false},
	} {
		admins := chatmapAdmins{Workspace: ChatFilterAuthority{Facts: chatfilterHTTPFacts{Roles: tc.roles}, Conversations: newChatServiceStub(), Now: func() time.Time { return now }}, Channel: chatmapStoreAdmin{channelManager: tc.channelAdmin}}
		if got := admins.IsLocationAdmin(ctx, alice, tc.tenant, tc.conversation); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	// Without the role facts nobody is a workspace administrator.
	if (chatmapAdmins{}).IsLocationAdmin(ctx, alice, "tenant-a", "") {
		t.Fatal("an administrator with no facts to read")
	}
}

func TestTodo_CHATMAP_006_CountryOfWorkLocation(t *testing.T) {
	for location, want := range map[string]string{
		"Boston, MA":              "US",
		"Austin, TX":              "US",
		"Berlin, Germany":         "DE",
		"Munich, Deutschland":     "DE",
		"Lyon, FR":                "FR",
		"London, United Kingdom":  "GB",
		"Leeds, UK":               "GB",
		"Toronto, Canada":         "CA",
		"Dallas, Texas, USA":      "US",
		"Headquarters":            "",
		"":                        "",
		"DE":                      "",
		"Somewhere, Atlantis":     "",
		" Paris ,  France ":       "FR",
		"Madrid, Spain":           "ES",
		"Wilmington, DE":          "US",
		"Singapore, Singapore":    "SG",
		"Cape Town, South Africa": "ZA",
	} {
		if got := chatmapCountryOf(location); got != want {
			t.Errorf("%q: %q, want %q", location, got, want)
		}
	}
	if chatmapNormalizeCountry(" germany ") != "DE" || chatmapNormalizeCountry("de") != "DE" || chatmapNormalizeCountry("uk") != "GB" || chatmapNormalizeCountry("*") != "*" {
		t.Fatal("country names are not stored under their codes")
	}
}

type chatmapDirectoryFake map[string]string

func (d chatmapDirectoryFake) WorkLocation(_ context.Context, _, subject string) (string, error) {
	location, ok := d[subject]
	if !ok {
		return "", errors.New("no such worker")
	}
	return location, nil
}

// chatmapPolicyRepo is a policy store with one country row.
type chatmapPolicyRepo struct {
	*chatmapPictureRepo
	jurisdictions map[string]chat.LocationJurisdiction
}

func (r *chatmapPolicyRepo) ReadLocationPolicy(context.Context, string, string) (chat.LocationPolicy, *chat.LocationPolicy, error) {
	return chat.DefaultLocationPolicy(), nil, nil
}
func (r *chatmapPolicyRepo) WriteLocationPolicy(context.Context, string, string, chat.LocationPolicy, string) error {
	return nil
}
func (r *chatmapPolicyRepo) ReadLocationJurisdiction(_ context.Context, _, country string) (chat.LocationJurisdiction, bool, error) {
	j, ok := r.jurisdictions[country]
	return j, ok, nil
}
func (r *chatmapPolicyRepo) WriteLocationJurisdiction(context.Context, string, chat.LocationJurisdiction, string) error {
	return nil
}

// A country switched off blocks sharing for a person who works there and for no
// one else, with the country read from the people directory.
func TestTodo_CHATMAP_006_CountrySwitch(t *testing.T) {
	now := time.Now().UTC()
	reader := &chatmapReader{post: chat.Post{TenantID: "tt", ConversationID: "c", ID: "m", AuthorID: "hans", Revision: 1}}
	repo := &chatmapPolicyRepo{chatmapPictureRepo: &chatmapPictureRepo{}, jurisdictions: map[string]chat.LocationJurisdiction{"DE": {Country: "DE", Enabled: false, Basis: "works council agreement pending"}}}
	service := &chat.LocationService{Chat: reader, Repo: repo, Now: func() time.Time { return now }, Country: chatmapCountries{Directory: chatmapDirectoryFake{"hans": "Berlin, Germany", "alice": "Boston, MA", "nobody": "Headquarters"}}}
	place := chat.LocationPlace{Source: chat.LocationDevice, Precision: "exact", CapturedAt: now, Position: &chat.LocationPosition{Latitude: 42.1, Longitude: -71.1, Accuracy: 20}}
	attach := func(subject string) error {
		reader.post.AuthorID, reader.post.ID = subject, "m-"+subject
		repo.v = chat.LocationShare{}
		expires := now.Add(time.Hour)
		_, err := service.Attach(chatmapHTTPContextFor(t, subject), chat.AttachLocationRequest{Principal: chat.Principal{TenantID: "tt", SubjectID: subject}, TenantID: "tt", ConversationID: "c", PostID: "m-" + subject, PostRevision: 1, Place: place, ExpiresAt: &expires})
		return err
	}
	if err := attach("hans"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("sharing allowed for a person in a country where it is switched off", err)
	}
	for _, other := range []string{"alice", "nobody"} {
		if err := attach(other); err != nil {
			t.Fatal("sharing refused for a person elsewhere", other, err)
		}
	}
	// A person the directory cannot read falls back to the workspace-wide row.
	repo.jurisdictions["*"] = chat.LocationJurisdiction{Country: "*", Enabled: false, Basis: "not yet agreed"}
	if err := attach("stranger"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("the workspace-wide switch did not apply", err)
	}
	// The country is read only for the signed-in person.
	if got := (chatmapCountries{Directory: chatmapDirectoryFake{"hans": "Berlin, Germany"}}).LocationCountry(chatmapHTTPContextFor(t, "alice"), chat.Principal{TenantID: "tt", SubjectID: "hans"}); got != "" {
		t.Fatal("a country was read for someone else", got)
	}
}

// The settings read is for administrators; anyone else is refused.
func TestTodo_CHATMAP_006_SettingsRoute(t *testing.T) {
	f := &chatmapSettingsFixture{}
	h := ChatmapHTTP{Surface: f}
	w := chatmapPost(t, h, "settings", `{"TenantID":"chatmap-tenant"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Country":"DE"`) || !strings.Contains(w.Body.String(), `"CanAdminister":true`) {
		t.Fatal("settings", w.Code, w.Body.String())
	}
	f.deny = true
	if w = chatmapPost(t, h, "settings", `{"TenantID":"chatmap-tenant"}`); w.Code != http.StatusForbidden {
		t.Fatal("settings for a non-administrator", w.Code)
	}
	// A country typed as a name is stored under its code.
	if w = chatmapPost(t, h, "setjurisdiction", `{"TenantID":"chatmap-tenant","Country":"germany","Enabled":false,"Basis":"pending"}`); w.Code != http.StatusOK || f.saved.Country != "DE" {
		t.Fatal("setjurisdiction", w.Code, f.saved)
	}
	if w = chatmapPost(t, ChatmapHTTP{Surface: &chatmapSurfaceFixture{}}, "settings", `{}`); w.Code != http.StatusServiceUnavailable {
		t.Fatal("settings answered without the capability", w.Code)
	}
}

type chatmapSettingsFixture struct {
	chatmapSurfaceFixture
	deny  bool
	saved chat.LocationJurisdiction
}

func (f *chatmapSettingsFixture) WorkspaceLocationSettings(context.Context, chat.Principal, string) (chatmapSettingsWire, error) {
	if f.deny {
		return chatmapSettingsWire{}, chat.ErrPermissionDenied
	}
	wire := chatmapSettingsWire{Policy: chatmapPolicyToWire(chat.DefaultLocationPolicy()), Jurisdictions: []chat.LocationJurisdiction{{Country: "DE", Enabled: false, Basis: "pending"}}}
	wire.Policy.CanAdminister = true
	return wire, nil
}
func (f *chatmapSettingsFixture) SetLocationJurisdiction(_ context.Context, _ chat.Principal, _ string, j chat.LocationJurisdiction) error {
	f.saved = j
	return nil
}
