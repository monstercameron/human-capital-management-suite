package chatui

// surfaceStyles covers the create and browse dialogs, the inline channel tray
// (to-do list and poll), person mention chips and the quiet-hours popover.
const surfaceStyles = `.chat-dialog{width:min(560px,100%);padding:0 24px 22px;border-radius:var(--hcm-radius-surface)}.chat-dialog .side-heading{margin:0 -24px 14px;padding:0 24px}` +
	// create dialog
	`.kind-cards{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}` +
	`.kind-card{display:flex;align-items:flex-start;gap:10px;padding:10px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;text-align:start;cursor:pointer}` +
	`.kind-card:hover{border-color:color-mix(in srgb,var(--accent) 55%,var(--line));background:var(--soft)}` +
	`.kind-card[aria-checked="true"]{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 12%,var(--surface));box-shadow:inset 0 0 0 1px var(--accent)}` +
	`.kind-card-glyph{display:inline-flex;align-items:center;justify-content:center;flex:none;width:30px;height:30px;border-radius:8px;background:var(--soft);color:var(--accent);font-weight:700}.kind-card-glyph .chat-icon{width:16px;height:16px}` +
	`.kind-card-text{display:grid;gap:2px;min-width:0}.kind-card-text strong{font-size:.875rem}.kind-card-text span{font-size:.75rem;color:var(--muted);line-height:1.35}` +
	`.create-name label,.member-picker>label{font-size:.8125rem;font-weight:600;color:var(--ink)}` +
	`.name-input{position:relative;display:flex;align-items:center}.name-prefix{position:absolute;inset-inline-start:11px;color:var(--muted);font-weight:600;pointer-events:none}.name-prefix+.chat-input{padding-inline-start:26px}` +
	`.chat-dialog .chat-input{height:38px}.field-hint{margin:2px 0 0;color:var(--muted);font-size:.75rem;line-height:1.35}` +
	`.member-picker{position:relative}.chip-field{display:flex;flex-wrap:wrap;align-items:center;gap:6px;min-height:40px;padding:4px 6px;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);background:var(--surface);cursor:text}` +
	`.chip-field:focus-within{border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 22%,transparent)}` +
	`.person-chip{display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 4px;border-radius:999px;background:var(--soft);color:var(--ink);font-size:.8125rem;font-weight:600}.person-chip .avatar.tiny{border-radius:50%}` +
	`.chip-remove{display:inline-flex;align-items:center;justify-content:center;width:20px;height:20px;padding:0;border:0;border-radius:50%;background:transparent;color:var(--muted);cursor:pointer}.chip-remove:hover{background:color-mix(in srgb,var(--ink) 10%,transparent);color:var(--ink)}.chip-remove .chat-icon{width:12px;height:12px}` +
	`.chip-input{flex:1;min-width:120px;height:30px;border:0;outline:0;background:transparent;color:inherit;font:inherit;font-size:.875rem}` +
	`.pick-options{position:static;margin-top:4px;display:flex;flex-direction:column;max-height:240px;overflow-y:auto;padding:6px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised)}` +
	`.dialog-actions{margin-top:6px}` +
	// browse dialog
	`.browse-dialog{width:min(640px,100%)}.browse-dialog .browse-list{max-height:min(420px,56vh);overflow-y:auto;margin:0 -8px;padding:0 8px 8px}` +
	`.browse-row{gap:12px;padding:10px 8px;border-radius:var(--hcm-radius-control);border-bottom:1px solid var(--line)}.browse-row .kind-glyph{width:32px;height:32px;border-radius:8px;background:var(--soft);color:var(--accent);opacity:1}` +
	`.browse-text{gap:1px}.browse-text strong{font-size:.9375rem}.browse-topic{font-size:.8125rem;color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.browse-actions{display:inline-flex;align-items:center;gap:10px;flex:none}.joined-label{display:inline-flex;align-items:center;gap:4px;color:var(--accent)}.joined-label .chat-icon{width:14px;height:14px}` +
	// C-15: the row itself is the click target now (its own data-action
	// mirrors the button's), so the button reads as a hint rather than the
	// only way in -- visible on hover/focus, and always visible where hover
	// does not exist.
	`.browse-row{cursor:pointer}.browse-row .browse-actions>.button{opacity:0;transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing)}.browse-row:focus-within .browse-actions>.button{opacity:1}@media(hover:hover){.browse-row:hover .browse-actions>.button{opacity:1}}@media(hover:none),(pointer:coarse){.browse-row .browse-actions>.button{opacity:1}}` +
	`.browse-footer{justify-content:flex-start;border-top:1px solid var(--line);margin:8px -24px 0;padding:12px 24px 0}.browse-footer .button .chat-icon{width:14px;height:14px}` +
	// channel tray
	`.channel-tray{flex:none;display:flex;flex-direction:column;gap:8px;padding:8px 20px 0}` +
	`.channel-tray-bar{display:flex;flex-wrap:wrap;gap:6px}` +
	`.tray-chip{display:inline-flex;align-items:center;gap:6px;max-width:100%;height:28px;padding:0 10px;border:1px solid var(--line);border-radius:999px;background:var(--surface);color:var(--ink);font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer}` +
	`.tray-chip .chat-icon{width:14px;height:14px;color:var(--accent)}.tray-chip-label{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.tray-chip:hover{background:var(--soft)}.tray-chip.active{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 12%,var(--surface))}` +
	// CHAT-05: a card tall enough to need scrolling can also crowd out its
	// own primary action (Create poll) below the fold. The card scrolls as a
	// column; .channel-poll-form's submit button stays sticky to the bottom
	// of it, so the action that matters is never scrolled out of reach.
	`.channel-tray-card{display:flex;flex-direction:column;max-height:min(46vh,420px);overflow-y:auto;padding:12px 14px 14px;border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--canvas);box-shadow:0 6px 18px -12px color-mix(in srgb,var(--ink) 40%,transparent)}` +
	// Round 3 C-3: the sticky strip is .channel-poll-actions, painted with the
	// card's own --canvas; the button inside keeps the chat .button fill
	// (--accent / --on-brand) and the shared :disabled pair. Setting the
	// canvas background on the button itself (0,4,0) beat both and left
	// --on-brand ink on --canvas: invisible in light and dark.
	`.channel-tray-card .channel-poll-form{position:relative}.channel-tray-card .channel-poll-actions{position:sticky;bottom:-14px;z-index:1;margin:4px -14px -14px;padding:8px 14px 14px;background:var(--canvas)}.channel-poll-actions .button{width:100%;min-width:0}` +
	// C-18: the empty-state line is a note under the card title, not a
	// second, larger heading.
	`.channel-tray-card .channel-poll>p.muted{margin:0 0 2px;color:var(--muted);font-size:.8125rem}` +
	`.channel-tray-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:6px}.channel-tray-head h2{margin:0;font-size:.9375rem}` +
	`.channel-tray-card .details-section{border-top:0;padding:0}.channel-tray-card .details-section-head h3{display:none}.channel-tray-card .details-section-head{justify-content:flex-end;margin-top:-34px;margin-inline-end:38px;min-height:34px}` +
	`.channel-tray-card .channel-todo-list{margin:4px 0 10px}.channel-tray-card .channel-todo-row{grid-template-columns:28px minmax(0,1fr) 32px;padding:7px 0}` +
	`.channel-tray-card .channel-todo-check{font-size:1.125rem;line-height:1}.channel-tray-card .channel-todo-text{font-size:.875rem}` +
	`.channel-tray-card .channel-poll-question{margin:0 0 2px;font-size:1rem}.channel-tray-card .channel-poll-list{display:grid;gap:6px;margin:10px 0 2px}` +
	`.channel-tray-card .channel-poll-option{grid-template-columns:minmax(0,1fr) auto auto;gap:4px 10px;padding:8px 10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}` +
	`.channel-tray-card .channel-poll-progress{grid-column:1 / -1;height:6px}.channel-tray-card .channel-poll-vote{grid-column:3;grid-row:1;justify-self:end;min-height:28px}` +
	`.channel-tray-card .channel-poll-form{display:grid;gap:8px;max-width:520px}` +
	// A poll option is one click target: the vote button covers its row, and
	// the row shows the choice, so there is no "Change your vote" on every line.
	`.channel-tray-card .channel-poll-option{position:relative;cursor:pointer}.channel-tray-card .channel-poll-option:hover{border-color:color-mix(in srgb,var(--accent) 55%,var(--line))}` +
	`.channel-tray-card .channel-poll-vote{position:absolute;inset:0;width:100%;height:100%;min-height:0;margin:0;padding:0;border:0;background:transparent;color:transparent;font-size:0;cursor:pointer}.channel-tray-card .channel-poll-vote:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.channel-tray-card .channel-poll-option.selected{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 10%,var(--surface))}.channel-tray-card .channel-poll-option.selected .channel-poll-option-label::after{content:" ✓";color:var(--accent)}` +
	`.channel-tray-card .channel-poll-progress{accent-color:var(--accent)}` +
	// to-do rules and options fold away
	`.channel-todo-add-row{display:flex;gap:8px}.channel-todo-add-row .chat-input{flex:1;min-width:0}.channel-todo-add-row .button{flex:none;width:auto}` +
	`.channel-todo-options,.channel-todo-rule{margin-top:6px;font-size:.8125rem}.channel-todo-options>summary,.channel-todo-rule>summary{cursor:pointer;color:var(--muted);width:fit-content}.channel-todo-options>summary:hover,.channel-todo-rule>summary:hover{color:var(--ink)}` +
	`.channel-todo-options[open],.channel-todo-rule[open]{display:grid;gap:6px}.channel-tray-card .channel-todo-row{align-items:start}` +
	`.composer-notice{margin:2px 6px 6px;padding:6px 10px;border-radius:var(--hcm-radius-control);background:color-mix(in srgb,var(--hcm-color-warning) 18%,transparent);color:var(--ink);font-size:.8125rem}` +
	// mention chips
	`.mention-chip{display:inline;padding:0 3px;border:0;border-radius:4px;background:color-mix(in srgb,var(--accent) 16%,transparent);color:var(--accent);font:inherit;font-weight:600;cursor:pointer}` +
	`.mention-chip:hover{background:color-mix(in srgb,var(--accent) 26%,transparent);text-decoration:underline}.mention-chip.self{background:color-mix(in srgb,var(--hcm-color-warning) 28%,transparent);color:var(--ink)}` +
	// quiet hours popover
	`.rail-prefs{position:relative}.rail-prefs[open]>summary{color:var(--ink)}` +
	`.rail-prefs-body{position:absolute;inset-inline:8px;bottom:calc(100% + 6px);z-index:40;gap:12px;padding:14px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised)}` +
	`.switch-row{justify-content:space-between;align-items:flex-start;gap:12px}.switch-text{display:grid;gap:3px;min-width:0}.switch-text strong{font-size:.875rem}` +
	`.switch{appearance:none;-webkit-appearance:none;flex:none;position:relative;width:38px;height:22px;margin:0;border-radius:999px;background:color-mix(in srgb,var(--ink) 22%,transparent);cursor:pointer;transition:background var(--hcm-motion-fast) var(--hcm-motion-easing)}` +
	`.switch::after{content:"";position:absolute;top:3px;inset-inline-start:3px;width:16px;height:16px;border-radius:50%;background:var(--surface);box-shadow:0 1px 2px rgba(0,0,0,.3);transition:inset-inline-start var(--hcm-motion-fast) var(--hcm-motion-easing)}` +
	`.switch:checked{background:var(--accent)}.switch:checked::after{inset-inline-start:19px}.switch:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.rail-prefs-body .chat-input:disabled{opacity:.5}` +
	// Review fixes: the popover's controls fit inside it, the switch has a
	// visible track, and the picker and dialogs stop jumping.
	`.rail-prefs-body{box-sizing:border-box;max-width:calc(100% - 16px)}.rail-prefs-body *{min-width:0}.rail-prefs-body .prefs-times{grid-template-columns:minmax(0,1fr) minmax(0,1fr)}.rail-prefs-body .chat-input{width:100%;box-sizing:border-box}.rail-prefs-body .chat-input:disabled{opacity:.7}` +
	`.chat-workspace .switch{display:inline-block;width:38px;min-width:38px;height:22px;min-height:22px;padding:0;border:1px solid color-mix(in srgb,var(--ink) 30%,transparent);background:color-mix(in srgb,var(--ink) 18%,var(--surface))}.chat-workspace .switch:checked{background:var(--accent);border-color:var(--accent)}` +
	`.chip-input:focus,.chip-input:focus-visible{outline:2px solid transparent;box-shadow:none}.chip-field{min-height:38px}` +
	`.chat-dialog-backdrop{align-items:flex-start;padding-top:max(16px,10vh)}` +
	`.create-name>label::after{content:" *";color:var(--hcm-color-danger)}` +
	`.thread-composer .send-label{display:none}.thread-composer .send-button{padding:0 10px;min-width:36px}` +
	`.message-list{padding-top:18px}` +
	`.search-filter-bar{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin:0 0 12px}.search-filter-hint{color:var(--muted);font-size:.75rem}` +
	// "Add channels" in the Channels section opens the same browser, so the
	// footer link only costs the rail a row while that section is expanded;
	// the row it frees is what keeps Direct messages on screen at 1024px.
	`.chat-rail:has(.rail-add) .rail-footer{display:none}` +
	`.channel-tray-card .channel-todo-row>.channel-todo-rule,.channel-tray-card .channel-todo-row>.channel-todo-policy-summary{grid-column:2 / -1;grid-row:auto}` +
	`.channel-tray-card li.channel-poll-option{position:relative}.channel-tray-card .channel-poll-option .button.channel-poll-vote,.channel-tray-card .channel-poll-option .button.channel-poll-vote:hover{position:absolute;inset:0;width:100%;height:100%;color:transparent;background:transparent;border:0;font-size:0;overflow:hidden;box-shadow:none}` +
	`.rail-prefs-body input[type=time]{padding:0 6px;font-size:.8125rem}.rail-prefs summary .prefs-value{display:none}.rail-prefs summary .prefs-summary-text{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}` +
	// CHAT-05: this unscoped rule silently shrank every .channel-tray-card to
	// 340px, clipping the poll form's Create poll button below the visible
	// area; the card's real max-height is set once, above.
	`.channel-poll-form textarea.chat-input{height:auto;min-height:96px;padding:8px 10px;resize:vertical}.channel-poll-form .muted{margin:0;font-size:.75rem;color:var(--muted)}` +
	`@media(max-width:560px){.kind-cards{grid-template-columns:repeat(2,minmax(0,1fr))}.kind-card{padding:8px}.kind-card-text span{display:none}.chat-dialog-backdrop{padding:0;align-items:stretch}.chat-dialog{width:100%;max-height:100dvh;border-radius:0}}` +
	`@media(max-width:560px){.kind-cards{grid-template-columns:minmax(0,1fr)}.chat-dialog{padding:0 16px 16px}.chat-dialog .side-heading{margin:0 -16px 12px;padding:0 16px}.browse-footer{margin:8px -16px 0;padding:12px 16px 0}.channel-tray{padding:8px 12px 0}}` +
	// Round 3 C-9: channel team/project widgets read as summary rows
	// (label, value, Edit) and open to their form; Save is a secondary
	// button sized to its label, not a full-width primary per field.
	// .channel-widget .channel-widget-edit .button (0,3,0) outranks the
	// older full-width .channel-widget .button (0,2,0) in styles.go.
	`.channel-widget-edit>summary{list-style:none;display:flex;align-items:baseline;gap:8px;min-height:36px;padding:6px 0;cursor:pointer;border-radius:var(--hcm-radius-control)}.channel-widget-edit>summary::-webkit-details-marker{display:none}` +
	`.channel-widget-edit>summary:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.channel-widget-summary-label{flex:none;color:var(--muted);font-size:.75rem;font-weight:600}.channel-widget-row .channel-widget-summary-label{flex:0 1 auto;min-width:0;color:var(--ink);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.channel-widget-summary-value{flex:1 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink);font-size:.8125rem}.channel-widget-row .channel-widget-summary-value{color:var(--muted);font-size:.75rem}` +
	`.channel-widget-edit-cue{flex:none;margin-inline-start:auto;padding:2px 8px;border-radius:var(--hcm-radius-control);color:var(--accent);font-size:.75rem;font-weight:600}.channel-widget-edit>summary:hover .channel-widget-edit-cue{background:var(--soft)}.channel-widget-edit[open]>summary .channel-widget-edit-cue{display:none}` +
	`.channel-widget-edit.adder>summary{align-items:center;gap:6px;color:var(--accent);font-size:.8125rem;font-weight:600}.channel-widget-edit.adder>summary .chat-icon{width:14px;height:14px}.channel-widget-edit.adder .channel-widget-summary-label{color:inherit;font-size:inherit}` +
	`.channel-widget-fields{display:flex;flex-direction:column;gap:5px;padding-bottom:8px}.channel-widget-edit>.channel-widget-form{padding-top:0}` +
	`.channel-widget .channel-widget-edit .button{width:auto;align-self:flex-end}` +
	`@media(pointer:coarse){.channel-widget-edit>summary{min-height:44px;align-items:center}}` +
	// Round 3 C-15 / C-21: the Integrations note is read at body-small size,
	// and the Copy button's glyph matches its 13px label.
	`.integrations-section .field-hint{margin:4px 0 8px;font-size:.8125rem}.integrations-section .button .chat-icon{width:14px;height:14px}` +
	// Round 4 C-4: the add-people picker's label is visible again; it now
	// names what the field takes ("People") rather than repeating the
	// dialog title (memberPicker).
	// Round 3 C-17: the status label ends the row. The hover-revealed row
	// button kept its width while invisible and pushed "Joined" ~70px in
	// from the edge; it now sits before the label instead.
	`.browse-actions>.joined-label{order:2}` +
	// Round 3 S-4 (chat dialogs): the 50% near-black scrim barely dims an
	// already dark page; dark mode (explicit, or system on a dark OS) uses a
	// deeper one so the dialog separates from the page behind it.
	`:root[data-hcm-color-mode="dark"] .chat-workspace .chat-dialog-backdrop{background:rgba(0,0,0,.62)}@media(prefers-color-scheme:dark){:root[data-hcm-color-mode="system"] .chat-workspace .chat-dialog-backdrop{background:rgba(0,0,0,.62)}}` +
	// Round 3 C-8: phone dialogs are bottom sheets, as docs' are
	// (docs_library_styles.go, max-width:40rem): full width, anchored to the
	// bottom edge, rounded top corners, at most 88dvh, rising in from below.
	// The UA's dialog height:fit-content had turned the old stretch rule
	// into a top sheet that ended mid-screen over the timeline. Kept last so
	// it outranks the unconditional flex-start backdrop above.
	`@media(max-width:640px){.chat-dialog-backdrop{padding:0;align-items:flex-end;justify-content:center}.chat-dialog{width:100%;max-width:none;max-height:88dvh;border-bottom:0;border-radius:var(--hcm-radius-surface) var(--hcm-radius-surface) 0 0}}` +
	`@keyframes chat-dialog-in{from{opacity:0;translate:0 .4rem}}@media(prefers-reduced-motion:no-preference){:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-dialog{animation:chat-dialog-in var(--hcm-motion-fast,.14s) var(--hcm-motion-easing,ease)}}` +
	`.browse-meta:has(.meta-part){display:flex;flex-wrap:wrap}.browse-meta .meta-part{white-space:nowrap}.browse-meta:has(.meta-part){column-gap:10px}` +
	// Round 3 C-10: the poll card is sized to its form (it was an 840px card
	// around a 520px form); labels are ink field labels and the options help
	// is muted small text under its box; Create sits at the end at its own
	// width and, while the form cannot succeed, uses the same outlined
	// disabled look as Send instead of a 45%-opacity filled primary.
	`.channel-tray-card:has(.channel-poll-form){max-width:600px;margin-inline-start:auto}.channel-tray-card .channel-poll-form{max-width:none}` +
	`.channel-poll-form label{color:var(--ink);font-size:.8125rem;font-weight:600}.channel-poll-form label:not(:first-child){margin-top:4px}.channel-poll-form .field-help{margin:-4px 0 0;color:var(--muted);font-size:.75rem}` +
	`.channel-tray-card .channel-poll-actions{display:flex;justify-content:flex-end}.channel-poll-actions .button{width:auto;min-width:0}.channel-poll-actions .button:disabled{background:transparent;border-color:var(--line);color:var(--muted);opacity:.5;cursor:not-allowed}.channel-poll-actions .button:not(:disabled){background:var(--accent);border-color:var(--accent);color:var(--on-brand,#fff)}` +
	// Round 3 C-18: a soft shadow above the browse footer says the list goes
	// on under it, instead of a row sliced flush at the border.
	`.browse-dialog .browse-footer{position:relative;box-shadow:0 -8px 10px -10px color-mix(in srgb,var(--ink) 30%,transparent)}` +
	// r6: on a narrow conversation column the subtitle drops its kind
	// ("Public channel"; the leading # or lock glyph carries it) so the
	// member count stays whole, and list rows and dialog buttons meet the
	// 44px touch target at phone width as well as under a coarse pointer.
	`@container chatmain (max-width:560px){.topic-kind,.topic-count>.topic-sep{display:none}}` +
	`@media(max-width:560px),(pointer:coarse){.mention-option,.add-people-option{min-height:44px}.chat-dialog .dialog-actions .button{min-height:44px}}`
