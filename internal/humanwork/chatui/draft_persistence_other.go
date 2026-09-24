//go:build !(js && wasm)

package chatui

type unavailableDraftPersistence struct{}

func newDraftPersistence() draftPersistence                        { return unavailableDraftPersistence{} }
func (unavailableDraftPersistence) load(string) map[string]string  { return nil }
func (unavailableDraftPersistence) save(string, map[string]string) {}
func (unavailableDraftPersistence) clear()                         {}

func installDraftLogoutListener(func()) func() { return func() {} }
