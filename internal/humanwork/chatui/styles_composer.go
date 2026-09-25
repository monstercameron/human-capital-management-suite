package chatui

// composerPolishStyles covers the mention list, the formatting toolbar, the
// rail's "Add channels" row and the richer search rows. It is appended to
// Stylesheet so it shares the same scope, tokens and dark-mode re-declarations.
const composerPolishStyles = `.thread-composer{position:relative}` +
	`.mention-menu{position:absolute;inset-inline-start:0;width:min(340px,100%);inset-block-end:calc(100% + 6px);z-index:32;display:flex;flex-direction:column;max-height:min(320px,50vh);overflow-y:auto;padding:6px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised)}` +
	`.mention-heading{margin:2px 8px 6px;color:var(--muted);font-size:.75rem;font-weight:600}` +
	`.mention-option{display:flex;align-items:center;gap:10px;min-height:34px;padding:4px 8px;border:0;border-radius:var(--hcm-radius-control);background:none;color:var(--ink);font:inherit;font-size:.875rem;text-align:start;cursor:pointer}` +
	`.mention-option:hover{background:color-mix(in srgb,var(--ink) 6%,transparent)}` +
	`.mention-option.active{background:var(--accent);color:var(--on-brand,#fff)}.mention-option.active .mention-detail{color:inherit;opacity:.85}` +
	`.mention-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:600}` +
	`.mention-detail{color:var(--muted);font-size:.75rem;white-space:nowrap}` +
	`.mention-hint{margin:6px 8px 2px;padding-top:6px;border-top:1px solid var(--line);color:var(--muted);font-size:.6875rem}` +
	`.mention-empty{margin:4px 8px;color:var(--muted);font-size:.8125rem}` +
	`.format-tools{display:flex;align-items:center;gap:1px;padding-inline-end:6px;margin-inline-end:4px;border-inline-end:1px solid var(--line)}` +
	`@media(hover:hover){.tool-button:hover:not(:disabled){background:color-mix(in srgb,var(--ink) 8%,transparent);color:var(--ink)}}` +
	`.chat-row.rail-add{color:var(--muted);font-size:.875rem}.chat-row.rail-add .kind-glyph{border-radius:5px;background:color-mix(in srgb,var(--ink) 8%,transparent)}.chat-row.rail-add .kind-glyph .chat-icon{width:14px;height:14px}` +
	`.search-hit{background:color-mix(in srgb,var(--hcm-color-warning) 32%,transparent);color:inherit;border-radius:2px;padding:0 1px}` +
	`.search-result.search-message-result,.search-result.search-person-result{flex-direction:row;align-items:flex-start;gap:10px}` +
	`.search-person-result{align-items:center}` +
	`.search-result-main{display:flex;flex-direction:column;gap:2px;min-width:0;flex:1}` +
	`.search-result-meta{display:flex;align-items:baseline;flex-wrap:wrap;gap:4px 8px}` +
	`.search-result .search-result-time{font-size:.75rem}` +
	`.search-result .search-result-snippet{color:var(--ink);font-size:.875rem;white-space:normal}` +
	`.channel-todo-count:empty{display:none}` +
	`.attachment-image.measured[data-frame-width]{width:attr(data-frame-width px,240px);aspect-ratio:attr(data-frame-w type(<number>),4)/attr(data-frame-h type(<number>),3)}` +
	// Round-2 critique: Slack's scanning cues and a composer that starts short.
	`.message-author{font-weight:700}.message-time{font-weight:400}` +
	`.chat-composer .composer-input{min-height:44px;resize:none;field-sizing:content}.thread-composer .composer-input{resize:none;field-sizing:content;max-height:30vh}` +
	`.chat-composer .composer-input:focus-visible,.thread-composer .composer-input:focus-visible{outline:2px solid transparent}` +
	`.mention-option.active{background:color-mix(in srgb,var(--accent) 16%,transparent);color:var(--ink);box-shadow:inset 3px 0 0 var(--accent)}.mention-heading.outside{margin-top:8px}` +
	`.chat-row.selected,.chat-row.selected:hover{background:var(--accent);color:var(--on-brand,#fff)}.chat-row.selected::before{display:none}.chat-row.selected .kind-glyph{opacity:1}.chat-row.selected .chat-badge{background:var(--on-brand,#fff);color:var(--accent)}` +
	`.section-count{display:none}` +
	`.conversation-heading h1{line-height:1.3}.conversation-topic{line-height:1.35}` +
	`.mention-hint{position:sticky;bottom:-6px;margin-bottom:-6px;padding-bottom:8px;background:var(--surface)}.rail-search .chat-icon{top:calc(50% - 4px);transform:translateY(-50%)}` +
	`.chat-rail-row{position:relative}.chat-rail-row .rail-row-more{position:absolute;inset-inline-end:2px;top:2px;height:28px;width:26px;z-index:1}.chat-rail-row:has(.chat-row.selected) .rail-row-more{color:var(--on-brand,#fff)}.chat-rail-row:hover .chat-badge,.chat-rail-row:focus-within .chat-badge{visibility:hidden}` +
	`@media(hover:none){.chat-rail-row .rail-row-more{position:static}.chat-rail-row:hover .chat-badge{visibility:visible}.attachment-image>.attachment-download{display:none}}` +
	`.message-stats{font-weight:700;padding:3px 8px;margin-inline-start:-8px;border-radius:6px}.message-stats:hover{background:var(--soft);border-color:var(--line)}` +
	`.side-heading{border-bottom:1px solid var(--line);margin-inline:-16px;padding-inline:16px}` +
	// Round-3 critique.
	`.chat-composer .composer-input,.chat-composer .composer-input:focus,.chat-composer .composer-input:focus-visible,.thread-composer .composer-input,.thread-composer .composer-input:focus,.thread-composer .composer-input:focus-visible{border:0;outline:0;box-shadow:none;background:transparent}` +
	`.chat-composer .composer-toolbar{min-height:36px;padding-top:2px}` +
	`.mention-menu{max-height:min(372px,50vh)}` +
	`.chat-row.selected,.chat-row.selected:hover{background:color-mix(in srgb,var(--accent) 28%,var(--canvas));color:var(--ink)}.chat-row.selected .chat-badge{background:var(--accent);color:var(--on-brand,#fff)}.chat-rail-row:has(.chat-row.selected) .rail-row-more{color:var(--ink)}` +
	`.chat-workspace:has(.chat-search-results) .chat-row.selected{background:transparent;color:var(--muted);font-weight:500}` +
	`.thread-count{display:flex;align-items:center;gap:10px;margin:12px 0 2px;color:var(--muted);font-size:.75rem;font-weight:600}.thread-count::after{content:"";flex:1;border-top:1px solid var(--line)}.thread-root{border-bottom:0;padding-bottom:4px}` +
	`.thread-more{opacity:0}.thread-message:hover .thread-more,.thread-message:focus-within .thread-more,.thread-root:hover .thread-more,.thread-root:focus-within .thread-more,.thread-more[aria-expanded="true"]{opacity:1}@media(hover:none){.thread-more{opacity:1}}` +
	`.quick-react{font-size:1rem;line-height:1}` +
	// Round-7 critique.
	`.thread-root-body .message-meta{position:relative}.thread-view-in-channel{position:absolute;inset-inline-end:34px;top:2px;pointer-events:none}.thread-root:hover .thread-view-in-channel,.thread-view-in-channel:focus-visible{pointer-events:auto}@media (hover:none),(max-width:760px){.thread-view-in-channel{opacity:1;pointer-events:auto}}` +
	`.chat-workspace:has(.chat-search-results) .conversation-header{animation:none;box-shadow:none}` +
	`.composer-tools>button.tool-button[disabled]:not(.giphy-trigger):not(.emoji-trigger){display:none}` +
	`.rail-pill{position:relative}.rail-pill-dot{position:absolute;top:5px;inset-inline-end:5px;width:8px;height:8px;border-radius:50%;background:var(--accent);box-shadow:0 0 0 2px var(--surface)}` +
	`@container chatmain (max-width:560px){.chat-composer:not(:focus-within){flex-direction:row;align-items:center;padding:4px 6px 4px 4px}.chat-composer:not(:focus-within) .composer-input{flex:1;min-height:40px;padding:9px 8px}.chat-composer:not(:focus-within) .composer-toolbar{border-top:0;min-height:0;padding:0;flex:none}.chat-composer:not(:focus-within) .composer-tools,.chat-composer:not(:focus-within) .composer-embeds{display:none}}` +
	`@media(max-width:760px){.thread-composer:not(:focus-within) .format-tools{display:none}}` +
	// Round-6 critique.
	`.chat-workspace:has(.chat-search-results) .chat-row.selected{box-shadow:none}` +
	`.rail-pill .icon-arrow-left{display:none}@media(max-width:760px){.rail-pill .icon-panel-left{display:none}.rail-pill .icon-arrow-left{display:inline}}` +
	`.chat-main{timeline-scope:--chat-list}.message-list{scroll-timeline:--chat-list block}.conversation-header{box-shadow:none;animation:chat-header-lift linear both;animation-timeline:--chat-list;animation-range:0 24px}@keyframes chat-header-lift{from{box-shadow:none}to{box-shadow:0 6px 10px -8px color-mix(in srgb,var(--ink) 30%,transparent)}}` +
	`.channel-todo-trigger,.channel-poll-trigger{border-color:transparent;background:transparent}.channel-todo-trigger:hover,.channel-poll-trigger:hover{border-color:var(--line);background:var(--soft)}` +
	`.chat-search-results>.search-count{position:absolute;clip:rect(0 0 0 0);clip-path:inset(50%);width:1px;height:1px;overflow:hidden;white-space:nowrap}.search-result-group:first-of-type{margin-top:0}.search-group-heading{display:flex;align-items:baseline;gap:8px}.search-group-count{font-weight:500;color:var(--muted)}.search-result-context{display:inline-flex;align-items:center;gap:4px}.search-result-context .avatar.tiny,.search-result-context .kind-glyph{width:16px;height:16px}` +
	`.thread-view-in-channel{border:0;background:none;padding:0;color:var(--muted);font:inherit;font-size:.75rem;cursor:pointer;margin-inline-start:auto;opacity:0}.thread-root:hover .thread-view-in-channel,.thread-view-in-channel:focus-visible{opacity:1;color:var(--accent)}@media(hover:none){.thread-view-in-channel{opacity:1}}` +
	`.chat-composer{padding-bottom:3px}.chat-composer .composer-toolbar{min-height:36px;padding:2px 2px 0;align-items:center}` +
	`@media(pointer:coarse){.message-action,.reaction.add,.conversation-header .icon-button,.conversation-header .channel-todo-trigger,.conversation-header .channel-poll-trigger,.conversation-header .rail-pill,.thread-more,.tool-button{min-width:44px;min-height:44px}}` +
	`@container chatmain (max-width:560px){.chat-composer:not(:focus-within) .format-tools{display:none}}` +
	// Round-5 critique.
	`.chat-row.selected,.chat-row.selected:hover{box-shadow:inset 3px 0 0 var(--accent)}` +
	`.mention-hint{font-size:.75rem;letter-spacing:.01em}` +
	`.chat-workspace:has(.chat-search-results) .conversation-header{box-shadow:none}.chat-search-results .search-result{max-width:840px}.chat-search-results .search-result-snippet{max-width:none}` +
	`@media(min-width:761px){.chat-composer .send-button{height:32px;min-height:32px}}` +
	`@media(max-width:760px){.thread-more{opacity:1}}` +
	`@container chatmain (max-width:380px){.chat-composer .composer-tools>.tool-button[disabled]:not(.giphy-trigger):not(.emoji-trigger){display:none}.chat-composer .composer-toolbar{gap:2px}.chat-composer .send-button{flex:none}}` +
	`.thread-composer .composer-toolbar{justify-content:flex-start;flex-wrap:nowrap;min-width:0;padding:2px 6px 6px}.thread-composer .send-button{margin-inline-start:auto}.thread-composer .format-button[data-extra=code],.thread-composer .format-button[data-extra=bullets],.thread-composer .format-button[data-extra=quote]{display:none}` +
	`.conversation-header{position:relative;z-index:3;box-shadow:0 6px 10px -8px color-mix(in srgb,var(--ink) 30%,transparent)}.conversation-title{gap:4px}` +
	`.search-results-head>h2{position:absolute;clip:rect(0 0 0 0);clip-path:inset(50%);width:1px;height:1px;overflow:hidden;white-space:nowrap}.search-result-meta strong{font-weight:700}` +
	// C-5: the back-to-list toggle (.rail-pill.mobile-chat-toggle) keeps its
	// natural width here -- it is excluded from the icon-square squeeze below
	// so its label (kept visible, see the max-width:560px rule further down)
	// has room instead of being clipped into a 36px box.
	`@container chatmain (max-width:560px){.conversation-header .icon-button,.conversation-header .channel-todo-trigger,.conversation-header .channel-poll-trigger,.conversation-header .rail-pill:not(.mobile-chat-toggle){border:0;background:transparent;width:36px;min-width:36px;height:36px;padding:0;justify-content:center;border-radius:var(--hcm-radius-control)}.format-tools{display:flex}.format-button[data-extra=code],.format-button[data-extra=bullets],.format-button[data-extra=quote]{display:none}}` +
	`@container chatmain (max-width:360px){.conversation-topic{display:none}}` +
	// Round-8 critique; kept last so these overrides win the cascade.
	`.thread-root .thread-view-in-channel{opacity:0}.thread-root:hover .thread-view-in-channel,.thread-root .thread-view-in-channel:focus-visible{opacity:1}@media (hover:none),(max-width:760px){.thread-root .thread-view-in-channel{opacity:1;pointer-events:auto}}` +
	`@container chat (max-width:1100px){.thread-composer:not(:focus-within){flex-direction:row;align-items:center;padding:4px 6px 4px 4px}.thread-composer:not(:focus-within) .composer-input{flex:1;min-height:40px}.thread-composer:not(:focus-within) .composer-toolbar{padding:0;flex:none}.thread-composer:not(:focus-within) .format-tools,.thread-composer:not(:focus-within) .emoji-control{display:none}}` +
	`.chat-search-results>.search-status:not(.search-count){display:block}` +
	// Round-9 critique: names keep their own direction inside the other script.
	`.chat-row-name,.mention-name,.conversation-heading h1{unicode-bidi:plaintext}.message-author,.search-result-meta strong,.search-result-context,.person-name{unicode-bidi:isolate}` +
	`.chat-composer .composer-input,.thread-composer .composer-input{unicode-bidi:plaintext;text-align:start}` +
	`.chat-workspace[dir="rtl"] :is(.chat-row-name,.mention-name){text-align:match-parent}` +
	// Round-11 critique.
	// C-3 (r3): the hint takes the free space itself (flex:1, text-align:end)
	// instead of an auto margin. The old ".composer-help+.send-button
	// {margin-inline-start:8px}" (0,3,0) still matched while the hint was
	// display:none (<=1180px, narrow chatmain) and beat Send's own
	// margin-inline-start:auto (styles.go, 0,1,0), leaving Send mid-row.
	`.chat-composer .composer-help{position:static;flex:1 1 0%;min-width:0;overflow:hidden;text-overflow:ellipsis;text-align:end;margin-top:0;font-size:.75rem}@container chatmain (max-width:560px){.chat-composer .composer-help{display:none}}` +
	`.chat-workspace[dir="rtl"] .thread-pane .message-body{text-align:match-parent}` +
	// C-17: "View in channel" moved out to 44px (clear of the thread-more
	// trigger at the meta line's own end) but the meta line's reserved
	// padding stayed at the desktop 34px, so a long author/time line still
	// ran under both controls on a phone.
	`@media (hover:none),(max-width:760px){.thread-root .thread-view-in-channel{inset-inline-end:44px;top:3px}.thread-root-body .message-meta,.thread-message-body .message-meta{padding-inline-end:84px}}` +
	`.chat-workspace[dir="rtl"] .chat-row.selected,.chat-workspace[dir="rtl"] .chat-row.selected:hover{box-shadow:inset -3px 0 0 var(--accent)}.chat-workspace[dir="rtl"] .mention-option.active{box-shadow:inset -3px 0 0 var(--accent)}.chat-workspace:has(.chat-search-results) .chat-row.selected{box-shadow:none}` +
	`@container chatmain (max-width:560px){.conversation-header{flex-wrap:nowrap;height:52px;min-height:52px;padding-block:0}.conversation-header>.conversation-actions{flex:none;order:3;flex-wrap:nowrap;justify-content:flex-end}.conversation-header>.conversation-title{flex:1 1 0%;min-width:0}.channel-todo-trigger,.channel-poll-trigger{min-width:34px;padding:0 8px}.channel-todo-count{display:none}}` +
	// C-5: the back-to-list toggle keeps its text label even at this width --
	// icon-only was the affordance a reader on a phone could not tell apart
	// from the other icon buttons in the header.
	`@container chatmain (max-width:560px){.channel-todo-trigger-label,.channel-poll-trigger-label{display:none}.rail-pill{padding:0 9px}.conversation-title{min-width:96px}}` +
	// Round 3 C-11: the phone back pills are the only way back to the
	// conversation list, so they get the full 44px target on any pointer
	// (the coarse-pointer rule above never matched a narrow fine-pointer
	// window), and the unread dot sits inside the pill's curve instead of
	// on its border.
	`@media(max-width:760px){.conversation-header .rail-pill.mobile-chat-toggle,.search-results-back{height:44px;min-height:44px;padding:0 14px 0 12px}.rail-pill-dot{top:8px;inset-inline-end:10px}}` +
	// Round 3 C-5: results lifted over the phone drawer get a visible head:
	// the back pill to the conversation list and the query title. Elsewhere
	// the conversation header already carries the title, so the head stays
	// empty and the h2 screen-reader-only.
	`.search-results-head{display:flex;align-items:center;gap:10px;min-width:0}` +
	`@media(max-width:760px){.chat-workspace[data-sidebar-open="true"] .search-results-head{margin:0 0 12px}.chat-workspace[data-sidebar-open="true"] .search-results-back{display:inline-flex;flex:none}.chat-workspace[data-sidebar-open="true"] .search-results-head>h2{position:static;clip:auto;clip-path:none;width:auto;height:auto;flex:1 1 0%;min-width:0;margin:0;overflow:hidden;white-space:nowrap;text-overflow:ellipsis;font-size:1rem}}` +
	// Round 3 C-7: channel hits lead with the rail's glyph (# or lock); the
	// result count is muted text under the header title, and on the phone
	// search sheet beside its own visible title.
	`.search-channel-name{display:inline-flex;align-items:center;gap:6px;min-width:0}.search-channel-name .kind-glyph{width:16px;height:16px;color:var(--muted)}` +
	`.search-results-head>.search-head-count{display:none}.conversation-heading>.search-head-count{margin:0;color:var(--muted);font-size:.75rem}` +
	`@media(max-width:760px){.chat-workspace[data-sidebar-open="true"] .search-results-head>.search-head-count:not(:empty){display:inline;flex:none;color:var(--muted);font-size:.8125rem;white-space:nowrap}}` +
	// Round 3 C-13: in a narrow conversation column the back control is a
	// 44x44 round icon button (its aria-label and title still say
	// "Conversations"); the 145px labelled pill had squeezed the channel
	// subtitle to "Public ch…" and put the unread dot on the final "s".
	`@container chatmain (max-width:560px){.conversation-header .rail-pill.mobile-chat-toggle{width:44px;min-width:44px;height:44px;min-height:44px;padding:0;justify-content:center}.conversation-header .rail-pill.mobile-chat-toggle>span:not(.rail-pill-dot){display:none}.conversation-header .rail-pill.mobile-chat-toggle .rail-pill-dot{top:8px;inset-inline-end:8px}}`
