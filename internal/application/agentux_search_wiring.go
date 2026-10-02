package application

// bindWorkspaceSearch registers Assistant's second read-only skill on the one
// document searcher and decorates the Agent setup catalog with what that skill
// can search. A persona that does not pin the skill is unaffected: the skill
// only becomes a tool for a run whose current persona version pins it.
func (w *personaServeWiring) bindWorkspaceSearch(searcher *PersonaPolicyDocumentSearcher) error {
	if w == nil || searcher == nil {
		return ErrPersonaCatalogDenied
	}
	if _, err := BindPersonaWorkspaceSearchSkill(w.capabilities, w.skills, searcher); err != nil {
		return err
	}
	if catalog, ok := w.adminCatalog.(readOnlyPersonaAdminCatalog); ok && catalog.service != nil {
		catalog.service.Versions = WorkspacePersonaCatalogVersions{Base: catalog.service.Versions, Search: searcher}
	}
	return nil
}
