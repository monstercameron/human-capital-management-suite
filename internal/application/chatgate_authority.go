package application

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CHATGATE-005 and CHATGATE-006: the gate service was never composed on the
// served product, because nothing implemented its authority outside the tests.
// This is that authority, written from what the product already knows:
//
//   - who may build a gate, review requests, read answers as an administrator
//     and export them: the channel's managers, and workspace administrators in
//     a channel they can read. The power over a private channel's answers is
//     not handed to an administrator who is not in it, for the reason managing
//     filters does not buy private-room access;
//   - who may see a gate's questions and answer them: anyone in the workspace
//     for a public channel, which is who can find it in Browse; the members of
//     a private channel, for answering again after its questions change. A
//     person invited into a gated private channel cannot answer first: adding
//     them takes the service's administrator override;
//   - the facts a rule may test (team, location, person) and the people, teams
//     and locations an answer may name: the people directory, where a team is
//     a person's organisation unit;
//   - what a gate may ask: questions up to the INTERNAL class; a channel that
//     is not public starts in review mode.
//
// An acknowledgement question names a document, and Chat has no port to the
// document hub here, so such an answer is refused rather than accepted
// unchecked. Consumers of answers (role labels, segments, polls, agents) are
// not composed, and their permissions are refused.

// chatgatePerson is one person of the people directory as a gate needs them.
type chatgatePerson struct {
	Subject, Name, Team, Location string
	Active                        bool
}

// chatgatePeople is the people directory a gate reads its facts from.
type chatgatePeople interface {
	GatePeople(ctx context.Context, tenant string) ([]chatgatePerson, error)
}

// GatePeople lists the workspace's people from the workforce records the chat
// authority already reads, for the signed-in person's own workspace.
func (f currentRoleChatFacts) GatePeople(ctx context.Context, tenant string) ([]chatgatePerson, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Tenant().String() != tenant {
		return nil, chatgate.ErrDenied
	}
	if f.workers == nil || f.tenantUUID == nil {
		return nil, chatgate.ErrUnavailable
	}
	tenantID := f.tenantUUID(values.TenantId(tenant))
	tx, err := f.workers.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := (workforce.Store{}).List(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]chatgatePerson, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.PreferredName)
		if name == "" {
			name = strings.TrimSpace(row.LegalName)
		}
		out = append(out, chatgatePerson{Subject: row.WorkerKey, Name: name, Team: strings.TrimSpace(row.OrgUnit), Location: strings.TrimSpace(row.Location), Active: strings.EqualFold(row.LifecycleStatus, "active")})
	}
	return out, nil
}

// chatgateConversations is the unguarded read of a conversation and its
// memberships the authority decides from. The chat store implements it.
type chatgateConversations interface {
	GetConversation(ctx context.Context, tenant, id string) (chat.Conversation, error)
	GetMembership(ctx context.Context, tenant, conversation, home, subject string) (chat.Membership, error)
	ListMemberships(ctx context.Context, tenant, conversation string, page chat.Page) (chat.ListMembershipsResponse, error)
}

// chatgateReader is the one read of the chat service the authority makes: the
// conversation as the person asking may read it.
type chatgateReader interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
}

// ChatgateAuthority is the production chatgate.Authority and the directory the
// gate pages read their names and choices from.
type ChatgateAuthority struct {
	Roles ChatAuthorityFacts
	// Readable is the chat service as a person reaches it: a workspace
	// administrator administers the gate of a channel they can read.
	Readable chatgateReader
	Store    chatgateConversations
	People   chatgatePeople
	// Purposes reads the purpose a channel's managers wrote for it.
	Purposes interface {
		GatePurpose(context.Context, chatgate.Scope) (string, error)
	}
	Now func() time.Time
}

