package chatrecipient

// SidebarChangedDeliveryID is the id the person-scoped "sidebar layout changed"
// notice carries on the watch stream (an ephemeral delivery with no thread). The
// delivery's body is the new layout revision in decimal. A client that sees it
// reads the layout again; it holds nothing private itself.
const SidebarChangedDeliveryID = "signal:sidebar-layout-changed"

// Issue 3796: the person-scoped count notices ride the watch stream as
// ephemeral deliveries with these ids. The body is a small JSON object that
// names one conversation and the person's counts for it (chatstream.CountsSignal);
// it holds no content.
const (
	UnreadChangedDeliveryID = "signal:conversation-unread-changed"
	ReadChangedDeliveryID   = "signal:conversation-read-changed"
)
