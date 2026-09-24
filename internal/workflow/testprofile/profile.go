// Package testprofile stores versioned fake-data profiles and deterministic
// sample responses used by workflow test runs.
package testprofile

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const Format = "hcmnext.workflow.test-profile/v1"

var (
	ErrNotFound           = errors.New("testprofile: profile not found")
	ErrTenantMismatch     = errors.New("testprofile: profile belongs to another tenant")
	ErrMalformed          = errors.New("testprofile: malformed profile")
	ErrResponseAbsent     = errors.New("testprofile: sample response not found")
	ErrCapabilityMismatch = errors.New("testprofile: sample response capability does not match the block")
)

//go:embed profiles/*.json
var embedded embed.FS

// ProfileRef pins the identity and immutable dataset version used by a run.
type ProfileRef struct {
	ID      string `json:"id"`
	Version uint32 `json:"version"`
}

func (r ProfileRef) Validate() error {
	if !validID(r.ID) || r.Version == 0 {
		return fmt.Errorf("%w: profile id must be a lowercase slug and version must be positive", ErrMalformed)
	}
	return nil
}

func validID(id string) bool {
	if id == "" || id[0] == '-' || id[len(id)-1] == '-' {
		return false
	}
	previousHyphen := false
	for _, r := range id {
		hyphen := r == '-'
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && !hyphen {
			return false
		}
		if hyphen && previousHyphen {
			return false
		}
		previousHyphen = hyphen
	}
	return true
}

// DatasetRef cites an immutable source dataset from the demo seed or a
// workflow conformance environment. The source is a repository path so the
// data behind a profile can be inspected and regenerated.
type DatasetRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Source  string `json:"source"`
}

// FieldSample is one canonical typed output field in a block response.
type FieldSample struct {
	Path  string             `json:"path"`
	Type  workflow.ValueType `json:"type"`
	Value string             `json:"value"`
}

// BlockResponse is a block's deterministic answer for one selected outcome.
// Capability identity and version bind that answer to the workflow node's
// exact capability contract.
type BlockResponse struct {
	NodeID            string           `json:"node_id"`
	CapabilityID      string           `json:"capability_id"`
	CapabilityVersion uint32           `json:"capability_version"`
	Outcome           workflow.Outcome `json:"outcome"`
	Fields            []FieldSample    `json:"fields,omitempty"`
	Detail            string           `json:"detail,omitempty"`
}

// Profile is a named, versioned set of fake source datasets and optional
// per-block response choices. Tenant is an isolation boundary, not a hint.
type Profile struct {
	Format    string          `json:"format"`
	Ref       ProfileRef      `json:"ref"`
	Name      string          `json:"name"`
	Tenant    string          `json:"tenant"`
	Datasets  []DatasetRef    `json:"datasets"`
	Responses []BlockResponse `json:"responses,omitempty"`
}

// Registry owns immutable profile versions. Lookups return copies so callers
// cannot alter the registry's data after it has been validated.
type Registry struct {
	profiles map[ProfileRef]Profile
}

// NewRegistry builds a registry from profiles in an fs.FS. Each file contains
// one versioned profile; duplicate IDs and malformed versions are refused.
func NewRegistry(source fs.FS) (*Registry, error) {
	paths, err := fs.Glob(source, "profiles/*.json")
	if err != nil {
		return nil, fmt.Errorf("%w: list profile files: %v", ErrMalformed, err)
	}
	r := &Registry{profiles: make(map[ProfileRef]Profile, len(paths))}
	for _, path := range paths {
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return nil, fmt.Errorf("%w: read %s: %v", ErrMalformed, path, err)
		}
		p, err := Decode(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if _, exists := r.profiles[p.Ref]; exists {
			return nil, fmt.Errorf("%w: duplicate profile %s@%d", ErrMalformed, p.Ref.ID, p.Ref.Version)
		}
		r.profiles[p.Ref] = clone(p)
	}
	if len(r.profiles) == 0 {
		return nil, fmt.Errorf("%w: no profile files", ErrMalformed)
	}
	return r, nil
}

// DefaultRegistry loads the checked-in demo and conformance profiles.
func DefaultRegistry() (*Registry, error) { return NewRegistry(embedded) }

// Resolve returns an exact immutable profile version.
func (r *Registry) Resolve(ref ProfileRef) (Profile, error) {
	if err := ref.Validate(); err != nil {
		return Profile{}, err
	}
	p, ok := r.profiles[ref]
	if !ok {
		return Profile{}, fmt.Errorf("%w: %s@%d", ErrNotFound, ref.ID, ref.Version)
	}
	return clone(p), nil
}

