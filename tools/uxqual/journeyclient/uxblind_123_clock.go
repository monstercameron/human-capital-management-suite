package journeyclient

// Clock mirrors internal/humanwork/workspace.ClockConfig: the server's
// workspace-level time clock availability for the signed-in viewer
// (UXBLIND-123). It is display state only; the clock service authorizes every
// read and action itself.
type Clock struct {
	Enabled       bool `json:"enabled"`
	ViewerIsAdmin bool `json:"viewer_is_admin"`
}
