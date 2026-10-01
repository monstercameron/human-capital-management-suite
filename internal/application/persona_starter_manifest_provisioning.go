package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

var (
	errPersonaStarterProvisioningUnavailable = errors.New("application: local persona starter provisioning unavailable")
	ErrPersonaStarterSkillUnavailable        = errors.New("application: starter skill is not active at its pinned digest")
)

// LocalDevPersonaStarterManifestStore is the durable tenant-scoped manifest
// and instruction surface used only by local-development bootstrap.
type LocalDevPersonaStarterManifestStore interface {
	SaveInstructionContent(context.Context, uuid.UUID, string) (string, error)
	SaveManifest(context.Context, uuid.UUID, agentmanifest.Manifest, uint64) (uint64, error)
	CurrentManifest(context.Context, uuid.UUID, string) (agentmanifest.Manifest, uint64, error)
}

// LocalDevPersonaStarterReferenceResolver resolves the exact trusted records
// a starter manifest pins. Implementations must read registered immutable
// policy, schema, and evaluation records rather than synthesize references.
type LocalDevPersonaStarterReferenceResolver interface {
	ResolveModelPolicy(context.Context) (agentmanifest.Reference, error)
	ResolveOutputSchema(context.Context) (agentmanifest.Reference, error)
	ResolveEvaluationSuite(context.Context, string) (agentmanifest.Reference, error)
}

// ProvisionLocalDevPersonaStarterManifests stores the trusted instructions and
// current tenant manifest required by each starter draft. It validates all
// pinned skills before writing anything and refuses to replace a different
// current manifest. Callers must invoke it only for an explicitly selected
// local-development tenant; it grants no skills and creates no review,
// evaluation, publication, or persona lifecycle evidence.
func ProvisionLocalDevPersonaStarterManifests(ctx context.Context, store LocalDevPersonaStarterManifestStore, tenant uuid.UUID, skills agentpersona.SkillResolver, references LocalDevPersonaStarterReferenceResolver) error {
	if ctx == nil || store == nil || tenant == uuid.Nil || skills == nil || references == nil {
		return errPersonaStarterProvisioningUnavailable
	}
	starters := agenttemplate.PersonaStarters()
	manifests := make([]agentmanifest.Manifest, 0, len(starters))
	instructions := make([]string, 0, len(starters))
	for _, starter := range starters {
		for _, pin := range starter.SkillPins {
			record, err := skills.ResolvePin(pin)
			if err != nil || record.Status != agentskills.StatusActive || record.Digest != pin.Digest {
				return fmt.Errorf("%w: %s@%d", ErrPersonaStarterSkillUnavailable, pin.ID, pin.Version)
			}
		}
		refs, err := resolveLocalDevPersonaStarterReferences(ctx, references, starter)
		if err != nil {
			return err
		}
		text := personaStarterInstructions(starter)
		manifest := localDevPersonaStarterManifest(starter, text, refs)
		if err := manifest.Validate(); err != nil {
			return fmt.Errorf("%w: invalid trusted starter manifest %s: %w", errPersonaStarterProvisioningUnavailable, starter.ID, err)
		}
		manifests = append(manifests, manifest)
		instructions = append(instructions, text)
	}
	for i, manifest := range manifests {
		current, _, currentErr := store.CurrentManifest(ctx, tenant, manifest.ID)
		if currentErr != nil && !errors.Is(currentErr, agentstore.ErrNotFound) {
			return fmt.Errorf("%w: read %s manifest: %w", errPersonaStarterProvisioningUnavailable, manifest.ID, currentErr)
		}
		missing := errors.Is(currentErr, agentstore.ErrNotFound)
		if !missing && !samePersonaStarterManifest(current, manifest) {
			return fmt.Errorf("%w: refusing to replace tenant manifest %s", errPersonaStarterProvisioningUnavailable, manifest.ID)
		}
		digest, err := store.SaveInstructionContent(ctx, tenant, instructions[i])
		if err != nil {
			return fmt.Errorf("%w: save %s instructions: %w", errPersonaStarterProvisioningUnavailable, manifest.ID, err)
		}
		if digest != manifest.InstructionsDigest {
			return fmt.Errorf("%w: instruction digest mismatch for %s", errPersonaStarterProvisioningUnavailable, manifest.ID)
		}
		if missing {
			if _, err := store.SaveManifest(ctx, tenant, manifest, 0); err != nil {
				if !errors.Is(err, agentstore.ErrConflict) {
					return fmt.Errorf("%w: save %s manifest: %w", errPersonaStarterProvisioningUnavailable, manifest.ID, err)
				}
				winner, _, readErr := store.CurrentManifest(ctx, tenant, manifest.ID)
				if readErr != nil || !samePersonaStarterManifest(winner, manifest) {
					return fmt.Errorf("%w: concurrent manifest mismatch for %s", errPersonaStarterProvisioningUnavailable, manifest.ID)
				}
			}
		}
	}
	return nil
}

