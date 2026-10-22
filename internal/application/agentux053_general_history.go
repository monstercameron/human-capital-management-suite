package application

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// localAgentDemoHistoryPrefix names every message the history seeds, so a run
// can tell what it has already written.
const localAgentDemoHistoryPrefix = "agentux053-history-v1:"

// localAgentDemoHistoryPerson is a member of #general the history may name: who
// they are, the day and month of their birthday (never a year) and whether they
// share it. A person who does not share is never named.
type localAgentDemoHistoryPerson struct {
	Key, Name string
	Month     time.Month
	Day       int
	Shares    bool
}

// localAgentDemoHistoryDeps are what the history is written from. Document links
// are resolved by the caller from the documents officially placed in #general.
type localAgentDemoHistoryDeps struct {
	Chat         *chatstore.Store
	Tenant       string
	General      string
	Admin        string
	OwnerName    string
	Members      []localAgentDemoHistoryPerson
	DocumentLink func(ctx context.Context, probe string) (id, title string, err error)
	Now          time.Time
}

// ensureLocalAgentDemoGeneralHistory gives #general a short, dated, realistic
// history of what agents do there (AGENTUX-053): a holiday announcement from
// Assistant with its source, a birthday greeting on the day, two answered
// policy questions with linked sources and one question an agent declined with
// a clear reason. Every message is written under a fixed key, so a second run
// writes nothing. It never deletes: the old test questions and failures stay
// where they are until the owner runs the separate cleanup command.
//
// The questions carry no @mention. A mention is what makes an agent run, and
// seeding history must never cost a model call.
func ensureLocalAgentDemoGeneralHistory(ctx context.Context, d localAgentDemoHistoryDeps) (int, error) {
	if d.Chat == nil || ctx == nil || d.Tenant == "" || d.General == "" || d.Admin == "" || d.DocumentLink == nil || d.Now.IsZero() {
		return 0, ErrLocalDevPersonaChatBootstrap
	}
	before, err := localAgentDemoHistoryCount(ctx, d)
	if err != nil {
		return 0, err
	}
	day := func(daysAgo, hour, minute int) time.Time {
		base := d.Now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -daysAgo)
		return base.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	link := func(probe string) (chatui.AgentAnnouncementSource, error) {
		id, title, err := d.DocumentLink(ctx, probe)
		if err != nil || id == "" {
			return chatui.AgentAnnouncementSource{}, fmt.Errorf("document for %q: %w", probe, errNoHistoryDocument(err))
		}
		return chatui.AgentAnnouncementSource{Title: title, Href: "/workspace/app/docs?" + url.Values{"document": {id}}.Encode()}, nil
	}
	holiday, err := link(localAgentDemoHolidayProbe)
	if err != nil {
		return 0, err
	}
	policy, err := link(localAgentDemoPolicyProbe)
	if err != nil {
		return 0, err
	}
	// The people who ask: two members who are not the administrator, so the
	// history reads as a workspace, not as one person's test run.
	var askers []string
	for _, member := range d.Members {
		if member.Key != d.Admin {
			askers = append(askers, member.Key)
		}
	}
	for len(askers) < 2 {
		askers = append(askers, d.Admin)
	}
	human := chatstore.NewAdapter(d.Chat)
	ask := func(key, author, body string, at time.Time) (chatcore.Post, error) {
		return human.SendPost(ctx, chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: d.Tenant, SubjectID: author}, TenantID: d.Tenant, ConversationID: d.General, IdempotencyKey: localAgentDemoHistoryPrefix + key}, chatcore.Post{AuthorID: author, AuthorHomeTenantID: d.Tenant, Body: body, CreatedAt: at})
	}
	say := func(key, agent, body, parent string, at time.Time) error {
		_, err := d.Chat.SeedAgentMessage(ctx, d.Tenant, d.General, agent, body, localAgentDemoHistoryPrefix+key, parent, at)
		return err
	}
	announce := func(agent, text string, scheduled bool, at time.Time, sources ...chatui.AgentAnnouncementSource) (string, error) {
		return chatui.AnnouncementMessageBody(chatui.AgentAnnouncementMessage{AgentName: agent, OwnerName: d.OwnerName, Text: text, Scheduled: scheduled, PostedAt: at, Sources: sources})
	}

	if q, err := ask("pto-question", askers[0], "How many PTO hours can I carry over into next year?", day(11, 10, 12)); err != nil {
		return 0, err
	} else if err := say("pto-answer", localAgentDemoAgentID, fmt.Sprintf("You can carry over up to 40 hours of unused PTO into the next calendar year. Carried-over hours must be used by March 31 or they are forfeited.\n\nSources: [%s · Carryover](%s)", policy.Title, policy.Href), q.ID, day(11, 10, 13)); err != nil {
		return 0, err
	}
	body, err := announce("Assistant", "Heads up: Thanksgiving Day is Thursday, Nov 26, and the day after, Friday, Nov 27, is a company holiday too. Offices are closed on both days.", false, day(9, 8, 30), holiday)
	if err != nil {
		return 0, err
	}
	if err := say("holiday-announcement", localAgentDemoAssistantAgentID, body, "", day(9, 8, 30)); err != nil {
		return 0, err
	}
	if q, err := ask("time-off-question", askers[1], "How far ahead do I have to ask for time off?", day(6, 14, 5)); err != nil {
		return 0, err
	} else if err := say("time-off-answer", localAgentDemoAgentID, fmt.Sprintf("Request PTO in the Time Off page at least two weeks before the first day away. A manager approves or declines within five working days.\n\nSources: [%s · Requesting time off](%s)", policy.Title, policy.Href), q.ID, day(6, 14, 6)); err != nil {
		return 0, err
	}
	if q, err := ask("salary-question", askers[0], "What is Priya's salary?", day(4, 16, 40)); err != nil {
		return 0, err
	} else if err := say("salary-declined", localAgentDemoAgentID, "I can't answer that. Pay is private, and I only answer from policy documents everyone in this channel may read. Ask People Operations if you need pay information.", q.ID, day(4, 16, 41)); err != nil {
		return 0, err
	}
	if person, birthday, found := localAgentDemoLatestBirthday(d.Members, d.Now); found {
		text := "Happy birthday, " + person.Name + "! 🎂 Hope you have a wonderful day!"
		if err := AgentUXDemoValidateBirthdayText(AgentAnnouncementSource{Kind: AgentUXDemoBirthdaySourceKind, People: []BirthdayPerson{{DisplayName: person.Name, BirthdayToday: true}}, Digest: "history"}, text, "en-US"); err != nil {
			return 0, err
		}
		body, err := announce("Birthday Buddy", text, true, birthday)
		if err != nil {
			return 0, err
		}
		if err := say("birthday-"+person.Key+"-"+birthday.Format(time.DateOnly), "birthday-buddy", body, "", birthday); err != nil {
			return 0, err
		}
	}
	after, err := localAgentDemoHistoryCount(ctx, d)
	return after - before, err
}

