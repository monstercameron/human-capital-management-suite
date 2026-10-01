package journeyclient

// validatePayOnBlur re-projects the proposal from the controlled value after
// the pay field loses focus. The shared proposalPayRangeFor/proposalPayError
// projection then supplies the same localized range message used by submit
// validation, without making a blur an RPC or inventing a second pay rule.
func (a *App) validatePayOnBlur(fieldID string) {
	if fieldID != FieldBase {
		return
	}
	a.show(a.store.Page().Notice)
}
