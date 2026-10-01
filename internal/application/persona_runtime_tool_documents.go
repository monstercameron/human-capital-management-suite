package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// PersonaRuntimeDocumentVersionSource reads the exact version's owner
// classification under the invoker's current document read grant.
type PersonaRuntimeDocumentVersionSource interface {
	PersonaRuntimeDocumentClass(context.Context, PersonaRunT0ToolInvocation, string, string) (trustdlp.DataClass, error)
}

type DatabasePersonaRuntimeDocumentVersions struct{ Store *documenthubstore.Store }

func (s DatabasePersonaRuntimeDocumentVersions) PersonaRuntimeDocumentClass(ctx context.Context, identity PersonaRunT0ToolInvocation, documentID, versionID string) (trustdlp.DataClass, error) {
	if s.Store == nil || ctx == nil || documentID == "" || versionID == "" {
		return "", errPersonaRuntimeTools
	}
	version, err := s.Store.ReadVersion(ctx, identity.TenantID, documentID, versionID, "person", identity.InvokerID)
	if err != nil || version.DocumentID != documentID || version.ID != versionID {
		return "", errPersonaRuntimeTools
	}
	switch strings.ToUpper(strings.TrimSpace(version.Classification)) {
	case "PUBLIC":
		return trustdlp.ClassPublic, nil
	case "INTERNAL":
		return trustdlp.ClassInternal, nil
	case "CONFIDENTIAL":
		return trustdlp.ClassConfidential, nil
	case "RESTRICTED":
		return trustdlp.ClassRestricted, nil
	default:
		return "", errPersonaRuntimeTools
	}
}
