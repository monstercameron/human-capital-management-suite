package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	AgentUXDemoBirthdayPersonaID  = "hcmnext.local.persona.birthday_buddy"
	AgentUXDemoBirthdaySourceKind = "BIRTHDAYS_TODAY"
	AgentUXDemoDocumentSourceKind = "DOCUMENTS"
)

// BirthdayPerson contains exactly the facts permitted to reach the model.
// Worker identifiers remain in the local authorization witness, never here.
type BirthdayPerson struct {
	DisplayName   string `json:"display_name"`
	BirthdayToday bool   `json:"birthday_today"`
}

type AgentAnnouncementSource struct {
	Kind      string
	Documents []AgentAnnouncementResolvedDocument
	People    []BirthdayPerson
	Digest    string
}

type AgentAnnouncementSourceProvider interface {
	ResolveAnnouncementSource(context.Context, agentstore.Announcement, time.Time) (AgentAnnouncementSource, error)
}

type AgentUXDemoDocumentSource struct {
	Documents AgentAnnouncementDocumentResolver
}

func (s AgentUXDemoDocumentSource) ResolveAnnouncementSource(ctx context.Context, r agentstore.Announcement, _ time.Time) (AgentAnnouncementSource, error) {
	if s.Documents == nil {
		return AgentAnnouncementSource{}, ErrAgentAnnouncementUnavailable
	}
	docs, err := s.Documents.ResolveAnnouncementDocuments(ctx, r.TenantKey, r.InstallationID, r.Documents)
	if err != nil {
		return AgentAnnouncementSource{}, err
	}
	raw, err := json.Marshal(docs)
	return AgentAnnouncementSource{Kind: AgentUXDemoDocumentSourceKind, Documents: docs, Digest: personaRunBytesDigest(raw)}, err
}

type AgentUXDemoBirthdayAudience interface {
	BirthdayConversationAudience(context.Context, string, string) (chatrecipient.AudienceSnapshot, error)
}
type AgentUXDemoBirthdayDirectory interface {
	BirthdayToday(context.Context, string, string, time.Time) (BirthdayPerson, error)
}
type AgentUXDemoBirthdayPreferences interface {
	BirthdayPreference(context.Context, uuid.UUID, string, bool) (agentstore.BirthdayPreference, error)
}

// AgentUXDemoBirthdaySource projects the complete current audience before any
// people query. An opted-out person is never sent to the name/birthday reader.
type AgentUXDemoBirthdaySource struct {
	Audience    AgentUXDemoBirthdayAudience
	Directory   AgentUXDemoBirthdayDirectory
	Preferences AgentUXDemoBirthdayPreferences
	TenantUUID  func(values.TenantId) uuid.UUID
}

