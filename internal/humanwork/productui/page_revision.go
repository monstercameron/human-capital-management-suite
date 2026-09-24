package productui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pageledger"
)

// PageDefinitionSnapshot is the render-free, storable projection of one
// product surface: every PageDefinition field except the render func,
// which cannot be compared, digested, or persisted. Snapshots are the
// unit the revision log versions.
type PageDefinitionSnapshot struct {
	Page        PageID   `json:"page"`
	Route       string   `json:"route"`
	Label       string   `json:"label"`
	Icon        string   `json:"icon"`
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	LabelKey    string   `json:"label_key"`
	TitleKey    string   `json:"title_key"`
	SubtitleKey string   `json:"subtitle_key"`
	SearchTerms []string `json:"search_terms"`
	PrimaryNav  bool     `json:"primary_nav"`
	ParentNav   PageID   `json:"parent_nav"`
	RenderOrder int      `json:"render_order"`
}

// SnapshotPageDefinition projects one registry definition into its
// snapshot. Search terms are deep-copied so later registry or caller
// mutation cannot reach recorded revisions.
func SnapshotPageDefinition(definition PageDefinition) PageDefinitionSnapshot {
	return PageDefinitionSnapshot{
		Page: definition.ID, Route: definition.Route, Label: definition.Label, Icon: definition.Icon,
		Title: definition.Title, Subtitle: definition.Subtitle,
		LabelKey: definition.LabelKey, TitleKey: definition.TitleKey, SubtitleKey: definition.SubtitleKey,
		SearchTerms: append([]string(nil), definition.SearchTerms...),
		PrimaryNav:  definition.PrimaryNav, ParentNav: definition.ParentNav, RenderOrder: definition.RenderOrder,
	}
}

// revisionCanonical is the digest-bound encoding: snapshot fields plus
// the bound version, in declaration order.
type revisionCanonical struct {
	Page        PageID   `json:"page"`
	Route       string   `json:"route"`
	Label       string   `json:"label"`
	Icon        string   `json:"icon"`
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	LabelKey    string   `json:"label_key"`
	TitleKey    string   `json:"title_key"`
	SubtitleKey string   `json:"subtitle_key"`
	SearchTerms []string `json:"search_terms"`
	PrimaryNav  bool     `json:"primary_nav"`
	ParentNav   PageID   `json:"parent_nav"`
	RenderOrder int      `json:"render_order"`
	Version     int64    `json:"version"`
}

// CanonicalRevisionBytes encodes one snapshot plus its bound version.
// encoding/json marshals structs deterministically in field order, so
// identical snapshots always produce identical bytes.
func CanonicalRevisionBytes(snapshot PageDefinitionSnapshot, version int64) []byte {
	encoded, err := json.Marshal(revisionCanonical{
		Page: snapshot.Page, Route: snapshot.Route, Label: snapshot.Label, Icon: snapshot.Icon,
		Title: snapshot.Title, Subtitle: snapshot.Subtitle,
		LabelKey: snapshot.LabelKey, TitleKey: snapshot.TitleKey, SubtitleKey: snapshot.SubtitleKey,
		SearchTerms: snapshot.SearchTerms,
		PrimaryNav:  snapshot.PrimaryNav, ParentNav: snapshot.ParentNav, RenderOrder: snapshot.RenderOrder,
		Version: version,
	})
	if err != nil {
		return nil
	}
	return encoded
}

// DigestRevision digests one snapshot bound to its version.
func DigestRevision(snapshot PageDefinitionSnapshot, version int64) string {
	digest := sha256.Sum256(CanonicalRevisionBytes(snapshot, version))
	return hex.EncodeToString(digest[:])
}

// PageDefinitionRevision is one immutable published version of a page:
// the snapshot, the bound version, and the content digest addressing
// it. Values are deep copies; mutating a retrieved revision never
// reaches the log.
type PageDefinitionRevision struct {
	Snapshot PageDefinitionSnapshot
	Version  int64
	Digest   string
}

// persistedRevision is the stored form: canonical content plus digest.
type persistedRevision struct {
	Snapshot PageDefinitionSnapshot `json:"snapshot"`
	Version  int64                  `json:"version"`
	Digest   string                 `json:"digest"`
}

// MarshalRevision encodes one revision into persistable bytes.
func MarshalRevision(revision PageDefinitionRevision) []byte {
	encoded, err := json.Marshal(persistedRevision(revision))
	if err != nil {
		return nil
	}
	return encoded
}

// ParseRevision decodes persisted bytes and re-verifies the digest.
// Tampered, truncated, or foreign bytes fail closed.
func ParseRevision(encoded []byte) (PageDefinitionRevision, error) {
	var stored persistedRevision
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision bytes do not parse: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return PageDefinitionRevision{}, fmt.Errorf("productui: trailing revision bytes")
	}
	if stored.Digest == "" || stored.Digest != DigestRevision(stored.Snapshot, stored.Version) {
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision digest mismatch for page %q version %d", stored.Snapshot.Page, stored.Version)
	}
	stored.Snapshot.SearchTerms = append([]string(nil), stored.Snapshot.SearchTerms...)
	return PageDefinitionRevision(stored), nil
}

// PageRevisionLog is the append-only ledger of published page revisions,
// keyed by tenant, page and version, addressed by digest. The zero value is
// an in-memory log; NewDurablePageRevisionLog binds a persistent tenant store.
type PageRevisionLog struct {
	revisions   map[PageID]map[int64]PageDefinitionRevision
	rollouts    []PageRollout
	retirements map[PageID]PageRetirement
	tenant      string
	store       pageledger.Store
}

