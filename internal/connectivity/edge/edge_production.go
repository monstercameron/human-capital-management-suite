package edge

import "strings"

// ProductionManifest is the selected implementation record for the served
// HCM Next edge. It is intentionally an infrastructure contract: products
// remain adapters to the endpoint and authorization registries, while this
// record fixes the implementation, route, trust budgets, egress destination,
// and replacement path that must be qualified before serving.
//
// The address values are deployment-owned identifiers, not customer data. A
// deployment changes them by publishing a new qualified manifest; it does
// not mutate this record at runtime or fall back to a wildcard/default route.
func ProductionManifest() Manifest {
	return Manifest{
		SchemaVersion:         schemaVersion,
		QualificationID:       "qualification:hcmnext-edge:production:v1",
		ImplementationRef:     "envoy-gateway/hcmnext-edge",
		ImplementationVersion: "v1.29.5",
		Routes: []Route{
			{
				ID:                "intent-simulate",
				Host:              "api.hcmnext.internal",
				PathPrefix:        "/hcmnext.intents.v1.IntentService/SimulateIntent",
				Methods:           []string{"POST"},
				Backend:           "hcmnext-http-edge",
				TLSVersion:        "TLS1.3",
				CertificateRef:    "cert:hcmnext-api",
				CertificateDigest: "sha256:" + strings.Repeat("b", 64),
				DNSName:           "api.hcmnext.internal",
				DNSIPs:            []string{"10.42.0.10"},
				WAFRef:            "waf:hcmnext-strict",
				WAFVersion:        "2026.09.01",
				OwnerRef:          "owner:platform-edge",
				MaxRPS:            1000,
				BodyLimitBytes:    1048576,
				FailureAction:     "DENY",
			},
		},
		EastWest: []EastWestRule{
			{
				FromService: "hcmnext-http-edge",
				ToService:   "hcmnext-cell",
				Protocol:    "h2",
				Port:        8443,
				OwnerRef:    "owner:platform-edge",
				FailClosed:  true,
			},
		},
		Egress: []EgressRule{
			{
				ID:            "provider-observation",
				Destination:   "provider-observation.hcmnext.internal",
				Port:          443,
				Purpose:       "provider-observation",
				OwnerRef:      "owner:integrations",
				Logged:        true,
				RedactionRef:  "redaction:provider-observation-v1",
				FailureAction: "DENY",
			},
		},
		Outage: OutagePlan{
			DetectionSignal: "telemetry.edge.error_budget",
			DegradedAction:  "deny-new-effects",
			ReplacementRef:  "envoy-gateway/hcmnext-edge:v1.30.0",
			MigrationPlan:   "drain-requalify-and-cutover",
		},
	}
}

// QualifyProduction compiles the selected served-edge implementation. A
// caller must treat any error as a composition failure; no unqualified edge
// is allowed to publish a listener.
func QualifyProduction() (Evidence, error) {
	return Compile(ProductionManifest())
}