func errNoHistoryDocument(err error) error {
	if err != nil {
		return err
	}
	return ErrLocalDevPersonaChatBootstrap
}

func localAgentDemoHistoryCount(ctx context.Context, d localAgentDemoHistoryDeps) (int, error) {
	var count int
	err := d.Chat.RunTenantTx(ctx, d.Tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_idempotency WHERE tenant_id=$1 AND conversation_id=$2 AND client_key LIKE $3`, d.Tenant, d.General, localAgentDemoHistoryPrefix+"%").Scan(&count)
	})
	return count, err
}

// localAgentDemoLatestBirthday is the most recent birthday, within the last six
// weeks, of a member who shares it, and the morning it was greeted on. A person
// who does not share their birthday is never a candidate, and no year or age is
// read: only the day and month.
func localAgentDemoLatestBirthday(members []localAgentDemoHistoryPerson, now time.Time) (localAgentDemoHistoryPerson, time.Time, bool) {
	var best localAgentDemoHistoryPerson
	var bestDay time.Time
	sorted := append([]localAgentDemoHistoryPerson(nil), members...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	today := now.UTC().Truncate(24 * time.Hour)
	for _, member := range sorted {
		if !member.Shares || !agentuxDemoBirthdayName(member.Name) {
			continue
		}
		for daysAgo := 1; daysAgo <= 42; daysAgo++ {
			candidate := today.AddDate(0, 0, -daysAgo)
			if demoworkforce.DemoBirthdayToday(member.Month, member.Day, candidate) {
				if bestDay.IsZero() || candidate.After(bestDay) {
					best, bestDay = member, candidate
				}
				break
			}
		}
	}
	if bestDay.IsZero() {
		return localAgentDemoHistoryPerson{}, time.Time{}, false
	}
	return best, bestDay.Add(8 * time.Hour), true
}
