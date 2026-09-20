package payrollsim

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// MaxAPIKeys bounds the key set one rotation may install.
const MaxAPIKeys = 16

// keyHash is the SHA-256 of an API key: the only form the server keeps, which
// also equalises lengths so comparison time does not depend on the key.
type keyHash [sha256.Size]byte

// hashKeys hashes every non-empty key.
func hashKeys(keys []string) []keyHash {
	var out []keyHash
	for _, k := range keys {
		if k != "" {
			out = append(out, sha256.Sum256([]byte(k)))
		}
	}
	return out
}

// matchAny reports whether got is one of keys. Every entry is compared, and
// the results are OR-ed without branching, so timing reveals neither whether
// nor at which index a key matched.
func matchAny(keys []keyHash, got string) bool {
	h := sha256.Sum256([]byte(got))
	match := 0
	for i := range keys {
		match |= subtle.ConstantTimeCompare(h[:], keys[i][:])
	}
	return match == 1
}

// readControlJSON decodes a bounded control-plane body into v, writing the
// error response itself when it returns false.
func readControlJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
			return false
		}
		writeInvalid(w, "body")
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		writeInvalid(w, "body")
		return false
	}
	return true
}

// handlePutAPIKeys implements PUT /v1/_control/api-keys {"keys":[...]}: the
// listed keys become the complete valid set, so rotating is "add the new key
// next to the old", then later "drop the old". An empty set is refused
// because it would silently turn authentication off. Keys are never echoed.
func (s *Server) handlePutAPIKeys(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keys []string `json:"keys"`
	}
	if !readControlJSON(w, r, &body) {
		return
	}
	if len(body.Keys) == 0 || len(body.Keys) > MaxAPIKeys {
		writeInvalid(w, "keys")
		return
	}
	for _, k := range body.Keys {
		if k == "" {
			writeInvalid(w, "keys")
			return
		}
	}
	hashed := hashKeys(body.Keys)
	s.mu.Lock()
	s.apiKeys = hashed
	s.mu.Unlock()
	s.log.Info("api keys rotated", "keys", len(hashed))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "keys": len(hashed)})
}

// handlePutWebhookSecret implements PUT /v1/_control/webhook-secret
// {"secret":"..."}: callbacks sent from now on are signed with the new
// secret, so a receiver's dual-secret verification window can be exercised.
// The secret is never echoed or logged.
func (s *Server) handlePutWebhookSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret string `json:"secret"`
	}
	if !readControlJSON(w, r, &body) {
		return
	}
	if body.Secret == "" {
		writeInvalid(w, "secret")
		return
	}
	s.mu.Lock()
	s.secret = []byte(body.Secret)
	s.mu.Unlock()
	s.log.Info("webhook secret rotated")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
