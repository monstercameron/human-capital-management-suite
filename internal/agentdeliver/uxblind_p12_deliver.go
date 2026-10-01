// Package agentdeliver owns the commit-time disclosure boundary for persona
// results. It has no authority of its own: callers provide the signed-in
// invoker, the current audience, and the existing authorization decision.
package agentdeliver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// ErrAudienceChanged means a membership commit raced a public post. The
// caller must use the returned ShareConflictError preview, when present, and
// obtain a fresh confirmation rather than retrying the old post.
var (
	ErrInvalidRequest     = errors.New("agentdeliver: invalid request")
	ErrAudienceChanged    = errors.New("agentdeliver: audience changed before commit")
	ErrUnauthorizedShare  = errors.New("agentdeliver: only the invoker may share")
	ErrConfirmationNeeded = errors.New("agentdeliver: confirmation is required")
	ErrStaleSharePreview  = errors.New("agentdeliver: share preview is stale")
	ErrNothingShareable   = errors.New("agentdeliver: no result items are shareable")
	ErrUnsafeOutput       = errors.New("agentdeliver: output is not safe to render")
	ErrOutputDenied       = errors.New("agentdeliver: invoker cannot read the current output")
)

// EffectTier is the side-effect tier required by a delivery operation.
type EffectTier string

const (
	TierCommunicate EffectTier = "T2"
)

// ConversationKind is deliberately local to the delivery port. The chat
// service maps its conversation kind to this closed set at the composition
// root, avoiding a dependency on a concrete chat store.
type ConversationKind string

const (
	PublicChannel  ConversationKind = "PUBLIC_CHANNEL"
	PrivateChannel ConversationKind = "PRIVATE_CHANNEL"
	Direct         ConversationKind = "DIRECT"
	GroupDM        ConversationKind = "GROUP_DM"
)

// DataClass is the platform's stable disclosure class label. AGENT-026 owns
// the meaning of the label; delivery only intersects it with channel policy.
type DataClass string

// MaterialKind identifies every governed piece of evidence that must be
// reauthorized for every audience member.
type MaterialKind string

const (
	MaterialSource        MaterialKind = "SOURCE"
	MaterialRecord        MaterialKind = "RECORD"
	MaterialField         MaterialKind = "FIELD"
	MaterialCitationTitle MaterialKind = "CITATION_TITLE"
)

// AudienceMember contains only routing identity. Guest and external members
// remain first-class audience terms and are never filtered out by delivery.
type AudienceMember struct {
	TenantID  string
	SubjectID string
	Guest     bool
	External  bool
}

func (m AudienceMember) key() string { return m.TenantID + "\x00" + m.SubjectID }

// ChannelPolicy is the disclosure ceiling for a conversation.
type ChannelPolicy struct {
	AlwaysPrivate  bool
	AllowedClasses []DataClass
}

// Conversation is the routing and output-policy projection needed by this
// package. TenantOrigin and AdminAllowedOrigins are origin-only allowlists;
// they are not URL grants.
type Conversation struct {
	TenantID            string
	ConversationID      string
	Kind                ConversationKind
	Policy              ChannelPolicy
	TenantOrigin        string
	AdminAllowedOrigins []string
}

// AudienceSnapshot is a revisioned read of the current audience. For public
// channels, EligibilityPopulation is unioned with CurrentMembers. For all
// other conversation kinds, only CurrentMembers are used.
type AudienceSnapshot struct {
	Revision              uint64
	CurrentMembers        []AudienceMember
	EligibilityPopulation []AudienceMember
}

// AudienceSource is the shared evaluator seam used by the existing
// AGENT-026 authorization implementation and chat sharing checks.
type AudienceSource interface {
	Snapshot(context.Context, Conversation) (AudienceSnapshot, error)
}

// ReauthorizationRequest is intentionally granular: every source, record,
// field and citation title is checked separately for every audience member.
// The existing AGENT-026 implementation can adapt to this port without
// giving delivery a second authorization implementation.
type ReauthorizationRequest struct {
	Conversation Conversation
	Audience     AudienceMember
	Material     Material
}

// Reauthorizer is the local interface for AGENT-026's current authorization
// decision. An error is a denial or an unavailable authorization decision.
type Reauthorizer interface {
	Authorize(context.Context, ReauthorizationRequest) error
}

