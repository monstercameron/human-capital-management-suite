package clockpartner

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var requiredOpenAPIMethods = []string{"CreateEnrollmentCode", "EnrollDevice", "RevokeDevice", "SyncRoster", "IdentifyWorker", "SubmitPunches", "Heartbeat"}

// CheckOpenAPI verifies that the supplied generated OpenAPI document publishes
// the public clock service and examples. It accepts bytes from the existing
// openapidoc handler or a fetched /openapi.yaml response.
func CheckOpenAPI(document []byte) error {
	var root yaml.Node
	if err := yaml.Unmarshal(document, &root); err != nil {
		return fmt.Errorf("%w: invalid OpenAPI YAML: %v", ErrConformance, err)
	}
	text := string(document)
	if !strings.Contains(text, "paths:") || !strings.Contains(text, "hcmnext.time.v1.ClockDeviceService") {
		return fmt.Errorf("%w: ClockDeviceService is absent from OpenAPI", ErrConformance)
	}
	for _, method := range requiredOpenAPIMethods {
		if !strings.Contains(text, method) {
			return fmt.Errorf("%w: %s is absent from OpenAPI", ErrConformance, method)
		}
	}
	if !strings.Contains(text, "requestBody:") || (!strings.Contains(text, "example:") && !strings.Contains(text, "examples:")) {
		return fmt.Errorf("%w: generated OpenAPI has no example payload", ErrConformance)
	}
	return nil
}