// newChatgateAuthority builds the authority, or nil when the product has no
// people directory to read facts from (a composition with no worker records):
// the gate service then stays uncomposed, as it was.
func newChatgateAuthority(facts ChatAuthorityFacts, readable chatgateReader, store chatgateConversations, purposes *chatstore.GateRepository, now func() time.Time) *ChatgateAuthority {
	if cached, ok := facts.(cachedChatFacts); ok {
		facts = cached.inner
	}
	people, ok := facts.(chatgatePeople)
	if !ok || readable == nil || store == nil {
		return nil
	}
	if direct, ok := facts.(currentRoleChatFacts); ok && direct.workers == nil {
		return nil
	}
	return &ChatgateAuthority{Roles: facts, Readable: readable, Store: store, People: people, Purposes: purposes, Now: now}
}

func (a *ChatgateAuthority) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

// channel reads the conversation a gate belongs to. A gate is a channel's: a
// direct or group conversation has none.
func (a *ChatgateAuthority) channel(ctx context.Context, scope chatgate.Scope) (chat.Conversation, error) {
	c, err := a.Store.GetConversation(ctx, scope.Tenant, scope.Conversation)
	if err != nil {
		if errors.Is(err, chat.ErrNotFound) {
			return chat.Conversation{}, chatgate.ErrNotFound
		}
		return chat.Conversation{}, chatgate.ErrUnavailable
	}
	if c.Kind != chat.PublicChannel && c.Kind != chat.PrivateChannel {
		return chat.Conversation{}, chatgate.ErrDenied
	}
	return c, nil
}

// membership is the person's current membership of the channel, if any.
func (a *ChatgateAuthority) membership(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) (chat.Membership, bool) {
	m, err := a.Store.GetMembership(ctx, scope.Tenant, scope.Conversation, actor.Tenant, actor.Person)
	if err != nil || m.JoinedAt == nil || m.LeftAt != nil {
		return chat.Membership{}, false
	}
	return m, true
}

// administers reports whether the person acting may build, review, read as an
// administrator and export. The roles are read for the signed-in person only:
// asked about anybody else it answers no.
func (a *ChatgateAuthority) administers(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) bool {
	if _, err := a.channel(ctx, scope); err != nil {
		return false
	}
	if m, ok := a.membership(ctx, actor, scope); ok && m.Role == chat.Manager {
		return true
	}
	if !a.workspaceAdministrator(ctx, actor) {
		return false
	}
	_, err := a.Readable.GetConversation(ctx, chat.GetConversationRequest{Principal: chat.Principal{TenantID: actor.Tenant, SubjectID: actor.Person}, TenantID: scope.Tenant, ConversationID: scope.Conversation})
	return err == nil
}

func (a *ChatgateAuthority) workspaceAdministrator(ctx context.Context, actor chatgate.Actor) bool {
	p, err := newChatAuthoritySource(a.Roles).Resolve(ctx, actor.Tenant, actor.Person, a.now())
	if err != nil {
		return false
	}
	for _, role := range p.Roles {
		if role == chatpolicy.WorkspaceAdministratorRole {
			return true
		}
	}
	return false
}

// discovers reports whether the person may see the gate's questions: the
// channel is public, or they are in it.
func (a *ChatgateAuthority) discovers(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) error {
	c, err := a.channel(ctx, scope)
	if err != nil {
		return err
	}
	if c.Kind == chat.PublicChannel {
		return nil
	}
	if _, ok := a.membership(ctx, actor, scope); ok {
		return nil
	}
	return chatgate.ErrDenied
}

func (a *ChatgateAuthority) Check(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope, permission string) error {
	if a == nil || a.Store == nil || actor.Tenant == "" || actor.Tenant != scope.Tenant || actor.Person == "" {
		return chatgate.ErrDenied
	}
	switch permission {
	case "self", "read_answers":
		// A person's own submission and answers are theirs wherever they stand
		// with the channel now. The service decides whose answers a read may
		// return: their own, an administrator's view, or a member's.
		return nil
	case "discover", "eligible":
		return a.discovers(ctx, actor, scope)
	case chatgate.PermissionMember:
		if _, ok := a.membership(ctx, actor, scope); ok {
			return nil
		}
		return chatgate.ErrDenied
	case "admin", "export", "retention":
		if a.administers(ctx, actor, scope) {
			return nil
		}
		return chatgate.ErrDenied
	case "privacy_report":
		if a.workspaceAdministrator(ctx, actor) {
			return nil
		}
		return chatgate.ErrDenied
	}
	// A consumer's permission: no consumer of answers is composed here.
	return chatgate.ErrDenied
}

