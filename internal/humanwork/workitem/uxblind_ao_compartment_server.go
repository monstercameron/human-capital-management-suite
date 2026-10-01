//go:build !js

package workitem

import (
	"github.com/monstercameron/human-capital-management-suite/internal/engines/messagetemplate"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

// DeriveMessage scans one derived message for forbidden compartment
// material. A derived message carrying medical facts refuses: the scan
// verdict, not the sender, decides.
func DeriveMessage(renderedSubject, renderedBody string, metadata map[string]string, forbidden []string) (delivery.ScanResult, error) {
	return delivery.Scan(
		messagetemplate.Rendered{Subject: renderedSubject, Body: renderedBody},
		delivery.OperationalSurfaces{Metadata: metadata},
		forbidden,
	)
}
