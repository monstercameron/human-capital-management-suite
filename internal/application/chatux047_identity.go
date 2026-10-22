package application

import (
	"context"
	"strings"
	"sync"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
)

// CHATUX-047: the names and pictures a conversation list and a timeline carry.
// The browser used to draw a row from its ids and wait for the people directory
// and the agent directory before the row had a name; this source answers from the
// same owners (the workforce directory for people, the persona store for agents)
// in the answer that carries the row, so the first frame is complete. It only
// describes: nothing it returns grants access, and a subject it cannot name is
// left out so the client falls back to its own directory reads.

// chatIdentitySource is the transport's port, named here so the served chat
// service holds it without its file importing the transport.
type chatIdentitySource = transportchat.IdentitySource

var _ transportchat.IdentitySource = (*streamingChatService)(nil)

// chatIdentityTTL is how long a directory answer is reused. A rename or a new
// icon shows after at most this long, and the client's own directory reads refine
// it sooner.
const chatIdentityTTL = 20 * time.Second

// chatIdentityDirectoryMax bounds how many direct conversations one listing
// resolves peers for, so a listing of a hundred rooms stays one short answer.
const chatIdentityDirectoryMax = 100

// chatIdentityWorkers is the workforce read the people names come from.
type chatIdentityWorkers interface {
	ListWorkers(ctx context.Context) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error)
}

// chatIdentityMemberships lists who is in a conversation, as the caller.
type chatIdentityMemberships interface {
	ListMemberships(context.Context, chatcore.ListMembershipsRequest) (chatcore.ListMembershipsResponse, error)
}

// chatIdentityPersonas is the persona store's read side.
type chatIdentityPersonas interface {
	ListPersonaChatIdentities(ctx context.Context, tenant string) ([]agentpersonastore.PersonaChatIdentity, error)
	Scoped(tenant values.TenantId) (*agentpersonastore.TenantStore, error)
}

type chatIdentityDirectory struct {
	workers     chatIdentityWorkers
	personas    chatIdentityPersonas
	memberships chatIdentityMemberships
	now         func() time.Time

	mu     sync.Mutex
	people map[string]chatIdentityPeople
	agents map[string]chatIdentityAgents
}

type chatIdentityPeople struct {
	at   time.Time
	byID map[string]*chatv1.Identity
}

type chatIdentityAgents struct {
	at   time.Time
	byID map[string]*chatv1.Identity
}

func newChatIdentityDirectory(workers chatIdentityWorkers, personas chatIdentityPersonas, memberships chatIdentityMemberships, now func() time.Time) *chatIdentityDirectory {
	if now == nil {
		now = time.Now
	}
	return &chatIdentityDirectory{workers: workers, personas: personas, memberships: memberships, now: now,
		people: map[string]chatIdentityPeople{}, agents: map[string]chatIdentityAgents{}}
}

// Peers names the other participant of each direct conversation.
func (d *chatIdentityDirectory) Peers(ctx context.Context, caller chatcore.Principal, rooms []chatcore.Conversation) map[string]*chatv1.Identity {
	out := make(map[string]*chatv1.Identity, len(rooms))
	if d == nil || d.memberships == nil || strings.TrimSpace(caller.SubjectID) == "" {
		return out
	}
	for i, room := range rooms {
		if i >= chatIdentityDirectoryMax {
			break
		}
		listed, err := d.memberships.ListMemberships(ctx, chatcore.ListMembershipsRequest{Principal: caller, TenantID: room.TenantID, ConversationID: room.ID, Page: chatcore.Page{PageSize: 100}})
		if err != nil {
			continue
		}
		peer := chatIdentityDirectPeer(listed.Memberships, caller.SubjectID)
		if peer == "" {
			continue
		}
		identity := d.identify(ctx, room.TenantID, []string{peer})[peer]
		if identity == nil {
			identity = &chatv1.Identity{SubjectId: peer}
		}
		out[room.ID] = identity
	}
	return out
}

