package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectviewstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
)

// Issue 3745: a local run with the document and project databases wired has
// nothing in them, so every document or ticket link pasted into Chat resolves
// to "unavailable". PrepareLocalWorkSurface lays down a small, deterministic
// set of demo work for the allowlisted demo workspaces: one project and one
// document that a handful of colleagues can open, and one project and one
// document that only the administrator and a named reviewer can open. A
// person can then paste each link into Chat and see an unfurl, a withheld
// card and the open action, depending on who is signed in.
//
// Everything goes through the stores' own operations, is idempotent (a second
// run changes nothing) and is refused unless the profile is local-dev, the
// workspace is an allowlisted demo workspace and every database is on a
// loopback host.

const (
	// WorkSurfaceOpenProjectID and WorkSurfaceRestrictedProjectID are the two
	// demo project identifiers.
	WorkSurfaceOpenProjectID       = "workplace-requests"
	WorkSurfaceRestrictedProjectID = "compensation-review"

	workSurfaceOpenProjectName       = "Workplace requests"
	workSurfaceRestrictedProjectName = "Compensation review"
	workSurfaceOpenDocumentTitle     = "Office move FAQ"
	workSurfaceRestrictedDocTitle    = "Pay band adjustments 2026 (restricted)"

	// workSurfaceOpenAudience bounds how many colleagues the open work is
	// shared with, besides the administrator.
	workSurfaceOpenAudience = 24
	// workSurfaceListLimit bounds the administrator's document listing used to
	// find a demo document that already exists.
	workSurfaceListLimit = 2000

	workSurfaceOpenDocumentMarkdown = `# Office move FAQ

The Denver office moves to the new building on the first Monday of next month. Badges are re-issued at the front desk on the Friday before the move.

## What do I pack?

Pack your desk drawers into the crates left at each desk. Monitors, chairs and standing desks are moved by the facilities team.

## Who do I ask?

Raise a request in the Workplace requests project and the facilities team will pick it up.
`
	workSurfaceRestrictedDocMarkdown = `# Pay band adjustments 2026 (restricted)

This document is shared only with the people named on it. It lists the proposed adjustments to the pay bands for the next review cycle.

## Proposed adjustments

The proposal moves the junior bands up by three percent and leaves the senior bands unchanged. It is not final until the compensation review closes.
`
)

// WorkSurfacePerson is one person the demo work may be shared with.
type WorkSurfacePerson struct{ Key, Name string }

// LocalWorkSurfaceConfig configures one preparation run.
type LocalWorkSurfaceConfig struct {
	Profile, Tenant                                                       string
	DatabaseURL, ChatDatabaseURL, DocumentDatabaseURL, ProjectDatabaseURL string
	// People are the active people of the workspace, as read from the core
	// database by the command. The administrator is not one of the audience.
	People []WorkSurfacePerson
	Now    func() time.Time
}

// ValidateLocalWorkSurfaceConfig refuses the run before any connection opens
// unless it is the local-development profile on an allowlisted demo workspace
// with the document and project databases set and every endpoint on loopback.
func ValidateLocalWorkSurfaceConfig(config LocalWorkSurfaceConfig) error {
	if config.Profile != ServeProfileLocalDev || !isLocalAgentDemoTenant(config.Tenant) {
		return fmt.Errorf("work surface preparation requires -profile local-dev and -tenant %s", localAgentDemoTenantChoices())
	}
	urls := []struct{ name, raw string }{
		{"database-url", config.DatabaseURL}, {"document-database-url", config.DocumentDatabaseURL}, {"project-database-url", config.ProjectDatabaseURL},
	}
	if strings.TrimSpace(config.ChatDatabaseURL) != "" {
		urls = append(urls, struct{ name, raw string }{"chat-database-url", config.ChatDatabaseURL})
	}
	for _, u := range urls {
		if strings.TrimSpace(u.raw) == "" {
			return fmt.Errorf("-%s is required: the work surface needs the %s", u.name, strings.TrimSuffix(strings.ReplaceAll(u.name, "-", " "), " url"))
		}
		parsed, err := url.Parse(u.raw)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || !loopbackDatabaseHost(parsed.Hostname()) {
			return fmt.Errorf("-%s must be a loopback PostgreSQL URL", u.name)
		}
	}
	return nil
}

