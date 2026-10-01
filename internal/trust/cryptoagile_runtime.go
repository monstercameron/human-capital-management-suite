package trust

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/cryptoagile"
)

// CryptoAgilityRuntime is the served trust boundary's crypto-agility
// composition. The alias keeps algorithm registry configuration behind the
// cryptoagile package while making the production seam reachable wherever the
// trust package is already composed.
type CryptoAgilityRuntime = cryptoagile.Runtime

// NewCryptoAgilityRuntime constructs a value-owned registry, dual signer and
// dual-read verifier for one declared migration plan.
func NewCryptoAgilityRuntime(plan cryptoagile.MigrationPlan, suites []cryptoagile.AlgorithmSuite, keys cryptoagile.KeySource, now func() time.Time) (*CryptoAgilityRuntime, error) {
	return cryptoagile.NewRuntime(plan, suites, keys, now)
}
