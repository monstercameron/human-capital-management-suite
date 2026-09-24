// Package openapidoc serves the generated OpenAPI document embedded in the
// cell binary.
package openapidoc

import (
	_ "embed"
	"net/http"
)

// Path is the stable URL for the published machine-readable API reference.
const Path = "/openapi.yaml"

//go:embed rpcs.openapi.yaml
var document []byte

// Handler returns the immutable OpenAPI artifact. The cell composes it behind
// its normal authenticated HTTP admission boundary.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(document)
	})
}
