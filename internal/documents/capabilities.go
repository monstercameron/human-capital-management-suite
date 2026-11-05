package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/template"
)

// EventSignatureSigned is the signal event a completed signature emits. The
// document fragment's correlated SIGNAL awaits it.
const EventSignatureSigned = "documents.signature.signed"

// Capability identities: the governed document operations (WF-EXT-015). The
// document fragment binds to them; the served handlers adapt runner calls
// onto [Render] and [RequestSignature].
const (
	CapRender           = "hcmnext.documents.render"
	CapRequestSignature = "hcmnext.documents.request_signature"
)

func documentSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func documentDefinition(id string, effect capability.EffectClass, read, write []string, idempotency, scope string) capability.Definition {
	return capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          "documents",
		RequestSchema:        documentSchema(id, "request"),
		ResponseSchema:       documentSchema(id, "response"),
		ErrorSchema:          documentSchema(id, "error"),
		EffectClass:          effect,
		ReadData:             capability.DataDomainFieldSet{DataDomains: read},
		WriteData:            capability.DataDomainFieldSet{DataDomains: write},
		RiskClass:            "HIGH",
		IdempotencyPolicyRef: idempotency,
		AgentEligible:        false,
		AuthZScopeRef:        scope,
		LegalBasisRef:        "legal.documents.render.v1",
		EntitlementRef:       "entitlement.documents.render.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
}

// Definitions returns the document capability manifests: render is a pure
// read over published templates; requesting a signature opens ceremony
// state, an internal mutation.
func Definitions() []capability.Definition {
	return []capability.Definition{
		documentDefinition(CapRender, capability.EffectReadOnly,
			[]string{"template"}, nil, "idempotency.read-safe.v1", "scope:documents.read"),
		documentDefinition(CapRequestSignature, capability.EffectInternalMutation,
			[]string{"ceremony"}, []string{"ceremony"}, "idempotency.effect-key.v1", "scope:documents.write"),
	}
}

// TemplateStore resolves published templates by id and version. The render
// operation reads through it; compositions back it with the publication
// index, tests with [TemplateMap].
type TemplateStore interface {
	Lookup(id, version string) (template.Template, bool)
}

// TemplateMap is an in-memory template store keyed "id@version".
type TemplateMap map[string]template.Template

// Lookup resolves one published template.
func (m TemplateMap) Lookup(id, version string) (template.Template, bool) {
	t, ok := m[id+"@"+version]
	return t, ok
}

// RenderOutcome is what the render operation returns.
type RenderOutcome struct {
	ArtifactDigest string
	Content        string
}

// Render renders a published template against caller bindings. Binding kinds
// come from the approved placeholders, never the caller: the renderer
// receives typed values, not caller-asserted types.
func Render(store TemplateStore, inputs map[string]string) (RenderOutcome, error) {
	if store == nil {
		return RenderOutcome{}, fmt.Errorf("documents: render requires a template store")
	}
	tpl, ok := store.Lookup(inputs["template_id"], inputs["template_version"])
	if !ok {
		return RenderOutcome{}, fmt.Errorf("documents: template %q@%q is not published",
			inputs["template_id"], inputs["template_version"])
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(inputs["bindings_json"]), &raw); err != nil {
		return RenderOutcome{}, fmt.Errorf("documents: bindings_json: %v", err)
	}
	if raw == nil {
		raw = map[string]string{}
	}
	def := templateDefinitionOf(tpl)
	bindings := make(map[string]template.Binding, len(raw))
	for name, value := range raw {
		kind, ok := def[name]
		if !ok {
			return RenderOutcome{}, fmt.Errorf("documents: binding %q is not a declared placeholder", name)
		}
		bindings[name] = template.Binding{Kind: kind, Value: value}
	}
	artifact, err := tpl.Render(bindings)
	if err != nil {
		return RenderOutcome{}, err
	}
	return RenderOutcome{ArtifactDigest: artifact.Digest, Content: string(artifact.Content)}, nil
}

// templateDefinitionOf reads the approved placeholder kinds: the caller
// supplies values, the approved template supplies their types.
func templateDefinitionOf(tpl template.Template) map[string]template.ValueKind {
	out := map[string]template.ValueKind{}
	for _, p := range tpl.Definition.Placeholders {
		out[p.Name] = p.Kind
	}
	return out
}

// SignatureOutcome is what the request-signature operation returns.
type SignatureOutcome struct {
	CeremonyID string
	State      string
	Digest     string
}

// CeremonyRegistry retains opened ceremonies so provider delivery, signer
// acknowledgement and the correlated completion signal all address the same
// ceremony the operation opened.
type CeremonyRegistry struct {
	mu sync.RWMutex
	m  map[string]Signature
}

// NewCeremonyRegistry returns an empty registry.
func NewCeremonyRegistry() *CeremonyRegistry {
	return &CeremonyRegistry{m: make(map[string]Signature)}
}

// Store retains one opened ceremony.
func (r *CeremonyRegistry) Store(sig Signature) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[sig.Ceremony.ID] = sig
}

// Get returns one retained ceremony.
func (r *CeremonyRegistry) Get(id string) (Signature, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sig, ok := r.m[id]
	return sig, ok
}

// RequestSignature opens a proof-bound ceremony for one artifact hash and
// retains it for the completion path. The ceremony identity derives from the
// artifact, signer and key, so repeating the same request addresses the same
// ceremony; at is the trusted tick the runner recorded for the step.
func RequestSignature(reg *CeremonyRegistry, inputs map[string]string, at time.Time) (SignatureOutcome, error) {
	if reg == nil {
		return SignatureOutcome{}, fmt.Errorf("documents: request_signature requires a ceremony registry")
	}
	expiresIn, err := strconv.ParseInt(strings.TrimSpace(inputs["expires_in_seconds"]), 10, 64)
	if err != nil || expiresIn <= 0 {
		return SignatureOutcome{}, fmt.Errorf("documents: expires_in_seconds is positive seconds: %v", err)
	}
	now := at.Unix()
	id := ceremonyID(inputs["artifact_hash"], inputs["signer_id"], inputs["ceremony_key"])
	sig, err := BeginSignature(SignatureRequest{
		ID: id, ArtifactHash: inputs["artifact_hash"], SignerID: inputs["signer_id"],
		AssuranceLevel: AssuranceLevel(inputs["assurance"]),
		TrustedNow:     now, ExpiresAt: now + expiresIn,
	})
	if err != nil {
		return SignatureOutcome{}, err
	}
	reg.Store(sig)
	return SignatureOutcome{CeremonyID: sig.Ceremony.ID, State: sig.Ceremony.State, Digest: sig.Ceremony.Digest}, nil
}

// ceremonyID derives the stable ceremony identity.
func ceremonyID(artifactHash, signerID, key string) string {
	sum := sha256.Sum256([]byte("documents-ceremony\x00" + artifactHash + "\x00" + signerID + "\x00" + key))
	return "sig-" + hex.EncodeToString(sum[:])[:16]
}
