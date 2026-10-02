package productui

// PersonaAdminDocumentReference is the viewing administrator's safe catalog
// projection. Title is absent when the current viewer cannot read the document.
type PersonaAdminDocumentReference struct {
	Title string `json:"title,omitempty"`
	// Location disambiguates same-title documents for the administrator. It is
	// presentation metadata (normally the folder or owner), never authority.
	Location      string `json:"location,omitempty"`
	DocumentID    string `json:"document_id"`
	VersionMode   string `json:"version_mode"`
	PinnedVersion uint64 `json:"pinned_version,omitempty"`
	SectionAnchor string `json:"section_anchor,omitempty"`
	Label         string `json:"label"`
	Readable      bool   `json:"readable"`
}
