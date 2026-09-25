// Package projectrefs adapts current Chat and Docs read authority to the
// project link resolver. It carries no target permissions of its own.
package projectrefs

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatroutingadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatLinks is the narrow current-authorized reference surface forwarded by
// the routed Chat service.
type ChatLinks interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
}

// DocumentPlacements resolves a viewer-authorized official Docs placement.
type DocumentPlacements interface {
	GetDocumentPlacement(context.Context, string, string, string, string) (document.Deployment, error)
}

// WorkItemProjection reads only the safe board projection through the current
// WorkService read and visibility policy. Implementations must recheck current
// authorization on every call because Resolver brackets a target read with
// two authorization checks.
type WorkItemProjection interface {
	ReadAuthorizedWorkItem(context.Context, *trust.Principal, string) (projectlink.Preview, bool, error)
}

// WorkOrderProjection reads only the identity-safe project link preview from
// the owning work-order authority. Implementations must check the principal's
// current access to the work order and its tenant/project on every call. A
// project link must never grant access to the target work order.
type WorkOrderProjection interface {
	ReadAuthorizedWorkOrder(context.Context, *trust.Principal, string) (projectlink.Preview, bool, error)
}

// WorkItemReadPort is the tenant-scoped owning store read needed by the
// projection. It deliberately exposes no mutation operations.
type WorkItemReadPort interface {
	ReadWorkItem(context.Context, values.TenantId, uuid.UUID) (workitem.WorkItem, error)
}

// WorkItemProjector applies the same action and visibility policy as
// WorkService.GetWorkItem, then returns only fields allowed on a project
// board. Authorization must be the WorkService's current durable capability
// check; the raw application reader alone does not authorize disclosure.
type WorkItemProjector struct {
	Source    WorkItemReadPort
	Authorize func(context.Context, *trust.Principal, string) bool
	TenantID  func(values.TenantId) uuid.UUID
	Now       func() time.Time
}

var _ WorkItemProjection = WorkItemProjector{}

