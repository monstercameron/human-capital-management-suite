package application

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CHATSEARCH-003: the agents a person may use, the tasks they own and the
// announcements they own are found from Chat search, each with its own kind.
// Every source reads through the owner of the record with the verified person
// of the request, so a result is exactly what that person's Agents page would
// list: no agent they may not use, nobody else's task or announcement, and
// never an agent's instructions.

// ChatSearchAgentTasks lists the tasks the verified person owns.
type ChatSearchAgentTasks interface {
	ListAgentTasks(context.Context, *trust.Principal) ([]agentrun.AgentTask, error)
}

// ChatSearchAgentAnnouncements lists the announcements the verified person owns.
type ChatSearchAgentAnnouncements interface {
	SearchableAnnouncements(context.Context) ([]agentstore.Announcement, error)
}

// ChatSearchAgentRegistry is the part of the search registry a late composition
// registers a new kind on.
type ChatSearchAgentRegistry interface {
	Register(chatsearch.Declaration, chatsearch.Source) error
}

// SearchableAnnouncements is the announcements of the request's own person,
// without the deleted ones. It reads the stored records only: no schedule is
// consulted and nothing is run.
func (s *AgentAnnouncementControlSurface) SearchableAnnouncements(ctx context.Context) ([]agentstore.Announcement, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	records, err := s.Service.Store.ListOwner(ctx, actor.TenantUUID, actor.SubjectID)
	if err != nil {
		return nil, err
	}
	kept := records[:0]
	for _, record := range records {
		if record.State != agentstore.AnnouncementDeleted && record.OwnerID == actor.SubjectID {
			kept = append(kept, record)
		}
	}
	return kept, nil
}

// chatsearchAgentPerson is the verified person a search runs for, provided the
// request names the same person in their own workspace. A request made for
// anybody else finds nothing here.
func chatsearchAgentPerson(ctx context.Context, actor chatsearch.Actor) (*trust.Principal, bool) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return nil, false
	}
	if actor.TenantID != actor.HomeTenantID || p.Tenant().String() != actor.TenantID || p.Subject() != actor.PersonID {
		return nil, false
	}
	return p, true
}

// chatsearchAgentSource turns one reader of the person's own records into a
// search source. rows is asked for everything the person may see; matching and
// paging happen after it, so nothing is matched that was not already allowed.
func chatsearchAgentSource(kind chatsearch.Kind, rows func(context.Context, *trust.Principal) ([]chatsearch.Row, error)) chatsearch.Source {
	search := func(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		p, ok := chatsearchAgentPerson(ctx, q.Actor)
		if !ok {
			return nil, nil
		}
		all, err := rows(ctx, p)
		if err != nil {
			return nil, err
		}
		out := []chatsearch.Row{}
		for _, row := range all {
			row.Kind, row.TenantID = kind, q.Actor.TenantID
			if row.ID != "" && chatsearch.Match(row, q) {
				out = append(out, row)
			}
		}
		return chatsearch.WindowRows(out, q), nil
	}
	return chatsearch.SourceFuncs{SearchRows: search, OpenRow: func(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
		current, err := search(ctx, chatsearch.Request{Actor: actor, At: time.Now().UTC(), OpenIDs: []string{row.ID}})
		for _, fresh := range current {
			if fresh.ID == row.ID && fresh.Text == row.Text && fresh.Target == row.Target {
				return true, err
			}
		}
		return false, err
	}}
}