func (s AgentUXDemoBirthdaySource) ResolveAnnouncementSource(ctx context.Context, r agentstore.Announcement, now time.Time) (AgentAnnouncementSource, error) {
	if ctx == nil || s.Audience == nil || s.Directory == nil || s.Preferences == nil || s.TenantUUID == nil || r.PersonaID != AgentUXDemoBirthdayPersonaID || r.TenantKey == "" || r.TenantID == uuid.Nil || s.TenantUUID(values.TenantId(r.TenantKey)) != r.TenantID || r.ConversationID == "" || now.IsZero() || len(r.Documents) != 0 {
		return AgentAnnouncementSource{}, ErrAgentAnnouncementDenied
	}
	zone, err := time.LoadLocation(r.Zone)
	if err != nil {
		return AgentAnnouncementSource{}, ErrAgentAnnouncementInvalid
	}
	a, err := s.Audience.BirthdayConversationAudience(ctx, r.TenantKey, r.ConversationID)
	if err != nil || a.TenantID != r.TenantKey || a.ConversationID != r.ConversationID || !a.Complete || !a.GuestAndExternalComplete || a.Revision == 0 {
		return AgentAnnouncementSource{}, ErrAgentAnnouncementDenied
	}
	_, demo := demoworkforce.PackFor(r.TenantKey)
	people := make([]BirthdayPerson, 0)
	seen := map[string]bool{}
	for _, member := range a.CurrentMembers {
		if member.TenantID != r.TenantKey || strings.TrimSpace(member.SubjectID) == "" {
			return AgentAnnouncementSource{}, ErrAgentAnnouncementDenied
		}
		if seen[member.SubjectID] {
			continue
		}
		seen[member.SubjectID] = true
		pref, err := s.Preferences.BirthdayPreference(ctx, r.TenantID, member.SubjectID, demo)
		if err != nil {
			return AgentAnnouncementSource{}, err
		}
		if !pref.Share {
			continue
		}
		person, err := s.Directory.BirthdayToday(ctx, r.TenantKey, member.SubjectID, now.In(zone))
		if errors.Is(err, dbport.ErrNoRows) {
			continue
		}
		if err != nil {
			return AgentAnnouncementSource{}, err
		}
		if !person.BirthdayToday {
			continue
		}
		if !agentuxDemoBirthdayName(person.DisplayName) {
			return AgentAnnouncementSource{}, ErrAgentAnnouncementDenied
		}
		people = append(people, person)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].DisplayName < people[j].DisplayName })
	raw, err := json.Marshal([]any{r.TenantKey, r.ConversationID, a.Revision, now.In(zone).Format(time.DateOnly), people})
	return AgentAnnouncementSource{Kind: AgentUXDemoBirthdaySourceKind, People: people, Digest: personaRunBytesDigest(raw)}, err
}

func agentuxDemoBirthdayName(name string) bool {
	if !utf8.ValidString(name) || name == "" || name != strings.TrimSpace(name) || utf8.RuneCountInString(name) > 200 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '<' || r == '>' || r == '\u202e' {
			return false
		}
	}
	return true
}

