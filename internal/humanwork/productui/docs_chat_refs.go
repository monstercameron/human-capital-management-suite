package productui

import "time"

// DocumentChatRefs is the reader-safe projection of chat references attached
// to a document. It mirrors the Docs presentation contract without exposing
// the transport package's wire model to product UI code.
type DocumentChatRefs struct {
	Channels []DocumentChatChannelReference
	People   []DocumentChatPersonReference
	Messages []DocumentChatMessageReference
}

// DocumentChatChannelReference describes a channel referenced by a document.
type DocumentChatChannelReference struct {
	Key, ConversationID, Name string
	MemberCount               int
	Locked, Joined, Private   bool
}

// DocumentChatPersonReference describes a resolved person reference.
type DocumentChatPersonReference struct {
	Key, SubjectID, DisplayName string
}

// DocumentChatMessageReference contains message details only when Readable;
// otherwise the opaque Token can be retained without revealing message data.
type DocumentChatMessageReference struct {
	Token, ConversationID, ChannelName, PostID, AuthorID, AuthorName, Body string
	CreatedAt                                                              time.Time
	Readable                                                               bool
}
