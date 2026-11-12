package chatui

// chatDesignDayPillStyles keeps the day pill whole (#3773). The message list
// fades its first 16px under the conversation header (the mask in styles.go),
// so a day pill scrolled to the top edge was drawn half-faded and cut by the
// header. The pill now sticks 16px below the header edge, the first pixel the
// mask leaves fully opaque, and stays there while that day's messages pass
// under it: it is never inside the fade and never under the header.
//
// A stuck rule line would strike through the messages scrolling behind it, so
// the day separator in the list is the centred pill alone; the row lets
// pointer events through to the messages it floats over. Jumps and reveals
// park below the pill (scroll-padding), so a landed message is not hidden by
// it. In the virtual list the pill sticks for as long as its own row is in
// view.
const chatDesignDayPillStyles = `.chat-workspace .message-list .day-divider{position:sticky;top:16px;z-index:2;justify-content:center;pointer-events:none}` +
	`.chat-workspace .message-list .day-divider::before,.chat-workspace .message-list .day-divider::after{content:none}` +
	`.chat-workspace .message-list .day-divider span{box-shadow:var(--hcm-shadow-resting)}` +
	`.chat-workspace .message-list{scroll-padding-top:48px}`
