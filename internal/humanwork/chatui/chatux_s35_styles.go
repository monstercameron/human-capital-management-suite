package chatui

// ChatS35Styles joins the phone, right-to-left, preferences and accent rules.
// It is the last block of the stylesheet, so each of them wins ties against the
// older rules of the same specificity.
const ChatS35Styles = ChatUX033Styles + ChatUX035Styles + ChatUX031Styles + ChatUX036Styles
