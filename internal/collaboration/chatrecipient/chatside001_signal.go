package chatrecipient

// SidebarChangedDeliveryID is the id the person-scoped "sidebar layout changed"
// notice carries on the watch stream (an ephemeral delivery with no thread). The
// delivery's body is the new layout revision in decimal. A client that sees it
// reads the layout again; it holds nothing private itself.
const SidebarChangedDeliveryID = "signal:sidebar-layout-changed"
