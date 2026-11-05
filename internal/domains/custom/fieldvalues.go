// Tenant field values extend plain typed fields with three governed kinds
// (WF-DATA-031): a tenant sequence with a format, a reference to a governed
// reference-data release, and an attachment stored as a document artifact.
// Sequence numbers allocate atomically per tenant, namespace, kind and
// field; reference values validate against the pinned release; attachment
// values pin the scanned derivative of a SAFE artifact under the field's
// declared classification.
package custom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/refdata"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	// ErrInvalidFieldKind reports an unknown field kind or an incoherent
	// kind configuration.
	ErrInvalidFieldKind = errors.New("custom: invalid field kind")
	// ErrUnknownReferenceValue reports a value outside the pinned release.
	ErrUnknownReferenceValue = errors.New("custom: value is not a member of the pinned reference release")
	// ErrReferenceNotGoverned reports a reference pin that does not resolve
	// to a published release.
	ErrReferenceNotGoverned = errors.New("custom: reference pin is not a published release")
	// ErrAttachmentNotSafe reports an attachment that is not a SAFE scanned
	// artifact.
	ErrAttachmentNotSafe = errors.New("custom: attachment is not a safe scanned artifact")
	// ErrSequenceImmutable reports a write changing an allocated sequence value.
	ErrSequenceImmutable = errors.New("custom: sequence value is immutable")
)

// FieldKind names the governed behavior of a field beyond its storage type.
// Values of every kind are canonical text, so kind fields store strings.
type FieldKind string

const (
	// FieldKindPlain is an ordinary typed field.
	FieldKindPlain FieldKind = ""
	// FieldKindSequence draws record numbers from a tenant sequence.
	FieldKindSequence FieldKind = "sequence"
	// FieldKindReference chooses from a governed reference-data release.
	FieldKindReference FieldKind = "reference"
	// FieldKindAttachment pins a scanned document artifact.
	FieldKindAttachment FieldKind = "attachment"
)

// Valid reports whether k is a declared field kind.
func (k FieldKind) Valid() bool {
	switch k {
	case FieldKindPlain, FieldKindSequence, FieldKindReference, FieldKindAttachment:
		return true
	default:
		return false
	}
}

// SequenceConfig declares one tenant sequence. Format renders each number;
// it holds literal text plus exactly one {seq} or {seq:N} counter and any
// number of {tenant} placeholders. StartAt is the first number; zero means
// one.
type SequenceConfig struct {
	Format  string
	StartAt uint64
}

// ReferenceConfig pins the governed release a pick list chooses from.
type ReferenceConfig struct {
	DatasetID     string
	Version       string
	ReleaseDigest string
}

// AttachmentConfig declares which scanned artifacts a field accepts. An
// empty allowlist accepts any detected content type.
type AttachmentConfig struct {
	AllowedContentTypes []string
}

// ValidateFieldKind proves one field's kind configuration is coherent.
func ValidateFieldKind(name string, def FieldDefinition) error {
	if !def.Kind.Valid() {
		return fmt.Errorf("%w: field %q kind %q is not declared", ErrInvalidFieldKind, name, def.Kind)
	}
	if def.Kind == FieldKindPlain {
		if def.Sequence != nil || def.Reference != nil || def.Attachment != nil {
			return fmt.Errorf("%w: plain field %q carries a kind configuration", ErrInvalidFieldKind, name)
		}
		return nil
	}
	if def.Type != "string" {
		return fmt.Errorf("%w: field %q of kind %q stores canonical text, not %q", ErrInvalidFieldKind, name, def.Kind, def.Type)
	}
	switch def.Kind {
	case FieldKindSequence:
		if def.Sequence == nil {
			return fmt.Errorf("%w: sequence field %q names no sequence", ErrInvalidFieldKind, name)
		}
		if def.Reference != nil || def.Attachment != nil {
			return fmt.Errorf("%w: sequence field %q carries another kind", ErrInvalidFieldKind, name)
		}
		return def.Sequence.validate(name)
	case FieldKindReference:
		if def.Reference == nil {
			return fmt.Errorf("%w: reference field %q pins no release", ErrInvalidFieldKind, name)
		}
		if def.Sequence != nil || def.Attachment != nil {
			return fmt.Errorf("%w: reference field %q carries another kind", ErrInvalidFieldKind, name)
		}
		return def.Reference.validate(name)
	case FieldKindAttachment:
		if def.Attachment == nil {
			return fmt.Errorf("%w: attachment field %q declares no artifact policy", ErrInvalidFieldKind, name)
		}
		if def.Sequence != nil || def.Reference != nil {
			return fmt.Errorf("%w: attachment field %q carries another kind", ErrInvalidFieldKind, name)
		}
		if !validLatticeLabel(def.Classification.Classification) {
			return fmt.Errorf("%w: attachment field %q classification %q is not a lattice member",
				ErrInvalidFieldKind, name, def.Classification.Classification)
		}
		return def.Attachment.validate(name)
	default:
		return fmt.Errorf("%w: field %q kind %q is not declared", ErrInvalidFieldKind, name, def.Kind)
	}
}

