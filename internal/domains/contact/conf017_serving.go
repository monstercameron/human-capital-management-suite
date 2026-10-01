package contact

import "fmt"

// ValidateServingContract checks the endpoint and verification vocabularies
// that a serving cell must preserve. It is a pure contract check; provider
// observations remain non-authoritative.
func ValidateServingContract() error {
	for _, endpointType := range []EndpointType{EndpointEmail, EndpointPhone} {
		if _, _, err := NormalizeEndpoint(endpointType, servingSample(endpointType)); err != nil {
			return fmt.Errorf("contact: serving contract endpoint type %q: %w", endpointType, err)
		}
	}
	for _, state := range []VerificationState{Unverified, Verified} {
		if state != Unverified && state != Verified {
			return fmt.Errorf("contact: serving contract verification state %q is not declared", state)
		}
	}
	for _, status := range []ContactChallengeStatus{
		ContactChallengeIssued, ContactChallengeVerified, ContactChallengeExpired,
		ContactChallengeExhausted, ContactChallengeRevoked,
	} {
		if !status.Valid() {
			return fmt.Errorf("contact: serving contract challenge status %q is not declared", status)
		}
	}
	return nil
}

func servingSample(endpointType EndpointType) string {
	if endpointType == EndpointPhone {
		return "+15555550123"
	}
	return "person@example.com"
}