// NewDurablePageRevisionLog recovers one tenant's append-only revision and
// rollout ledger. The returned log is tenant-bound for its lifetime.
func NewDurablePageRevisionLog(ctx context.Context, tenant string, store pageledger.Store) (*PageRevisionLog, error) {
	if tenant == "" || store == nil {
		return nil, fmt.Errorf("productui: durable page log requires tenant and store")
	}
	log := &PageRevisionLog{tenant: tenant}
	revisions, err := store.LoadRevisions(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("productui: load page revisions: %w", err)
	}
	for _, row := range revisions {
		revision, err := ParseRevision(row.Payload)
		if err != nil {
			return nil, err
		}
		if revision.Snapshot.Page == "" || revision.Version <= 0 || row.Page != string(revision.Snapshot.Page) || row.Version != revision.Version || row.Digest != revision.Digest {
			return nil, fmt.Errorf("productui: invalid stored revision key")
		}
		if _, err := log.Record(revision.Snapshot.Page, revision.Snapshot, revision.Version); err != nil {
			return nil, err
		}
	}
	rollouts, err := store.LoadRollouts(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("productui: load page rollouts: %w", err)
	}
	seenRollouts := make(map[string]struct{}, len(rollouts))
	for _, row := range rollouts {
		rollout, err := ParsePageRollout(row.Payload)
		if err != nil {
			return nil, err
		}
		// 00338 backfills 00332 rollout bytes unchanged. Their database row
		// sequence is the trusted append order because the original payload
		// predates the record_version field.
		if rollout.RecordVersion == 0 {
			rollout.RecordVersion = row.RecordVersion
		}
		if err := VerifyRolloutTarget(log, rollout); err != nil {
			return nil, err
		}
		if verdict := ValidatePageRollout(rollout); !verdict.Compatible {
			return nil, fmt.Errorf("productui: invalid stored rollout: %v", verdict.Reasons)
		}
		if row.Page != string(rollout.Page) || row.RecordVersion != rollout.RecordVersion || row.TargetVersion != rollout.Version || row.Digest != rollout.Digest || rollout.RecordVersion <= 0 {
			return nil, fmt.Errorf("productui: invalid stored rollout key")
		}
		key := fmt.Sprintf("%s\x00%d", rollout.Page, rollout.RecordVersion)
		if _, exists := seenRollouts[key]; exists {
			return nil, fmt.Errorf("productui: duplicate stored rollout for page %q version %d", rollout.Page, rollout.Version)
		}
		seenRollouts[key] = struct{}{}
		log.rollouts = append(log.rollouts, clonePageRollout(rollout))
	}
	if err := loadRetirements(ctx, log, store, tenant); err != nil {
		return nil, err
	}
	log.store = store
	return log, nil
}

// Record persists one snapshot at one version. Re-recording the
// identical snapshot is idempotent; re-recording different content at
// an existing version is a conflict and refused — published history
// is immutable. Non-positive versions are refused.
func (log *PageRevisionLog) Record(page PageID, snapshot PageDefinitionSnapshot, version int64) (PageDefinitionRevision, error) {
	return log.RecordContext(context.Background(), page, snapshot, version)
}

// RecordContext records one immutable revision and persists it first when the
// log was constructed with NewDurablePageRevisionLog.
func (log *PageRevisionLog) RecordContext(ctx context.Context, page PageID, snapshot PageDefinitionSnapshot, version int64) (PageDefinitionRevision, error) {
	if page == "" || snapshot.Page != page {
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision page key %q does not match snapshot page %q", page, snapshot.Page)
	}
	if version <= 0 {
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision version must be positive, got %d", version)
	}
	snapshot.SearchTerms = append([]string(nil), snapshot.SearchTerms...)
	revision := PageDefinitionRevision{Snapshot: snapshot, Version: version, Digest: DigestRevision(snapshot, version)}
	if log.revisions == nil {
		log.revisions = make(map[PageID]map[int64]PageDefinitionRevision)
	}
	versions := log.revisions[page]
	if versions == nil {
		versions = make(map[int64]PageDefinitionRevision)
		log.revisions[page] = versions
	}
	if existing, ok := versions[version]; ok {
		if existing.Digest != revision.Digest {
			return PageDefinitionRevision{}, fmt.Errorf("productui: revision conflict for page %q version %d", page, version)
		}
		return existing, nil
	}
	if log.store != nil {
		if err := log.store.PutRevision(ctx, log.tenant, string(page), version, revision.Digest, MarshalRevision(revision)); err != nil {
			return PageDefinitionRevision{}, fmt.Errorf("productui: persist revision: %w", err)
		}
	}
	versions[version] = revision
	return revision, nil
}

// Revision returns one recorded revision. Returned snapshots are deep
// copies.
func (log *PageRevisionLog) Revision(page PageID, version int64) (PageDefinitionRevision, bool) {
	revision, ok := log.revisions[page][version]
	if !ok {
		return PageDefinitionRevision{}, false
	}
	revision.Snapshot.SearchTerms = append([]string(nil), revision.Snapshot.SearchTerms...)
	return revision, true
}

// Latest returns the maximum recorded version of one page.
func (log *PageRevisionLog) Latest(page PageID) (PageDefinitionRevision, bool) {
	versions := log.revisions[page]
	var latest PageDefinitionRevision
	found := false
	for version, revision := range versions {
		if !found || version > latest.Version {
			latest, found = revision, true
		}
	}
	if !found {
		return PageDefinitionRevision{}, false
	}
	latest.Snapshot.SearchTerms = append([]string(nil), latest.Snapshot.SearchTerms...)
	return latest, true
}

// Pages inventories every page carrying at least one revision, sorted.
func (log *PageRevisionLog) Pages() []PageID {
	pages := make([]PageID, 0, len(log.revisions))
	for page, versions := range log.revisions {
		if len(versions) > 0 {
			pages = append(pages, page)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i] < pages[j] })
	return pages
}
