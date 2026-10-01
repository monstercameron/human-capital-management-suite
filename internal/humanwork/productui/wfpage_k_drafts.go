package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const WorkflowPageDraftTTL = 30 * 24 * time.Hour

var ErrWorkflowPageDraftInvalid = errors.New("productui: invalid workflow page draft")

type WorkflowPageDraftKey struct {
	TenantID   string
	UserID     string
	WorkflowID string
	SubjectID  string
}

func (key WorkflowPageDraftKey) valid() bool {
	return strings.TrimSpace(key.TenantID) != "" && strings.TrimSpace(key.UserID) != "" &&
		strings.TrimSpace(key.WorkflowID) != "" && strings.TrimSpace(key.SubjectID) != ""
}

type WorkflowPageDraft struct {
	Key         WorkflowPageDraftKey
	PageID      string
	PageVersion int64
	Values      map[string]string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   time.Time
}

func (draft WorkflowPageDraft) Digest() string {
	encoded, err := json.Marshal(struct {
		PageID      string            `json:"page_id"`
		PageVersion int64             `json:"page_version"`
		Values      map[string]string `json:"values"`
	}{draft.PageID, draft.PageVersion, draft.Values})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneWorkflowPageDraft(draft WorkflowPageDraft) WorkflowPageDraft {
	draft.Values = copyStringMap(draft.Values)
	return draft
}

type WorkflowPageDraftResume struct {
	Draft  WorkflowPageDraft
	Found  bool
	Stale  bool
	Status DraftAutosaveStatus
}

// WorkflowPageDraftStore is a tenant/user/workflow/subject keyed server-side
// draft store. The value owns its lock and clock so tests and deployments can
// choose a deterministic clock without a package-level mutable registry.
type WorkflowPageDraftStore struct {
	mu      sync.Mutex
	now     func() time.Time
	drafts  map[WorkflowPageDraftKey]WorkflowPageDraft
	SaveErr error
}

func NewWorkflowPageDraftStore(now func() time.Time) *WorkflowPageDraftStore {
	if now == nil {
		now = time.Now
	}
	return &WorkflowPageDraftStore{now: now, drafts: make(map[WorkflowPageDraftKey]WorkflowPageDraft)}
}

func (store *WorkflowPageDraftStore) Save(draft WorkflowPageDraft) (WorkflowPageDraft, error) {
	if store == nil || !draft.Key.valid() || strings.TrimSpace(draft.PageID) == "" || draft.PageVersion < 1 {
		return WorkflowPageDraft{}, ErrWorkflowPageDraftInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.SaveErr != nil {
		return WorkflowPageDraft{}, store.SaveErr
	}
	now := store.now().UTC()
	if existing, ok := store.drafts[draft.Key]; ok && now.Before(existing.ExpiresAt) {
		draft.CreatedAt = existing.CreatedAt
	} else {
		draft.CreatedAt = now
	}
	draft.UpdatedAt = now
	draft.ExpiresAt = now.Add(WorkflowPageDraftTTL)
	stored := cloneWorkflowPageDraft(draft)
	store.drafts[draft.Key] = stored
	return cloneWorkflowPageDraft(stored), nil
}

func (store *WorkflowPageDraftStore) Resume(key WorkflowPageDraftKey, currentPageVersion int64, locale LocaleContext, localValues map[string]string) WorkflowPageDraftResume {
	result := WorkflowPageDraftResume{Status: DraftAutosaveUnsavedStatus(locale)}
	if store == nil || !key.valid() {
		return result
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now().UTC()
	draft, ok := store.drafts[key]
	if !ok || !now.Before(draft.ExpiresAt) {
		if ok {
			delete(store.drafts, key)
		}
		return result
	}
	result.Draft, result.Found = cloneWorkflowPageDraft(draft), true
	result.Stale = currentPageVersion > 0 && draft.PageVersion != currentPageVersion
	status := DraftAutosaveStatus{State: DraftAutosaveSaved, Text: locale.Text("draft.autosave_saved"), Detail: locale.Text("draft.autosave_last_saved", map[string]string{"time": formatAutosaveTime(locale, draft.UpdatedAt)})}
	if localValues != nil && workflowPageValuesDigest(draft.PageID, draft.PageVersion, localValues) != draft.Digest() {
		status = DraftAutosaveStatus{State: DraftAutosaveUnsaved, Text: locale.Text("draft.autosave_unsaved")}
	}
	result.Status = status
	return result
}

func DraftAutosaveUnsavedStatus(locale LocaleContext) DraftAutosaveStatus {
	return DraftAutosaveStatus{State: DraftAutosaveUnsaved, Text: locale.Text("draft.autosave_unsaved")}
}

func workflowPageValuesDigest(pageID string, pageVersion int64, values map[string]string) string {
	return (WorkflowPageDraft{PageID: pageID, PageVersion: pageVersion, Values: values}).Digest()
}

// SaveWorkflowPageDraft keeps the local copy available when the durable save
// fails. Callers can render LocalValues immediately and retry without losing
// the user's work.
func SaveWorkflowPageDraft(store *WorkflowPageDraftStore, draft WorkflowPageDraft, localValues map[string]string, locale LocaleContext) (WorkflowPageDraft, DraftAutosaveStatus, error) {
	saved, err := store.Save(draft)
	if err != nil {
		return WorkflowPageDraft{Key: draft.Key, PageID: draft.PageID, PageVersion: draft.PageVersion, Values: copyStringMap(localValues)}, DraftAutosaveStatus{State: DraftAutosaveFailed, Text: locale.Text("draft.autosave_failed"), Detail: err.Error()}, err
	}
	return saved, DraftAutosaveStatus{State: DraftAutosaveSaved, Text: locale.Text("draft.autosave_saved"), Detail: locale.Text("draft.autosave_last_saved", map[string]string{"time": formatAutosaveTime(locale, saved.UpdatedAt)})}, nil
}