// Material is a source/record/field/citation-title disclosure claim. Value is
// never logged or copied into receipts; it exists only so tests and adapters
// can bind the claim to the result item being rendered.
type Material struct {
	Kind      MaterialKind
	ID        string
	DataClass DataClass
	Value     string
}

// Link is a non-embedding citation or reference. Tainted URL construction is
// rejected even when its final origin happens to be allowlisted.
type Link struct {
	URL     string
	Label   string
	Tainted bool
}

// Image and Embed are explicit so the output boundary cannot silently turn
// model-provided markup into a network request in a client.
type Image struct {
	URL      string
	AutoLoad bool
}

type Embed struct {
	URL string
}

// Citation has a visible title plus a link. The title is governed material;
// the URL is governed by the output-origin allowlist.
type Citation struct {
	Title     string
	URL       string
	SourceID  string
	DataClass DataClass
}

// ResultItem is the smallest independently shareable unit. A denied item is
// dropped during a share preview with a user-visible reason.
type ResultItem struct {
	ID        string
	Text      string
	Materials []Material
	Citations []Citation
	Links     []Link
	Images    []Image
	Embeds    []Embed
}

// Result is the typed persona output consumed by delivery. It contains no
// authorization decision from the computation phase; delivery always
// rechecks its own audience.
type Result struct {
	PersonaLabel string
	Items        []ResultItem
}

// RenderedItem is an output-safe projection. Unsafe images and embeds never
// reach a post writer.
type RenderedItem struct {
	ID   string
	Body string
}

// RenderedResult is the post body assembled from safe items.
type RenderedResult struct {
	Items []RenderedItem
	Body  string
}

// PublicPost and PrivatePost are effect requests. Implementations must bind
// PublicPost.ExpectedAudienceRevision atomically to membership state before
// committing the post.
type PublicPost struct {
	Conversation             Conversation
	ParentPostID             string
	AuthorID                 string
	Tier                     EffectTier
	Body                     string
	ExpectedAudienceRevision uint64
	Attribution              string
}

type PrivatePost struct {
	Conversation Conversation
	ParentPostID string
	Recipient    AudienceMember
	PersonaLabel string
	Body         string
	Ephemeral    bool
}

type PublicPoster interface {
	CommitPublic(context.Context, PublicPost) error
}

type PrivatePoster interface {
	DeliverPrivate(context.Context, PrivatePost) error
}

// Service is the delivery gate. All effects happen through the two writer
// ports after the audience and output checks have passed.
type Service struct {
	Audience  AudienceSource
	Authorize Reauthorizer
	Public    PublicPoster
	Private   PrivatePoster
}

// DeliveryRequest binds one result to the original invocation.
type DeliveryRequest struct {
	Conversation Conversation
	ParentPostID string
	Invoker      AudienceMember
	Result       Result
}

type DeliveryMode string

const (
	DeliveryPublic  DeliveryMode = "PUBLIC_THREAD"
	DeliveryPrivate DeliveryMode = "PRIVATE_INVOKER"
)

// DeliveryReceipt is safe to expose to a channel. It contains no result
// values, source identifiers, or citation titles.
type DeliveryReceipt struct {
	Mode             DeliveryMode
	PublicPosted     bool
	PrivatePosted    bool
	NeutralReceipt   string
	AudienceRevision uint64
}

