package chatui

import (
	"encoding/base64"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// Issue3896RowToken is how an opened result is found again when the person
// returns to the results (#3896): the same encoded target and kind the result's
// open control carries in data-target and data-kind, joined by a NUL.
func Issue3896RowToken(row chatsearch.Row) string {
	target, _ := json.Marshal(row.Target)
	return base64.RawURLEncoding.EncodeToString(target) + "\x00" + string(row.Kind)
}
