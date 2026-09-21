// Package designerpalette projects the workflow kernel and extension
// registries into the bounded, tenant-filtered catalog used by authors.
package designerpalette

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Kind distinguishes a single insertable node from a reusable fragment or a
// full workflow template. The designer renders all three through one catalog.
type Kind string

const (
	KindBlock    Kind = "BLOCK"
	KindFragment Kind = "FRAGMENT"
	KindTemplate Kind = "TEMPLATE"
)

// Entry is one immutable palette item. RequiredCapabilities is server-side
// filter metadata and is never accepted from the browser.
type Entry struct {
	ID                   string
	Version              uint32
	Name                 string
	Kind                 Kind
	Domain               string
	Description          string
	EffectClass          capability.EffectClass
	Reversal             string
	Status               string
	StepType             workflow.StepType
	RequiredCapabilities []capability.Key
	// PublishedPlanDigest optionally binds a source template to the exact
	// immutable compiled plan it produced. It remains server-only metadata and
	// lets successor authoring hydrate the source definition without trusting
	// a workflow ID or display name as proof of parity.
	PublishedPlanDigest string
	// Expansion is the server-owned artifact inserted when an author selects
	// this entry. It is deliberately omitted from the transport projection:
	// callers name an admitted entry and the server resolves its exact,
	// versioned expansion instead of accepting workflow bytes from the
	// browser.
	Expansion Expansion
}

// Expansion carries either one full template definition or the nodes and
// internal edges of a reusable fragment. Kernel and capability block entries
// leave it empty because the designer can derive their single node from the
// entry's declared StepType and effect class.
type Expansion struct {
	Template             *workflow.Definition
	Nodes                []workflow.Node
	Edges                []workflow.Edge
	ApprovalRequirements []workflow.ApprovalRequirement
	Obligations          []workflow.ObligationRequirement
}

// CapabilitySource is the immutable global registry projection. Tenant
// authorization is always applied separately through Policy.
type CapabilitySource interface{ List() []capability.Record }

// Catalog combines closed-kernel blocks with open-registry entries.
type Catalog struct {
	Capabilities CapabilitySource
	Policy       draftcompile.CapabilityPolicy
	Extensions   []Entry
}

// List returns only entries usable by tenant. A missing policy hides every
// capability and every extension that depends on one; kernel blocks remain
// visible because they execute no tenant-specific capability.
func (c Catalog) List(ctx context.Context, tenant values.TenantId) []Entry {
	ctx, op := observe.Begin(ctx, "workflow.designer.palette", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.Done(op, nil) }()
	entries := kernelEntries()
	for _, record := range c.CapabilitiesList() {
		key := record.Definition.Key()
		if record.Status == capability.StatusRetired || c.Policy == nil || !c.Policy.AllowsCapability(ctx, tenant, key) {
			continue
		}
		entries = append(entries, capabilityEntry(record))
	}
	for _, extension := range c.Extensions {
		if validExtension(extension) && c.allowsAll(ctx, tenant, extension.RequiredCapabilities) {
			entries = append(entries, cloneEntry(extension))
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.Domain != right.Domain {
			return left.Domain < right.Domain
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.Version < right.Version
	})
	return entries
}

func (c Catalog) CapabilitiesList() []capability.Record {
	if c.Capabilities == nil {
		return nil
	}
	return c.Capabilities.List()
}

func (c Catalog) allowsAll(ctx context.Context, tenant values.TenantId, keys []capability.Key) bool {
	if len(keys) == 0 {
		return true
	}
	if c.Policy == nil {
		return false
	}
	for _, key := range keys {
		if key.ID == "" || key.Version == 0 || !c.Policy.AllowsCapability(ctx, tenant, key) {
			return false
		}
	}
	return true
}

func kernelEntries() []Entry {
	types := []workflow.StepType{
		workflow.StepApproval, workflow.StepTask, workflow.StepWait,
		workflow.StepSignal, workflow.StepDecision, workflow.StepTransform,
		workflow.StepObserve, workflow.StepParallel, workflow.StepJoin,
		workflow.StepSubworkflow, workflow.StepEnd,
	}
	entries := make([]Entry, 0, len(types))
	for _, stepType := range types {
		entries = append(entries, Entry{
			ID: "kernel." + strings.ToLower(string(stepType)), Version: 1,
			Name: humanize(string(stepType)), Kind: KindBlock, Domain: "Control flow",
			Description: "Add a " + strings.ToLower(humanize(string(stepType))) + " step.",
			EffectClass: capability.EffectPure, Reversal: "NO_EFFECT", Status: "ACTIVE", StepType: stepType,
		})
	}
	return entries
}

func capabilityEntry(record capability.Record) Entry {
	definition := record.Definition
	return Entry{
		ID: definition.ID, Version: definition.Version, Name: humanize(capabilityLeaf(definition.ID)),
		Kind: KindBlock, Domain: humanize(definition.OwnerDomain),
		Description: "Run the " + humanize(capabilityLeaf(definition.ID)) + " capability.",
		EffectClass: definition.EffectClass, Reversal: reversalFor(definition.EffectClass),
		Status: string(record.Status), StepType: workflow.StepCapability,
		RequiredCapabilities: []capability.Key{definition.Key()},
	}
}

func reversalFor(effect capability.EffectClass) string {
	switch effect {
	case capability.EffectPure, capability.EffectReadOnly:
		return "NO_EFFECT"
	case capability.EffectInternalMutation:
		return "COMPENSATION_REQUIRED"
	case capability.EffectExternalMutation:
		return "COMPENSATE_OR_CORRECT"
	case capability.EffectIrreversibleExternalMutation:
		return "FORWARD_CORRECTION"
	default:
		return "UNRESOLVED"
	}
}

func validExtension(entry Entry) bool {
	return strings.TrimSpace(entry.ID) != "" && entry.Version > 0 && strings.TrimSpace(entry.Name) != "" &&
		(entry.Kind == KindFragment || entry.Kind == KindTemplate)
}

func cloneEntry(entry Entry) Entry {
	entry.RequiredCapabilities = append([]capability.Key(nil), entry.RequiredCapabilities...)
	entry.Expansion = cloneExpansion(entry.Expansion)
	return entry
}

func cloneExpansion(expansion Expansion) Expansion {
	// Workflow nodes contain nested maps and slices (metadata, bindings,
	// observe policy, and step-specific payloads). Copying only the outer
	// slices would let a caller mutate the server-owned registry through a
	// value returned by List. JSON is the canonical workflow representation,
	// so round-tripping here gives the catalog the same deep-copy boundary as
	// persisted definitions. A malformed in-process extension fails closed as
	// an empty expansion and is refused by the edit kernel.
	encoded, err := json.Marshal(expansion)
	if err != nil {
		return Expansion{}
	}
	var cloned Expansion
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return Expansion{}
	}
	return cloned
}

func capabilityLeaf(id string) string {
	parts := strings.Split(id, ".")
	return parts[len(parts)-1]
}

func humanize(value string) string {
	value = strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(value))
	if value == "" {
		return ""
	}
	runes := []rune(strings.ToLower(value))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