// ResolveForTenant resolves the exact profile version only when it is scoped
// to the caller's tenant.
func (r *Registry) ResolveForTenant(ref ProfileRef, tenant string) (Profile, error) {
	p, err := r.Resolve(ref)
	if err != nil {
		return Profile{}, err
	}
	if strings.TrimSpace(tenant) == "" || p.Tenant != tenant {
		return Profile{}, fmt.Errorf("%w: profile %s is scoped to %s", ErrTenantMismatch, ref.ID, p.Tenant)
	}
	return p, nil
}

// Response resolves one declared response for a node and selected outcome.
func (p Profile) Response(nodeID string, outcome workflow.Outcome) (BlockResponse, error) {
	for _, response := range p.Responses {
		if response.NodeID == nodeID && response.Outcome == outcome {
			return cloneResponse(response), nil
		}
	}
	return BlockResponse{}, fmt.Errorf("%w: node %s outcome %s", ErrResponseAbsent, nodeID, outcome)
}

// ResponseFor additionally checks the exact capability identity and version
// compiled into the block before returning its selected sample.
func (p Profile) ResponseFor(nodeID, capabilityID string, capabilityVersion uint32, outcome workflow.Outcome) (BlockResponse, error) {
	response, err := p.Response(nodeID, outcome)
	if err != nil {
		return BlockResponse{}, err
	}
	if response.CapabilityID != capabilityID || response.CapabilityVersion != capabilityVersion {
		return BlockResponse{}, fmt.Errorf("%w: profile has %s@%d, block binds %s@%d", ErrCapabilityMismatch,
			response.CapabilityID, response.CapabilityVersion, capabilityID, capabilityVersion)
	}
	return response, nil
}

// Digest hashes the canonical JSON representation under the profile format.
func (p Profile) Digest() string {
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(Format))
	h.Write([]byte{0})
	h.Write(b)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Decode parses one profile, refuses unknown fields, and validates its
// version-bound references and response choices.
func Decode(data []byte) (Profile, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return Profile{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (p Profile) Validate() error {
	if p.Format != Format || p.Ref.Validate() != nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Tenant) == "" {
		return fmt.Errorf("%w: format, profile ref, name and tenant are required", ErrMalformed)
	}
	if len(p.Datasets) == 0 {
		return fmt.Errorf("%w: at least one source dataset is required", ErrMalformed)
	}
	seenDatasets := map[string]bool{}
	for _, dataset := range p.Datasets {
		if strings.TrimSpace(dataset.ID) == "" || strings.TrimSpace(dataset.Version) == "" || strings.TrimSpace(dataset.Source) == "" || seenDatasets[dataset.ID] {
			return fmt.Errorf("%w: dataset id, version and source must be present and IDs unique", ErrMalformed)
		}
		seenDatasets[dataset.ID] = true
	}
	seenResponses := map[string]bool{}
	for _, response := range p.Responses {
		if response.NodeID == "" || response.CapabilityID == "" || response.CapabilityVersion == 0 || !allowedOutcome(response.Outcome) {
			return fmt.Errorf("%w: each response needs node, capability version and supported outcome", ErrMalformed)
		}
		key := response.NodeID + "\x00" + string(response.Outcome)
		if seenResponses[key] {
			return fmt.Errorf("%w: duplicate response for %s/%s", ErrMalformed, response.NodeID, response.Outcome)
		}
		seenResponses[key] = true
		seenFields := map[string]bool{}
		for _, field := range response.Fields {
			if strings.TrimSpace(field.Path) == "" || field.Type.Kind == "" || seenFields[field.Path] {
				return fmt.Errorf("%w: response %s has an invalid or duplicate field", ErrMalformed, response.NodeID)
			}
			seenFields[field.Path] = true
		}
	}
	return nil
}

func allowedOutcome(outcome workflow.Outcome) bool {
	switch outcome {
	case workflow.OutcomeSucceeded, workflow.OutcomeRejected, workflow.OutcomeUnknown, workflow.OutcomeAmbiguous,
		workflow.OutcomePass, workflow.OutcomeFail, workflow.OutcomePartial:
		return true
	default:
		return false
	}
}

func clone(p Profile) Profile {
	p.Datasets = append([]DatasetRef(nil), p.Datasets...)
	p.Responses = append([]BlockResponse(nil), p.Responses...)
	for i := range p.Responses {
		p.Responses[i] = cloneResponse(p.Responses[i])
	}
	return p
}

func cloneResponse(response BlockResponse) BlockResponse {
	response.Fields = append([]FieldSample(nil), response.Fields...)
	sort.Slice(response.Fields, func(i, j int) bool { return response.Fields[i].Path < response.Fields[j].Path })
	return response
}