// WorkSurfaceProjects is the project database the preparation writes through.
// *projectservice.StoreAdapter satisfies it.
type WorkSurfaceProjects interface {
	CreateProject(context.Context, projectservice.ProjectRecord, string, string) (projectservice.ProjectRecord, error)
	GetProject(context.Context, string, string) (projectservice.ProjectRecord, error)
	GetPublished(context.Context, string, string) (projectservice.WorkflowConfiguration, error)
	CreateTask(context.Context, projectservice.TaskRecord, uint64, string, string) (projectservice.TaskRecord, error)
	GetTask(context.Context, string, string, string) (projectservice.TaskRecord, error)
	GetMemberships(context.Context, string, string) (projectservice.MembershipSnapshot, error)
	InviteMember(ctx context.Context, tenant, project, actor, user string, role projectaccess.Role, inviteClass uint8, expected uint64, key string) error
	AcceptMemberInvitation(ctx context.Context, tenant, project, user string, expected uint64, key string) error
}

// WorkSurfaceDocuments is the document database the preparation writes
// through. *documenthubstore.Store satisfies it.
type WorkSurfaceDocuments interface {
	ListPersonalDocuments(ctx context.Context, tenant, actor string, limit int) ([]documenthubstore.DocumentSummary, error)
	CreateDocument(ctx context.Context, tenant, owner, home string) (string, error)
	SubmitCandidate(ctx context.Context, tenant string, v documenthubstore.Version, expectedBase string) (documenthubstore.Version, error)
	ResolveDeployment(ctx context.Context, tenant, docID, scopeKind, scopeID string) (documenthubstore.Deployment, error)
	RecordReview(ctx context.Context, tenant string, in documenthubstore.ReviewInput) (documenthubstore.Review, error)
	Deploy(ctx context.Context, tenant string, in documenthubstore.DeployInput) (documenthubstore.Deployment, error)
	ShareDocument(ctx context.Context, tenant, docID, actor string, in documenthubstore.GrantInput) (documenthubstore.Grant, error)
	Authorize(ctx context.Context, tenant, docID, subjectKind, subjectID, action string) error
}

// WorkSurfaceDeps are the ports ensureWorkSurface works through.
type WorkSurfaceDeps struct {
	Projects  WorkSurfaceProjects
	Documents WorkSurfaceDocuments
	Tenant    string
	Admin     string
	People    []WorkSurfacePerson
}

// WorkSurfaceItem is one prepared document or ticket the person can paste
// into Chat.
type WorkSurfaceItem struct {
	Kind, Title string
	// Link is the in-app address to paste into a message.
	Link string
	// Restricted is true when only the administrator and the reviewer can
	// open it.
	Restricted bool
}

// WorkSurfaceReceipt says what one run did and what it left to try.
type WorkSurfaceReceipt struct {
	Tenant, Admin string
	Reviewer      WorkSurfacePerson
	// Colleagues are the people the open work is shared with.
	Colleagues []WorkSurfacePerson
	Items      []WorkSurfaceItem
	// Created counts the projects, tickets, documents and memberships this
	// run added; zero means everything was already prepared.
	Created int
}

// Changed reports whether this run added anything.
func (r WorkSurfaceReceipt) Changed() bool { return r.Created > 0 }

type workSurfaceTicket struct{ id, title, description string }

var workSurfaceOpenTickets = []workSurfaceTicket{
	{"wr-1", "Replace the badge reader at the north entrance", "The reader rejects valid badges on cold mornings. Facilities to swap the unit before the office move."},
	{"wr-2", "Order standing desks for the Denver team", "Twelve standing desks for the new floor plan."},
	{"wr-3", "Update the visitor parking signs", "The signs still point to the old lot."},
}

var workSurfaceRestrictedTickets = []workSurfaceTicket{
	{"cr-1", "Review the 2026 pay band proposal", "Compare the proposed adjustments against the market data. Only the review group can see this ticket."},
}

// WorkSurfaceDocumentLink is the in-app address Chat unfurls for a document.
func WorkSurfaceDocumentLink(documentID string) string {
	return "/workspace/app/docs?document=" + url.QueryEscape(documentID)
}

// WorkSurfaceTicketLink is the in-app address Chat unfurls for a ticket.
func WorkSurfaceTicketLink(projectID, taskID string) string {
	return "/workspace/app/project?project=" + url.QueryEscape(projectID) + "&task=" + url.QueryEscape(taskID)
}

