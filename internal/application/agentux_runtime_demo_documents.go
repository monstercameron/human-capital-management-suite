package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

const (
	localAgentDemoPolicyTitle     = "Paid time off policy"
	localAgentDemoPolicyAuthority = "team:people-operations"
	localAgentDemoPolicyReviewDue = 180 * 24 * time.Hour
	localAgentDemoPolicyProbe     = "carryover"
	localAgentDemoPolicyMarkdown  = `# Paid time off policy

## Accrual

Full-time employees accrue paid time off (PTO) every pay period. The accrual rate rises with tenure: 15 days a year in years one to three, 20 days a year in years four to seven, and 25 days a year from year eight.

## Carryover

Employees may carry over up to 40 hours of unused PTO into the next calendar year. Carried-over hours must be used by March 31; any carried-over hours left after that date are forfeited. Hours above the 40 hour carryover limit are forfeited on December 31 unless a manager and People Operations approve an exception in writing before that date.

## Requesting time off

Request PTO in the Time Off page at least two weeks before the first day away. A manager approves or declines within five working days. Requests for more than ten consecutive working days also need approval from the department head.

## Payout

Accrued, unused PTO is paid out at separation at the employee's current base rate, up to the carryover limit plus the current year's accrued balance.
`
	localAgentDemoHolidayTitle    = "2026 holiday guide"
	localAgentDemoHolidayProbe    = "Juneteenth"
	localAgentDemoHolidayMarkdown = `# 2026 holiday guide

Offices are closed on each listed day. A holiday that falls on a Saturday is observed on the Friday before, and a holiday that falls on a Sunday is observed on the Monday after. Hourly staff who work a listed holiday are paid at one and a half times their regular rate.

| Holiday | Date observed | Day |
| --- | --- | --- |
| New Year's Day | Jan 1 | Thursday |
| Martin Luther King Jr. Day | Jan 19 | Monday |
| Presidents' Day | Feb 16 | Monday |
| Memorial Day | May 25 | Monday |
| Juneteenth | Jun 19 | Friday |
| Independence Day (observed) | Jul 3 | Friday |
| Labor Day | Sep 7 | Monday |
| Thanksgiving Day | Nov 26 | Thursday |
| Day after Thanksgiving | Nov 27 | Friday |
| Christmas Day | Dec 25 | Friday |
`
)

type localAgentDemoDocument struct {
	title, probe, markdown string
}

// ensureLocalAgentDemoPolicyDocument gives the local Policy Helper something
// to read. A persona searches only documents officially placed in the
// conversation it is invoked in, and the local-dev seed places none, so
// without this every policy question finds no source. The document goes
// through the normal store operations: candidate, independent review,
// placement with a custodian and review date, sharing, and indexing.
func ensureLocalAgentDemoPolicyDocument(ctx context.Context, documents *documenthubstore.Store, chat *chatstore.Store, tenant, admin, audienceConversation string, scopes []string, now time.Time) (int, error) {
	return ensureLocalAgentDemoDocument(ctx, documents, chat, tenant, admin, audienceConversation, scopes, localAgentDemoDocument{title: localAgentDemoPolicyTitle, probe: localAgentDemoPolicyProbe, markdown: localAgentDemoPolicyMarkdown}, now)
}

func ensureLocalAgentDemoHolidayDocument(ctx context.Context, documents *documenthubstore.Store, chat *chatstore.Store, tenant, admin, audienceConversation string, now time.Time) (int, error) {
	return ensureLocalAgentDemoDocument(ctx, documents, chat, tenant, admin, audienceConversation, []string{audienceConversation}, localAgentDemoDocument{title: localAgentDemoHolidayTitle, probe: localAgentDemoHolidayProbe, markdown: localAgentDemoHolidayMarkdown}, now)
}

