package workspace

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PathPersonaAdminData is the authenticated metadata transport used when a
// browser enters Persona Admin through SPA navigation or changes preview
// targets. It never accepts a tenant or principal from the query string.
const PathPersonaAdminData = "/workspace/persona-admin"

type personaAdminDataResponse struct {
	Snapshot productui.PersonaAdminSnapshot `json:"snapshot"`
}

func (h *Handler) servePersonaAdminData(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	access, err := h.resolveProductAccess(admitted.Context(), principal)
	if err != nil {
		http.Error(w, "persona administration unavailable", http.StatusServiceUnavailable)
		return
	}
	if principal == nil || !access.can(productui.PagePersonaAdmin, roleaccess.ActionView) || h.personaAdmin == nil {
		http.Error(w, "persona administration unavailable", http.StatusForbidden)
		return
	}
	client := h.personaAdmin.ClientForRequest(admitted.Context())
	if client == nil {
		http.Error(w, "persona administration unavailable", http.StatusForbidden)
		return
	}
	snapshot, err := client.Snapshot(admitted.Context(), productui.PersonaAdminSnapshotRequest{
		TenantID: string(principal.Tenant()), Principal: principal.Subject(),
	})
	if err != nil || !snapshot.Available {
		h.logPersonaAdminSnapshotFailure(err)
		status := http.StatusServiceUnavailable
		var staged interface{ PersonaCatalogFailureStage() string }
		if errors.As(err, &staged) && staged.PersonaCatalogFailureStage() == "authorization" {
			status = http.StatusForbidden
		}
		http.Error(w, "persona administration unavailable", status)
		return
	}
	q := r.URL.Query()
	personaID, subjectID, conversationID := strings.TrimSpace(q.Get("persona_id")), strings.TrimSpace(q.Get("subject_id")), strings.TrimSpace(q.Get("conversation_id"))
	if personaID != "" || subjectID != "" || conversationID != "" {
		if personaID == "" || subjectID == "" || conversationID == "" {
			http.Error(w, "invalid persona preview", http.StatusBadRequest)
			return
		}
		preview, previewErr := client.Preview(admitted.Context(), productui.PersonaAdminPreviewRequest{PersonaID: personaID, SubjectID: subjectID, ConversationID: conversationID})
		if previewErr != nil {
			snapshot.Preview = productui.PersonaAdminPreview{}
			snapshot.PreviewState.Unavailable = true
			snapshot.PreviewUnavailable = true
		} else {
			snapshot.Preview = preview
		}
		snapshot.PreviewPersonaID = personaID
		snapshot.PreviewSubjectID = subjectID
		snapshot.PreviewConversationID = conversationID
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(personaAdminDataResponse{Snapshot: snapshot})
}
