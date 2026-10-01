package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaPublicPostClassificationWriter interface {
	PutPublicChatPostClassification(context.Context, string, string, string, string, dlp.DataClass) error
}

// LocalDevPersonaSyntheticPostSource verifies exact checked-in fictional post
// bytes and identity; a matching demo tenant alone cannot attest a seed post.
type LocalDevPersonaSyntheticPostSource interface {
	IsKnownSyntheticPersonaPost(context.Context, chat.Post) bool
}

// LocalDevPersonaPostClassifier is explicitly restricted to the fictional
// local development tenants. Production requires its own reviewed classifier.
// Sensitive findings are preserved as restricted classes, never downgraded.
type LocalDevPersonaPostClassifier struct {
	store     personaPublicPostClassificationWriter
	inspector *dlp.Inspector
	enabled   bool
}

func NewLocalDevPersonaPostClassifier(store *chatstore.Store, localDev bool) (*LocalDevPersonaPostClassifier, error) {
	inspector, err := dlp.NewInspector(
		dlp.Detector{ID: "local-demo-compensation", Class: dlp.ClassCompensation, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:salary|compensation|base\s+pay|bonus|hourly\s+rate|annual\s+pay|payroll)\b|(?:\$|USD\s+)\s*\d`},
		dlp.Detector{ID: "local-demo-medical", Class: dlp.ClassMedical, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:medical|patient|diagnosis|diagnosed|diagnoses|treatment|prescription|medication|hospitalized|PHI|HIV|cancer|pregnan(?:t|cy))\b`},
		dlp.Detector{ID: "local-demo-bank", Class: dlp.ClassBank, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:bank\s+account|routing\s+number|IBAN|SWIFT|credit\s+card)\b`},
		dlp.Detector{ID: "local-demo-identity", Class: dlp.ClassPII, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:SSN|social\s+security|date\s+of\s+birth|home\s+address)\b|\b\d{3}-\d{2}-\d{4}\b|[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}|\b\d{3}[-. ]\d{3}[-. ]\d{4}\b`},
		dlp.Detector{ID: "local-demo-immigration", Class: dlp.ClassImmigration, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:immigration|visa|passport|work\s+permit)\b`},
		dlp.Detector{ID: "local-demo-case", Class: dlp.ClassCase, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:disciplinary|grievance|investigation|harassment|misconduct)\b`},
		dlp.Detector{ID: "local-demo-special", Class: dlp.ClassSpecialCategory, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:religion|religious|sexual\s+orientation|ethnicity|disability|union\s+membership|biometric|genetic)\b`},
	)
	if err != nil {
		return nil, err
	}
	return &LocalDevPersonaPostClassifier{store: store, inspector: inspector, enabled: localDev}, nil
}

// ClassifyAcceptedHumanPost runs only after the authenticated write succeeds,
// before audience capture and persona handoff. The writer binds exact bytes.
func (c *LocalDevPersonaPostClassifier) ClassifyAcceptedHumanPost(ctx context.Context, post chat.Post) error {
	if !c.validPost(post) || ctx == nil {
		return ErrPersonaAudienceFloorUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != post.TenantID || principal.Subject() != post.AuthorID || post.AuthorHomeTenantID != post.TenantID || !time.Now().UTC().Before(principal.ExpiresAt()) {
		return ErrPersonaAudienceFloorUnavailable
	}
	return c.classify(ctx, post)
}

// ClassifyKnownSyntheticPost requires the seed owner's exact corpus verifier.
func (c *LocalDevPersonaPostClassifier) ClassifyKnownSyntheticPost(ctx context.Context, post chat.Post, source LocalDevPersonaSyntheticPostSource) error {
	if !c.validPost(post) || ctx == nil || isNilPersonaOutputPort(source) || !source.IsKnownSyntheticPersonaPost(ctx, post) {
		return ErrPersonaAudienceFloorUnavailable
	}
	return c.classify(ctx, post)
}

// ClassifyPersonaReplyText independently inspects the exact rendered reply.
// Source annotations and room ceilings cannot classify newly generated text.
// This local inspector is restricted to authenticated fictional demo tenants.
func (c *LocalDevPersonaPostClassifier) ClassifyPersonaReplyText(ctx context.Context, tenantID, text string) (dlp.DataClass, error) {
	if c == nil || !c.enabled || c.inspector == nil || ctx == nil || strings.TrimSpace(text) == "" {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	pack, known := demoworkforce.PackFor(tenantID)
	if !known || pack.Key != tenantID {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != tenantID || !time.Now().UTC().Before(principal.ExpiresAt()) {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	return c.inspectText(text)
}

func (c *LocalDevPersonaPostClassifier) validPost(post chat.Post) bool {
	if c == nil || !c.enabled || isNilPersonaOutputPort(c.store) || c.inspector == nil || post.ID == "" || post.ConversationID == "" || post.Body == "" || post.Deleted {
		return false
	}
	pack, known := demoworkforce.PackFor(post.TenantID)
	return known && pack.Key == post.TenantID
}

func (c *LocalDevPersonaPostClassifier) classify(ctx context.Context, post chat.Post) error {
	class, err := c.inspectText(post.Body)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(post.Body))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	return c.store.PutPublicChatPostClassification(ctx, post.TenantID, post.ConversationID, post.ID, digest, class)
}

func (c *LocalDevPersonaPostClassifier) inspectText(text string) (dlp.DataClass, error) {
	inspection, err := c.inspector.Inspect([]byte(text))
	if err != nil {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	class := dlp.ClassInternal
	for _, finding := range inspection.Findings {
		if class == dlp.ClassInternal {
			class = finding.Class
		} else if class != finding.Class {
			class = dlp.ClassSpecialCategory
		}
	}
	return class, nil
}
