// Package signing re-exports the proof-bound e-signature ceremony from
// internal/esign. The ceremony moved to a pure leaf so the browser client can
// link its value types without the documents engine; existing callers keep
// this path.
package signing

import "github.com/monstercameron/human-capital-management-suite/internal/esign"

// Ceremony states, re-exported from the leaf.
const (
	StateRequested    = esign.StateRequested
	StateDelivered    = esign.StateDelivered
	StateAcknowledged = esign.StateAcknowledged
	StateSigned       = esign.StateSigned
	StateDeclined     = esign.StateDeclined
	StateExpired      = esign.StateExpired
	StateFailed       = esign.StateFailed
	StateAmbiguous    = esign.StateAmbiguous
)

// Ceremony is one proof-bound signing.
type Ceremony = esign.Ceremony

// Request opens one ceremony for an exact artifact hash.
func Request(id, artifactHash, signerID string, trustedNow, expiresAt int64) (Ceremony, error) {
	return esign.Request(id, artifactHash, signerID, trustedNow, expiresAt)
}

// SignerProofFor binds a signer to an artifact.
func SignerProofFor(signerID, artifactHash string) string {
	return esign.SignerProofFor(signerID, artifactHash)
}

// Replay recovers a crashed ceremony from its journaled states.
func Replay(states []Ceremony) (Ceremony, error) { return esign.Replay(states) }
