package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrPersonaCatalogDirectoryUnavailable identifies a missing or malformed
// authoritative member label. A caller must treat it as a denial rather than
// substituting the subject id or enumerating the tenant workforce.
var ErrPersonaCatalogDirectoryUnavailable = errors.New("application: persona catalog directory unavailable")

// PersonaCatalogMember is the narrow core projection needed to label one
// already-authorized chat member. It deliberately contains no role, tenant
// population, or task data.
type PersonaCatalogMember struct {
	TenantID  values.TenantId
	SubjectID string
	Label     string
}

// PersonaCatalogMemberReader resolves one exact member from the authoritative
// core directory. Implementations must scope the lookup to tenant and must
// never implement this contract by listing all tenant members.
type PersonaCatalogMemberReader interface {
	ResolvePersonaCatalogMember(context.Context, values.TenantId, string) (PersonaCatalogMember, error)
}

// CorePersonaCatalogDirectory adapts the exact core member projection to the
// persona catalog target contract. Current membership and preview visibility
// remain owned by ChatDirectoryPersonaCatalogTargets; this adapter only adds
// the current label for the exact member chat disclosed.
type CorePersonaCatalogDirectory struct {
	Core PersonaCatalogMemberReader
}

// NewCorePersonaCatalogDirectory constructs a fail-closed core directory
// adapter. A nil reader is invalid because there is no safe label fallback.
func NewCorePersonaCatalogDirectory(core PersonaCatalogMemberReader) (*CorePersonaCatalogDirectory, error) {
	if core == nil {
		return nil, fmt.Errorf("%w: core member reader is required", ErrPersonaCatalogDirectoryUnavailable)
	}
	return &CorePersonaCatalogDirectory{Core: core}, nil
}

// ResolvePersonaCatalogTarget resolves one exact, tenant-scoped member label.
// It does not authorize a subject, infer roles, or enumerate a directory;
// those decisions are made by the trusted chat and admin authorities.
func (d *CorePersonaCatalogDirectory) ResolvePersonaCatalogTarget(ctx context.Context, tenant values.TenantId, subject string) (productui.PersonaAdminTarget, error) {
	if d == nil || d.Core == nil || ctx == nil || tenant.Validate() != nil || strings.TrimSpace(subject) == "" || strings.TrimSpace(subject) != subject {
		return productui.PersonaAdminTarget{}, ErrPersonaCatalogDirectoryUnavailable
	}
	member, err := d.Core.ResolvePersonaCatalogMember(ctx, tenant, subject)
	if err != nil {
		return productui.PersonaAdminTarget{}, fmt.Errorf("%w: resolve member: %w", ErrPersonaCatalogDirectoryUnavailable, err)
	}
	if member.TenantID != tenant || member.SubjectID != subject || strings.TrimSpace(member.Label) == "" {
		return productui.PersonaAdminTarget{}, ErrPersonaCatalogDirectoryUnavailable
	}
	return productui.PersonaAdminTarget{ID: member.SubjectID, Label: strings.TrimSpace(member.Label)}, nil
}

var _ PersonaCatalogDirectory = (*CorePersonaCatalogDirectory)(nil)
