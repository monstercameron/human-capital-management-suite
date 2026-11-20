package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatbarrier"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Issue 3783: information barriers. A barrier keeps two groups apart: here a
// group is a person's team (organisation unit) in the people directory, the
// same fact a channel gate reads. The guard below is the chat service's
// membership barrier port; the admin surface that writes the rules is in
// issue3783_barrier_http.go.

// chatBarrierRules is the workspace's barrier rules. The chat store implements it.
type chatBarrierRules interface {
	ListBarrierRules(ctx context.Context, tenant string) (chatbarrier.Rules, error)
	AddBarrierRule(ctx context.Context, tenant string, rule chatbarrier.Rule) (chatbarrier.Rule, error)
	RemoveBarrierRule(ctx context.Context, tenant, id, actor string) error
}

// chatBarrierPeople is the people directory a barrier reads teams from.
type chatBarrierPeople interface {
	BarrierTeams(ctx context.Context, tenant string, subjects []string) (map[string]string, error)
}

// BarrierTeams answers the team of each named person in one read of the
// workspace's worker records. A person with no record is absent from the answer
// and so belongs to no group.
func (f currentRoleChatFacts) BarrierTeams(ctx context.Context, tenant string, subjects []string) (map[string]string, error) {
	if f.workers == nil || f.tenantUUID == nil {
		return nil, chat.ErrUnavailable
	}
	want := make(map[string]bool, len(subjects))
	for _, s := range subjects {
		want[s] = true
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
	out := map[string]string{}
	for _, row := range rows {
		if want[row.WorkerKey] {
			if team := strings.TrimSpace(row.OrgUnit); team != "" {
				out[row.WorkerKey] = team
			}
		}
	}
	return out, nil
}

// chatBarrierDirectory finds the people directory behind the chat authority
// facts, or nothing when this deployment has no worker records.
func chatBarrierDirectory(facts ChatAuthorityFacts) chatBarrierPeople {
	if cached, ok := facts.(cachedChatFacts); ok {
		facts = cached.inner
	}
	if direct, ok := facts.(currentRoleChatFacts); ok && direct.workers != nil && direct.tenantUUID != nil {
		return direct
	}
	return nil
}

// chatBarrierGuard decides membership against the workspace's rules. It reads
// the rules on every decision, so a rule an administrator just wrote or revoked
// applies to the next add. A person with no team is on no side. When the rules
// or the directory cannot be read it answers with an error, which the chat
// service turns into a refusal: an undecided barrier never admits.
type chatBarrierGuard struct {
	Rules  chatBarrierRules
	People chatBarrierPeople
}

var _ chat.MembershipBarrierPort = chatBarrierGuard{}

func (g chatBarrierGuard) CheckJoin(ctx context.Context, tenant string, joining []string, present func(context.Context) ([]string, error)) error {
	if g.Rules == nil || g.People == nil {
		return chat.ErrUnavailable
	}
	rules, err := g.Rules.ListBarrierRules(ctx, tenant)
	if err != nil {
		return err
	}
	if len(rules) == 0 || len(joining) == 0 {
		return nil
	}
	teams, err := g.People.BarrierTeams(ctx, tenant, joining)
	if err != nil {
		return err
	}
	// Only a person on a side of some rule can break one, so a join of people
	// who are on no side never reads the conversation's roster.
	sides := map[string]bool{}
	for _, r := range rules {
		sides[r.GroupA], sides[r.GroupB] = true, true
	}
	involved := false
	in := make([]chatbarrier.Member, 0, len(joining))
	for _, subject := range joining {
		in = append(in, chatbarrier.Member{Subject: subject, Group: teams[subject]})
		involved = involved || sides[chatbarrier.Key(teams[subject])]
	}
	if !involved {
		return nil
	}
	var here []chatbarrier.Member
	if present != nil {
		subjects, err := present(ctx)
		if err != nil {
			return err
		}
		held, err := g.People.BarrierTeams(ctx, tenant, subjects)
		if err != nil {
			return err
		}
		for _, subject := range subjects {
			here = append(here, chatbarrier.Member{Subject: subject, Group: held[subject]})
		}
	}
	if _, bad := rules.Check(here, in); bad {
		return chat.ErrInformationBarrier
	}
	return nil
}

// chatBarrierGateMembership puts the barrier in front of the join-request
// approval path, which writes the membership itself inside the gate's own
// transaction and so never reaches the chat service's AddMembership.
func chatBarrierGateMembership(inner chatstore.GateMembershipPort, guard chatBarrierGuard, roster func(context.Context, dbport.Tx, chatgate.Scope) ([]string, error)) chatstore.GateMembershipPort {
	return func(ctx context.Context, tx dbport.Tx, scope chatgate.Scope, actor chatgate.Actor, key string, admit bool) error {
		if admit {
			present := func(ctx context.Context) ([]string, error) { return roster(ctx, tx, scope) }
			if err := guard.CheckJoin(ctx, scope.Tenant, []string{actor.Person}, present); err != nil {
				if errors.Is(err, chat.ErrInformationBarrier) {
					// The gate's edge answers a denial as permission denied; a
					// bare chat error would read as "unavailable" and invite a retry.
					return fmt.Errorf("%w: %w", chatgate.ErrDenied, err)
				}
				return chat.ErrUnavailable
			}
		}
		return inner(ctx, tx, scope, actor, key, admit)
	}
}

func chatBarrierActiveMembers(ctx context.Context, tx dbport.Tx, scope chatgate.Scope) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND state='active' AND left_at IS NULL LIMIT 20000`, scope.Tenant, scope.Conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
