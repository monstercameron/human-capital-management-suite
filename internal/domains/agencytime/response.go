package agencytime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// ResponseStatus classifies a VMS or agency response to a delivered export.
// Malformed is a first-class status, never an error the caller has to catch:
// a payload the connector cannot parse is classified and kept, not dropped.
type ResponseStatus string

const (
	ResponseAccepted  ResponseStatus = "ACCEPTED"
	ResponseRejected  ResponseStatus = "REJECTED"
	ResponseMalformed ResponseStatus = "MALFORMED"
)

// Response is the classified outcome of one VMS or agency acceptance/
// rejection payload. RawDigest is always populated, even when Status is
// Malformed, so the original bytes remain traceable through the digest even
// though the parsed fields could not be recovered.
type Response struct {
	Status          ResponseStatus
	ExternalRef     string // the agency/VMS's own reference for the accepted export
	RejectionReason string
	RawDigest       string
}

// vmsWireResponse is the generic JSON shape a VMS or agency connector is
// expected to send back. A payload that parses but omits the fields its own
// declared status requires is still classified Malformed rather than treated
// as a partial acceptance.
type vmsWireResponse struct {
	Status      string `json:"status"`
	ExternalRef string `json:"externalRef"`
	Reason      string `json:"reason"`
}

// ParseVMSResponse classifies a raw VMS/agency response payload. It never
// returns an error for a malformed payload — the FAULT requirement is that a
// malformed acceptance/rejection payload is classified, never lost — so a
// non-error return always carries a usable Response with the raw content's
// digest recorded.
func ParseVMSResponse(raw []byte) Response {
	digest := sha256.Sum256(raw)
	rawDigest := hex.EncodeToString(digest[:])

	var wire vmsWireResponse
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Response{Status: ResponseMalformed, RawDigest: rawDigest}
	}
	switch strings.ToUpper(strings.TrimSpace(wire.Status)) {
	case "ACCEPTED":
		if strings.TrimSpace(wire.ExternalRef) == "" {
			return Response{Status: ResponseMalformed, RawDigest: rawDigest}
		}
		return Response{Status: ResponseAccepted, ExternalRef: wire.ExternalRef, RawDigest: rawDigest}
	case "REJECTED":
		if strings.TrimSpace(wire.Reason) == "" {
			return Response{Status: ResponseMalformed, RawDigest: rawDigest}
		}
		return Response{Status: ResponseRejected, RejectionReason: wire.Reason, RawDigest: rawDigest}
	default:
		return Response{Status: ResponseMalformed, RawDigest: rawDigest}
	}
}