// chatsearchAgentRows is the agents the person may use, one row each: the name
// and the purpose, which is what the Agents page shows them.
func chatsearchAgentRows(agents AvailablePersonaReader) func(context.Context, *trust.Principal) ([]chatsearch.Row, error) {
	return func(ctx context.Context, p *trust.Principal) ([]chatsearch.Row, error) {
		versions, err := agents.ListAvailable(ctx, p)
		if err != nil {
			return nil, err
		}
		// Several published versions of one agent are one agent: the newest.
		newest := map[string]int{}
		for i, version := range versions {
			if version.Verify() != nil || strings.TrimSpace(version.Profile.PersonaID) == "" || strings.TrimSpace(version.Profile.DisplayName) == "" {
				continue
			}
			if held, ok := newest[version.Profile.PersonaID]; !ok || versions[held].Profile.Version < version.Profile.Version {
				newest[version.Profile.PersonaID] = i
			}
		}
		ids := make([]string, 0, len(newest))
		for id := range newest {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows := make([]chatsearch.Row, 0, len(ids))
		for _, id := range ids {
			profile := versions[newest[id]].Profile
			text := profile.DisplayName
			if purpose := strings.TrimSpace(profile.Purpose); purpose != "" {
				text += "\n" + purpose
			}
			rows = append(rows, chatsearch.Row{ID: id, Text: text, ByAgent: true, Target: chatsearch.Target{ItemID: id}})
		}
		return rows, nil
	}
}

// chatsearchTaskRows is the person's own tasks by their goal.
func chatsearchTaskRows(tasks ChatSearchAgentTasks) func(context.Context, *trust.Principal) ([]chatsearch.Row, error) {
	return func(ctx context.Context, p *trust.Principal) ([]chatsearch.Row, error) {
		owned, err := tasks.ListAgentTasks(ctx, p)
		if err != nil {
			return nil, err
		}
		rows := make([]chatsearch.Row, 0, len(owned))
		for _, task := range owned {
			if task.TenantID != p.Tenant().String() || task.UserID != p.Subject() || strings.TrimSpace(task.Goal) == "" {
				continue
			}
			rows = append(rows, chatsearch.Row{ID: task.ID, Text: task.Goal, OwnerID: task.UserID, Private: true, At: task.CreatedAt, Target: chatsearch.Target{ItemID: task.ID}})
		}
		return rows, nil
	}
}

// chatsearchAnnouncementRows is the person's own announcements by what they
// were told to announce.
func chatsearchAnnouncementRows(announcements ChatSearchAgentAnnouncements) func(context.Context, *trust.Principal) ([]chatsearch.Row, error) {
	return func(ctx context.Context, p *trust.Principal) ([]chatsearch.Row, error) {
		owned, err := announcements.SearchableAnnouncements(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]chatsearch.Row, 0, len(owned))
		for _, record := range owned {
			if record.OwnerID != p.Subject() || record.State == agentstore.AnnouncementDeleted || strings.TrimSpace(record.Instruction) == "" {
				continue
			}
			rows = append(rows, chatsearch.Row{ID: record.ID, Text: record.Instruction, OwnerID: record.OwnerID, Private: true, ByAgent: true, At: record.UpdatedAt, Target: chatsearch.Target{ConversationID: record.ConversationID, ItemID: record.ID}})
		}
		return rows, nil
	}
}

// composeChatSearchAgents is the served composition's one call: it hands the
// search what the served cell has of the agent pages. Each part may be absent.
func composeChatSearchAgents(search ChatSearchHTTP, surface *PersonaChatSurface, runtime *agentRuntime, announcements *AgentAnnouncementControlSurface) error {
	var agents AvailablePersonaReader
	if surface != nil {
		agents = surface.Personas
	}
	var tasks ChatSearchAgentTasks
	if runtime != nil && runtime.Starter != nil {
		tasks = runtime.Starter
	}
	var owned ChatSearchAgentAnnouncements
	if announcements != nil {
		owned = announcements
	}
	return RegisterChatSearchAgents(search.Port, agents, tasks, owned)
}

// RegisterChatSearchAgents adds the agent kinds to a search registry. A reader
// that is not composed leaves its kind out, so the page never offers a kind
// nothing here can answer. port is the registry behind the served search; a
// search with no registry behind it is left as it is.
func RegisterChatSearchAgents(port any, agents AvailablePersonaReader, tasks ChatSearchAgentTasks, announcements ChatSearchAgentAnnouncements) error {
	registry, ok := port.(ChatSearchAgentRegistry)
	if !ok || isNilPersonaOutputPort(registry) {
		return nil
	}
	sources := map[chatsearch.Kind]chatsearch.Source{}
	if !isNilPersonaOutputPort(agents) {
		sources[chatsearch.Agent] = chatsearchAgentSource(chatsearch.Agent, chatsearchAgentRows(agents))
	}
	if !isNilPersonaOutputPort(tasks) {
		sources[chatsearch.AgentTask] = chatsearchAgentSource(chatsearch.AgentTask, chatsearchTaskRows(tasks))
	}
	if !isNilPersonaOutputPort(announcements) {
		sources[chatsearch.AgentAnnouncement] = chatsearchAgentSource(chatsearch.AgentAnnouncement, chatsearchAnnouncementRows(announcements))
	}
	for _, declaration := range chatsearch.AgentDeclarations() {
		source, composed := sources[declaration.Kind]
		if !composed {
			continue
		}
		if err := registry.Register(declaration, source); err != nil {
			return err
		}
	}
	return nil
}
