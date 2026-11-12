package workitem

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// ChecklistStore is the SMB-003 persistence and serving adapter for
// repeatable procedures. Like [RequestInbox] it is a value-owned experience
// layer: it stores only template and checklist state, validates every write,
// and never hands out a live reference. Employee-home and team-surface
// shells serve [ChecklistStore.ListByAssignee],
// [ChecklistStore.ProgressByOwner] and [ChecklistStore.View]; durable
// WorkItem persistence stays with the caller and the owning workflows.
type ChecklistStore struct {
	mu        sync.RWMutex
	templates map[uuid.UUID]ChecklistTemplate
	lists     map[uuid.UUID]Checklist
}

// SaveTemplate stores a validated template. Identity collisions are refused
// rather than overwritten.
func (s *ChecklistStore) SaveTemplate(_ context.Context, template ChecklistTemplate) error {
	if err := template.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.templates == nil {
		s.templates = make(map[uuid.UUID]ChecklistTemplate)
	}
	if _, exists := s.templates[template.TemplateID]; exists {
		return refuse(CodeInvalidRecord, template.TemplateID.String(), "template id already exists")
	}
	s.templates[template.TemplateID] = cloneChecklistTemplate(template)
	return nil
}

// LoadTemplate returns the tenant's template. A cross-tenant read looks like
// a missing template: this package never discloses what it may not see.
func (s *ChecklistStore) LoadTemplate(_ context.Context, tenantID, templateID uuid.UUID) (ChecklistTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	template, ok := s.templates[templateID]
	if !ok || template.TenantID != tenantID {
		return ChecklistTemplate{}, refuse(CodeWorkItemNotFound, templateID.String(), "template is not visible to tenant")
	}
	return cloneChecklistTemplate(template), nil
}

// Save stores a fresh validated checklist. Identity collisions are refused;
// progress on a stored checklist goes through [ChecklistStore.Update].
func (s *ChecklistStore) Save(_ context.Context, list Checklist) error {
	if err := list.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lists == nil {
		s.lists = make(map[uuid.UUID]Checklist)
	}
	if _, exists := s.lists[list.ChecklistID]; exists {
		return refuse(CodeInvalidRecord, list.ChecklistID.String(), "checklist id already exists")
	}
	s.lists[list.ChecklistID] = cloneChecklist(list)
	return nil
}

