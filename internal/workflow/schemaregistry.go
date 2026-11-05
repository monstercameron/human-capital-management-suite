// WF-DATA-003: schema-registry entries for tenant-defined types. A
// published custom-object definition and a registered external document
// type each yield a SCHEMA entry carrying the generated protobuf
// descriptor, pinned by definition version. Descriptors are
// tenant-relative: the registry row (or compile input) binds them to
// their tenant, so the compiler's MESSAGE and typed-path checks accept
// them exactly like any published schema, unchanged.
package workflow

import (
	"fmt"
	"strings"
)

// NewSchemaEntry builds the publishable SCHEMA registry entry for one
// tenant-defined type: id and descriptor name the type, version pins the
// definition version verbatim (numeric for custom definitions, template
// versions verbatim for document types), and digest pins the content.
// Blank components and unknown statuses are refused; descriptor shape is
// the emitter's contract (customobject.Compile, template.Definition).
func NewSchemaEntry(id, version, digest, descriptor string, status ReferenceStatus) (ResolvedReference, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(version) == "" {
		return ResolvedReference{}, fmt.Errorf("%w: schema identity and version are required", ErrInvalidReference)
	}
	if strings.TrimSpace(digest) == "" {
		return ResolvedReference{}, fmt.Errorf("%w: schema %s carries no digest", ErrInvalidReference, id)
	}
	if strings.TrimSpace(descriptor) == "" {
		return ResolvedReference{}, fmt.Errorf("%w: schema %s declares no descriptor", ErrInvalidReference, id)
	}
	if !status.Valid() {
		return ResolvedReference{}, fmt.Errorf("%w: schema %s status %q", ErrInvalidReference, id, status)
	}
	return ResolvedReference{Kind: RefSchema, ID: id, Version: version, Digest: digest, Status: status, Descriptor: descriptor}, nil
}
