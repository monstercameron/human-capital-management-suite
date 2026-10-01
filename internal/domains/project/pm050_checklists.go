package project

import (
	"errors"
	"strings"
	"time"
)

const MaxChecklistEntries = 100

type ChecklistParentSemantics string

const ChecklistDoesNotChangeParent ChecklistParentSemantics = "DOES_NOT_CHANGE_PARENT"

var (
	ErrInvalidChecklist  = errors.New("project: invalid checklist")
	ErrChecklistConflict = errors.New("project: checklist revision conflict")
	ErrChecklistFull     = errors.New("project: checklist entry limit reached")
)

type ChecklistCompletion struct {
	EvidenceID  string
	ActorID     string
	Note        string
	CompletedAt time.Time
}

type ChecklistEntry struct {
	ID         string
	Title      string
	OwnerID    string
	Position   int
	Revision   uint64
	Completion *ChecklistCompletion
}

// Checklist is owned by exactly one project task. Completing an entry never
// completes, reopens, or otherwise mutates that parent task.
type Checklist struct {
	TaskID          TaskID
	Revision        uint64
	ParentSemantics ChecklistParentSemantics
	Entries         []ChecklistEntry
}

func NewChecklist(taskID TaskID) (Checklist, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return Checklist{}, ErrInvalidChecklist
	}
	return Checklist{TaskID: taskID, Revision: 1, ParentSemantics: ChecklistDoesNotChangeParent, Entries: []ChecklistEntry{}}, nil
}

func (c Checklist) AddEntry(expected uint64, id, title, ownerID string) (Checklist, error) {
	if err := c.checkRevision(expected); err != nil {
		return Checklist{}, err
	}
	if len(c.Entries) >= MaxChecklistEntries {
		return Checklist{}, ErrChecklistFull
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(title) == "" || strings.TrimSpace(ownerID) == "" || len([]rune(title)) > 200 {
		return Checklist{}, ErrInvalidChecklist
	}
	for _, entry := range c.Entries {
		if entry.ID == id {
			return Checklist{}, ErrInvalidChecklist
		}
	}
	result := c.clone()
	result.Entries = append(result.Entries, ChecklistEntry{ID: id, Title: title, OwnerID: ownerID, Position: len(result.Entries) + 1, Revision: result.Revision + 1})
	result.Revision++
	return result, nil
}

func (c Checklist) CompleteEntry(expected uint64, entryID string, completion ChecklistCompletion) (Checklist, error) {
	if err := c.checkRevision(expected); err != nil {
		return Checklist{}, err
	}
	if strings.TrimSpace(entryID) == "" || strings.TrimSpace(completion.EvidenceID) == "" || strings.TrimSpace(completion.ActorID) == "" || completion.CompletedAt.IsZero() || strings.TrimSpace(completion.Note) == "" {
		return Checklist{}, ErrInvalidChecklist
	}
	result := c.clone()
	found := false
	for i := range result.Entries {
		if result.Entries[i].ID != entryID {
			continue
		}
		if result.Entries[i].Completion != nil {
			return Checklist{}, ErrInvalidChecklist
		}
		value := completion
		result.Entries[i].Completion = &value
		result.Entries[i].Revision = result.Revision + 1
		found = true
	}
	if !found {
		return Checklist{}, ErrInvalidChecklist
	}
	result.Revision++
	return result, nil
}

func (c Checklist) ReorderEntries(expected uint64, orderedIDs []string) (Checklist, error) {
	if err := c.checkRevision(expected); err != nil {
		return Checklist{}, err
	}
	if len(orderedIDs) != len(c.Entries) {
		return Checklist{}, ErrInvalidChecklist
	}
	byID := make(map[string]ChecklistEntry, len(c.Entries))
	for _, entry := range c.Entries {
		byID[entry.ID] = entry
	}
	result := c.clone()
	seen := make(map[string]bool, len(orderedIDs))
	for i, id := range orderedIDs {
		entry, ok := byID[id]
		if !ok || seen[id] {
			return Checklist{}, ErrInvalidChecklist
		}
		seen[id] = true
		entry.Position = i + 1
		entry.Revision = result.Revision + 1
		result.Entries[i] = entry
	}
	result.Revision++
	return result, nil
}

func (c Checklist) checkRevision(expected uint64) error {
	if expected == 0 || c.Revision != expected {
		return ErrChecklistConflict
	}
	return nil
}

func (c Checklist) clone() Checklist {
	result := c
	result.Entries = append([]ChecklistEntry(nil), c.Entries...)
	for i := range result.Entries {
		if c.Entries[i].Completion != nil {
			completion := *c.Entries[i].Completion
			result.Entries[i].Completion = &completion
		}
	}
	return result
}
