//go:build js && wasm

package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// Issues #3837, #3682: the reconciler can append a select's own options to it
// again on the commit after the select is mounted, and the browser then leaves
// the last option chosen. That is not particular to Chat, so the guard is in
// place before the first render of any page this client draws; Chat installs the
// same guard (it is installed once) for a host that is not this client.
func init() {
	chatui.InstallSelectReappendGuard()
}
