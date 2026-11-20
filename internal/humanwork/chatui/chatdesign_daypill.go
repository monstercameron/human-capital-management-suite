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
//
// #3843: the pill's row is an opaque strip in the list's colour. Its shadows
// paint the 16px of header fade above it and the list's side padding beside it,
// so a message scrolling to the top passes under the strip and is clipped at its
// edge instead of showing its name and time around the pill. The strip takes the
// pointer, since what is under it cannot be seen.
const chatDesignDayPillStyles = `.chat-workspace .message-list .day-divider{position:sticky;top:16px;z-index:2;justify-content:center;padding-block:4px;background:var(--surface);box-shadow:0 -16px 0 0 var(--surface),-24px 0 0 0 var(--surface),24px 0 0 0 var(--surface)}` +
	`.chat-workspace .message-list .day-divider::before,.chat-workspace .message-list .day-divider::after{content:none}` +
	`.chat-workspace .message-list .day-divider span{box-shadow:var(--hcm-shadow-resting)}` +
	`.chat-workspace .message-list{scroll-padding-top:48px}`
