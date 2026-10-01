package documents

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

type LinkMode string

const (
	LinkLatest LinkMode = "latest"
	LinkPinned LinkMode = "pinned"
)

var workflowDocumentID = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$")

var (
	ErrInvalidDocumentReference = errors.New("documents: invalid workflow document reference")
	ErrPinnedVersionUnavailable = errors.New("documents: pinned version is not reviewed")
)

// WorkflowDocumentLinkSlot is the persisted, URL-free page binding.
type WorkflowDocumentLinkSlot struct {
	ID, SectionID string
	DocumentRef   string
	Mode          LinkMode
}

type DocumentReference struct {
	DocumentID, VersionID, Block string
}

// ParseWorkflowDocumentReference accepts the Documents hub's doc: reference
// and the page-definition document: alias. Both forms are identifiers.
func ParseWorkflowDocumentReference(value string) (DocumentReference, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "document:") {
		value = "doc:" + value[len("document:"):]
	}
	if !strings.HasPrefix(value, "doc:") {
		return DocumentReference{}, ErrInvalidDocumentReference
	}
	body := strings.TrimPrefix(value, "doc:")
	if body == "" || strings.ContainsAny(body, "/\\?%<>\r\n") {
		return DocumentReference{}, ErrInvalidDocumentReference
	}
	base, block, _ := strings.Cut(body, "#")
	documentID, versionID, hasVersion := strings.Cut(base, "@")
	if !workflowDocumentID.MatchString(documentID) || (block != "" && !workflowDocumentID.MatchString(block)) {
		return DocumentReference{}, ErrInvalidDocumentReference
	}
	if hasVersion && !workflowDocumentID.MatchString(versionID) {
		return DocumentReference{}, ErrInvalidDocumentReference
	}
	return DocumentReference{DocumentID: documentID, VersionID: versionID, Block: block}, nil
}

type WorkflowDocumentVersion struct {
	ID       string
	Reviewed bool
	Deployed bool
	Status   string
}

// WorkflowDocument is an authorized candidate returned by the Documents
// service. Readable is evaluated for the current caller before page rendering.
type WorkflowDocument struct {
	ID, Title string
	Readable  bool
	Versions  []WorkflowDocumentVersion
}

// WorkflowDocumentLink is the viewer-safe result. When Readable is false,
// DocumentID, VersionID, Title and Href are deliberately empty.
type WorkflowDocumentLink struct {
	SlotID, SectionID string
	DocumentID        string
	VersionID         string
	Title             string
	Href              string
	Readable          bool
	RequestAccess     bool
	Stale             bool
}

// ResolveWorkflowDocumentLink chooses the deployed latest version by default,
// or a reviewed pinned version. Unreadable targets never disclose existence.
func ResolveWorkflowDocumentLink(slot WorkflowDocumentLinkSlot, catalog []WorkflowDocument) (WorkflowDocumentLink, error) {
	if strings.TrimSpace(slot.ID) == "" || strings.TrimSpace(slot.SectionID) == "" || (slot.Mode != LinkLatest && slot.Mode != LinkPinned) {
		return WorkflowDocumentLink{}, ErrInvalidDocumentReference
	}
	ref, err := ParseWorkflowDocumentReference(slot.DocumentRef)
	if err != nil {
		return WorkflowDocumentLink{}, err
	}
	result := WorkflowDocumentLink{SlotID: slot.ID, SectionID: slot.SectionID}
	var document WorkflowDocument
	found := false
	for _, candidate := range catalog {
		if candidate.ID == ref.DocumentID {
			document, found = candidate, true
			break
		}
	}
	if !found || !document.Readable {
		result.RequestAccess = true
		return result, nil
	}
	result.Readable, result.DocumentID, result.Title = true, document.ID, document.Title
	version := WorkflowDocumentVersion{}
	if slot.Mode == LinkPinned {
		version.ID = ref.VersionID
		for _, candidate := range document.Versions {
			if candidate.ID == ref.VersionID {
				version = candidate
				break
			}
		}
		if version.ID == "" || !version.Reviewed {
			return result, ErrPinnedVersionUnavailable
		}
	} else {
		for _, candidate := range document.Versions {
			if candidate.Deployed {
				version = candidate
				break
			}
		}
		if version.ID == "" {
			result.Stale = true
			return result, nil
		}
	}
	result.VersionID = version.ID
	result.Stale = strings.EqualFold(version.Status, "retired") || strings.EqualFold(version.Status, "withdrawn") || (!version.Deployed && slot.Mode == LinkLatest)
	result.Href = WorkflowDocumentHref(result.DocumentID, result.VersionID, ref.Block)
	return result, nil
}

func WorkflowDocumentHref(documentID, versionID, block string) string {
	if !workflowDocumentID.MatchString(documentID) || (versionID != "" && !workflowDocumentID.MatchString(versionID)) || (block != "" && !workflowDocumentID.MatchString(block)) {
		return ""
	}
	query := url.Values{"document": []string{documentID}}
	if versionID != "" {
		query.Set("version", versionID)
	}
	href := "/workspace/app/docs?" + query.Encode()
	if block != "" {
		href += "#" + url.PathEscape(block)
	}
	return href
}

// WorkflowDocumentPicker returns only records the designer may read.
func WorkflowDocumentPicker(catalog []WorkflowDocument) []WorkflowDocument {
	seen := make(map[string]bool, len(catalog))
	result := make([]WorkflowDocument, 0, len(catalog))
	for _, document := range catalog {
		if !document.Readable || !workflowDocumentID.MatchString(document.ID) || seen[document.ID] {
			continue
		}
		seen[document.ID] = true
		result = append(result, WorkflowDocument{ID: document.ID, Title: document.Title, Readable: true, Versions: append([]WorkflowDocumentVersion(nil), document.Versions...)})
	}
	return result
}

// WorkflowBacklink records a page referrer in the Documents backlink index.
// It contains no submitted field values.
type WorkflowBacklink struct {
	TargetDocumentID, WorkflowID, PageID, SectionID, SlotID string
	PageVersion                                             int64
}

// WorkflowBacklinkIndex is an in-memory composition seam for the durable
// Documents backlink adapter.
type WorkflowBacklinkIndex struct {
	rows []WorkflowBacklink
}

func (index *WorkflowBacklinkIndex) Record(link WorkflowBacklink) {
	if index == nil || !workflowDocumentID.MatchString(link.TargetDocumentID) || strings.TrimSpace(link.WorkflowID) == "" || strings.TrimSpace(link.PageID) == "" || link.PageVersion < 1 {
		return
	}
	for _, row := range index.rows {
		if row == link {
			return
		}
	}
	index.rows = append(index.rows, link)
}

func (index WorkflowBacklinkIndex) ForDocument(documentID string) []WorkflowBacklink {
	result := make([]WorkflowBacklink, 0)
	for _, row := range index.rows {
		if row.TargetDocumentID == documentID {
			result = append(result, row)
		}
	}
	return result
}