// ensureWorkSurface prepares the demo documents, projects and tickets.
func ensureWorkSurface(ctx context.Context, deps WorkSurfaceDeps, now time.Time) (WorkSurfaceReceipt, error) {
	receipt := WorkSurfaceReceipt{Tenant: deps.Tenant, Admin: deps.Admin}
	if ctx == nil || deps.Projects == nil || deps.Documents == nil || !isLocalAgentDemoTenant(deps.Tenant) || deps.Admin != localAgentDemoAdminFor(deps.Tenant) {
		return receipt, ErrLocalDevPersonaChatBootstrap
	}
	var others []WorkSurfacePerson
	seen := map[string]bool{deps.Admin: true}
	for _, p := range deps.People {
		if p.Key == "" || seen[p.Key] || localAgentDemoAgentIDMatch(p.Key) {
			continue
		}
		seen[p.Key] = true
		others = append(others, p)
	}
	sort.Slice(others, func(i, j int) bool { return others[i].Key < others[j].Key })
	if len(others) < 2 {
		return receipt, errors.New("the work surface demo needs at least two other people in the workspace: seed the demo people first")
	}
	// The reviewer is the one colleague who can open the restricted work; the
	// open audience is the next group, so that someone is left who can open
	// neither.
	receipt.Reviewer = others[0]
	audience := others[1:]
	if len(audience) > workSurfaceOpenAudience {
		audience = audience[:workSurfaceOpenAudience]
	}
	receipt.Colleagues = audience

	openMembers := keysOf(audience)
	openMembers = append(openMembers, receipt.Reviewer.Key)
	restrictedMembers := []string{receipt.Reviewer.Key}

	openDocID, created, err := ensureWorkSurfaceDocument(ctx, deps, now, workSurfaceOpenDocumentTitle, workSurfaceOpenDocumentMarkdown, openMembers, receipt.Reviewer.Key)
	if err != nil {
		return receipt, err
	}
	receipt.Created += created
	restrictedDocID, created, err := ensureWorkSurfaceDocument(ctx, deps, now, workSurfaceRestrictedDocTitle, workSurfaceRestrictedDocMarkdown, restrictedMembers, receipt.Reviewer.Key)
	if err != nil {
		return receipt, err
	}
	receipt.Created += created

	openLinks, created, err := ensureWorkSurfaceProject(ctx, deps, WorkSurfaceOpenProjectID, workSurfaceOpenProjectName, workSurfaceOpenTickets, openMembers)
	if err != nil {
		return receipt, err
	}
	receipt.Created += created
	restrictedLinks, created, err := ensureWorkSurfaceProject(ctx, deps, WorkSurfaceRestrictedProjectID, workSurfaceRestrictedProjectName, workSurfaceRestrictedTickets, restrictedMembers)
	if err != nil {
		return receipt, err
	}
	receipt.Created += created

	receipt.Items = append(receipt.Items, WorkSurfaceItem{Kind: "document", Title: workSurfaceOpenDocumentTitle, Link: WorkSurfaceDocumentLink(openDocID)})
	receipt.Items = append(receipt.Items, openLinks...)
	receipt.Items = append(receipt.Items, WorkSurfaceItem{Kind: "document", Title: workSurfaceRestrictedDocTitle, Link: WorkSurfaceDocumentLink(restrictedDocID), Restricted: true})
	receipt.Items = append(receipt.Items, restrictedLinks...)
	return receipt, nil
}

func keysOf(people []WorkSurfacePerson) []string {
	keys := make([]string, 0, len(people)+1)
	for _, p := range people {
		keys = append(keys, p.Key)
	}
	return keys
}

