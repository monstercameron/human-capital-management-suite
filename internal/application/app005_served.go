package application

import (
	"time"

	connectivityapplication "github.com/monstercameron/human-capital-management-suite/internal/connectivity/application"
)

// ComponentInstallationLifecycle names the governed installation lifecycle
// composed by the serve role. Keeping this in the application graph makes
// the connectivity lifecycle reachable from the shipped command without
// moving its upgrade, quarantine, or revocation rules out of their semantic
// owner.
const ComponentInstallationLifecycle = "installation-lifecycle"

const installationLifecyclePropagationSLO = 5 * time.Minute

func composeInstallationLifecycle(now func() time.Time) (*connectivityapplication.Manager, error) {
	return connectivityapplication.NewManager(installationLifecyclePropagationSLO, now)
}

// InstallationLifecycle returns the serve role's governed installation
// lifecycle, or nil for an uncomposed application role.
func (a *App) InstallationLifecycle() *connectivityapplication.Manager {
	if a == nil {
		return nil
	}
	return a.installationLifecycle
}