// Authors names the given subjects of one host tenant.
func (d *chatIdentityDirectory) Authors(ctx context.Context, _ chatcore.Principal, tenantID string, subjectIDs []string) map[string]*chatv1.Identity {
	if d == nil {
		return nil
	}
	return d.identify(ctx, tenantID, subjectIDs)
}

// chatIdentityDirectPeer is the other active member of a direct conversation;
// the caller themself for a conversation only they are in (their notes space);
// and nobody for any other shape, which is never guessed.
func chatIdentityDirectPeer(memberships []chatcore.Membership, viewer string) string {
	seen := make(map[string]bool, len(memberships))
	for _, membership := range memberships {
		if membership.LeftAt != nil {
			continue
		}
		subject := strings.TrimSpace(membership.SubjectID)
		if subject == "" || seen[subject] {
			return ""
		}
		seen[subject] = true
	}
	if !seen[viewer] {
		return ""
	}
	if len(seen) == 1 {
		return viewer
	}
	if len(seen) == 2 {
		for subject := range seen {
			if subject != viewer {
				return subject
			}
		}
	}
	return ""
}

// identify names each subject as an agent of the tenant's registered identities
// or as a person of its workforce directory. A subject neither knows is left out.
func (d *chatIdentityDirectory) identify(ctx context.Context, tenantID string, subjectIDs []string) map[string]*chatv1.Identity {
	out := make(map[string]*chatv1.Identity, len(subjectIDs))
	caller, ok := trust.FromContext(ctx)
	if !ok || caller == nil || tenantID == "" || caller.Tenant().String() != tenantID {
		return out
	}
	agents := d.agentTable(ctx, tenantID)
	var people map[string]*chatv1.Identity
	for _, id := range subjectIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if agent := agents[id]; agent != nil {
			named := proto.Clone(agent).(*chatv1.Identity)
			named.SubjectId = id
			out[id] = named
			continue
		}
		if people == nil {
			people = d.peopleTable(ctx, tenantID)
		}
		if person := people[id]; person != nil {
			out[id] = person
		}
	}
	return out
}

func (d *chatIdentityDirectory) peopleTable(ctx context.Context, tenantID string) map[string]*chatv1.Identity {
	if d.workers == nil {
		return nil
	}
	d.mu.Lock()
	cached, ok := d.people[tenantID]
	d.mu.Unlock()
	if ok && d.now().Sub(cached.at) < chatIdentityTTL {
		return cached.byID
	}
	workers, _, err := d.workers.ListWorkers(ctx)
	if err != nil {
		return cached.byID
	}
	photos := map[string]string{}
	if pack, found := demoworkforce.PackFor(tenantID); found {
		if entries, derr := pack.Directory(); derr == nil {
			for key, entry := range entries {
				photos[strings.TrimSpace(key)] = entry.PhotoURL
			}
		}
	}
	byID := make(map[string]*chatv1.Identity, len(workers))
	for _, worker := range workers {
		subject := strings.TrimSpace(worker.SubjectID)
		if subject == "" {
			continue
		}
		name := chatIdentityPersonName(worker.PreferredName, worker.LegalName)
		if name == "" {
			continue
		}
		photo := strings.TrimSpace(worker.ProfilePhotoURL)
		if photo == "" {
			if photo = photos[strings.TrimSpace(worker.WorkerRef)]; photo == "" {
				photo = photos[subject]
			}
		}
		byID[subject] = &chatv1.Identity{SubjectId: subject, DisplayName: name, AvatarRef: photo}
	}
	d.mu.Lock()
	d.people[tenantID] = chatIdentityPeople{at: d.now(), byID: byID}
	d.mu.Unlock()
	return byID
}