// Deliver performs the commit-time audience-floor check. If any audience
// member fails any material or class check, the full result is sent privately
// and only a neutral receipt is posted publicly.
func (s *Service) Deliver(ctx context.Context, req DeliveryRequest) (DeliveryReceipt, error) {
	if err := validateDeliveryRequest(req); err != nil {
		return DeliveryReceipt{}, err
	}
	if s == nil || s.Audience == nil || s.Authorize == nil || s.Public == nil || s.Private == nil {
		return DeliveryReceipt{}, ErrInvalidRequest
	}
	rendered, err := renderResult(req.Conversation, req.Result)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	if err := s.authorizeInvoker(ctx, req); err != nil {
		return DeliveryReceipt{}, err
	}
	snapshot, err := s.Audience.Snapshot(ctx, req.Conversation)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	if err := validateAudienceSnapshot(req.Conversation, snapshot); err != nil {
		return DeliveryReceipt{}, err
	}
	if requiresPrivate(req.Conversation) {
		return s.deliverPrivate(ctx, req, rendered, snapshot.Revision)
	}
	allowed, err := s.publicAllowed(ctx, req.Conversation, req.Result, snapshot)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	if !allowed {
		return s.deliverPrivateWithReceipt(ctx, req, rendered, snapshot.Revision)
	}
	err = s.Public.CommitPublic(ctx, PublicPost{
		Conversation: req.Conversation, ParentPostID: req.ParentPostID,
		AuthorID: req.Result.PersonaLabel, Tier: TierCommunicate, Body: rendered.Body,
		ExpectedAudienceRevision: snapshot.Revision,
		Attribution:              "acting for @" + req.Invoker.SubjectID,
	})
	if errors.Is(err, ErrAudienceChanged) {
		fresh, freshErr := s.Audience.Snapshot(ctx, req.Conversation)
		if freshErr != nil {
			return DeliveryReceipt{}, freshErr
		}
		if err := validateAudienceSnapshot(req.Conversation, fresh); err != nil {
			return DeliveryReceipt{}, err
		}
		return s.deliverPrivateWithReceipt(ctx, req, rendered, fresh.Revision)
	}
	if err != nil {
		return DeliveryReceipt{}, err
	}
	return DeliveryReceipt{Mode: DeliveryPublic, PublicPosted: true, AudienceRevision: snapshot.Revision}, nil
}

func (s *Service) deliverPrivate(ctx context.Context, req DeliveryRequest, rendered RenderedResult, revision uint64) (DeliveryReceipt, error) {
	if err := s.authorizeInvoker(ctx, req); err != nil {
		return DeliveryReceipt{}, err
	}
	if err := s.Private.DeliverPrivate(ctx, PrivatePost{
		Conversation: req.Conversation, ParentPostID: req.ParentPostID,
		Recipient: req.Invoker, PersonaLabel: req.Result.PersonaLabel,
		Body: rendered.Body, Ephemeral: true,
	}); err != nil {
		return DeliveryReceipt{}, err
	}
	return DeliveryReceipt{Mode: DeliveryPrivate, PrivatePosted: true, AudienceRevision: revision}, nil
}

func (s *Service) deliverPrivateWithReceipt(ctx context.Context, req DeliveryRequest, rendered RenderedResult, revision uint64) (DeliveryReceipt, error) {
	if err := s.authorizeInvoker(ctx, req); err != nil {
		return DeliveryReceipt{}, err
	}
	if err := s.deliverNeutralReceipt(ctx, req, revision); err != nil {
		return DeliveryReceipt{}, err
	}
	receipt, err := s.deliverPrivate(ctx, req, rendered, revision)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	receipt.NeutralReceipt = neutralReceipt(req.Invoker, req.Result.PersonaLabel)
	return receipt, nil
}

func (s *Service) authorizeInvoker(ctx context.Context, req DeliveryRequest) error {
	for _, item := range req.Result.Items {
		for _, material := range itemMaterials(item) {
			if !validMaterial(material) || s.Authorize.Authorize(ctx, ReauthorizationRequest{Conversation: req.Conversation, Audience: req.Invoker, Material: material}) != nil {
				return ErrOutputDenied
			}
		}
	}
	return nil
}

func (s *Service) deliverNeutralReceipt(ctx context.Context, req DeliveryRequest, revision uint64) error {
	err := s.Public.CommitPublic(ctx, PublicPost{
		Conversation: req.Conversation, ParentPostID: req.ParentPostID,
		AuthorID: req.Result.PersonaLabel, Tier: TierCommunicate,
		Body: neutralReceipt(req.Invoker, req.Result.PersonaLabel),
		// A neutral receipt is itself safe only after the same membership fence.
		ExpectedAudienceRevision: revision,
		Attribution:              "acting for @" + req.Invoker.SubjectID,
	})
	if !errors.Is(err, ErrAudienceChanged) {
		return err
	}
	// Re-read once after a membership race. The receipt contains no result
	// material, but it still must be committed against the latest audience.
	fresh, snapshotErr := s.Audience.Snapshot(ctx, req.Conversation)
	if snapshotErr != nil {
		return snapshotErr
	}
	if err := validateAudienceSnapshot(req.Conversation, fresh); err != nil {
		return err
	}
	return s.Public.CommitPublic(ctx, PublicPost{
		Conversation: req.Conversation, ParentPostID: req.ParentPostID,
		AuthorID: req.Result.PersonaLabel, Tier: TierCommunicate,
		Body:                     neutralReceipt(req.Invoker, req.Result.PersonaLabel),
		ExpectedAudienceRevision: fresh.Revision,
		Attribution:              "acting for @" + req.Invoker.SubjectID,
	})
}