func validLatticeLabel(label string) bool {
	switch label {
	case "PUBLIC", "INTERNAL", "CONFIDENTIAL", "RESTRICTED":
		return true
	default:
		return false
	}
}

func (c SequenceConfig) validate(field string) error {
	placeholders := 0
	for _, directive := range scanDirectives(c.Format) {
		switch {
		case directive == "seq":
			placeholders++
		case strings.HasPrefix(directive, "seq:"):
			width, err := strconv.Atoi(strings.TrimPrefix(directive, "seq:"))
			if err != nil || width < 1 || width > 20 {
				return fmt.Errorf("%w: sequence field %q counters pad 1 to 20 digits", ErrInvalidFieldKind, field)
			}
			placeholders++
		case directive == "tenant":
		case directive == "unbalanced":
			return fmt.Errorf("%w: sequence field %q format has unbalanced braces", ErrInvalidFieldKind, field)
		default:
			return fmt.Errorf("%w: sequence field %q format names unknown {%s}", ErrInvalidFieldKind, field, directive)
		}
	}
	if placeholders != 1 {
		return fmt.Errorf("%w: sequence field %q format holds exactly one counter", ErrInvalidFieldKind, field)
	}
	return nil
}

// scanDirectives lists the {directives} of a format in order, or
// "unbalanced" when braces do not pair.
func scanDirectives(format string) []string {
	var out []string
	for rest := format; ; {
		open := strings.Index(rest, "{")
		if open < 0 {
			if strings.Contains(rest, "}") {
				return []string{"unbalanced"}
			}
			return out
		}
		after := rest[open+1:]
		close := strings.Index(after, "}")
		if close < 0 {
			return []string{"unbalanced"}
		}
		directive := after[:close]
		if strings.Contains(directive, "{") {
			return []string{"unbalanced"}
		}
		out = append(out, directive)
		rest = after[close+1:]
	}
}

func (c ReferenceConfig) validate(field string) error {
	if strings.TrimSpace(c.DatasetID) == "" || strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("%w: reference field %q pins no dataset version", ErrInvalidFieldKind, field)
	}
	if len(c.ReleaseDigest) != sha256.Size*2 {
		return fmt.Errorf("%w: reference field %q pins no release digest", ErrInvalidFieldKind, field)
	}
	if _, err := hex.DecodeString(c.ReleaseDigest); err != nil {
		return fmt.Errorf("%w: reference field %q pins a malformed release digest", ErrInvalidFieldKind, field)
	}
	return nil
}

func (c AttachmentConfig) validate(field string) error {
	seen := map[string]bool{}
	for _, contentType := range c.AllowedContentTypes {
		normalized := strings.ToLower(strings.TrimSpace(contentType))
		if normalized == "" || seen[normalized] {
			return fmt.Errorf("%w: attachment field %q allows a blank or duplicate content type", ErrInvalidFieldKind, field)
		}
		seen[normalized] = true
	}
	return nil
}

// encodeFieldKind appends a field's kind configuration to a canonical
// definition encoding. Plain fields encode nothing, so definitions without
// governed kinds keep their exact bytes and digest.
func encodeFieldKind(w *canonicalbytes.Writer, name string, def FieldDefinition) {
	if def.Kind != FieldKindPlain {
		w.String(fmt.Sprintf("fields.%s.kind", name), string(def.Kind))
	}
	if def.Sequence != nil {
		w.String(fmt.Sprintf("fields.%s.sequence.format", name), def.Sequence.Format).
			Int(fmt.Sprintf("fields.%s.sequence.start_at", name), int64(def.Sequence.StartAt))
	}
	if def.Reference != nil {
		w.String(fmt.Sprintf("fields.%s.reference.dataset", name), def.Reference.DatasetID).
			String(fmt.Sprintf("fields.%s.reference.version", name), def.Reference.Version).
			String(fmt.Sprintf("fields.%s.reference.digest", name), def.Reference.ReleaseDigest)
	}
	if def.Attachment != nil {
		allowed := append([]string(nil), def.Attachment.AllowedContentTypes...)
		sort.Strings(allowed)
		for i, contentType := range allowed {
			w.String(fmt.Sprintf("fields.%s.attachment.allow.%d", name, i), contentType)
		}
	}
}

// Sequences allocates tenant sequence numbers. Allocation is atomic per
// tenant, namespace, kind and field, so concurrent callers never share a
// value. The allocator is in-memory; a durable allocator persists the same
// counters without changing the format contract.
type Sequences struct {
	mu   sync.Mutex
	next map[sequenceKey]uint64
}

