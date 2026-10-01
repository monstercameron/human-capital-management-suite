package journey

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
)

const historyPageCursorVersion = 1

type historyPageCursor struct {
	Principal string `json:"p"`
	Tenant    string `json:"t"`
	Filter    string `json:"f"`
	Engine    string `json:"c"`
	Version   int    `json:"v"`
	ExpiresAt int64  `json:"e"`
	Nonce     string `json:"n"`
}

// prepareHistoryPageRequest puts the engine's opaque position inside the
// transport-owned page cursor. The outer token binds the position to the
// authenticated requester, tenant, and complete request filter, so a cursor
// cannot be replayed against another result set or principal.
func (s *server) prepareHistoryPageRequest(principal *trust.Principal, inv *transport.Invocation, req *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysRequest, func(string) string, *envelope.Error) {
	if req == nil {
		req = &journeyv1.ListJourneysRequest{}
	}
	cloned, ok := proto.Clone(req).(*journeyv1.ListJourneysRequest)
	if !ok {
		return nil, nil, pageCursorRefused(inv, principal)
	}

	filter := historyPageFilterFingerprint(cloned)
	if cloned.GetPage().GetCursor() != "" {
		if len(s.deps.CursorKey) == 0 {
			// Keep the pre-codec behavior for hermetic legacy compositions that
			// have no page key and therefore never issue signed cursors.
			return cloned, func(next string) string { return next }, nil
		}
		cursor, err := decodeHistoryPageCursor(cloned.GetPage().GetCursor(), s.deps.CursorKey, s.deps.PreviousCursorKey, principal, filter, s.deps.nowFunc()())
		if err != nil {
			return nil, nil, pageCursorRefused(inv, principal)
		}
		cloned.Page.Cursor = cursor.Engine
	}

	return cloned, func(next string) string {
		if next == "" || len(s.deps.CursorKey) == 0 || principal == nil {
			return next
		}
		stamp := s.deps.nowFunc()()
		cursor, err := encodeHistoryPageCursor(historyPageCursor{
			Principal: principal.Subject(), Tenant: principal.Tenant().String(), Filter: filter,
			Engine: next, Version: historyPageCursorVersion, ExpiresAt: stamp.Add(s.deps.cursorTTL()).Unix(),
			Nonce: stamp.UTC().Format(time.RFC3339Nano),
		}, s.deps.CursorKey)
		if err != nil {
			return ""
		}
		return cursor
	}, nil
}

func historyPageFilterFingerprint(req *journeyv1.ListJourneysRequest) string {
	clone, ok := proto.Clone(req).(*journeyv1.ListJourneysRequest)
	if !ok {
		return ""
	}
	if clone.Page == nil {
		clone.Page = &commonv1.PageRequest{}
	} else {
		clone.Page.Cursor = ""
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(clone)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func encodeHistoryPageCursor(cursor historyPageCursor, key []byte) (string, error) {
	if len(key) == 0 || cursor.Principal == "" || cursor.Tenant == "" || cursor.Engine == "" {
		return "", pageCursorCodecError{}
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func decodeHistoryPageCursor(value string, key, previous []byte, principal *trust.Principal, filter string, now time.Time) (historyPageCursor, error) {
	var cursor historyPageCursor
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return cursor, pageCursorCodecError{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return cursor, pageCursorCodecError{}
	}
	sig, err := hex.DecodeString(parts[1])
	if err != nil || (!verifyHistoryPageMAC(raw, sig, key) && !verifyHistoryPageMAC(raw, sig, previous)) {
		return cursor, pageCursorCodecError{}
	}
	if err := json.Unmarshal(raw, &cursor); err != nil || principal == nil ||
		cursor.Version != historyPageCursorVersion || cursor.Engine == "" ||
		cursor.Principal != principal.Subject() || cursor.Tenant != principal.Tenant().String() ||
		cursor.Filter != filter || now.Unix() >= cursor.ExpiresAt {
		return historyPageCursor{}, pageCursorCodecError{}
	}
	return cursor, nil
}

func verifyHistoryPageMAC(raw, signature, key []byte) bool {
	if len(key) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	return hmac.Equal(mac.Sum(nil), signature)
}

type pageCursorCodecError struct{}

func (pageCursorCodecError) Error() string { return "invalid history page cursor" }