// Update replaces a stored checklist with a re-validated copy, so hand-edited
// state (forged completions, foreign items, skipped-required steps) can never
// be written back over a good one.
func (s *ChecklistStore) Update(_ context.Context, list Checklist) (Checklist, error) {
	if err := list.Validate(); err != nil {
		return Checklist{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.lists[list.ChecklistID]
	if !ok || stored.TenantID != list.TenantID {
		return Checklist{}, refuse(CodeWorkItemNotFound, list.ChecklistID.String(), "checklist is not visible to tenant")
	}
	s.lists[list.ChecklistID] = cloneChecklist(list)
	return cloneChecklist(list), nil
}

// Load returns the tenant's checklist.
func (s *ChecklistStore) Load(_ context.Context, tenantID, checklistID uuid.UUID) (Checklist, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list, ok := s.lists[checklistID]
	if !ok || list.TenantID != tenantID {
		return Checklist{}, refuse(CodeWorkItemNotFound, checklistID.String(), "checklist is not visible to tenant")
	}
	return cloneChecklist(list), nil
}

// LinkDocument attaches an authorized document link to one checklist item.
// The actor must be the item assignee or name the checklist's sharing roles
// through the caller, must itself be an authorized viewer, and the link must
// be complete and tenant-local; anything else is refused so a checklist can
// never expose a private document.
func (s *ChecklistStore) LinkDocument(_ context.Context, tenantID, checklistID, itemID uuid.UUID, actorID string, link AuthorizedDocumentLink) (Checklist, error) {
	if strings.TrimSpace(actorID) == "" || link.TenantID == uuid.Nil || strings.TrimSpace(link.DocumentVersionID) == "" || strings.TrimSpace(link.DocumentDigest) == "" || len(link.AuthorizedViewerIDs) == 0 {
		return Checklist{}, refuse(CodeInvalidRecord, checklistID.String(), "document link is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, ok := s.lists[checklistID]
	if !ok || list.TenantID != tenantID || link.TenantID != tenantID || !contains(link.AuthorizedViewerIDs, actorID) {
		return Checklist{}, refuse(CodeWorkItemNotFound, checklistID.String(), "document link is not authorized")
	}
	found := false
	for i := range list.Items {
		if list.Items[i].ItemID != itemID {
			continue
		}
		found = true
		if list.Items[i].Assignee != actorID {
			return Checklist{}, refuse(CodeWorkItemNotFound, checklistID.String(), "document link is not authorized")
		}
		list.Items[i].Documents = append(list.Items[i].Documents, cloneDocumentLink(link))
	}
	if !found {
		return Checklist{}, refuse(CodeWorkItemNotFound, checklistID.String(), "checklist item is not on this checklist")
	}
	if err := list.Validate(); err != nil {
		return Checklist{}, err
	}
	s.lists[checklistID] = cloneChecklist(list)
	return cloneChecklist(list), nil
}

// ListByAssignee is the employee-home queue: the tenant's checklists reduced
// to the viewer's own items, in position order. Each item keeps only the
// document links the assignee is authorized to view, as [ChecklistStore.View]
// does, so the queue never exposes a link its viewer cannot open.
func (s *ChecklistStore) ListByAssignee(_ context.Context, tenantID uuid.UUID, assigneeID string) []Checklist {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Checklist, 0)
	for _, list := range s.lists {
		if list.TenantID != tenantID {
			continue
		}
		mine := cloneChecklist(list)
		mine.Items = mine.Items[:0]
		for _, item := range list.Items {
			if item.Assignee != assigneeID {
				continue
			}
			own := cloneChecklistItem(item)
			kept := own.Documents[:0]
			for _, link := range own.Documents {
				if contains(link.AuthorizedViewerIDs, assigneeID) {
					kept = append(kept, link)
				}
			}
			own.Documents = kept
			mine.Items = append(mine.Items, own)
		}
		if len(mine.Items) > 0 {
			result = append(result, mine)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}

// OwnerProgress is one owner's done/total counts on a checklist.
type OwnerProgress struct {
	Done  int
	Total int
}

// ProgressByOwner shows completion by owner for a tenant's checklist.
func (s *ChecklistStore) ProgressByOwner(_ context.Context, tenantID, checklistID uuid.UUID) (map[string]OwnerProgress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list, ok := s.lists[checklistID]
	if !ok || list.TenantID != tenantID {
		return nil, refuse(CodeWorkItemNotFound, checklistID.String(), "checklist is not visible to tenant")
	}
	progress := make(map[string]OwnerProgress)
	for _, item := range list.Items {
		if item.Assignee == "" {
			continue
		}
		seen := progress[item.Assignee]
		seen.Total++
		if item.State == StepStateDone {
			seen.Done++
		}
		progress[item.Assignee] = seen
	}
	return progress, nil
}

// View is the team-surface projection: the full checklist with every
// document link independently filtered to the viewer's authorized set.
func (s *ChecklistStore) View(_ context.Context, tenantID, checklistID uuid.UUID, viewerID string) Checklist {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list, ok := s.lists[checklistID]
	if !ok || list.TenantID != tenantID {
		return Checklist{}
	}
	out := cloneChecklist(list)
	for i := range out.Items {
		kept := out.Items[i].Documents[:0]
		for _, link := range list.Items[i].Documents {
			if contains(link.AuthorizedViewerIDs, viewerID) {
				kept = append(kept, cloneDocumentLink(link))
			}
		}
		out.Items[i].Documents = kept
	}
	return out
}

func cloneChecklistTemplate(in ChecklistTemplate) ChecklistTemplate {
	in.RoleRefs = append([]string(nil), in.RoleRefs...)
	in.Items = append([]TemplateItem(nil), in.Items...)
	return in
}

func cloneChecklist(in Checklist) Checklist {
	in.RoleRefs = append([]string(nil), in.RoleRefs...)
	items := in.Items
	in.Items = make([]ChecklistItem, len(items))
	for i, item := range items {
		in.Items[i] = cloneChecklistItem(item)
	}
	return in
}

func cloneChecklistItem(in ChecklistItem) ChecklistItem {
	if in.CompletedAt != nil {
		stamp := *in.CompletedAt
		in.CompletedAt = &stamp
	}
	if in.DueAt != nil {
		due := *in.DueAt
		in.DueAt = &due
	}
	documents := in.Documents
	in.Documents = make([]AuthorizedDocumentLink, len(documents))
	for i, link := range documents {
		in.Documents[i] = cloneDocumentLink(link)
	}
	return in
}