// ReadAuthorizedWorkItem implements WorkItemProjection. Missing, malformed,
// cross-tenant, and invisible rows all produce found=false.
func (p WorkItemProjector) ReadAuthorizedWorkItem(ctx context.Context, principal *trust.Principal, id string) (projectlink.Preview, bool, error) {
	if principal == nil || p.Source == nil || p.Authorize == nil || p.TenantID == nil || !p.Authorize(ctx, principal, "get_work_item") {
		return projectlink.Preview{}, false, nil
	}
	workItemID, err := uuid.Parse(id)
	if err != nil {
		return projectlink.Preview{}, false, nil
	}
	item, err := p.Source.ReadWorkItem(ctx, values.TenantId(principal.Tenant().String()), workItemID)
	if err != nil {
		if workitem.CodeOf(err) == workitem.CodeWorkItemNotFound {
			return projectlink.Preview{}, false, nil
		}
		return projectlink.Preview{}, false, err
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	membership := workitem.MembershipOf(item, principal.Subject(), now)
	governed := p.Authorize(ctx, principal, "work_item_governance_view")
	inScope := item.OrganizationScopeID != "" && item.OrganizationScopeID == principal.OrganizationScopeID()
	if item.TenantID != p.TenantID(principal.Tenant()) || !workitem.Visible(item, membership, inScope, governed) {
		return projectlink.Preview{}, false, nil
	}
	return projectlink.Preview{
		Kind: projectlink.WorkItem, ID: item.WorkItemID.String(),
		Version: strconv.FormatInt(item.ItemVersion, 10), Status: string(item.Status),
		Freshness: "CURRENT",
	}, true, nil
}

// The production route adapter forwards authorized target reads, and the
// canonical document service contract includes the Docs placement method.
var _ ChatLinks = (*chatroutingadapter.Service)(nil)
var _ DocumentPlacements = (document.Service)(nil)

// Adapter implements projectlink's two resolver ports using the owning
// services. A deployment's exact placement scope and version are both pinned.
type Adapter struct {
	Chat       ChatLinks
	Documents  DocumentPlacements
	WorkItems  WorkItemProjection
	WorkOrders WorkOrderProjection
}

var _ projectlink.Authorizer = Adapter{}
var _ projectlink.TargetReader = Adapter{}

func (a Adapter) Authorize(ctx context.Context, actor string, ref projectlink.Reference) (bool, error) {
	caller, ok := callerFor(ctx, actor)
	if !ok {
		return false, nil
	}
	switch ref.Kind {
	case projectlink.ChatConversation, projectlink.ChatPost:
		if a.Chat == nil {
			return false, nil
		}
		conversation := ref.ID
		post := ""
		if ref.Kind == projectlink.ChatPost {
			conversation = ref.ConversationID
			post = ref.ID
		}
		_, _, err := a.Chat.ReadAuthorizedReference(ctx, caller.chat, caller.tenant, conversation, post)
		return allowedOrDenied(err)
	case projectlink.DeployedDocument:
		deployment, err := a.placement(ctx, caller, ref)
		if err != nil {
			return false, err
		}
		return validPlacement(deployment, ref), nil
	case projectlink.WorkItem:
		if a.WorkItems == nil {
			return false, nil
		}
		_, found, err := a.WorkItems.ReadAuthorizedWorkItem(ctx, trustedPrincipal(ctx), ref.ID)
		return found, err
	case projectlink.WorkOrder:
		principal := trustedPrincipal(ctx)
		if a.WorkOrders == nil || principal == nil {
			return false, nil
		}
		_, found, err := a.WorkOrders.ReadAuthorizedWorkOrder(ctx, principal, ref.ID)
		return found, err
	case projectlink.Journey:
		// The link discloses nothing but the ID the linker supplied; the
		// journey service authorizes the viewer's read of the journey.
		return true, nil
	default:
		return false, nil
	}
}

func (a Adapter) ReadAuthorized(ctx context.Context, ref projectlink.Reference) (projectlink.Preview, bool, error) {
	trusted, ok := trust.FromContext(ctx)
	if !ok || trusted == nil {
		return projectlink.Preview{}, false, nil
	}
	caller := caller{tenant: trusted.Tenant().String(), actor: trusted.Subject(), chat: chat.Principal{
		TenantID: trusted.Tenant().String(), SubjectID: trusted.Subject(), Roles: trusted.Roles(),
	}}
	switch ref.Kind {
	case projectlink.ChatConversation, projectlink.ChatPost:
		if a.Chat == nil {
			return projectlink.Preview{}, false, nil
		}
		conversation := ref.ID
		postID := ""
		if ref.Kind == projectlink.ChatPost {
			conversation = ref.ConversationID
			postID = ref.ID
		}
		room, post, err := a.Chat.ReadAuthorizedReference(ctx, caller.chat, caller.tenant, conversation, postID)
		if err != nil {
			return projectlink.Preview{}, false, propagateOrRestrict(err)
		}
		if room.TenantID != caller.tenant || room.ID != conversation {
			return projectlink.Preview{}, false, nil
		}
		preview := projectlink.Preview{Kind: projectlink.ChatConversation, ID: room.ID, Title: room.Name}
		if ref.Kind == projectlink.ChatPost {
			if post == nil || post.ID != ref.ID || post.TenantID != caller.tenant || post.ConversationID != room.ID || post.Deleted {
				return projectlink.Preview{}, false, nil
			}
			preview = projectlink.Preview{Kind: projectlink.ChatPost, ID: post.ID, ConversationID: room.ID, Snippet: clipRunes(post.Body, 600)}
		} else if post != nil {
			return projectlink.Preview{}, false, nil
		}
		return preview, true, nil
	case projectlink.DeployedDocument:
		if a.Documents == nil {
			return projectlink.Preview{}, false, nil
		}
		deployment, err := a.placement(ctx, caller, ref)
		if err != nil {
			return projectlink.Preview{}, false, err
		}
		if !validPlacement(deployment, ref) {
			return projectlink.Preview{}, false, nil
		}
		// The placement check is the owning read authority. Do not load a
		// personal draft or version history to decorate this preview.
		return projectlink.Preview{Kind: projectlink.DeployedDocument, ID: deployment.DocumentID, Version: deployment.VersionID, ScopeID: deployment.ScopeID}, true, nil
	case projectlink.Journey:
		return projectlink.Preview{Kind: projectlink.Journey, ID: ref.ID}, true, nil
	case projectlink.WorkItem:
		if a.WorkItems == nil {
			return projectlink.Preview{}, false, nil
		}
		return a.WorkItems.ReadAuthorizedWorkItem(ctx, trustedPrincipal(ctx), ref.ID)
	case projectlink.WorkOrder:
		principal := trustedPrincipal(ctx)
		if a.WorkOrders == nil || principal == nil {
			return projectlink.Preview{}, false, nil
		}
		return a.WorkOrders.ReadAuthorizedWorkOrder(ctx, principal, ref.ID)
	default:
		return projectlink.Preview{}, false, nil
	}
}

func trustedPrincipal(ctx context.Context) *trust.Principal {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil
	}
	return principal
}

type caller struct {
	tenant string
	actor  string
	chat   chat.Principal
}

func callerFor(ctx context.Context, actor string) (caller, bool) {
	trusted, ok := trust.FromContext(ctx)
	if !ok || trusted == nil || trusted.Subject() != actor || actor == "" {
		return caller{}, false
	}
	tenant := trusted.Tenant().String()
	return caller{tenant: tenant, actor: actor, chat: chat.Principal{TenantID: tenant, SubjectID: actor, Roles: trusted.Roles()}}, tenant != ""
}

func (a Adapter) placement(ctx context.Context, c caller, ref projectlink.Reference) (document.Deployment, error) {
	if a.Documents == nil {
		return document.Deployment{}, nil
	}
	deployment, err := a.Documents.GetDocumentPlacement(ctx, c.tenant, c.actor, ref.ID, ref.ScopeID)
	if denied(err) {
		return document.Deployment{}, nil
	}
	return deployment, err
}

func validPlacement(d document.Deployment, ref projectlink.Reference) bool {
	return d.Official && d.DocumentID == ref.ID && d.VersionID == ref.Version &&
		d.ScopeID == ref.ScopeID && d.ScopeKind == "placement"
}

func allowedOrDenied(err error) (bool, error) {
	if denied(err) {
		return false, nil
	}
	return err == nil, err
}

func propagateOrRestrict(err error) error {
	if denied(err) {
		return nil
	}
	return err
}

func denied(err error) bool {
	if err == nil || errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrPermissionDenied) {
		return err != nil
	}
	owned, ok := envelope.As(err)
	return ok && (owned.Code() == envelope.CodeNotFound || owned.Code() == envelope.CodePermissionDenied)
}

func clipRunes(body string, limit int) string {
	if !utf8.ValidString(body) {
		body = strings.ToValidUTF8(body, "�")
	}
	runes := []rune(body)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return body
}
