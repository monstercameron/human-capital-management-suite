package application

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AgentUXDemoSupportDeferredQueue is the queue the preparation seeds the support
// inbox through: it stores nothing and runs nothing. The seeded emails wait in
// the inbox, and the owner's "Simulate a customer email" (which replays the same
// email) is what turns one into a ticket.
type AgentUXDemoSupportDeferredQueue struct{}

func (AgentUXDemoSupportDeferredQueue) QueueSupportEmail(context.Context, string, string) error {
	return nil
}

var _ AgentUXDemoSupportRunQueue = AgentUXDemoSupportDeferredQueue{}

// LocalAgentDemoPlacements is what the preparation did after the agents
// themselves were prepared: where they were placed, what #general now shows, and
// the support inbox it seeded.
type LocalAgentDemoPlacements struct {
	AmbientInstallations, SupportAlertMemberships, GeneralHistoryPosts, SupportEmailsSeeded int
}

func (r LocalAgentDemoPlacements) Changed() bool {
	return r.AmbientInstallations > 0 || r.SupportAlertMemberships > 0 || r.GeneralHistoryPosts > 0 || r.SupportEmailsSeeded > 0
}

// prepareLocalAgentDemoTail places the agents added after Policy Helper and
// Assistant, writes #general's history and seeds the support inbox. Each step is
// idempotent, none deletes anything, and none turns an agent's "Reads every
// message here" switch on.
func prepareLocalAgentDemoTail(adminCtx context.Context, config LocalAgentDemoConfig, agents *agentstore.Store, chat *chatstore.Store, documents *documenthubstore.Store, mapper func(values.TenantId) uuid.UUID, general string, now time.Time) (LocalAgentDemoPlacements, error) {
	var receipt LocalAgentDemoPlacements
	var err error
	if receipt.AmbientInstallations, err = ensureLocalAgentDemoAmbientInstallations(adminCtx, chat, config.Tenant, localAgentDemoAdmin, now); err != nil {
		return receipt, err
	}
	if receipt.SupportAlertMemberships, err = ensureLocalAgentDemoSupportAlertMember(adminCtx, chat, config.Tenant); err != nil {
		return receipt, err
	}
	members, owner, err := localAgentDemoHistoryMembers(adminCtx, chat, agents, mapper, config.Tenant, general)
	if err != nil {
		return receipt, err
	}
	receipt.GeneralHistoryPosts, err = ensureLocalAgentDemoGeneralHistory(adminCtx, localAgentDemoHistoryDeps{Chat: chat, Tenant: config.Tenant, General: general, Admin: localAgentDemoAdmin, OwnerName: owner, Members: members, Now: now,
		DocumentLink: func(ctx context.Context, probe string) (string, string, error) {
			hits, err := documents.SearchOfficialPlacementLexical(ctx, config.Tenant, general, probe, "person", localAgentDemoAdmin)
			if err != nil || len(hits) == 0 {
				return "", "", err
			}
			return hits[0].DocumentID, hits[0].Title, nil
		}})
	if err != nil {
		return receipt, err
	}
	inbox, err := agentstore.NewSupportInboxStore(agents)
	if err != nil {
		return receipt, err
	}
	surface := AgentUXDemoInboxSurface{Inbox: inbox, Objects: AgentUXDemoLocalSupportObjects{Root: filepath.FromSlash(localAgentDemoObjectRoot)}, Queue: AgentUXDemoSupportDeferredQueue{}, Owners: AgentUXDemoSupportOwner{}, TenantUUID: mapper, Now: func() time.Time { return now }}
	receipts, err := AgentUXDemoSeedSupportEmails(adminCtx, surface)
	for _, seeded := range receipts {
		if !seeded.Replayed {
			receipt.SupportEmailsSeeded++
		}
	}
	return receipt, err
}

// localAgentDemoHistoryMembers reads who is in #general and, for each, the name
// and the day and month of the birthday the demo workforce gives them and
// whether they share it. A birth year or age is never read.
func localAgentDemoHistoryMembers(ctx context.Context, chat *chatstore.Store, agents *agentstore.Store, mapper func(values.TenantId) uuid.UUID, tenant, general string) ([]localAgentDemoHistoryPerson, string, error) {
	var keys []string
	if err := chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND state='active' AND left_at IS NULL`, tenant, general)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return rows.Err()
	}); err != nil {
		return nil, "", err
	}
	sort.Strings(keys)
	pack, ok := demoworkforce.PackFor(tenant)
	if !ok {
		return nil, "", ErrLocalDevPersonaChatBootstrap
	}
	employees, err := pack.Plan(pgstore.TenantID(tenant))
	if err != nil {
		return nil, "", err
	}
	names := map[string]string{}
	for _, employee := range employees {
		name := employee.Row.LegalName
		if name == "" {
			name = employee.Row.PreferredName
		}
		names[employee.Row.WorkerKey] = name
	}
	preferences, err := agentstore.NewSupportInboxStore(agents)
	if err != nil {
		return nil, "", err
	}
	var members []localAgentDemoHistoryPerson
	for _, key := range keys {
		name, known := names[key]
		month, day, hasBirthday := demoworkforce.DemoBirthday(key)
		if !known || !hasBirthday {
			continue
		}
		preference, err := preferences.BirthdayPreference(ctx, mapper(values.TenantId(tenant)), key, true)
		if err != nil {
			return nil, "", err
		}
		members = append(members, localAgentDemoHistoryPerson{Key: key, Name: name, Month: month, Day: day, Shares: preference.Share})
	}
	return members, names[localAgentDemoAdmin], nil
}