func ensureLocalAgentDemoDocument(ctx context.Context, documents *documenthubstore.Store, chat *chatstore.Store, tenant, admin, audienceConversation string, scopes []string, document localAgentDemoDocument, now time.Time) (int, error) {
	if documents == nil || chat == nil || tenant == "" || admin == "" || audienceConversation == "" {
		return 0, errors.New("local agent demo document dependencies are unavailable")
	}
	var missing []string
	documentID, versionID := "", ""
	for _, scope := range scopes {
		hits, err := documents.SearchOfficialPlacementLexical(ctx, tenant, scope, document.probe, "person", admin)
		if err != nil {
			return 0, fmt.Errorf("read policy placements in %s: %w", scope, err)
		}
		placed := false
		for _, hit := range hits {
			if hit.Title == document.title {
				placed, documentID, versionID = true, hit.DocumentID, hit.VersionID
			}
		}
		if !placed {
			missing = append(missing, scope)
		}
	}
	members, err := localAgentDemoConversationMembers(ctx, chat, tenant, audienceConversation)
	if err != nil {
		return 0, fmt.Errorf("read policy document readers: %w", err)
	}
	reviewer := ""
	for _, member := range members {
		if member != admin && !localAgentDemoAgentIDMatch(member) {
			reviewer = member
			break
		}
	}
	if reviewer == "" {
		return 0, errors.New("local agent demo policy document needs a reviewer other than its author")
	}
	if documentID == "" {
		documentID, err = documents.CreateDocument(ctx, tenant, admin, "PERSONAL")
		if err != nil {
			return 0, fmt.Errorf("create policy document: %w", err)
		}
		version, err := documents.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: documentID, CreatorID: admin, Title: document.title, Markdown: document.markdown, Classification: "INTERNAL"}, "")
		if err != nil {
			return 0, fmt.Errorf("submit policy document: %w", err)
		}
		versionID = version.ID
	}
	// Repeated preparation also grants newly added members access. Placement
	// selects the official version; the hub's explicit READ grant owns access.
	for _, member := range members {
		if documents.Authorize(ctx, tenant, documentID, "person", member, documenthubstore.ActionRead) == nil {
			continue
		}
		if _, err := documents.ShareDocument(ctx, tenant, documentID, admin, documenthubstore.GrantInput{SubjectKind: "person", SubjectID: member, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
			return 0, fmt.Errorf("share policy document: %w", err)
		}
		if err := documents.Authorize(ctx, tenant, documentID, "person", member, documenthubstore.ActionRead); err != nil {
			return 0, fmt.Errorf("policy document reader denied: %w", err)
		}
	}
	for _, scope := range missing {
		if _, err := documents.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: documentID, VersionID: versionID, ScopeKind: "placement", ScopeID: scope, ReviewerID: reviewer, Authority: localAgentDemoPolicyAuthority, Decision: documenthubstore.ReviewApproved}); err != nil {
			return 0, fmt.Errorf("review policy document for %s: %w", scope, err)
		}
		if _, err := documents.PlaceDocument(ctx, tenant, documenthubstore.PlaceInput{DocumentID: documentID, VersionID: versionID, ScopeKind: "placement", ScopeID: scope, ActorID: admin, CustodianID: admin, ReviewDueAt: now.Add(localAgentDemoPolicyReviewDue)}); err != nil {
			return 0, fmt.Errorf("place policy document in %s: %w", scope, err)
		}
	}
	if err := documents.IndexDeployedVersion(ctx, tenant, documentID, versionID); err != nil {
		return 0, fmt.Errorf("index policy document: %w", err)
	}
	return len(missing), nil
}

func localAgentDemoAgentIDMatch(subject string) bool {
	return subject == localAgentDemoAgentID || subject == localAgentDemoAssistantAgentID
}

func localAgentDemoConversationMembers(ctx context.Context, chat *chatstore.Store, tenant, conversation string) ([]string, error) {
	var members []string
	err := chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND state='active' ORDER BY member_id`, tenant, conversation)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var member string
			if err := rows.Scan(&member); err != nil {
				return err
			}
			members = append(members, member)
		}
		return rows.Err()
	})
	return members, err
}
