package chatstream

import (
	"encoding/json"
	"errors"
	"sort"
)

// Issue 3796: one person-scoped live channel. Besides the sidebar notice, the
// channel carries two count notices so a tab learns about every room at once,
// not only the one it watches. Both carry counts only, never content.
const (
	// SignalConversationUnreadChanged tells a person that the unread or mention
	// count of one conversation they belong to moved (a post arrived).
	SignalConversationUnreadChanged = "conversation.unread.changed"
	// SignalConversationReadChanged tells a person's other tabs that their read
	// position in one conversation moved (read, or marked unread again).
	SignalConversationReadChanged = "conversation.read.changed"
)

// CountsSignal is the whole payload of a count notice: which conversation and
// the person's counts for it. Nothing else rides on it.
type CountsSignal struct {
	Host         string `json:"h"`
	Conversation string `json:"c"`
	Unread       uint64 `json:"u"`
	Mentions     uint64 `json:"m"`
}

// EncodeCountsSignal is the payload bytes of a count notice.
func EncodeCountsSignal(v CountsSignal) []byte {
	out, _ := json.Marshal(v)
	return out
}

// DecodeCountsSignal reads a count notice's payload and refuses one that names
// no conversation.
func DecodeCountsSignal(payload []byte) (CountsSignal, error) {
	var v CountsSignal
	if err := json.Unmarshal(payload, &v); err != nil {
		return CountsSignal{}, err
	}
	if v.Conversation == "" {
		return CountsSignal{}, errors.New("chatstream: count notice names no conversation")
	}
	return v, nil
}

// IsCountsSignal reports whether the signal name is one of the count notices.
func IsCountsSignal(signal string) bool {
	return signal == SignalConversationUnreadChanged || signal == SignalConversationReadChanged
}

// LiveSubject is one person with at least one open subscription.
type LiveSubject struct{ HomeTenantID, SubjectID string }

// LiveSubjects lists the people who hold an open subscription on this node, so
// a publisher computes counts only for people who can receive them at once.
func (s *Stream) LiveSubjects() []LiveSubject {
	if s == nil || s.h == nil {
		return nil
	}
	seen := map[LiveSubject]struct{}{}
	s.h.mu.RLock()
	for _, group := range s.h.subs {
		for sub := range group {
			seen[LiveSubject{HomeTenantID: sub.access.HomeTenantID, SubjectID: sub.access.SubjectID}] = struct{}{}
		}
	}
	s.h.mu.RUnlock()
	out := make([]LiveSubject, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HomeTenantID != out[j].HomeTenantID {
			return out[i].HomeTenantID < out[j].HomeTenantID
		}
		return out[i].SubjectID < out[j].SubjectID
	})
	return out
}
