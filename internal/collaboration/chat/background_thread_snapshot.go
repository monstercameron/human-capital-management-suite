package chat

import "context"

// BackgroundThreadSnapshotRequest binds a tenant-scoped server recheck to the
// invoker recorded by an already admitted persona invocation. ReaderID is data
// for the store's current-membership predicate, not an authenticated principal.
type BackgroundThreadSnapshotRequest struct {
	TenantID, ReaderID, ConversationID string
	ThreadID, InvokingPostID           string
	Limit                              int
}

// BackgroundThreadSnapshot is a canonical bounded chat image captured for a
// background recheck. It deliberately carries reader identity as provenance,
// without constructing or asserting a human Principal.
type BackgroundThreadSnapshot struct {
	TenantID, ConversationID, ThreadID, InvokingPostID string
	ReaderTenantID, ReaderID                           string
	Revision, AuthorityRevision                        uint64
	Posts                                              []Post
	SnapshotID, Digest                                 string
}

// BackgroundThreadSnapshotSource is implemented only by the tenant-scoped
// chat store. It must enforce current membership and history visibility while
// reading the exact invoking post and bounded thread in one transaction.
type BackgroundThreadSnapshotSource interface {
	CaptureBackgroundThreadSnapshot(context.Context, BackgroundThreadSnapshotRequest) (BackgroundThreadSnapshot, error)
}