func neutralReceipt(invoker AudienceMember, persona string) string {
	return fmt.Sprintf("@%s asked %s; the answer was sent privately", invoker.SubjectID, persona)
}

func (s *Service) publicAllowed(ctx context.Context, conversation Conversation, result Result, snapshot AudienceSnapshot) (bool, error) {
	audience := audienceFor(conversation, snapshot)
	if len(audience) == 0 {
		return false, nil
	}
	allowed := true
	for _, item := range result.Items {
		if !classesAllowed(conversation.Policy, itemMaterials(item)) {
			allowed = false
		}
		for _, member := range audience {
			for _, material := range itemMaterials(item) {
				if err := s.Authorize.Authorize(ctx, ReauthorizationRequest{Conversation: conversation, Audience: member, Material: material}); err != nil {
					allowed = false
				}
			}
		}
	}
	return allowed, nil
}

func requiresPrivate(c Conversation) bool {
	return c.Policy.AlwaysPrivate || c.Kind == Direct
}

func audienceFor(c Conversation, snapshot AudienceSnapshot) []AudienceMember {
	all := append([]AudienceMember(nil), snapshot.CurrentMembers...)
	if c.Kind == PublicChannel {
		all = append(all, snapshot.EligibilityPopulation...)
	}
	seen := make(map[string]AudienceMember, len(all))
	for _, member := range all {
		if member.TenantID == "" || member.SubjectID == "" {
			continue
		}
		seen[member.key()] = member
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]AudienceMember, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

func itemMaterials(item ResultItem) []Material {
	materials := append([]Material(nil), item.Materials...)
	for _, citation := range item.Citations {
		if citation.SourceID != "" {
			materials = append(materials, Material{Kind: MaterialSource, ID: citation.SourceID, DataClass: citation.DataClass})
		}
		if citation.Title != "" {
			id := citation.SourceID
			if id == "" {
				id = citation.Title
			}
			materials = append(materials, Material{Kind: MaterialCitationTitle, ID: id, DataClass: citation.DataClass, Value: citation.Title})
		}
	}
	return materials
}

func classesAllowed(policy ChannelPolicy, materials []Material) bool {
	if len(materials) == 0 {
		return true
	}
	allowed := make(map[DataClass]struct{}, len(policy.AllowedClasses))
	for _, class := range policy.AllowedClasses {
		allowed[class] = struct{}{}
	}
	for _, material := range materials {
		if !validMaterial(material) {
			return false
		}
		if _, ok := allowed[material.DataClass]; !ok {
			return false
		}
	}
	return true
}

func validMaterial(material Material) bool {
	if material.ID == "" || material.DataClass == "" {
		return false
	}
	switch material.Kind {
	case MaterialSource, MaterialRecord, MaterialField, MaterialCitationTitle:
		return true
	default:
		return false
	}
}

// EphemeralResult is the invoker-bound handle exposed by the private result
// UI. A caller cannot turn another member's ephemeral result into a T2 post.
type EphemeralResult struct {
	ID           string
	Conversation Conversation
	ParentPostID string
	Invoker      AudienceMember
	Result       Result
}

type ShareRequest struct {
	Actor     AudienceMember
	Ephemeral EphemeralResult
}

type ShareItemDecision struct {
	ItemID   string
	Included bool
	Reason   string
}

// SharePreview is bound to the audience revision and an opaque digest. The
// digest binds item ids and material structure, not their values.
type SharePreview struct {
	Token            string
	AudienceRevision uint64
	Items            []ShareItemDecision
	DroppedCount     int
	Rendered         RenderedResult
}

type ShareOutcome struct {
	Posted       bool
	DroppedCount int
	Preview      *SharePreview
}

// ShareConflictError carries a fresh preview when membership changed between
// preview and confirmation. It deliberately does not post anything.
type ShareConflictError struct{ Preview SharePreview }

func (e *ShareConflictError) Error() string { return ErrStaleSharePreview.Error() }
func (e *ShareConflictError) Unwrap() error { return ErrStaleSharePreview }

// PreviewShare performs the first, current-audience check and returns every
// dropped item with a reason.
func (s *Service) PreviewShare(ctx context.Context, req ShareRequest) (SharePreview, error) {
	if err := validateShareRequest(req); err != nil {
		return SharePreview{}, err
	}
	if s == nil || s.Audience == nil || s.Authorize == nil || s.Public == nil {
		return SharePreview{}, ErrInvalidRequest
	}
	snapshot, err := s.Audience.Snapshot(ctx, req.Ephemeral.Conversation)
	if err != nil {
		return SharePreview{}, err
	}
	if err := validateAudienceSnapshot(req.Ephemeral.Conversation, snapshot); err != nil {
		return SharePreview{}, err
	}
	return s.makeSharePreview(ctx, req.Ephemeral, snapshot)
}

type ConfirmShareRequest struct {
	Actor     AudienceMember
	Preview   SharePreview
	Confirmed bool
}

// ConfirmShare re-runs the audience floor, compares the revision-bound
// preview, and only then commits a T2 post authored by the invoker.
func (s *Service) ConfirmShare(ctx context.Context, req ShareRequest, confirm ConfirmShareRequest) (ShareOutcome, error) {
	if err := validateShareRequest(req); err != nil {
		return ShareOutcome{}, err
	}
	if s == nil || s.Audience == nil || s.Authorize == nil || s.Public == nil {
		return ShareOutcome{}, ErrInvalidRequest
	}
	if confirm.Actor.key() != req.Ephemeral.Invoker.key() {
		return ShareOutcome{}, ErrUnauthorizedShare
	}
	if !confirm.Confirmed {
		return ShareOutcome{}, ErrConfirmationNeeded
	}
	snapshot, err := s.Audience.Snapshot(ctx, req.Ephemeral.Conversation)
	if err != nil {
		return ShareOutcome{}, err
	}
	if err := validateAudienceSnapshot(req.Ephemeral.Conversation, snapshot); err != nil {
		return ShareOutcome{}, err
	}
	fresh, err := s.makeSharePreview(ctx, req.Ephemeral, snapshot)
	if err != nil {
		return ShareOutcome{}, err
	}
	if fresh.Token != confirm.Preview.Token || fresh.AudienceRevision != confirm.Preview.AudienceRevision {
		return ShareOutcome{Preview: &fresh, DroppedCount: fresh.DroppedCount}, &ShareConflictError{Preview: fresh}
	}
	if fresh.DroppedCount == len(req.Ephemeral.Result.Items) {
		return ShareOutcome{Preview: &fresh, DroppedCount: fresh.DroppedCount}, ErrNothingShareable
	}
	err = s.Public.CommitPublic(ctx, PublicPost{
		Conversation:             req.Ephemeral.Conversation,
		ParentPostID:             req.Ephemeral.ParentPostID,
		AuthorID:                 req.Ephemeral.Invoker.SubjectID,
		Tier:                     TierCommunicate,
		Body:                     fresh.Rendered.Body,
		ExpectedAudienceRevision: fresh.AudienceRevision,
		Attribution:              "shared from " + req.Ephemeral.Result.PersonaLabel,
	})
	if errors.Is(err, ErrAudienceChanged) {
		newSnapshot, snapshotErr := s.Audience.Snapshot(ctx, req.Ephemeral.Conversation)
		if snapshotErr != nil {
			return ShareOutcome{}, snapshotErr
		}
		if err := validateAudienceSnapshot(req.Ephemeral.Conversation, newSnapshot); err != nil {
			return ShareOutcome{}, err
		}
		newPreview, previewErr := s.makeSharePreview(ctx, req.Ephemeral, newSnapshot)
		if previewErr != nil {
			return ShareOutcome{}, previewErr
		}
		return ShareOutcome{Preview: &newPreview, DroppedCount: newPreview.DroppedCount}, &ShareConflictError{Preview: newPreview}
	}
	if err != nil {
		return ShareOutcome{}, err
	}
	return ShareOutcome{Posted: true, DroppedCount: fresh.DroppedCount, Preview: &fresh}, nil
}

func (s *Service) makeSharePreview(ctx context.Context, ephemeral EphemeralResult, snapshot AudienceSnapshot) (SharePreview, error) {
	if requiresPrivate(ephemeral.Conversation) {
		return SharePreview{}, ErrNothingShareable
	}
	audience := audienceFor(ephemeral.Conversation, snapshot)
	if len(audience) == 0 {
		return SharePreview{}, ErrNothingShareable
	}
	decisions := make([]ShareItemDecision, 0, len(ephemeral.Result.Items))
	shareable := make([]ResultItem, 0, len(ephemeral.Result.Items))
	for _, item := range ephemeral.Result.Items {
		reason := ""
		if err := validateRenderedItem(ephemeral.Conversation, item); err != nil {
			reason = "unsafe output"
		} else if !classesAllowed(ephemeral.Conversation.Policy, itemMaterials(item)) {
			reason = "channel policy does not allow every data class"
		} else {
			for _, member := range audience {
				for _, material := range itemMaterials(item) {
					if authErr := s.Authorize.Authorize(ctx, ReauthorizationRequest{Conversation: ephemeral.Conversation, Audience: member, Material: material}); authErr != nil {
						reason = "an audience member cannot read every cited item"
						break
					}
				}
				if reason != "" {
					break
				}
			}
		}
		if reason == "" {
			shareable = append(shareable, item)
		}
		decisions = append(decisions, ShareItemDecision{ItemID: item.ID, Included: reason == "", Reason: reason})
	}
	rendered, err := renderResultForItems(ephemeral.Conversation, shareable)
	if err != nil {
		return SharePreview{}, err
	}
	dropped := len(ephemeral.Result.Items) - len(shareable)
	return SharePreview{Token: shareToken(ephemeral, snapshot.Revision, decisions), AudienceRevision: snapshot.Revision, Items: decisions, DroppedCount: dropped, Rendered: rendered}, nil
}

func renderResult(conversation Conversation, result Result) (RenderedResult, error) {
	return renderResultForItems(conversation, result.Items)
}

// Render applies the same output boundary used by delivery and sharing. It
// is exported for chat and task surfaces that need to render a private result
// without accidentally creating a second, weaker renderer.
func Render(conversation Conversation, result Result) (RenderedResult, error) {
	return renderResult(conversation, result)
}

func renderResultForItems(conversation Conversation, items []ResultItem) (RenderedResult, error) {
	rendered := RenderedResult{Items: make([]RenderedItem, 0, len(items))}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if err := validateRenderedItem(conversation, item); err != nil {
			return RenderedResult{}, err
		}
		body := strings.TrimSpace(item.Text)
		for _, citation := range item.Citations {
			if citation.Title != "" {
				body += "\nSource: " + citation.Title
			}
			if citation.URL != "" {
				body += " (" + citation.URL + ")"
			}
		}
		for _, link := range item.Links {
			label := link.Label
			if label == "" {
				label = link.URL
			}
			body += "\n" + label + ": " + link.URL
		}
		rendered.Items = append(rendered.Items, RenderedItem{ID: item.ID, Body: body})
		parts = append(parts, body)
	}
	rendered.Body = strings.Join(parts, "\n\n")
	return rendered, nil
}

