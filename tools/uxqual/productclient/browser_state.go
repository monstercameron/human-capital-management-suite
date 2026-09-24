package productclient

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Browser state is a presentation hint, never a snapshot of product truth.
// The browser may retain the HistoryRouter's bounded forward watermark and a
// small chat destination frequency list for this viewer's current session.
// Preferences, workflow state, authorization and transactions remain owned
// by the server and are deliberately outside this contract.
const (
	BrowserStateVersion        = "hcm-next.browser-state.v1"
	HistoryStorageKeyPrefix    = BrowserStateVersion + ".history."
	ChatVisitsStorageKeyPrefix = BrowserStateVersion + ".chat-visits."
	HistoryLedgerRandomBytes   = 16
	HistoryLedgerIDLength      = HistoryLedgerRandomBytes * 2
	// Browser history implementations cannot materialize anywhere near this
	// many entries, but the bound keeps hostile storage finite without making a
	// normal long-lived tab silently lose forward semantics.
	MaxHistoryIndex          = 2_147_483_647
	MaxChatVisitEntries      = 32
	MaxChatVisitIDLength     = 256
	MaxChatVisitCount        = 1_000_000
	MaxChatVisitPayloadBytes = 8192
)

var (
	ErrBrowserStateKey       = errors.New("browser state key is not an allowed presentation key")
	ErrBrowserStateValue     = errors.New("browser state value is not a bounded presentation record")
	ErrBrowserStateAuthority = errors.New("browser state cannot contain authority or business state")
)

// HistoryStorageKey returns the sole key shape that production code may write
// to browser session storage. An empty result means id is not the syntax
// produced by the history-ledger minting path. Syntax validation alone cannot
// prove where an identifier came from, so callers must still mint it locally
// and must never derive it from tenant, principal, session or workflow data.
func HistoryStorageKey(id string) string {
	if !ValidHistoryLedgerID(id) {
		return ""
	}
	return HistoryStorageKeyPrefix + id
}

// ChatVisitsStorageKey accepts only a SHA-256 digest of the current tenant and
// principal, so tab-local rankings cannot be shared between viewers and the
// browser key does not disclose either identity.
func ChatVisitsStorageKey(scopeDigest string) string {
	if len(scopeDigest) != 64 {
		return ""
	}
	for _, r := range scopeDigest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return ChatVisitsStorageKeyPrefix + scopeDigest
}

// ChatVisit is a bounded navigation frequency hint, not a membership grant.
type ChatVisit struct {
	ConversationID string `json:"id"`
	Count          int    `json:"count"`
}

// EncodeChatVisits creates the only supported chat-frequency session record.
// Conversation IDs are opaque and are never interpreted as access authority.
func EncodeChatVisits(visits []ChatVisit) (string, bool) {
	if len(visits) > MaxChatVisitEntries {
		return "", false
	}
	seen := make(map[string]bool, len(visits))
	for _, visit := range visits {
		if !validChatVisitID(visit.ConversationID) || visit.Count < 1 || visit.Count > MaxChatVisitCount || seen[visit.ConversationID] {
			return "", false
		}
		seen[visit.ConversationID] = true
	}
	data, err := json.Marshal(visits)
	if err != nil || len(data) > MaxChatVisitPayloadBytes {
		return "", false
	}
	return string(data), true
}

// DecodeChatVisits treats session storage as untrusted and fails closed.
func DecodeChatVisits(raw string) ([]ChatVisit, bool) {
	if len(raw) == 0 || len(raw) > MaxChatVisitPayloadBytes {
		return nil, false
	}
	var visits []ChatVisit
	if err := json.Unmarshal([]byte(raw), &visits); err != nil {
		return nil, false
	}
	encoded, ok := EncodeChatVisits(visits)
	if !ok || encoded != raw {
		return nil, false
	}
	return visits, true
}

func validChatVisitID(id string) bool {
	if id == "" || len(id) > MaxChatVisitIDLength || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ValidHistoryLedgerID accepts the exact lowercase hexadecimal syntax emitted
// for a 128-bit history-ledger nonce. It rejects delimiters, URLs and control
// characters, but deliberately makes no semantic claim about an arbitrary
// caller-supplied string; origin is guaranteed only by the random minting path.
func ValidHistoryLedgerID(id string) bool {
	if len(id) != HistoryLedgerIDLength {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// EncodeHistoryIndex validates and serializes a high-water mark. Keeping the
// value decimal and bounded makes malformed or hostile storage harmless.
func EncodeHistoryIndex(index int) (string, bool) {
	if index < 0 || index > MaxHistoryIndex {
		return "", false
	}
	return strconv.Itoa(index), true
}

// DecodeHistoryIndex parses an untrusted browser-storage value. Invalid,
// negative, oversized and non-canonical values fail closed to (0, false).
func DecodeHistoryIndex(raw string) (int, bool) {
	if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > 10 {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	if len(raw) > 1 && raw[0] == '0' {
		return 0, false
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 || index > MaxHistoryIndex {
		return 0, false
	}
	return index, true
}

// ValidateHistoryStorageEntry is the adapter boundary for sessionStorage.
// Callers must validate both the key and value before setItem, and must treat
// a validation error as a no-op. This means a copied browser storage object
// cannot introduce credentials, tenant data, workflow truth, or arbitrary
// JSON into the product client.
func ValidateHistoryStorageEntry(key, value string) error {
	if !strings.HasPrefix(key, HistoryStorageKeyPrefix) ||
		!ValidHistoryLedgerID(strings.TrimPrefix(key, HistoryStorageKeyPrefix)) {
		return ErrBrowserStateKey
	}
	if _, ok := DecodeHistoryIndex(value); !ok {
		return ErrBrowserStateValue
	}
	return nil
}

// ValidateBrowserStorageWrite is intentionally stricter than a generic
// browser-storage helper: this product has only bounded history and chat
// frequency presentation records. Any other key that resembles business,
// identity, authorization, workflow, transaction or credential state is
// refused with the same stable authority error used by policy tests.
func ValidateBrowserStorageWrite(key, value string) error {
	if strings.HasPrefix(key, ChatVisitsStorageKeyPrefix) {
		if ChatVisitsStorageKey(strings.TrimPrefix(key, ChatVisitsStorageKeyPrefix)) == "" {
			return ErrBrowserStateAuthority
		}
		if _, ok := DecodeChatVisits(value); !ok {
			return ErrBrowserStateValue
		}
		return nil
	}
	if err := ValidateHistoryStorageEntry(key, value); err != nil {
		if errors.Is(err, ErrBrowserStateValue) {
			return err
		}
		return ErrBrowserStateAuthority
	}
	return nil
}

// ValidateBrowserStorageRead limits reads to the same two presentation-only
// records the browser adapter is permitted to retain.
func ValidateBrowserStorageRead(key string) error {
	if strings.HasPrefix(key, ChatVisitsStorageKeyPrefix) {
		if ChatVisitsStorageKey(strings.TrimPrefix(key, ChatVisitsStorageKeyPrefix)) == "" {
			return ErrBrowserStateAuthority
		}
		return nil
	}
	if err := ValidateHistoryStorageEntry(key, "0"); err != nil {
		return ErrBrowserStateAuthority
	}
	return nil
}
