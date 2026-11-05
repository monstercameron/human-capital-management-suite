package workspace

import (
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// sessionWarningWindow is how close to expiry a session starts projecting
// the expiring-session warning.
const sessionWarningWindow = 5 * time.Minute

// SessionLifecycle carries the admitted session's expiry facts to the
// browser. Warning is the server's decision; the client renders it and never
// computes expiry itself.
type SessionLifecycle struct {
	SessionRef       string `json:"session_ref"`
	ExpiresAtUnix    int64  `json:"expires_at_unix"`
	Warning          bool   `json:"warning"`
	RemainingMinutes int    `json:"remaining_minutes"`
}

// resolveSessionLifecycle derives the expiry facts for an admitted principal
// at now. A missing or already expired principal yields nil: admission, not
// this projection, enforces expiry.
func resolveSessionLifecycle(principal *trust.Principal, now time.Time) *SessionLifecycle {
	if principal == nil {
		return nil
	}
	remaining := principal.ExpiresAt().Sub(now)
	if remaining <= 0 {
		return nil
	}
	return &SessionLifecycle{
		SessionRef:       principal.SessionRef(),
		ExpiresAtUnix:    principal.ExpiresAt().Unix(),
		Warning:          remaining <= sessionWarningWindow,
		RemainingMinutes: int(remaining / time.Minute),
	}
}

// applySessionLifecycle projects the expiring-session warning onto a page
// view. Healthy, absent and expired sessions leave the view unchanged.
func applySessionLifecycle(view productui.View, facts *SessionLifecycle, locale productui.LocaleContext) productui.View {
	if facts == nil || !facts.Warning {
		return view
	}
	view.SessionWarning = &productui.SessionWarningProps{
		Detail:     locale.Text("session_expiry.detail", map[string]string{"minutes": strconv.Itoa(facts.RemainingMinutes)}),
		ReauthHref: PathLogin,
	}
	return view
}