func validateRenderedItem(conversation Conversation, item ResultItem) error {
	for _, image := range item.Images {
		if image.AutoLoad || image.URL != "" {
			return fmt.Errorf("%w: images are not rendered in persona output", ErrUnsafeOutput)
		}
	}
	if len(item.Embeds) > 0 {
		return fmt.Errorf("%w: remote embeds are not rendered in persona output", ErrUnsafeOutput)
	}
	for _, citation := range item.Citations {
		if citation.URL != "" {
			if err := validateLink(conversation, Link{URL: citation.URL, Label: citation.Title}); err != nil {
				return err
			}
		}
	}
	for _, link := range item.Links {
		if err := validateLink(conversation, link); err != nil {
			return err
		}
	}
	return nil
}

func validateLink(conversation Conversation, link Link) error {
	if link.Tainted {
		return fmt.Errorf("%w: tainted URL construction is refused", ErrUnsafeOutput)
	}
	parsed, err := url.Parse(link.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("%w: link must be an absolute HTTP(S) URL", ErrUnsafeOutput)
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if sameOrigin(origin, conversation.TenantOrigin) {
		return nil
	}
	for _, allowed := range conversation.AdminAllowedOrigins {
		if sameOrigin(origin, allowed) {
			return nil
		}
	}
	return fmt.Errorf("%w: link origin is not allowlisted", ErrUnsafeOutput)
}

func sameOrigin(left, right string) bool {
	leftURL, leftErr := url.Parse(strings.TrimRight(strings.TrimSpace(left), "/"))
	rightURL, rightErr := url.Parse(strings.TrimRight(strings.TrimSpace(right), "/"))
	if leftErr != nil || rightErr != nil || leftURL.Scheme == "" || rightURL.Scheme == "" || leftURL.Host == "" || rightURL.Host == "" {
		return false
	}
	return strings.EqualFold(leftURL.Scheme, rightURL.Scheme) && strings.EqualFold(leftURL.Host, rightURL.Host)
}

func shareToken(ephemeral EphemeralResult, revision uint64, decisions []ShareItemDecision) string {
	material, _ := json.Marshal(struct {
		Conversation Conversation
		ParentPostID string
		Result       Result
	}{ephemeral.Conversation, ephemeral.ParentPostID, ephemeral.Result})
	materialSum := sha256.Sum256(material)
	text := ephemeral.ID + "\x00" + ephemeral.Conversation.TenantID + "\x00" + ephemeral.Conversation.ConversationID + "\x00" + ephemeral.Invoker.key() + "\x00" + fmt.Sprint(revision) + "\x00" + hex.EncodeToString(materialSum[:])
	for _, decision := range decisions {
		text += "\x00" + decision.ItemID + "\x00" + fmt.Sprint(decision.Included) + "\x00" + decision.Reason
	}
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validateDeliveryRequest(req DeliveryRequest) error {
	if req.Conversation.TenantID == "" || req.Conversation.ConversationID == "" || req.ParentPostID == "" || req.Invoker.TenantID == "" || req.Invoker.SubjectID == "" || req.Result.PersonaLabel == "" || len(req.Result.Items) == 0 || req.Conversation.Kind == "" {
		return ErrInvalidRequest
	}
	if req.Conversation.TenantID != req.Invoker.TenantID {
		return ErrInvalidRequest
	}
	if !validConversationKind(req.Conversation.Kind) {
		return ErrInvalidRequest
	}
	return nil
}

func validateShareRequest(req ShareRequest) error {
	if req.Ephemeral.ID == "" || req.Ephemeral.Conversation.TenantID == "" || req.Ephemeral.Conversation.ConversationID == "" || req.Ephemeral.ParentPostID == "" || req.Ephemeral.Invoker.TenantID == "" || req.Ephemeral.Invoker.SubjectID == "" || req.Ephemeral.Result.PersonaLabel == "" || len(req.Ephemeral.Result.Items) == 0 || req.Ephemeral.Conversation.Kind == "" {
		return ErrInvalidRequest
	}
	if req.Ephemeral.Invoker.TenantID != req.Ephemeral.Conversation.TenantID || req.Actor.key() != req.Ephemeral.Invoker.key() || req.Actor.TenantID == "" || req.Actor.SubjectID == "" {
		return ErrUnauthorizedShare
	}
	if !validConversationKind(req.Ephemeral.Conversation.Kind) {
		return ErrInvalidRequest
	}
	return nil
}

func validConversationKind(kind ConversationKind) bool {
	return kind == PublicChannel || kind == PrivateChannel || kind == Direct || kind == GroupDM
}

func validateAudienceSnapshot(conversation Conversation, snapshot AudienceSnapshot) error {
	if snapshot.Revision == 0 {
		return ErrInvalidRequest
	}
	members := append([]AudienceMember(nil), snapshot.CurrentMembers...)
	if conversation.Kind == PublicChannel {
		members = append(members, snapshot.EligibilityPopulation...)
	}
	for _, member := range members {
		if member.TenantID != conversation.TenantID || strings.TrimSpace(member.SubjectID) == "" {
			return ErrInvalidRequest
		}
	}
	return nil
}
