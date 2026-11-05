package productui

import (
	"fmt"
	"reflect"
	"strings"
)

// PageDraft is the mutable working copy in the page lifecycle: bound
// to the base revision it edits (or to nothing for a from-scratch
// page), freely editable by its owner, publishable exactly once its
// lineage is current. Drafts never touch the revision log until
// PublishDraft records them; discarding a draft drops the struct.
type PageDraft struct {
	Page PageID
	// BaseVersion and BaseDigest bind the revision this draft edits.
	// Both zero mark a from-scratch draft for a page carrying no
	// versions yet.
	BaseVersion int64
	BaseDigest  string
	// Snapshot is the working copy. Owners mutate it directly; the
	// log only ever sees it through PublishDraft.
	Snapshot PageDefinitionSnapshot
	// Composition is the draft-scoped composition document the
	// lifecycle validates: floorplan, primitives, and — as later
	// steps extend PageComposition — regions, widgets, and actions.
	Composition PageComposition
}

// NewPageDraft opens a draft on one recorded revision. The working
// copy starts identical to its base.
func NewPageDraft(base PageDefinitionRevision) PageDraft {
	snapshot := base.Snapshot
	// The working copy owns its search terms. The base may alias the
	// revision log stored backing array (Record returns the stored
	// struct), so a shared slice would let draft edits corrupt
	// published history under an unchanged digest.
	snapshot.SearchTerms = append([]string(nil), base.Snapshot.SearchTerms...)
	return PageDraft{Page: snapshot.Page, BaseVersion: base.Version, BaseDigest: base.Digest, Snapshot: snapshot}
}

// NewPageDraftFromScratch opens a draft for a page carrying no
// versions yet. The working copy starts blank.
func NewPageDraftFromScratch(page PageID) PageDraft {
	return PageDraft{Page: page, Snapshot: PageDefinitionSnapshot{Page: page}}
}

// DraftDigest identifies the working-copy state: the snapshot bound
// to its base version. Identical drafts always share a digest.
func DraftDigest(draft PageDraft) string {
	return DigestRevision(draft.Snapshot, draft.BaseVersion)
}

// PublishDraft records one draft as a new revision. Rules, in order:
// identical replays are idempotent; conflicting content at an
// existing version is refused; a draft publishes only onto its base
// while the base is still latest (a stale base refuses, so lineage
// never forks silently); a from-scratch draft publishes only onto a
// page carrying no versions yet.
func PublishDraft(log *PageRevisionLog, draft PageDraft, version int64) (PageDefinitionRevision, error) {
	if version <= 0 {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publish version must be positive, got %d", version)
	}
	if draft.Snapshot.Page != draft.Page {
		return PageDefinitionRevision{}, fmt.Errorf("productui: draft working copy targets page %q, draft binds page %q", draft.Snapshot.Page, draft.Page)
	}
	if existing, ok := log.Revision(draft.Page, version); ok {
		if existing.Digest == DigestRevision(draft.Snapshot, version) {
			return existing, nil
		}
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision conflict for page %q version %d", draft.Page, version)
	}
	if draft.BaseVersion == 0 {
		if _, ok := log.Latest(draft.Page); ok {
			return PageDefinitionRevision{}, fmt.Errorf("productui: from-scratch draft refuses versioned page %q: draft from latest instead", draft.Page)
		}
	} else {
		latest, ok := log.Latest(draft.Page)
		if !ok || latest.Version != draft.BaseVersion || latest.Digest != draft.BaseDigest {
			return PageDefinitionRevision{}, fmt.Errorf("productui: stale base for page %q: draft edits version %d, latest differs", draft.Page, draft.BaseVersion)
		}
	}
	return log.Record(draft.Page, draft.Snapshot, version)
}

// PublicationReview is one human review attestation: the named
// reviewer and their decision. The asset pipeline requires
// review before publication; the attestation is its record.
type PublicationReview struct {
	Reviewer string
	Approved bool
}

// PublicationRequest is one governed publication: the draft and
// version, the contracts it validates against, the preview plan
// with its expanded evidence, and the review attestation.
type PublicationRequest struct {
	Draft        PageDraft
	Version      int64
	Catalog      FloorplanCatalog
	Registry     WidgetRegistry
	Preview      PreviewPlan
	PreviewCases []PreviewCase
	Review       PublicationReview
}

// PublishGoverned publishes one draft through the asset-pipeline
// gates, in order: the composition must validate; the preview
// plan must validate, target the draft page, and expand exactly
// to the carried evidence; a named reviewer must approve; then
// the mechanical publish records. Evidence freshness beyond
// plan-equals-cases — whether the preview ran against this exact
// composition — stays a studio responsibility. First gate to
// fail reports; later gates never run.
func PublishGoverned(log *PageRevisionLog, request PublicationRequest) (PageDefinitionRevision, error) {
	report := ValidateComposition(request.Draft, request.Catalog, request.Registry)
	if !report.Compatible {
		var blocked []string
		for _, finding := range report.Findings {
			if !finding.Compatible {
				blocked = append(blocked, finding.Step+": "+strings.Join(finding.Reasons, "; "))
			}
		}
		return PageDefinitionRevision{}, fmt.Errorf("productui: publication blocked by validation: %s", strings.Join(blocked, "; "))
	}
	preview := ValidatePreviewPlan(request.Preview)
	if !preview.Compatible {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publication preview invalid: %s", strings.Join(preview.Reasons, "; "))
	}
	if request.Preview.Page != request.Draft.Page {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publication preview targets page %q, draft binds page %q", request.Preview.Page, request.Draft.Page)
	}
	expanded, err := ExpandPreviewPlan(request.Preview)
	if err != nil {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publication preview invalid: %s", err.Error())
	}
	if !reflect.DeepEqual(expanded, request.PreviewCases) {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publication preview evidence does not match the plan")
	}
	if !request.Review.Approved {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publication has no approved review")
	}
	if strings.TrimSpace(request.Review.Reviewer) == "" {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publication review names no reviewer")
	}
	return PublishDraft(log, request.Draft, request.Version)
}
