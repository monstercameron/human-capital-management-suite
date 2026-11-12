package productui

// chatDesignSearchStylesheet lowers the weight of the workspace search while
// Chat is on screen (#3775). Chat has its own search, which finds messages;
// the top-bar field finds pages and people and is the secondary one here. At
// rest it is a short field with a muted glyph and placeholder instead of the
// widest control on the page; with focus it is the full-width field again, so
// the results panel under it keeps its size. The field's boundary keeps the
// control-border colour: lowering that would cost the 3:1 control contrast.
//
// The rule reads the route from the page itself with :has(), the same hook
// the phone shell rules use: the shell has no other marker for a Chat route.
const chatDesignSearchStylesheet = `.app-shell:has(.chat-workspace) .global-search:not(:focus-within){max-inline-size:320px}` +
	`.app-shell:has(.chat-workspace) .global-search:not(:focus-within) .global-search-input{background:transparent}` +
	`.app-shell:has(.chat-workspace) .global-search:not(:focus-within) .global-search-glyph,.app-shell:has(.chat-workspace) .global-search:not(:focus-within) .global-search-input::placeholder{color:var(--muted)}`
