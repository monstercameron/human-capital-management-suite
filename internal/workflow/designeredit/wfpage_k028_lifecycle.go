package designeredit

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type PageDraftStatus string

const (
	PageDraftStatusDraft     PageDraftStatus = "DRAFT"
	PageDraftStatusValidated PageDraftStatus = "VALIDATED"
	PageDraftStatusApproved  PageDraftStatus = "APPROVED"
	PageDraftStatusPublished PageDraftStatus = "PUBLISHED"
)

type PageOverrideDraft struct {
	DraftID                string
	WorkflowID             string
	WorkflowVersion        uint32
	PageID                 string
	Revision               uint64
	BaseWorkflowDigest     string
	GeneratedDefaultDigest string
	OverrideDigest         string
	Bindings               []workflowversion.PageBinding
	UnresolvedBindings     []string
	Status                 PageDraftStatus
}

type PagePublication struct {
	WorkflowID       string
	WorkflowVersion  uint32
	PageID           string
	PageVersion      int64
	DefinitionDigest string
	PublishedBy      string
}

var (
	ErrPageDraftInvalid  = errors.New("designeredit: invalid page draft")
	ErrPageDraftConflict = errors.New("designeredit: page draft conflict")
	ErrPageDraftBlocked  = errors.New("designeredit: page draft is not publishable")
)

// PageDraftStore is a value-owned draft and active-version store used by the
// page authoring boundary. Publish commits the new active pointer only after
// all checks pass, so an interrupted publish keeps the previous version live.
type PageDraftStore struct {
	mu      sync.Mutex
	drafts  map[string]PageOverrideDraft
	active  map[string]PagePublication
	history map[string][]PagePublication
}

func NewPageDraftStore() *PageDraftStore {
	return &PageDraftStore{drafts: make(map[string]PageOverrideDraft), active: make(map[string]PagePublication), history: make(map[string][]PagePublication)}
}

func pageDraftTarget(draft PageOverrideDraft) string {
	return strings.TrimSpace(draft.WorkflowID) + "\x00" + strconv.FormatUint(uint64(draft.WorkflowVersion), 10) + "\x00" + strings.TrimSpace(draft.PageID)
}

func (store *PageDraftStore) Save(_ context.Context, draft PageOverrideDraft, expectedRevision uint64) (PageOverrideDraft, error) {
	if store == nil || strings.TrimSpace(draft.DraftID) == "" || strings.TrimSpace(draft.WorkflowID) == "" || draft.WorkflowVersion == 0 || strings.TrimSpace(draft.PageID) == "" || strings.TrimSpace(draft.BaseWorkflowDigest) == "" || strings.TrimSpace(draft.GeneratedDefaultDigest) == "" || strings.TrimSpace(draft.OverrideDigest) == "" {
		return PageOverrideDraft{}, ErrPageDraftInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	target := pageDraftTarget(draft)
	if existing, ok := store.drafts[target]; ok {
		if existing.DraftID != draft.DraftID || existing.Revision != expectedRevision {
			return PageOverrideDraft{}, ErrPageDraftConflict
		}
		draft.Revision = existing.Revision + 1
	} else if expectedRevision != 0 {
		return PageOverrideDraft{}, ErrPageDraftConflict
	} else {
		draft.Revision = 1
	}
	if draft.Status == "" {
		draft.Status = PageDraftStatusDraft
	}
	draft.Bindings = append([]workflowversion.PageBinding(nil), draft.Bindings...)
	store.drafts[target] = draft
	return clonePageOverrideDraft(draft), nil
}

func (store *PageDraftStore) Get(_ context.Context, draft PageOverrideDraft) (PageOverrideDraft, bool) {
	if store == nil {
		return PageOverrideDraft{}, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	found, ok := store.drafts[pageDraftTarget(draft)]
	return clonePageOverrideDraft(found), ok
}

func ValidatePageOverride(draft PageOverrideDraft, workflowInputs []workflowversion.PageBinding) (PageOverrideDraft, error) {
	if len(draft.UnresolvedBindings) > 0 {
		return PageOverrideDraft{}, workflowversion.ErrPageBindingUnresolved
	}
	if err := workflowversion.ValidatePageBindings(draft.Bindings, workflowInputs); err != nil {
		return PageOverrideDraft{}, err
	}
	draft.Status = PageDraftStatusValidated
	return clonePageOverrideDraft(draft), nil
}

// RebasePageOverride records the workflow-version diff on the draft. The
// unresolved list is durable draft state, so an author must explicitly repair
// every added, removed, or retyped input before validation can succeed.
func RebasePageOverride(draft PageOverrideDraft, previousInputs, nextInputs []workflowversion.PageBinding) PageOverrideDraft {
	rebased := workflowversion.RebasePageBindings(draft.Bindings, previousInputs, nextInputs)
	draft.Bindings = rebased.Bindings
	draft.UnresolvedBindings = append([]string(nil), rebased.Unresolved...)
	return clonePageOverrideDraft(draft)
}

func ApprovePageOverride(draft PageOverrideDraft, reviewer string) (PageOverrideDraft, error) {
	if draft.Status != PageDraftStatusValidated || strings.TrimSpace(reviewer) == "" {
		return PageOverrideDraft{}, ErrPageDraftBlocked
	}
	draft.Status = PageDraftStatusApproved
	return clonePageOverrideDraft(draft), nil
}

func (store *PageDraftStore) Publish(_ context.Context, draft PageOverrideDraft, pageVersion int64, publisher string, workflowInputs []workflowversion.PageBinding, interrupted bool) (PagePublication, error) {
	if store == nil || pageVersion < 1 || strings.TrimSpace(publisher) == "" || draft.Status != PageDraftStatusApproved {
		return PagePublication{}, ErrPageDraftBlocked
	}
	if err := workflowversion.ValidatePageBindings(draft.Bindings, workflowInputs); err != nil {
		return PagePublication{}, err
	}
	if interrupted {
		return PagePublication{}, errors.New("designeredit: publish interrupted before active pointer commit")
	}
	publication := PagePublication{WorkflowID: strings.TrimSpace(draft.WorkflowID), WorkflowVersion: draft.WorkflowVersion, PageID: strings.TrimSpace(draft.PageID), PageVersion: pageVersion, DefinitionDigest: draft.OverrideDigest, PublishedBy: publisher}
	store.mu.Lock()
	defer store.mu.Unlock()
	target := pageDraftTarget(draft)
	previous, hadPrevious := store.active[target]
	if hadPrevious {
		store.history[target] = append(store.history[target], previous)
	}
	store.active[target] = publication
	draft.Status = PageDraftStatusPublished
	store.drafts[target] = clonePageOverrideDraft(draft)
	return publication, nil
}

func (store *PageDraftStore) Active(draft PageOverrideDraft) (PagePublication, bool) {
	if store == nil {
		return PagePublication{}, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	publication, ok := store.active[pageDraftTarget(draft)]
	return publication, ok
}

func clonePageOverrideDraft(draft PageOverrideDraft) PageOverrideDraft {
	draft.Bindings = append([]workflowversion.PageBinding(nil), draft.Bindings...)
	draft.UnresolvedBindings = append([]string(nil), draft.UnresolvedBindings...)
	return draft
}
