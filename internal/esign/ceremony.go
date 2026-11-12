// Package esign executes proof-bound e-signature ceremonies
// (DOC-SIGN-001): states distinguish requested, delivered, acknowledged,
// signed, declined, expired, failed and ambiguous, and every terminal
// state binds signer proof, ceremony, trusted time, the exact artifact
// hash and the provider receipt. Provider acceptance alone never reports
// signed; manual fallback preserves equivalent authority and evidence.
package esign

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Ceremony states: the closed lifecycle vocabulary.
const (
	StateRequested    = "REQUESTED"
	StateDelivered    = "DELIVERED"
	StateAcknowledged = "ACKNOWLEDGED"
	StateSigned       = "SIGNED"
	StateDeclined     = "DECLINED"
	StateExpired      = "EXPIRED"
	StateFailed       = "FAILED"
	StateAmbiguous    = "AMBIGUOUS"
)

// Ceremony is one proof-bound signing. Time is caller-supplied trusted
// ticks, never the wall clock, so the lifecycle stays deterministic.
type Ceremony struct {
	ID              string
	ArtifactHash    string
	SignerID        string
	SignerProof     string
	CeremonyProof   []string
	TrustedTime     int64
	ExpiresAt       int64
	ProviderReceipt string
	CallbackIDs     []string
	State           string
	FailReason      string
	Journal         []string
	Digest          string
}

