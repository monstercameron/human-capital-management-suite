package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrPersonaAudienceDirectoryUnavailable identifies an incomplete or
// untrusted directory projection. Callers must fail closed when it is
// returned; the error is deliberately shared with no chat membership state.
var ErrPersonaAudienceDirectoryUnavailable = errors.New("persona audience directory: unavailable")

// PersonaAudienceDirectoryDB adapts the authoritative agent directory to the
// persona audience source. Every resolution reads roles, named populations,
// and organization scope afresh from the member's home tenant.
type PersonaAudienceDirectoryDB struct {
	Directory        *AgentDirectoryDB
	HomeOrganization PersonaHomeOrganizationDirectory
}

// PersonaHomeOrganizationDirectory resolves the current authoritative home
// organization for a member. Implementations must return an unavailable
// error when the fact is absent or ambiguous.
type PersonaHomeOrganizationDirectory interface {
	CurrentHomeOrganization(context.Context, values.TenantId, string) (string, error)
}

// PersonaAudienceDirectoryOption configures a persona audience directory.
type PersonaAudienceDirectoryOption func(*PersonaAudienceDirectoryDB)

// WithPersonaHomeOrganizationDirectory binds an explicit home-organization
// source, which is useful for composition roots and isolated tests.
func WithPersonaHomeOrganizationDirectory(source PersonaHomeOrganizationDirectory) PersonaAudienceDirectoryOption {
	return func(directory *PersonaAudienceDirectoryDB) {
		directory.HomeOrganization = source
	}
}

// NewPersonaAudienceDirectoryDB constructs a production persona audience
// directory over the authoritative agent directory.
func NewPersonaAudienceDirectoryDB(directory *AgentDirectoryDB, options ...PersonaAudienceDirectoryOption) *PersonaAudienceDirectoryDB {
	result := &PersonaAudienceDirectoryDB{Directory: directory}
	if directory != nil {
		result.HomeOrganization = NewAgentHomeOrganizationDirectoryDB(directory.DB, directory.TenantUUID)
	}
	for _, option := range options {
		if option != nil {
			option(result)
		}
	}
	return result
}

var _ PersonaAudienceDirectory = (*PersonaAudienceDirectoryDB)(nil)

// ResolvePersonaAudienceMember returns exact current audience facts for a
// member. It requires an authenticated human in context, but permits a
// member's home tenant to differ from the invoker's tenant: chat owns that
// membership relationship and this adapter only reads the member's home
// directory.
func (d *PersonaAudienceDirectoryDB) ResolvePersonaAudienceMember(ctx context.Context, homeTenant, subject string) (PersonaAudienceMember, error) {
	if d == nil || d.Directory == nil || ctx == nil {
		return PersonaAudienceMember{}, ErrPersonaAudienceDirectoryUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return PersonaAudienceMember{}, fmt.Errorf("%w: human principal required", ErrPersonaAudienceDirectoryUnavailable)
	}
	homeTenantID := values.TenantId(strings.TrimSpace(homeTenant))
	subject = strings.TrimSpace(subject)
	if homeTenantID.Validate() != nil || subject == "" {
		return PersonaAudienceMember{}, fmt.Errorf("%w: home tenant and subject are required", ErrPersonaAudienceDirectoryUnavailable)
	}

	roles, err := d.Directory.CurrentRoles(ctx, homeTenantID, subject)
	if err != nil {
		return PersonaAudienceMember{}, fmt.Errorf("%w: current roles: %w", ErrPersonaAudienceDirectoryUnavailable, err)
	}
	roles, err = strictFacts(roles, "roles")
	if err != nil {
		return PersonaAudienceMember{}, err
	}
	populations, err := d.Directory.CurrentPopulations(ctx, homeTenantID, subject)
	if err != nil {
		return PersonaAudienceMember{}, fmt.Errorf("%w: current populations: %w", ErrPersonaAudienceDirectoryUnavailable, err)
	}
	populations, err = strictFacts(populations, "populations")
	if err != nil {
		return PersonaAudienceMember{}, err
	}
	if d.HomeOrganization == nil {
		return PersonaAudienceMember{}, fmt.Errorf("%w: home organization source is unavailable", ErrPersonaAudienceDirectoryUnavailable)
	}
	organization, err := d.HomeOrganization.CurrentHomeOrganization(ctx, homeTenantID, subject)
	if err != nil {
		return PersonaAudienceMember{}, fmt.Errorf("%w: current home organization: %w", ErrPersonaAudienceDirectoryUnavailable, err)
	}
	organization = strings.TrimSpace(organization)
	if organization == "" {
		return PersonaAudienceMember{}, fmt.Errorf("%w: current home organization is empty", ErrPersonaAudienceDirectoryUnavailable)
	}

	return PersonaAudienceMember{SubjectID: subject, Roles: roles, Populations: populations, OrganizationScope: organization}, nil
}

func strictFacts(facts []string, name string) ([]string, error) {
	if len(facts) == 0 {
		return nil, fmt.Errorf("%w: %s are absent", ErrPersonaAudienceDirectoryUnavailable, name)
	}
	seen := make(map[string]struct{}, len(facts))
	result := make([]string, 0, len(facts))
	for _, fact := range facts {
		fact = strings.TrimSpace(fact)
		if fact == "" {
			return nil, fmt.Errorf("%w: %s contain an empty fact", ErrPersonaAudienceDirectoryUnavailable, name)
		}
		if _, exists := seen[fact]; exists {
			return nil, fmt.Errorf("%w: %s contain duplicate authority", ErrPersonaAudienceDirectoryUnavailable, name)
		}
		seen[fact] = struct{}{}
		result = append(result, fact)
	}
	return result, nil
}