// ensureWorkSurfaceDocument finds or creates one administrator-owned document,
// publishes it (a colleague reviews, the administrator deploys) and shares it
// with exactly readers. It returns the document id and how many things it
// added.
func ensureWorkSurfaceDocument(ctx context.Context, deps WorkSurfaceDeps, now time.Time, title, markdown string, readers []string, reviewer string) (string, int, error) {
	added := 0
	list, err := deps.Documents.ListPersonalDocuments(ctx, deps.Tenant, deps.Admin, workSurfaceListLimit)
	if err != nil {
		return "", 0, fmt.Errorf("read the administrator's documents: %w", err)
	}
	docID, versionID := "", ""
	for _, d := range list {
		if d.OwnerID == deps.Admin && d.Title == title {
			docID, versionID = d.ID, d.VersionID
			break
		}
	}
	if docID == "" {
		docID, err = deps.Documents.CreateDocument(ctx, deps.Tenant, deps.Admin, "PERSONAL")
		if err != nil {
			return "", added, fmt.Errorf("create %q: %w", title, err)
		}
		version, err := deps.Documents.SubmitCandidate(ctx, deps.Tenant, documenthubstore.Version{DocumentID: docID, CreatorID: deps.Admin, Title: title, Markdown: markdown, Classification: "INTERNAL"}, "")
		if err != nil {
			return "", added, fmt.Errorf("write %q: %w", title, err)
		}
		versionID = version.ID
		added++
	}
	if _, err := deps.Documents.ResolveDeployment(ctx, deps.Tenant, docID, "default", ""); err != nil {
		if !errors.Is(err, documenthubstore.ErrNoDeployment) {
			return "", added, fmt.Errorf("read the publication of %q: %w", title, err)
		}
		if versionID == "" {
			return "", added, fmt.Errorf("%q has no version to publish", title)
		}
		if _, err := deps.Documents.RecordReview(ctx, deps.Tenant, documenthubstore.ReviewInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", ReviewerID: reviewer, Authority: "personal", Decision: documenthubstore.ReviewApproved, Note: "Local demo document"}); err != nil {
			return "", added, fmt.Errorf("review %q: %w", title, err)
		}
		if _, err := deps.Documents.Deploy(ctx, deps.Tenant, documenthubstore.DeployInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", DeployerID: deps.Admin, CustodianID: deps.Admin, ReviewDueAt: now.Add(180 * 24 * time.Hour)}); err != nil {
			return "", added, fmt.Errorf("publish %q: %w", title, err)
		}
		added++
	}
	for _, reader := range readers {
		if deps.Documents.Authorize(ctx, deps.Tenant, docID, "person", reader, documenthubstore.ActionRead) == nil {
			continue
		}
		if _, err := deps.Documents.ShareDocument(ctx, deps.Tenant, docID, deps.Admin, documenthubstore.GrantInput{SubjectKind: "person", SubjectID: reader, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
			return "", added, fmt.Errorf("share %q: %w", title, err)
		}
		added++
	}
	return docID, added, nil
}

// ensureWorkSurfaceProject finds or creates one administrator-owned project,
// invites exactly members into it (they accept) and files its tickets.
func ensureWorkSurfaceProject(ctx context.Context, deps WorkSurfaceDeps, projectID, name string, tickets []workSurfaceTicket, members []string) ([]WorkSurfaceItem, int, error) {
	added := 0
	if _, err := deps.Projects.GetProject(ctx, deps.Tenant, projectID); err != nil {
		if _, createErr := deps.Projects.CreateProject(ctx, projectservice.ProjectRecord{ID: projectID, TenantID: deps.Tenant, OwnerID: deps.Admin, Name: name, Timezone: "UTC", State: project.LifecycleActive}, deps.Admin, "issue3745:project:"+projectID); createErr != nil {
			if _, err = deps.Projects.GetProject(ctx, deps.Tenant, projectID); err != nil {
				return nil, added, fmt.Errorf("create project %q: %w", name, createErr)
			}
		} else {
			added++
		}
	}
	published, err := deps.Projects.GetPublished(ctx, deps.Tenant, projectID)
	if err != nil || published.Version == 0 || len(published.Config.TaskTypes) == 0 || published.Config.TaskTypes[0].InitialStatus == "" {
		return nil, added, fmt.Errorf("project %q has no published workflow to file tickets in", name)
	}
	taskType := published.Config.TaskTypes[0]
	var items []WorkSurfaceItem
	for _, ticket := range tickets {
		if _, err := deps.Projects.GetTask(ctx, deps.Tenant, projectID, ticket.id); err != nil {
			record := projectservice.TaskRecord{ID: ticket.id, TenantID: deps.Tenant, ProjectID: projectID, Title: ticket.title, Description: ticket.description, StatusID: taskType.InitialStatus, TypeID: string(taskType.ID), Priority: "NORMAL", Revision: 1, CreatedBy: deps.Admin}
			if _, createErr := deps.Projects.CreateTask(ctx, record, published.Version, deps.Admin, "issue3745:task:"+projectID+":"+ticket.id); createErr != nil {
				if _, err = deps.Projects.GetTask(ctx, deps.Tenant, projectID, ticket.id); err != nil {
					return nil, added, fmt.Errorf("file ticket %q: %w", ticket.title, createErr)
				}
			} else {
				added++
			}
		}
		items = append(items, WorkSurfaceItem{Kind: "ticket", Title: ticket.title, Link: WorkSurfaceTicketLink(projectID, ticket.id), Restricted: projectID == WorkSurfaceRestrictedProjectID})
	}
	for _, member := range members {
		snapshot, err := deps.Projects.GetMemberships(ctx, deps.Tenant, projectID)
		if err != nil {
			return nil, added, fmt.Errorf("read the members of %q: %w", name, err)
		}
		state := projectaccess.MembershipState("")
		for _, m := range snapshot.Members {
			if m.UserID == member {
				state = m.State
			}
		}
		if state == projectaccess.MembershipActive {
			continue
		}
		if state == "" {
			if err := deps.Projects.InviteMember(ctx, deps.Tenant, projectID, deps.Admin, member, projectaccess.RoleContributor, 0, snapshot.Revision, "issue3745:invite:"+projectID+":"+member); err != nil {
				return nil, added, fmt.Errorf("invite %s to %q: %w", member, name, err)
			}
			if snapshot, err = deps.Projects.GetMemberships(ctx, deps.Tenant, projectID); err != nil {
				return nil, added, fmt.Errorf("read the members of %q: %w", name, err)
			}
		}
		if err := deps.Projects.AcceptMemberInvitation(ctx, deps.Tenant, projectID, member, snapshot.Revision, "issue3745:accept:"+projectID+":"+member); err != nil {
			return nil, added, fmt.Errorf("add %s to %q: %w", member, name, err)
		}
		added++
	}
	return items, added, nil
}

// PrepareLocalWorkSurface opens the document and project databases the
// command configured and prepares the demo work through them.
func PrepareLocalWorkSurface(ctx context.Context, config LocalWorkSurfaceConfig) (WorkSurfaceReceipt, error) {
	var receipt WorkSurfaceReceipt
	if ctx == nil {
		return receipt, errors.New("work surface preparation requires a context")
	}
	if err := ValidateLocalWorkSurfaceConfig(config); err != nil {
		return receipt, err
	}
	now := time.Now().UTC()
	if config.Now != nil {
		now = config.Now().UTC()
	}
	documents, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: config.DocumentDatabaseURL, ChatDSN: config.ChatDatabaseURL, CoreDSN: config.DatabaseURL})
	if err != nil {
		return receipt, fmt.Errorf("open document database: %w", err)
	}
	defer documents.Close()
	store, err := projectstore.New(ctx, projectstore.Config{DSN: config.ProjectDatabaseURL, CoreDSN: config.DatabaseURL, Schema: ProjectSchemaName, MaxConns: 4, MinConns: 1})
	if err != nil {
		return receipt, fmt.Errorf("open project database: %w", err)
	}
	defer store.Close()
	members, err := projectmemberstore.New(store)
	if err != nil {
		return receipt, err
	}
	adapter := &projectservice.StoreAdapter{Projects: store, Members: members, Configs: projectconfigstore.New(store), Views: projectviewstore.New(store)}
	return ensureWorkSurface(ctx, WorkSurfaceDeps{Projects: adapter, Documents: documents, Tenant: config.Tenant, Admin: localAgentDemoAdminFor(config.Tenant), People: config.People}, now)
}

