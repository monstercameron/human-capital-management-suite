package chatui

// issue3760ModerationPhoneStyles holds two phone repairs for the Moderation page.
//
// #3760: opened from the conversation list, the page starts at the top of the
// window, where the shell keeps the bell and the account menu as a fixed strip at
// the end of the list's bar (chatPhoneShellStylesheet). The strip is above the
// page, so it covered the Close button. The page's heading leaves the strip's
// width at its end, the same room the list's own heading leaves
// (ChatUX045Styles), in logical properties so it holds in right-to-left.
//
// #3762: the tabs are one segmented control with a count each. A label or a count
// never wraps (a long German word, an Arabic phrase, Arabic digits); the pill
// grows to the height of its buttons (44px on a phone), and a row wider than the
// screen scrolls sideways instead of breaking a word.
const issue3760ModerationPhoneStyles = `
@media(max-width:760px){.chat-workspace[data-sidebar-open="true"] .chatremove-overlay-page .chatmod005-heading.chat-panel-head{padding-inline-end:calc(2 * var(--hcm-control-height) + var(--hcm-space-1))}}
:root .chatremove .chatmod005-tabs.chatsave-seg{flex-wrap:nowrap;max-inline-size:100%;block-size:auto;overflow-x:auto;overflow-y:hidden;overscroll-behavior-x:contain;scrollbar-width:thin}
:root .chatremove .chatmod005-tabs .chatmod005-tab.chatsave-seg-button{flex:1 0 auto;white-space:nowrap;overflow-wrap:normal;word-break:normal;hyphens:manual}
:root .chatremove .chatmod005-tabs .chatsave-seg-count{white-space:nowrap;unicode-bidi:isolate}
`
