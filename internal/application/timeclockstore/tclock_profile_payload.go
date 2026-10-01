package timeclockstore

import (
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// EncodeProfilePayload is the one encoder of the profile document
// TimestoreProfileResolver decodes. An open-ended profile has no effective-to
// instant, and an unset values.Instant refuses to marshal, so the field is
// omitted rather than written; decoding leaves it unset again. A profile that
// does not validate is never persisted.
func EncodeProfilePayload(profile timeprofile.TimeProfile) ([]byte, error) {
	if err := profile.Validate(); err != nil {
		return nil, fmt.Errorf("timeclockstore: refusing to persist an invalid time profile: %w", err)
	}
	// The outer EffectiveTo shadows the embedded one, so exactly one
	// EffectiveTo is written, and only when the profile has an end.
	persisted := struct {
		timeprofile.TimeProfile
		EffectiveTo *values.Instant `json:"EffectiveTo,omitempty"`
	}{TimeProfile: profile}
	if profile.EffectiveTo.IsSet() {
		end := profile.EffectiveTo
		persisted.EffectiveTo = &end
	}
	return json.Marshal(persisted)
}