func (a *ChatgateAuthority) Policy(ctx context.Context, scope chatgate.Scope) (chatgate.Policy, error) {
	c, err := a.channel(ctx, scope)
	if err != nil {
		return chatgate.Policy{}, err
	}
	return chatgate.Policy{Ceiling: "INTERNAL", Private: c.Kind != chat.PublicChannel}, nil
}

func (a *ChatgateAuthority) people(ctx context.Context, tenant string) ([]chatgatePerson, error) {
	if a.People == nil {
		return nil, chatgate.ErrUnavailable
	}
	people, err := a.People.GatePeople(ctx, tenant)
	if err != nil {
		if errors.Is(err, chatgate.ErrDenied) {
			return nil, err
		}
		return nil, chatgate.ErrUnavailable
	}
	return people, nil
}

// Facts are what a rule may test about the person: their team (organisation
// unit), their location and who they are. A person the directory does not hold
// has no facts, so no rule about a fact matches them and their request waits
// for a person to review it.
func (a *ChatgateAuthority) Facts(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) (map[string]string, error) {
	people, err := a.people(ctx, scope.Tenant)
	if err != nil {
		return nil, err
	}
	for _, person := range people {
		if person.Subject == actor.Person && person.Active {
			return map[string]string{"team": person.Team, "location": person.Location, "person": person.Subject}, nil
		}
	}
	return map[string]string{}, nil
}

// Reference checks an answer that names something of the directory: a person
// who works here, a team or a location somebody is in.
func (a *ChatgateAuthority) Reference(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope, field chatgate.Field, value json.RawMessage) error {
	if field.Kind == "acknowledgement" {
		// The document cannot be checked from here (see the file comment).
		return chatgate.FieldError{Field: field.ID, Cause: chatgate.ErrDenied}
	}
	var named string
	if json.Unmarshal(value, &named) != nil {
		return chatgate.FieldError{Field: field.ID, Cause: chatgate.ErrInvalid}
	}
	if strings.TrimSpace(named) == "" {
		// An optional question left empty names nothing.
		return nil
	}
	people, err := a.people(ctx, scope.Tenant)
	if err != nil {
		return err
	}
	for _, person := range people {
		if !person.Active {
			continue
		}
		switch field.Kind {
		case "person":
			if person.Subject == named {
				return nil
			}
		case "team":
			if person.Team != "" && person.Team == named {
				return nil
			}
		case "location":
			if person.Location != "" && person.Location == named {
				return nil
			}
		}
	}
	return chatgate.FieldError{Field: field.ID, Cause: chatgate.ErrInvalid}
}