type localDevPersonaStarterReferences struct {
	modelPolicy  agentmanifest.Reference
	outputSchema agentmanifest.Reference
	evaluation   agentmanifest.Reference
}

func resolveLocalDevPersonaStarterReferences(ctx context.Context, resolver LocalDevPersonaStarterReferenceResolver, starter agenttemplate.PersonaStarter) (localDevPersonaStarterReferences, error) {
	modelPolicy, err := resolver.ResolveModelPolicy(ctx)
	if err != nil || !validTrustedPersonaReference(modelPolicy) {
		return localDevPersonaStarterReferences{}, fmt.Errorf("%w: trusted model policy reference unavailable", errPersonaStarterProvisioningUnavailable)
	}
	outputSchema, err := resolver.ResolveOutputSchema(ctx)
	if err != nil || !validTrustedPersonaReference(outputSchema) {
		return localDevPersonaStarterReferences{}, fmt.Errorf("%w: trusted output schema reference unavailable", errPersonaStarterProvisioningUnavailable)
	}
	evaluation, err := resolver.ResolveEvaluationSuite(ctx, starter.EvaluationSuite)
	if err != nil || !validTrustedPersonaReference(evaluation) || evaluation.ID != starter.EvaluationSuite {
		return localDevPersonaStarterReferences{}, fmt.Errorf("%w: trusted evaluation reference unavailable for %s", errPersonaStarterProvisioningUnavailable, starter.ID)
	}
	return localDevPersonaStarterReferences{modelPolicy: modelPolicy, outputSchema: outputSchema, evaluation: evaluation}, nil
}

func validTrustedPersonaReference(ref agentmanifest.Reference) bool {
	if ref.ID == "" || ref.Version == 0 || ref.SchemaVersion == 0 || len(ref.Digest) != len("sha256:")+64 || !strings.HasPrefix(ref.Digest, "sha256:") {
		return false
	}
	return strings.TrimPrefix(ref.Digest, "sha256:") != strings.Repeat("0", 64)
}

func personaInstructionDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func personaStarterInstructions(starter agenttemplate.PersonaStarter) string {
	return strings.TrimSpace(starter.Purpose) + " Follow only the pinned skills and the tenant's current authorization. Cite the records used, refuse actions outside the starter's tier ceiling, and never expose private information to a broader audience."
}

func localDevPersonaStarterManifest(starter agenttemplate.PersonaStarter, instructions string, refs localDevPersonaStarterReferences) agentmanifest.Manifest {
	return agentmanifest.Manifest{
		SchemaVersion:      agentmanifest.CurrentSchemaVersion,
		ID:                 "agent.starter." + strings.TrimPrefix(starter.ID, "hcmnext.persona_template."),
		Version:            1,
		OwnerID:            "platform:local-development-starters",
		Purpose:            starter.Purpose,
		InstructionsDigest: personaInstructionDigest(instructions),
		SourceCeiling:      []agentmanifest.Reference{},
		ToolCeiling:        personaStarterToolCeiling(starter),
		ModelPolicy:        refs.modelPolicy,
		AutonomyCeiling:    "ASSISTED",
		Budget: agentmanifest.Budget{
			MaxCostMicros: 100, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1,
		},
		OutputSchema:   refs.outputSchema,
		ContextGrants:  []agentmanifest.Reference{},
		EvaluationRefs: []agentmanifest.Reference{refs.evaluation},
	}
}

func personaStarterToolCeiling(starter agenttemplate.PersonaStarter) []agentmanifest.Reference {
	tools := make([]agentmanifest.Reference, 0, len(starter.SkillPins))
	for _, pin := range starter.SkillPins {
		tools = append(tools, agentmanifest.Reference{ID: pin.ID, Version: uint64(pin.Version), SchemaVersion: 1, Digest: personaRuntimeToolAdmissionDigest(pin.Digest)})
	}
	slices.SortFunc(tools, func(a, b agentmanifest.Reference) int { return strings.Compare(a.ID, b.ID) })
	return tools
}

func samePersonaStarterManifest(a, b agentmanifest.Manifest) bool {
	aDigest, aErr := a.Digest()
	bDigest, bErr := b.Digest()
	return aErr == nil && bErr == nil && aDigest == bDigest
}