func (d *chatIdentityDirectory) agentTable(ctx context.Context, tenantID string) map[string]*chatv1.Identity {
	if d.personas == nil {
		return nil
	}
	d.mu.Lock()
	cached, ok := d.agents[tenantID]
	d.mu.Unlock()
	if ok && d.now().Sub(cached.at) < chatIdentityTTL {
		return cached.byID
	}
	identities, err := d.personas.ListPersonaChatIdentities(ctx, tenantID)
	if err != nil {
		return cached.byID
	}
	scoped, err := d.personas.Scoped(values.TenantId(tenantID))
	if err != nil {
		return cached.byID
	}
	byID := make(map[string]*chatv1.Identity, len(identities)*2)
	for _, identity := range identities {
		if !identity.Active || identity.RevokedAt != nil || identity.TenantID != values.TenantId(tenantID) {
			continue
		}
		versions, verr := scoped.ListVersions(ctx, identity.PersonaID)
		if verr != nil {
			return cached.byID
		}
		var latest agentpersonastore.PersonaVersion
		for _, version := range versions {
			if version.PersonaID == identity.PersonaID && version.Version > latest.Version {
				latest = version
			}
		}
		name := strings.TrimSpace(latest.DisplayName)
		if latest.Version == 0 || name == "" {
			continue
		}
		named := &chatv1.Identity{DisplayName: name, Agent: true, AgentId: identity.AgentID}
		if icon, ierr := scoped.GetIcon(ctx, identity.PersonaID); ierr == nil && icon.Value.Valid() {
			named.AgentIcon = &chatv1.AgentIcon{Glyph: icon.Value.Glyph, Shape: icon.Value.Shape, Foreground: icon.Value.Foreground, Background: icon.Value.Background, Variation: icon.Value.Variation}
			named.AgentIconRevision = icon.Revision
		}
		byID[identity.PersonaID] = named
		byID[identity.AgentID] = named
	}
	d.mu.Lock()
	d.agents[tenantID] = chatIdentityAgents{at: d.now(), byID: byID}
	d.mu.Unlock()
	return byID
}

// chatIdentityPersonName is the name chat shows for a worker, the rule the
// client's directory applies too: the preferred name, completed with the legal
// surname when the preferred name is a single word, so a person is one name
// everywhere.
func chatIdentityPersonName(preferred, legal string) string {
	preferred, legal = strings.TrimSpace(preferred), strings.TrimSpace(legal)
	if preferred == "" {
		return legal
	}
	if legal == "" || strings.Contains(preferred, " ") {
		return preferred
	}
	parts := strings.Fields(legal)
	if len(parts) < 2 || strings.EqualFold(parts[len(parts)-1], preferred) {
		return preferred
	}
	return preferred + " " + parts[len(parts)-1]
}

// bindChatIdentities gives the served chat service its identity source: the
// workforce directory, the persona store and the service's own membership read.
// Either owner may be absent (a cell without a journey or without an agent
// database); the source then names only what it can.
func bindChatIdentities(service chatcore.ConversationService, journey workspace.JourneyEngine, personas *agentpersonastore.Store, now func() time.Time) {
	streaming, ok := service.(*streamingChatService)
	if !ok || streaming == nil || streaming.ConversationService == nil {
		return
	}
	var workers chatIdentityWorkers
	if journey != nil {
		workers = journey
	}
	var store chatIdentityPersonas
	if personas != nil {
		store = personas
	}
	streaming.bindIdentities(newChatIdentityDirectory(workers, store, streaming.ConversationService, now))
}

// Peers and Authors make the served chat service the identity source the
// transport asks. Unbound, they answer nothing and the transport answers as it
// always did.
func (s *streamingChatService) Peers(ctx context.Context, caller chatcore.Principal, rooms []chatcore.Conversation) map[string]*chatv1.Identity {
	if s == nil || s.identities == nil {
		return nil
	}
	return s.identities.Peers(ctx, caller, rooms)
}

func (s *streamingChatService) Authors(ctx context.Context, caller chatcore.Principal, tenantID string, subjectIDs []string) map[string]*chatv1.Identity {
	if s == nil || s.identities == nil {
		return nil
	}
	return s.identities.Authors(ctx, caller, tenantID, subjectIDs)
}

// bindIdentities installs the identity source. It runs once, while the cell is
// being composed, before the service answers a request.
func (s *streamingChatService) bindIdentities(source transportchat.IdentitySource) {
	if s != nil {
		s.identities = source
	}
}