func ceremonyDigest(ceremony Ceremony) string {
	proof := append([]string(nil), ceremony.CeremonyProof...)
	sort.Strings(proof)
	callbacks := append([]string(nil), ceremony.CallbackIDs...)
	sort.Strings(callbacks)
	parts := append([]string{"esign-ceremony", ceremony.ID, ceremony.ArtifactHash, ceremony.SignerID, ceremony.SignerProof}, proof...)
	parts = append(parts, ceremony.ProviderReceipt, ceremony.State, ceremony.FailReason, fmt.Sprint(ceremony.TrustedTime, ceremony.ExpiresAt))
	parts = append(parts, callbacks...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (ceremony *Ceremony) seal(event string) {
	ceremony.Journal = append(ceremony.Journal, fmt.Sprintf("t%d:%s:%s", len(ceremony.Journal), ceremony.State, event))
	ceremony.Digest = ceremonyDigest(*ceremony)
}

// Request opens one ceremony for an exact artifact hash.
func Request(id, artifactHash, signerID string, trustedNow, expiresAt int64) (Ceremony, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(artifactHash) == "" || strings.TrimSpace(signerID) == "" {
		return Ceremony{}, fmt.Errorf("signing: ceremony id, artifact hash and signer are required")
	}
	if expiresAt <= trustedNow {
		return Ceremony{}, fmt.Errorf("signing: ceremony expiry must follow trusted time")
	}
	ceremony := Ceremony{ID: id, ArtifactHash: artifactHash, SignerID: signerID, TrustedTime: trustedNow, ExpiresAt: expiresAt, State: StateRequested}
	ceremony.seal("requested")
	return ceremony, nil
}

// Deliver records provider delivery with its receipt and callback id.
// Replayed callbacks refuse: the ceremony turns AMBIGUOUS instead of
// advancing on a replay.
func (ceremony Ceremony) Deliver(receipt, callbackID string, trustedNow int64) (Ceremony, error) {
	if ceremony.State != StateRequested {
		return Ceremony{}, fmt.Errorf("signing: delivery expects a requested ceremony in %s", ceremony.State)
	}
	if strings.TrimSpace(receipt) == "" || strings.TrimSpace(callbackID) == "" {
		return Ceremony{}, fmt.Errorf("signing: delivery needs a provider receipt and callback id")
	}
	if trustedNow > ceremony.ExpiresAt {
		ceremony.State = StateExpired
		ceremony.seal("expired-before-delivery")
		return ceremony, nil
	}
	ceremony.ProviderReceipt = receipt
	ceremony.CallbackIDs = append(ceremony.CallbackIDs, callbackID)
	ceremony.TrustedTime = trustedNow
	ceremony.State = StateDelivered
	ceremony.seal("delivered")
	return ceremony, nil
}

// Acknowledge records signer acknowledgement of the exact artifact.
func (ceremony Ceremony) Acknowledge(artifactHash string, trustedNow int64) (Ceremony, error) {
	if ceremony.State != StateDelivered {
		return Ceremony{}, fmt.Errorf("signing: acknowledgement expects a delivered ceremony in %s", ceremony.State)
	}
	if artifactHash != ceremony.ArtifactHash {
		ceremony.State = StateFailed
		ceremony.FailReason = "stale-artifact"
		ceremony.seal("stale-artifact")
		return ceremony, nil
	}
	if trustedNow > ceremony.ExpiresAt {
		ceremony.State = StateExpired
		ceremony.seal("expired-before-acknowledgement")
		return ceremony, nil
	}
	ceremony.TrustedTime = trustedNow
	ceremony.State = StateAcknowledged
	ceremony.seal("acknowledged")
	return ceremony, nil
}

// SignerProofFor binds a signer to an artifact: proofs carry both.
func SignerProofFor(signerID, artifactHash string) string {
	sum := sha256.Sum256([]byte("esign-proof\x00" + signerID + "\x00" + artifactHash))
	return "proof:sha256:" + hex.EncodeToString(sum[:])
}

// Sign completes the ceremony. Signer substitution, stale artifacts,
// expired requests, replayed callbacks and provider-acceptance-alone all
// refuse to report signed.
func (ceremony Ceremony) Sign(proof, artifactHash, callbackID string, trustedNow int64) (Ceremony, error) {
	if ceremony.State != StateAcknowledged {
		return Ceremony{}, fmt.Errorf("signing: signature expects an acknowledged ceremony in %s", ceremony.State)
	}
	if proof != SignerProofFor(ceremony.SignerID, ceremony.ArtifactHash) {
		ceremony.State = StateFailed
		ceremony.FailReason = "signer-substitution"
		ceremony.seal("signer-substitution")
		return ceremony, nil
	}
	if artifactHash != ceremony.ArtifactHash {
		ceremony.State = StateFailed
		ceremony.FailReason = "stale-artifact"
		ceremony.seal("stale-artifact")
		return ceremony, nil
	}
	if trustedNow > ceremony.ExpiresAt {
		ceremony.State = StateExpired
		ceremony.seal("expired-before-signature")
		return ceremony, nil
	}
	for _, seen := range ceremony.CallbackIDs {
		if seen == callbackID {
			ceremony.State = StateAmbiguous
			ceremony.FailReason = "replay"
			ceremony.seal("replay")
			return ceremony, nil
		}
	}
	if strings.TrimSpace(ceremony.ProviderReceipt) == "" {
		ceremony.State = StateFailed
		ceremony.FailReason = "missing-provider-receipt"
		ceremony.seal("missing-provider-receipt")
		return ceremony, nil
	}
	ceremony.SignerProof = proof
	ceremony.CallbackIDs = append(ceremony.CallbackIDs, callbackID)
	ceremony.CeremonyProof = append(ceremony.CeremonyProof, "requested", "delivered", "acknowledged", "signed")
	ceremony.TrustedTime = trustedNow
	ceremony.State = StateSigned
	ceremony.seal("signed")
	return ceremony, nil
}

// Decline records an explicit signer refusal.
func (ceremony Ceremony) Decline(reason string, trustedNow int64) (Ceremony, error) {
	if ceremony.State != StateDelivered && ceremony.State != StateAcknowledged {
		return Ceremony{}, fmt.Errorf("signing: decline expects a delivered ceremony in %s", ceremony.State)
	}
	if strings.TrimSpace(reason) == "" {
		return Ceremony{}, fmt.Errorf("signing: decline needs a reason")
	}
	ceremony.FailReason = reason
	ceremony.TrustedTime = trustedNow
	ceremony.State = StateDeclined
	ceremony.seal("declined")
	return ceremony, nil
}

// ProviderNotify records a provider callback without advancing the state:
// provider acceptance alone never reports signed.
func (ceremony Ceremony) ProviderNotify(receipt, callbackID string) (Ceremony, error) {
	for _, seen := range ceremony.CallbackIDs {
		if seen == callbackID {
			ambiguous := ceremony
			ambiguous.State = StateAmbiguous
			ambiguous.FailReason = "replay"
			ambiguous.seal("replay")
			return ambiguous, nil
		}
	}
	ceremony.ProviderReceipt = receipt
	ceremony.CallbackIDs = append(ceremony.CallbackIDs, callbackID)
	ceremony.seal("provider-notify")
	return ceremony, nil
}

// Expire closes an outstanding ceremony at trusted time.
func (ceremony Ceremony) Expire(trustedNow int64) Ceremony {
	if ceremony.State == StateSigned || ceremony.State == StateDeclined || ceremony.State == StateFailed {
		return ceremony
	}
	ceremony.TrustedTime = trustedNow
	ceremony.State = StateExpired
	ceremony.seal("expired")
	return ceremony
}

// EvidenceItems exports the ceremony evidence in sequence: artifact,
// ceremony proof, signer proof and provider receipt.
func (ceremony Ceremony) EvidenceItems() []struct {
	ID     string
	Kind   string
	Digest string
} {
	items := []struct {
		ID     string
		Kind   string
		Digest string
	}{
		{ID: ceremony.ID + ":artifact", Kind: "artifact", Digest: ceremony.ArtifactHash},
		{ID: ceremony.ID + ":ceremony", Kind: "ceremony-proof", Digest: ceremony.Digest},
	}
	if ceremony.SignerProof != "" {
		items = append(items, struct {
			ID     string
			Kind   string
			Digest string
		}{ID: ceremony.ID + ":signer-proof", Kind: "signer-proof", Digest: ceremony.SignerProof})
	}
	if ceremony.ProviderReceipt != "" {
		items = append(items, struct {
			ID     string
			Kind   string
			Digest string
		}{ID: ceremony.ID + ":provider-receipt", Kind: "provider-receipt", Digest: "receipt:" + ceremony.ProviderReceipt})
	}
	return items
}

// Verify recomputes the ceremony seal.
func (ceremony Ceremony) Verify() error {
	if ceremony.Digest == "" || ceremonyDigest(ceremony) != ceremony.Digest {
		return fmt.Errorf("signing: ceremony seal is broken")
	}
	return nil
}

// Replay recovers a crashed ceremony from its journaled states. Every
// step must verify and extend the prior journal by exactly one entry:
// duplicated events and invented states refuse, and recovery never
// advances past the journal.
func Replay(states []Ceremony) (Ceremony, error) {
	if len(states) == 0 {
		return Ceremony{}, fmt.Errorf("signing: replay needs at least one journaled state")
	}
	prior := states[0]
	if err := prior.Verify(); err != nil {
		return Ceremony{}, err
	}
	for _, next := range states[1:] {
		if err := next.Verify(); err != nil {
			return Ceremony{}, err
		}
		if next.ID != prior.ID || len(next.Journal) != len(prior.Journal)+1 {
			return Ceremony{}, fmt.Errorf("signing: replay breaks journal continuity")
		}
		for i := range prior.Journal {
			if next.Journal[i] != prior.Journal[i] {
				return Ceremony{}, fmt.Errorf("signing: replay rewrites journal entry %d", i)
			}
		}
		prior = next
	}
	return prior, nil
}