type sequenceKey struct {
	tenant    string
	namespace string
	kind      string
	field     string
}

// NewSequences returns an empty allocator.
func NewSequences() *Sequences { return &Sequences{next: map[sequenceKey]uint64{}} }

// Next allocates and renders the next number of one sequence.
func (s *Sequences) Next(tenant, namespace, kind, field string, cfg SequenceConfig) (string, error) {
	if s == nil {
		return "", fmt.Errorf("%w: no sequence allocator", ErrInvalidFieldKind)
	}
	if err := cfg.validate(field); err != nil {
		return "", err
	}
	key := sequenceKey{tenant: tenant, namespace: namespace, kind: kind, field: field}
	s.mu.Lock()
	number := s.next[key]
	if number == 0 {
		number = cfg.StartAt
		if number == 0 {
			number = 1
		}
	}
	s.next[key] = number + 1
	s.mu.Unlock()
	return renderSequence(cfg.Format, tenant, number), nil
}

// renderSequence applies tenant and counter to a validated format.
func renderSequence(format, tenant string, number uint64) string {
	var out strings.Builder
	for rest := format; ; {
		open := strings.Index(rest, "{")
		if open < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:open])
		after := rest[open+1:]
		close := strings.Index(after, "}")
		directive := after[:close]
		switch {
		case directive == "seq":
			fmt.Fprintf(&out, "%d", number)
		case strings.HasPrefix(directive, "seq:"):
			width, _ := strconv.Atoi(strings.TrimPrefix(directive, "seq:"))
			fmt.Fprintf(&out, "%0*d", width, number)
		case directive == "tenant":
			out.WriteString(tenant)
		}
		rest = after[close+1:]
	}
}

// RefReleaseResolver resolves the governed release a reference pin names.
type RefReleaseResolver func(datasetID, version string) (refdata.Release, error)

// FieldValueDeps resolves governed field values: record-number sequences,
// reference releases and the artifact registry. A nil resolver or registry
// fails writes using that kind; sequences default per registration.
type FieldValueDeps struct {
	Sequences   *Sequences
	RefReleases RefReleaseResolver
	Artifacts   *documentsecurity.Registry
}

// ResolveGovernedFields assigns sequence numbers, proves reference values
// against their pinned releases and pins attachments to scanned
// derivatives. texts carries the kind-field texts under resolution; current
// carries the stored kind-field texts a change revises (nil on create).
// Sequence values are immutable once allocated. Releases resolve once per
// call.
func ResolveGovernedFields(ctx context.Context, def CustomObjectDefinition, tenant string, op Operation, texts, current map[string]string, now time.Time, deps FieldValueDeps) (map[string]string, error) {
	resolved := make(map[string]string, len(texts))
	for name, text := range texts {
		resolved[name] = text
	}
	releases := map[ReferenceConfig]refdata.Release{}
	for name, fieldDef := range def.Fields {
		switch fieldDef.Kind {
		case FieldKindPlain:
		case FieldKindSequence:
			text := strings.TrimSpace(texts[name])
			if op == OperationChange {
				prior, ok := current[name]
				if !ok || text != prior {
					return nil, fmt.Errorf("%w: sequence field %q keeps %q", ErrSequenceImmutable, name, prior)
				}
				resolved[name] = prior
				continue
			}
			if text != "" {
				continue
			}
			number, err := deps.Sequences.Next(tenant, def.Namespace, def.Kind, name, *fieldDef.Sequence)
			if err != nil {
				return nil, err
			}
			resolved[name] = number
		case FieldKindReference:
			if deps.RefReleases == nil {
				return nil, fmt.Errorf("%w: reference fields require a release resolver", ErrInvalidFieldKind)
			}
			text := strings.TrimSpace(texts[name])
			if text == "" {
				return nil, fmt.Errorf("%w: reference field %q names no member", ErrUnknownReferenceValue, name)
			}
			release, ok := releases[*fieldDef.Reference]
			if !ok {
				var err error
				release, err = deps.RefReleases(fieldDef.Reference.DatasetID, fieldDef.Reference.Version)
				if err != nil {
					return nil, fmt.Errorf("%w: field %q: %v", ErrReferenceNotGoverned, name, err)
				}
				releases[*fieldDef.Reference] = release
			}
			if err := ValidateReferenceValue(release, *fieldDef.Reference, text, now); err != nil {
				return nil, fmt.Errorf("field %q: %w", name, err)
			}
			resolved[name] = text
		case FieldKindAttachment:
			text := strings.TrimSpace(texts[name])
			if text == "" {
				return nil, fmt.Errorf("%w: attachment field %q names no artifact", ErrAttachmentNotSafe, name)
			}
			pin, err := ResolveAttachment(ctx, deps.Artifacts, text, *fieldDef.Attachment, fieldDef.Classification.Classification)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", name, err)
			}
			resolved[name] = pin.String()
		default:
			return nil, fmt.Errorf("%w: field %q kind %q is not declared", ErrInvalidFieldKind, name, fieldDef.Kind)
		}
	}
	return resolved, nil
}