// AgentUXDemoCoreBirthdayDirectory reads the deployed worker's display name
// only. The deterministic demo month/day is derived locally from its key;
// neither a birth year nor age is selected, calculated or serialized.
type AgentUXDemoCoreBirthdayDirectory struct {
	Core       dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

func (d AgentUXDemoCoreBirthdayDirectory) BirthdayToday(ctx context.Context, tenant, worker string, today time.Time) (BirthdayPerson, error) {
	if ctx == nil || d.Core == nil || d.TenantUUID == nil || tenant == "" || worker == "" || today.IsZero() {
		return BirthdayPerson{}, ErrAgentAnnouncementDenied
	}
	if _, ok := demoworkforce.PackFor(tenant); !ok {
		return BirthdayPerson{}, ErrAgentAnnouncementDenied
	}
	id := d.TenantUUID(values.TenantId(tenant))
	if id == uuid.Nil {
		return BirthdayPerson{}, ErrAgentAnnouncementDenied
	}
	tx, err := d.Core.Begin(ctx)
	if err != nil {
		return BirthdayPerson{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, id); err != nil {
		return BirthdayPerson{}, err
	}
	var name string
	err = tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(legal_name,''),preferred_name) FROM journey_worker WHERE tenant_id=$1 AND worker_key=$2 AND lower(lifecycle_status)='active'`, id, worker).Scan(&name)
	if err != nil {
		return BirthdayPerson{}, err
	}
	month, day, ok := demoworkforce.DemoBirthday(worker)
	return BirthdayPerson{DisplayName: name, BirthdayToday: ok && demoworkforce.DemoBirthdayToday(month, day, today)}, tx.Commit(ctx)
}

// Recheck verifies the exact source again at delivery, after the model turn.
// A membership change or opt-out invalidates the retained authorization.
func (s AgentUXDemoBirthdaySource) Recheck(ctx context.Context, r agentstore.Announcement, now time.Time, source AgentAnnouncementSource, text, locale string) error {
	current, err := s.ResolveAnnouncementSource(ctx, r, now)
	if err != nil || !reflect.DeepEqual(current, source) {
		return ErrAgentAnnouncementNotPublic
	}
	return AgentUXDemoValidateBirthdayText(source, text, locale)
}

// AgentUXDemoValidateBirthdayText constrains public output to the authorized
// names and a bounded warm greeting. The model chooses one of these two-sentence
// forms; arbitrary extra names, ages, personal details and instructions fail.
func AgentUXDemoValidateBirthdayText(source AgentAnnouncementSource, text, locale string) error {
	if source.Kind != AgentUXDemoBirthdaySourceKind || len(source.People) == 0 || source.Digest == "" || len(source.Documents) != 0 {
		return ErrAgentAnnouncementNotPublic
	}
	names := make([]string, 0, len(source.People))
	for _, p := range source.People {
		if !p.BirthdayToday || !agentuxDemoBirthdayName(p.DisplayName) {
			return ErrAgentAnnouncementNotPublic
		}
		names = append(names, p.DisplayName)
	}
	sort.Strings(names)
	prefix, joiner, endings := "Happy birthday, ", " and ", []string{"Hope you have a wonderful day!", "Wishing you a lovely day!"}
	switch locale {
	case "de-DE":
		prefix, joiner, endings = "Alles Gute zum Geburtstag, ", " und ", []string{"Habt einen wundervollen Tag!", "Wir wünschen euch einen schönen Tag!"}
	case "ar":
		prefix, joiner, endings = "عيد ميلاد سعيد، ", " و", []string{"نتمنى لكم يومًا رائعًا!", "استمتعوا بيوم جميل!"}
	case "en-US", "":
	default:
		return ErrAgentAnnouncementInvalid
	}
	joined := strings.Join(names, ", ")
	if len(names) > 1 {
		joined = strings.Join(names[:len(names)-1], ", ") + joiner + names[len(names)-1]
	}
	for _, end := range endings {
		if text == prefix+joined+"! 🎂 "+end {
			return nil
		}
	}
	return ErrAgentAnnouncementNotPublic
}

// AgentUXDemoAnnouncementSources preserves the document provider and selects
// the people provider only for the immutable Birthday Buddy identity.
type AgentUXDemoAnnouncementSources struct {
	Documents AgentUXDemoDocumentSource
	Birthdays AgentUXDemoBirthdaySource
}

func (s AgentUXDemoAnnouncementSources) ResolveAnnouncementSource(ctx context.Context, r agentstore.Announcement, now time.Time) (AgentAnnouncementSource, error) {
	if r.PersonaID == AgentUXDemoBirthdayPersonaID {
		return s.Birthdays.ResolveAnnouncementSource(ctx, r, now)
	}
	return s.Documents.ResolveAnnouncementSource(ctx, r, now)
}

func agentuxDemoBirthdayModelData(source AgentAnnouncementSource) ([]byte, error) {
	if source.Kind != AgentUXDemoBirthdaySourceKind || len(source.Documents) != 0 {
		return nil, ErrAgentAnnouncementDenied
	}
	return json.Marshal(slices.Clone(source.People))
}

func (r *AgentAnnouncementRunner) agentuxDemoResolveDocumentSource(ctx context.Context, record agentstore.Announcement) ([]AgentAnnouncementResolvedDocument, error) {
	provider := r.Sources
	if provider == nil {
		provider = AgentUXDemoDocumentSource{Documents: r.Documents}
	}
	source, err := provider.ResolveAnnouncementSource(ctx, record, r.Now())
	if err != nil {
		return nil, err
	}
	// The document executor must never mint document provenance for people.
	// People require their own sealed output and audience-floor branch.
	if source.Kind != AgentUXDemoDocumentSourceKind || len(source.People) != 0 {
		return nil, ErrAgentAnnouncementUnavailable
	}
	return source.Documents, nil
}
