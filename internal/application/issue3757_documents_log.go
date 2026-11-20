package application

import "log/slog"

// logPersonaAdminDocumentsFailure records a genuine reference-document load
// failure behind the Setup page's retry banner (#3757). It names the step and
// the error only, never document titles, ids or tenant content.
func logPersonaAdminDocumentsFailure(stage string, err error) {
	if err == nil {
		slog.Error("persona admin catalog: reference documents unavailable", "stage", stage)
		return
	}
	slog.Error("persona admin catalog: reference documents unavailable", "stage", stage, "error", err.Error())
}