// LocalWorkSurfaceServeFlags is the exact flag set a local run needs so the
// Documents and Projects pages and every link in Chat are served.
const LocalWorkSurfaceServeFlags = "-document-database-url <document url> -project-database-url <project url> -migrate"

// LocalWorkSurfaceGuide is the plain-words guide the command prints: what was
// prepared, how to start the server so it is served, and what to try as whom.
func LocalWorkSurfaceGuide(r WorkSurfaceReceipt) string {
	var b strings.Builder
	state := "already prepared; nothing changed"
	if r.Changed() {
		state = fmt.Sprintf("prepared (%d additions)", r.Created)
	}
	fmt.Fprintf(&b, "Work surface demo for %s: %s.\n", r.Tenant, state)
	fmt.Fprintf(&b, "Start the server with the document and project databases so Documents, Projects and the links in Chat are set up: %s.\n", LocalWorkSurfaceServeFlags)
	b.WriteString("Paste these addresses into a Chat message to see each card:\n")
	for _, item := range r.Items {
		who := "the administrator, the reviewer and the colleagues"
		if item.Restricted {
			who = "only the administrator and the reviewer"
		}
		fmt.Fprintf(&b, "  %s %q (%s): %s\n", item.Kind, item.Title, who, item.Link)
	}
	fmt.Fprintf(&b, "Sign in as the administrator (%s) to open everything. Sign in as the reviewer (%s, %s) to open everything too.\n", r.Admin, r.Reviewer.Name, r.Reviewer.Key)
	if len(r.Colleagues) > 0 {
		c := r.Colleagues[0]
		fmt.Fprintf(&b, "Sign in as a colleague such as %s (%s) to open the open work and see the restricted links withheld. Anyone outside both groups sees every card withheld.\n", c.Name, c.Key)
	}
	return b.String()
}

var (
	_ WorkSurfaceProjects  = (*projectservice.StoreAdapter)(nil)
	_ WorkSurfaceDocuments = (*documenthubstore.Store)(nil)
)
