package chatui

// issue3861Styles gives a card preview room inside the message box (#3861). The
// box is held to a third of the window, which a populated poll or to-do preview
// outgrew: its children spilled past the form and the draft field was drawn
// over the formatting toolbar. With a preview open the form may take most of
// the window; the preview takes what the draft and the toolbar leave and
// scrolls inside itself (its body scrolls, Post and Cancel stay under it); the
// draft, the formatting row and the toolbar never shrink, so they never overlap.
// The form stays overflow-visible: its mention, command and add menus open
// above it and must not be clipped. Logical
// properties only, so the layout holds right to left.
const issue3861Styles = `.chat-workspace form.chat-composer.chat-composer:has(.chatcmd003-preview){display:flex;flex-direction:column;box-sizing:border-box;min-block-size:0;max-block-size:min(80dvh,calc(100dvh - 120px))}` +
	`.chat-workspace form.chat-composer.chat-composer:has(.chatcmd003-preview) .chatcmd003-slot{flex:1 1 auto;min-block-size:0;display:flex;flex-direction:column}` +
	`.chat-workspace form.chat-composer.chat-composer .chatcmd003-slot>.chatcmd003-preview{flex:1 1 auto;min-block-size:0;max-height:none;overflow:hidden}` +
	`.chat-workspace form.chat-composer.chat-composer:has(.chatcmd003-preview) .composer-draft{flex:none}` +
	`.chat-workspace form.chat-composer.chat-composer:has(.chatcmd003-preview) .composer-draft .composer-input{max-block-size:min(20dvh,160px)}` +
	`.chat-workspace form.chat-composer.chat-composer:has(.chatcmd003-preview) :is(.composer-format-row,.composer-toolbar){flex:none}`
