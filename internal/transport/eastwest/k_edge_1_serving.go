package eastwest

import (
	"fmt"
	"time"
)

// ValidateServingContract checks the deny-by-default service dependency
// vocabulary used by a serving cell. It compiles a representative manifest
// and exercises both an allowed and an unauthorized path without contacting
// a peer or mutating dependency state.
func ValidateServingContract() error {
	const verified = "2026-09-03T00:00:00Z"
	const expires = "2026-12-31T23:59:59Z"
	m, err := CompileManifest(&Manifest{
		Version: 1,
		Module:  modulePath,
		Dependencies: []Dependency{{
			Consumer: "hcmnext", Dependency: "postgres-store", WorkloadID: "hcmnext",
			CellScope: "cell-local", Version: "v1", VerifiedAt: verified, ExpiresAt: expires,
		}},
	})
	if err != nil {
		return fmt.Errorf("eastwest: serving contract did not compile: %w", err)
	}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	if err := m.Allows("hcmnext", "postgres-store", "READ", "cell-local", now); err != nil {
		return fmt.Errorf("eastwest: serving contract refused declared read: %w", err)
	}
	if err := m.Allows("hcmnext", "postgres-store", "DELETE", "cell-local", now); err == nil {
		return fmt.Errorf("eastwest: serving contract admitted an unauthorized method")
	}
	return nil
}
