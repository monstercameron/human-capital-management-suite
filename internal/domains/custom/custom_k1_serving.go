package custom

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the governed custom-object contract composed by
// the shipped application service. It grants no authority and performs no
// durable writes.
const ServingContractID = "hcmnext.conformance.custom-object/v1"

// ValidateServingContract checks the custom-object seams that the application
// composes: typed definitions and policy projection, generated capabilities,
// effective-dated relationships, and the canonical event projection path.
// The check is deterministic and uses only the in-memory reference store.
func ValidateServingContract() error {
	definition := CustomObjectDefinition{
		Kind:      "ServingCustomObject",
		Namespace: "tenant.custom",
		Version:   1,
		Fields: map[string]FieldDefinition{
			"active": {Type: "bool", Classification: FieldClassification{
				AuthZDomain: "custom.serving", Classification: "INTERNAL",
				ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL",
			}},
			"label": {Type: "string", Classification: FieldClassification{
				AuthZDomain: "custom.serving", Classification: "INTERNAL",
				ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL",
			}},
		},
	}
	policy, err := ResolvePolicy(definition)
	if err != nil {
		return fmt.Errorf("custom: serving definition policy: %w", err)
	}
	if !policy.CanAuthorize(map[string]bool{"custom.serving": true}) {
		return fmt.Errorf("custom: serving definition policy did not authorize its declared domain")
	}
	manifest, err := GenerateCapabilities(definition, "custom.serving")
	if err != nil {
		return fmt.Errorf("custom: serving capabilities: %w", err)
	}
	if manifest.Digest == "" || len(manifest.Capabilities) != 5 {
		return fmt.Errorf("custom: serving capability manifest is incomplete")
	}
	if err := CheckClientParity(manifest, manifest); err != nil {
		return fmt.Errorf("custom: serving capability parity: %w", err)
	}

	relationship := CustomRelationshipDefinition{
		Name: "ServingCustomRelation", Namespace: "tenant.custom", Version: 1,
		SourceKind: definition.Kind, TargetKind: definition.Kind,
		Cardinality: CardinalityOneToMany, EffectiveDateRule: "INSTANT_INTERVAL",
	}
	if err := relationship.Validate(); err != nil {
		return fmt.Errorf("custom: serving relationship: %w", err)
	}

	start := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	interval, err := values.NewOpenInstantInterval(start)
	if err != nil {
		return fmt.Errorf("custom: serving effective interval: %w", err)
	}
	store := NewEventStore()
	receipt, err := store.Commit(context.Background(), MutationRequest{
		Tenant: values.TenantId("serving-tenant"), ObjectID: "serving-object",
		Definition: definition,
		Record: CustomRecordRevision{
			ObjectID: "serving-object", ObjectKind: definition.Kind,
			Namespace: definition.Namespace, DefinitionVersion: definition.Version,
			Effective: interval,
			FieldValues: map[string]TypedValue{
				"active": {FieldName: "active", Type: "bool", Value: true},
				"label":  {FieldName: "label", Type: "string", Value: "served"},
			},
		},
		Operation: OperationCreate, Actor: "serving-composer", Purpose: "custom.serving",
		EvidenceDigest: "serving-evidence", GrantedDomains: map[string]bool{"custom.serving": true},
		OccurredAt:  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return fmt.Errorf("custom: serving event commit: %w", err)
	}
	report, err := store.Rebuild(context.Background(), receipt.Event.Tenant, definition.Kind, receipt.Event.ObjectID)
	if err != nil {
		return fmt.Errorf("custom: serving projection rebuild: %w", err)
	}
	if report.Digest != receipt.Event.Digest || len(store.Outbox()) != 1 {
		return fmt.Errorf("custom: serving projection/outbox mismatch")
	}
	return nil
}
