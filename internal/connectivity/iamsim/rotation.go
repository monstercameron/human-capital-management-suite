package iamsim

import (
	"crypto/sha256"
	"net/http"
)

// MaxClientSecrets bounds the secret set one rotation may install.
const MaxClientSecrets = 16

// hashSecrets hashes every non-empty client secret. Only hashes are kept, the
// same discipline as for access tokens.
func hashSecrets(secrets []string) []tokenHash {
	var out []tokenHash
	for _, sec := range secrets {
		if sec != "" {
			out = append(out, sha256.Sum256([]byte(sec)))
		}
	}
	return out
}

// handlePutClientSecrets implements PUT /v1/_control/client-secrets
// {"secrets":[...]}: the listed secrets become the complete valid set for the
// registered client. Tokens already issued are untouched and stay valid until
// they expire, exactly as with a real vendor's secret rotation. An empty set
// is refused because it would lock the client out silently. Secrets are never
// echoed or logged.
func (s *Server) handlePutClientSecrets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secrets []string `json:"secrets"`
	}
	if !readControlJSON(w, r, &body) {
		return
	}
	if len(body.Secrets) == 0 || len(body.Secrets) > MaxClientSecrets {
		writeInvalid(w, "secrets")
		return
	}
	for _, sec := range body.Secrets {
		if sec == "" {
			writeInvalid(w, "secrets")
			return
		}
	}
	hashed := hashSecrets(body.Secrets)
	s.mu.Lock()
	s.clientSecrets = hashed
	s.mu.Unlock()
	s.log.Info("client secrets rotated", "secrets", len(hashed))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "secrets": len(hashed)})
}

// handlePutWebhookSecret implements PUT /v1/_control/webhook-secret
// {"secret":"..."}: callbacks sent from now on are signed with the new
// secret, so a receiver's dual-secret verification window can be exercised.
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
	s.webhookSecret = []byte(body.Secret)
	s.mu.Unlock()
	s.log.Info("webhook secret rotated")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