// ValidateReferenceValue proves code is currently choosable from release
// under pin: the pin names the release exactly, the release is published,
// and the member exists, is unretired and is valid at the write time.
func ValidateReferenceValue(release refdata.Release, pin ReferenceConfig, code string, at time.Time) error {
	if release.DatasetID != pin.DatasetID || release.Version != pin.Version || release.Digest != pin.ReleaseDigest {
		return fmt.Errorf("%w: pin names %s@%s, release is %s@%s",
			ErrReferenceNotGoverned, pin.DatasetID, pin.Version, release.DatasetID, release.Version)
	}
	if err := release.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrReferenceNotGoverned, err)
	}
	if release.State != refdata.StatePublished {
		return fmt.Errorf("%w: release %s@%s is %s", ErrReferenceNotGoverned, pin.DatasetID, pin.Version, release.State)
	}
	for _, member := range release.Members {
		if member.Code != code {
			continue
		}
		if member.Retired {
			return fmt.Errorf("%w: member %q is retired", ErrUnknownReferenceValue, code)
		}
		if at.Before(member.ValidFrom) || (!member.ValidTo.IsZero() && !at.Before(member.ValidTo)) {
			return fmt.Errorf("%w: member %q is not valid at the write time", ErrUnknownReferenceValue, code)
		}
		return nil
	}
	return fmt.Errorf("%w: member %q", ErrUnknownReferenceValue, code)
}

// AttachmentPin is the canonical value of an attachment field: the artifact
// identity, the scanned derivative it pins and the field classification
// under which it is stored. It renders as artifact@derivative.
type AttachmentPin struct {
	ArtifactID       string
	DerivativeDigest string
	Classification   string
}

// String renders the canonical pinned value.
func (p AttachmentPin) String() string { return p.ArtifactID + "@" + p.DerivativeDigest }

// ParseAttachmentPin parses a canonical pinned value.
func ParseAttachmentPin(text string) (AttachmentPin, error) {
	id, digest, ok := strings.Cut(text, "@")
	if !ok || strings.TrimSpace(id) == "" || !digestString(digest) {
		return AttachmentPin{}, fmt.Errorf("%w: %q is not artifact@derivative", ErrAttachmentNotSafe, text)
	}
	return AttachmentPin{ArtifactID: id, DerivativeDigest: digest}, nil
}

func digestString(s string) bool {
	p := strings.TrimPrefix(s, "sha256:")
	if len(p) != 64 {
		return false
	}
	_, err := hex.DecodeString(p)
	return err == nil
}

// ResolveAttachment proves an uploaded artifact may back an attachment
// field: it exists, its malware verdict is SAFE with a pinned derivative,
// its content type is allowed, and the field classification is a lattice
// member. Resolution never touches raw upload bytes.
func ResolveAttachment(ctx context.Context, registry *documentsecurity.Registry, artifactID string, cfg AttachmentConfig, fieldLabel string) (AttachmentPin, error) {
	if err := ctx.Err(); err != nil {
		return AttachmentPin{}, err
	}
	if registry == nil {
		return AttachmentPin{}, fmt.Errorf("%w: no artifact registry", ErrAttachmentNotSafe)
	}
	if !validLatticeLabel(fieldLabel) {
		return AttachmentPin{}, fmt.Errorf("%w: classification %q is not a lattice member", ErrAttachmentNotSafe, fieldLabel)
	}
	artifact, ok := registry.Get(strings.TrimSpace(artifactID))
	if !ok {
		return AttachmentPin{}, fmt.Errorf("%w: unknown artifact %q", ErrAttachmentNotSafe, artifactID)
	}
	if err := (documentsecurity.Validator{}).Validate(artifact); err != nil {
		return AttachmentPin{}, fmt.Errorf("%w: %v", ErrAttachmentNotSafe, err)
	}
	if len(cfg.AllowedContentTypes) > 0 {
		detected := strings.ToLower(strings.TrimSpace(artifact.DetectedContentType))
		if detected == "" {
			detected = strings.ToLower(strings.TrimSpace(artifact.ContentType))
		}
		allowed := false
		for _, contentType := range cfg.AllowedContentTypes {
			if strings.ToLower(strings.TrimSpace(contentType)) == detected {
				allowed = true
				break
			}
		}
		if !allowed {
			return AttachmentPin{}, fmt.Errorf("%w: content type %q is not allowed", ErrAttachmentNotSafe, detected)
		}
	}
	return AttachmentPin{ArtifactID: artifact.ID, DerivativeDigest: artifact.DerivativeDigest, Classification: fieldLabel}, nil
}
