package chatui

// ModAdminText lets the real-catalog tests in package chatui_test read the
// administrator's copy tables the way the panel does.
var ModAdminText = modadminText

// ModAdminCopyKeys lists every key of the administrator's copy tables.
func ModAdminCopyKeys() []string {
	keys := make([]string, 0, len(modadminCopy))
	for key := range modadminCopy {
		keys = append(keys, key)
	}
	return keys
}
