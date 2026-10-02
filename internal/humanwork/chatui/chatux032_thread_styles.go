package chatui

// ChatUX032ThreadStyles holds the rules of three todos that touch the same few
// elements (the shared panel header and buttons are ChatUX032Styles), joined
// after it so they win ties:
//
//   - CHATBUG-089: the document card's text is one size, its cut text ends at a
//     sentence or word, and "Show more" opens the rest in place under the card.
//   - CHATUX-032: the thread's reply composer is the channel composer in compact
//     form, with "Also send to #channel" as a quiet line of the tool row.
//   - CHATUX-034: one focus ring on the emoji search, a labelled skin-tone
//     control, one-line "/" rows (the argument hint only on the highlighted
//     row), add-menu rows of one height whose highlight runs edge to edge, no
//     chevron on New section, and a saved bookmark that is filled, not coloured.
const ChatUX032ThreadStyles = `
.chat-embed-body{font-size:.875rem;line-height:1.45}
.chat-doc-embed-wrap{display:grid;inline-size:min(100%,520px);margin-block-start:8px}
.chat-doc-embed-wrap>.chat-embed{margin-block-start:0;inline-size:100%;max-block-size:none;border-end-start-radius:0;border-end-end-radius:0}
.chat-doc-embed-wrap:has(>.chat-embed-more[open])>.chat-embed .chat-embed-body{display:none}
.chat-embed-more{box-sizing:border-box;padding:0 12px 8px;border:1px solid var(--line);border-block-start:0;border-inline-start:3px solid var(--accent);border-end-start-radius:var(--hcm-radius-control);border-end-end-radius:var(--hcm-radius-control);background:var(--soft);font-size:.8125rem}
.chat-embed-more>summary{display:inline-block;list-style:none;cursor:pointer;color:var(--hcm-color-brand-primary);font-weight:600}
.chat-embed-more>summary::-webkit-details-marker{display:none}
.chat-embed-more>summary:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px;border-radius:2px}
.chat-embed-more .chat-embed-more-close,.chat-embed-more[open] .chat-embed-more-open{display:none}
.chat-embed-more[open] .chat-embed-more-close{display:inline}
.chat-embed-more .chat-embed-full{margin:4px 0 0;white-space:pre-wrap;max-height:none;unicode-bidi:plaintext;overflow-wrap:anywhere}
.chat-workspace .thread-composer .composer-toolbar{flex-wrap:wrap;align-items:center;gap:2px 6px}
.chat-workspace .thread-composer .composer-toolbar .thread-also{order:3;flex:1 1 100%;min-block-size:24px;padding:0 4px 2px;font-size:.75rem}
.chat-workspace .thread-composer .composer-toolbar .thread-also input{inline-size:14px;block-size:14px}
.chat-workspace .emoji-pop .emoji-pop-input:is(:focus,:focus-visible){outline:0;border-color:var(--hcm-color-focus);box-shadow:0 0 0 1px var(--hcm-color-focus)}
.chat-workspace .emoji-pop-tone-wrap{display:inline-flex;align-items:center;gap:6px;flex:none}
.chat-workspace .emoji-pop-tone-label{color:var(--hcm-color-text-muted);font-size:.75rem;white-space:nowrap}
.chat-workspace .command-option{flex-wrap:nowrap;align-items:center}
.chat-workspace .command-option .command-args{display:none}
.chat-workspace .command-option:is(.active,[aria-selected="true"]){flex-wrap:wrap}
.chat-workspace .command-option:is(.active,[aria-selected="true"]) .command-args{display:block}
.chat-workspace .composer-add-menu{padding-inline:0}
.chat-workspace .composer-add-item{min-block-size:56px;border-radius:0;padding-inline:12px}
.chat-workspace .composer-add-item .composer-add-note{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chat-workspace .composer-add-item .composer-add-text{min-inline-size:0}
.chatux002-layer .section-create-trigger>.chat-icon{display:none}
.chat-workspace .message-actions .chatsave-action[aria-pressed="true"]{color:var(--hcm-color-text)}
`
