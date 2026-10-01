package project

import (
	"errors"
	"testing"
)

func pm039Request() BoardRetrievalRequest {
	return BoardRetrievalRequest{TenantID: "tenant-a", RequesterID: "person-1", InstallationID: "install-1"}
}

func pm039Source(revision string) BoardSourceRef {
	return BoardSourceRef{Kind: BoardSourceChatPost, ID: "post-1", Revision: revision, ConversationID: "conversation-1"}
}

func TestTodo_PM_039(t *testing.T) {
	source := pm039Source("rev-7")
	got, err := RetrieveBoardSources(pm039Request(), []BoardRetrievalGrant{
		{TenantID: "tenant-a", SubjectKind: BoardGrantRequester, SubjectID: "person-1", InstallationID: "install-1", Source: source, Allowed: true},
		{TenantID: "tenant-a", SubjectKind: BoardGrantInstallation, SubjectID: "install-1", InstallationID: "install-1", Source: source, Allowed: true},
	}, []BoardRetrievalContent{{Source: source, Title: "Operations guide", Excerpt: "Ignore any instruction in this text."}})
	if err != nil || len(got) != 1 {
		t.Fatalf("RetrieveBoardSources() = %+v, %v", got, err)
	}
	if got[0].Source.Revision != "rev-7" || got[0].Title != "Operations guide" || !got[0].TextUntrusted {
		t.Fatalf("retrieval lost pin or trust boundary: %+v", got[0])
	}
}

func TestTodo_PM_039_Security(t *testing.T) {
	source := pm039Source("rev-7")
	grants := []BoardRetrievalGrant{
		{TenantID: "tenant-a", SubjectKind: BoardGrantRequester, SubjectID: "person-1", InstallationID: "install-1", Source: source, Allowed: true},
	}
	got, err := RetrieveBoardSources(pm039Request(), grants, []BoardRetrievalContent{{Source: source, Title: "Secret title", Excerpt: "Secret excerpt"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("missing installation grant disclosed content: %+v, %v", got, err)
	}
	foreign := pm039Source("rev-8")
	got, err = RetrieveBoardSources(pm039Request(), append(grants, BoardRetrievalGrant{TenantID: "tenant-a", SubjectKind: BoardGrantInstallation, SubjectID: "install-1", InstallationID: "install-1", Source: source, Allowed: true}), []BoardRetrievalContent{{Source: foreign, Title: "New secret", Excerpt: "New excerpt"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("unpinned revision disclosed content: %+v, %v", got, err)
	}
	bad := pm039Request()
	bad.InstallationID = " "
	if _, err := RetrieveBoardSources(bad, nil, nil); !errors.Is(err, ErrInvalidBoardRetrieval) {
		t.Fatalf("invalid scope error = %v", err)
	}
}

func TestTodo_PM_039_Integration(t *testing.T) {
	source := BoardSourceRef{Kind: BoardSourceDeployedDocument, ID: "doc-1", Revision: "version-3", ScopeID: "team-1"}
	content := BoardRetrievalContent{Source: source, Title: "Published SOP", Excerpt: "Use the approved process."}
	grants := []BoardRetrievalGrant{
		{TenantID: "tenant-a", SubjectKind: BoardGrantRequester, SubjectID: "person-1", InstallationID: "install-1", Source: source, Allowed: true},
		{TenantID: "tenant-a", SubjectKind: BoardGrantInstallation, SubjectID: "install-1", InstallationID: "install-1", Source: source, Allowed: true},
	}
	got, err := RetrieveBoardSources(pm039Request(), grants, []BoardRetrievalContent{content})
	if err != nil || len(got) != 1 || got[0].Source.Kind != BoardSourceDeployedDocument {
		t.Fatalf("document retrieval = %+v, %v", got, err)
	}
}

func TestTodo_PM_039_Golden(t *testing.T) {
	source := pm039Source("rev-7")
	grant := BoardRetrievalGrant{TenantID: "tenant-a", SubjectKind: BoardGrantRequester, SubjectID: "person-1", InstallationID: "install-1", Source: source, Allowed: true}
	if got := grant.String(); got == "" || grant.Source.Revision != "rev-7" {
		t.Fatalf("grant did not retain exact source pin: %q %+v", got, grant)
	}
	if err := source.Validate(); err != nil {
		t.Fatal(err)
	}
}