// GateDirectory is what the gate pages show beside a gate: the people, teams
// and locations a question can be answered with, the names of the people whose
// requests an administrator reads, and the channel's purpose. An applicant
// gets the choices (the people directory is the workspace's own) and no names
// of other applicants.
func (a *ChatgateAuthority) GateDirectory(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) (map[string][]chatui.GateChoice, map[string]string, string, error) {
	if err := a.discovers(ctx, actor, scope); err != nil && !a.administers(ctx, actor, scope) {
		return nil, nil, "", err
	}
	people, err := a.people(ctx, scope.Tenant)
	if err != nil {
		return nil, nil, "", err
	}
	administrator := a.administers(ctx, actor, scope)
	choices := map[string][]chatui.GateChoice{}
	names := map[string]string{}
	teams, locations := map[string]bool{}, map[string]bool{}
	for _, person := range people {
		if person.Name != "" && (administrator || person.Subject == actor.Person) {
			names[person.Subject] = person.Name
		}
		if !person.Active {
			continue
		}
		if person.Name != "" {
			choices["person"] = append(choices["person"], chatui.GateChoice{ID: person.Subject, Label: person.Name})
		}
		if person.Team != "" && !teams[person.Team] {
			teams[person.Team] = true
			choices["team"] = append(choices["team"], chatui.GateChoice{ID: person.Team, Label: person.Team})
		}
		if person.Location != "" && !locations[person.Location] {
			locations[person.Location] = true
			choices["location"] = append(choices["location"], chatui.GateChoice{ID: person.Location, Label: person.Location})
		}
	}
	for kind := range choices {
		list := choices[kind]
		sort.SliceStable(list, func(i, j int) bool { return strings.ToLower(list[i].Label) < strings.ToLower(list[j].Label) })
	}
	purpose := ""
	if a.Purposes != nil {
		// A purpose that cannot be read is left out; the form does not need it.
		purpose, _ = a.Purposes.GatePurpose(ctx, scope)
	}
	return choices, names, purpose, nil
}

// GateReviewers names the people who decide a request: the channel's managers.
// An applicant is told who they are waiting for.
func (a *ChatgateAuthority) GateReviewers(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) ([]string, error) {
	if err := a.discovers(ctx, actor, scope); err != nil && !a.administers(ctx, actor, scope) {
		return nil, err
	}
	people, err := a.people(ctx, scope.Tenant)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, person := range people {
		names[person.Subject] = person.Name
	}
	var out []string
	page := chat.Page{PageSize: 200}
	for range 10 {
		listed, err := a.Store.ListMemberships(ctx, scope.Tenant, scope.Conversation, page)
		if err != nil {
			return nil, chatgate.ErrUnavailable
		}
		for _, m := range listed.Memberships {
			if m.Role == chat.Manager && m.JoinedAt != nil && m.LeftAt == nil && m.HomeTenantID == scope.Tenant && names[m.SubjectID] != "" {
				out = append(out, names[m.SubjectID])
			}
		}
		if listed.NextCursor == "" {
			break
		}
		page.Cursor = listed.NextCursor
	}
	sort.Strings(out)
	return out, nil
}

var (
	_ chatgate.Authority = (*ChatgateAuthority)(nil)
	_ ChatgateDirectory  = (*ChatgateAuthority)(nil)
	_ ChatgateReviewers  = (*ChatgateAuthority)(nil)
)

// chatgateReadableCSV rewrites the service's export, which names people and
// questions by their identifiers, as a file a person can read: the person's
// name, the question as it was asked, the answer as it was given. The service
// has already left out every answer an administrator may not see.
func chatgateReadableCSV(raw string, names, labels map[string]string) (string, error) {
	rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if err != nil || len(rows) == 0 {
		return "", chatgate.ErrUnavailable
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"person", "question", "answer"})
	for _, row := range rows[1:] {
		if len(row) != 3 {
			return "", chatgate.ErrUnavailable
		}
		person, question, answer := row[0], row[1], row[2]
		if name := names[person]; name != "" {
			person = name
		}
		if label := labels[question]; label != "" {
			question = label
		}
		var several []string
		if json.Unmarshal([]byte(answer), &several) == nil {
			answer = strings.Join(several, "; ")
		}
		for _, cell := range []*string{&person, &question, &answer} {
			// A cell a spreadsheet would run as a formula is written as text: a
			// name or a question can begin with "=" as well as an answer can.
			if text := *cell; len(text) > 0 && strings.ContainsAny(text[:1], "=+-@\t\r") {
				*cell = "'" + text
			}
		}
		if err = w.Write([]string{person, question, answer}); err != nil {
			return "", err
		}
	}
	w.Flush()
	return b.String(), w.Error()
}
