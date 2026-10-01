package workflow

// CatalogMetadata is the viewer-safe presentation contract for a published
// workflow. It is part of the definition so a release cannot silently change
// the name or discoverability of an immutable version after publication.
type CatalogMetadata struct {
	DisplayName string   `json:"display_name,omitempty"`
	Category    string   `json:"category,omitempty"`
	Description string   `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
}

// Clone returns an independent metadata value for a publication boundary.
func (m *CatalogMetadata) Clone() *CatalogMetadata {
	if m == nil {
		return nil
	}
	clone := *m
	clone.Keywords = append([]string(nil), m.Keywords...)
	return &clone
}
