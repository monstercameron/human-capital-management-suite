package chatui

// ChatUX041Styles is round three of Conversation details. One secondary button
// (accent text, no border, 32 px) for every secondary action in the panel: Copy
// link, Unpin, Ask, Show all, Cancel. Text in the panel is in three sizes only,
// 12 px for labels and help, 13 px for rows and body, 16 px for the panel's
// title and the channel's name. Every switch sits at the end of its row with a
// 16 px gap from its label; an off switch is light (a white track with a grey
// outline and knob) and an on switch is the accent. An agent's facts are one
// grid with one label style, as wide as the panel. A channel's word-list grid has
// a read-only workspace column beside the channel's own switch. The sticky
// header gets a shadow only once the panel is scrolled. The rules start with
// .chat-workspace .chat-details so they outrank the shared button, switch and
// disclosure rules that load after the details rules.
const ChatUX041Styles = `
.chat-workspace .chat-details .button{block-size:32px;min-block-size:32px;font-size:.8125rem;font-weight:500}
.chat-workspace .chat-details .button.secondary{padding-inline:8px;border-color:transparent;background:transparent;color:var(--accent);box-shadow:none}
.chat-workspace .chat-details .button.secondary:hover:not(:disabled){background:var(--soft);color:var(--accent)}
.chat-workspace .chat-details .button.secondary:disabled{opacity:.5;color:var(--accent)}
.chat-workspace .chat-details .pin-actions{display:flex;flex-wrap:wrap;gap:4px;margin-inline-start:-8px;padding:0 0 8px}
.chat-workspace .chat-details .details-members-more{margin-block-start:4px;margin-inline-start:-8px}
.chat-workspace .chat-details .persona-member-ask{padding-inline:8px;font-size:.8125rem}
.chat-workspace .chat-details .details-summary>:first-child{font-size:1rem}
.chat-workspace .chat-details .details-summary>h3{font-size:1rem}
.chat-workspace .chat-details .persona-member-name,.chat-workspace .chat-details .member-name{font-size:.8125rem}
.chat-workspace .chat-details .agent-badge{font-size:.75rem}
@supports(animation-timeline:scroll()){
.chat-workspace .chat-details .chat-panel-head{animation:chatux041-edge linear both;animation-timeline:scroll(nearest block);animation-range:0 24px}
.chat-workspace .chat-details .details-summary{animation:chatux041-away linear both;animation-timeline:scroll(nearest block);animation-range:0 28px}
@keyframes chatux041-away{from{opacity:1}to{opacity:0}}
@keyframes chatux041-edge{from{box-shadow:0 0 0 transparent}to{box-shadow:0 1px 0 var(--hcm-color-border),0 8px 8px -8px color-mix(in srgb,var(--hcm-color-text) 20%,transparent)}}
}
@media(prefers-reduced-motion:reduce){.chat-workspace .chat-details .chat-panel-head,.chat-workspace .chat-details .details-summary{animation:none}}
.chat-workspace .chat-details .chatstate-section button.button:not(.secondary){border-color:var(--accent);background:var(--accent);color:var(--on-brand)}
.chat-workspace .chat-details .chatstate-section button.button:disabled{opacity:.5}
.chat-workspace .chat-details .manage-sec-body label:has(>input.switch),.chat-workspace .chat-details .manage-sec-body .chatbug058-switch{display:flex;align-items:center;gap:16px;min-block-size:36px;margin:0;font-size:.8125rem;font-weight:400;color:var(--hcm-color-text)}
.chat-workspace .chat-details .manage-sec-body label:has(>input.switch)>input.switch{order:3;flex:none;margin-inline-start:auto}
.chat-workspace .chat-details .manage-sec-body label:has(>input.switch)>span:first-of-type{order:1;flex:1 1 auto;min-inline-size:0;font-weight:400}
.chat-workspace .chat-details .manage-sec-body label:has(>input.switch)>.chatvoice-settings-state{order:2;flex:none}
.chat-workspace .chat-details .manage-sec-body .chatlangadmin-check{font-weight:400}
.chat-workspace .chat-details .manage-sec-body .chatlangadmin-check>span{font-weight:400}
.chat-workspace .chat-details :is(input.switch:not(:checked),.chatmod-switch[aria-checked=false] .chatmod-switch-track){border:1.5px solid var(--hcm-color-control-border);background:var(--hcm-color-surface)}
.chat-workspace .chat-details input.switch:not(:checked)::after,.chat-workspace .chat-details .chatmod-switch[aria-checked=false] .chatmod-switch-track::after{background:var(--hcm-color-control-border)}
.chat-workspace .chat-details :is(input.switch:checked,.chatmod-switch[aria-checked=true] .chatmod-switch-track){border-color:var(--accent);background:var(--accent)}
.chat-workspace .chat-details input.switch:checked::after,.chat-workspace .chat-details .chatmod-switch[aria-checked=true] .chatmod-switch-track::after{background:var(--hcm-color-surface)}
.chat-workspace .chat-details .chatmod-cols{display:flex;align-items:flex-end;justify-content:flex-end;gap:8px;margin:12px 0 0;padding-inline:8px;font-size:.75rem;line-height:1.2;color:var(--hcm-color-text-muted)}
.chat-workspace .chat-details .chatmod-cols>span{flex:none;text-align:end;overflow-wrap:anywhere}
.chat-workspace .chat-details .chatmod-cols>span:first-child{inline-size:88px}
.chat-workspace .chat-details .chatmod-cols>span:last-child{inline-size:72px}
.chat-workspace .chat-details .chatmod .chatmod-row-main .chatmod-default{flex:none;inline-size:88px;text-align:end;font-size:.75rem;color:var(--hcm-color-text-muted)}
.chat-workspace .chat-details .chatmod .chatmod-switch{min-inline-size:72px;inline-size:72px;justify-content:flex-end}
.chat-workspace .chat-details .chatmod button:not([class]),.chat-workspace .chat-details .chatmod .chatmod-link{min-block-size:32px;block-size:32px;padding-inline:8px;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--accent);font-size:.75rem;font-weight:500;text-decoration:none}
.chat-workspace .chat-details .chatmod button:not([class]):hover:not(:disabled),.chat-workspace .chat-details .chatmod .chatmod-link:hover:not(:disabled){background:var(--soft)}
.chat-workspace .chat-details .avatar{font-size:.75rem}
.chat-workspace .chat-details .integrations-developers>.manage-sec-body{margin-inline-start:0;padding-inline-start:0;border:0}
.chat-workspace .chat-details .chatmod .chatmod-row-main{display:flex;flex-wrap:nowrap;align-items:center}
@media(pointer:fine){.chat-workspace .chat-details .icon-button:not(.chat-panel-close){inline-size:32px;block-size:32px;min-inline-size:32px;min-block-size:32px}}
.chat-workspace .chat-details .persona-member-label{min-block-size:40px}
.chat-workspace .chat-details .member-list .member-person-button{min-block-size:36px}
.chat-workspace .chat-details .manage-sec-body .manage-role-field input:not([type=checkbox]):not([type=radio]){border-color:var(--hcm-color-border)}
.chat-workspace .chat-details .chatmod .chatmod-group>h5{margin-block-start:8px}
.chat-workspace .chat-details .member-row.persona-member-row{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;column-gap:8px;row-gap:4px}
.chat-workspace .chat-details .persona-member-row>[data-chat-disclosure]{display:contents}
.chat-workspace .chat-details .persona-member-row>[data-chat-disclosure]>.chat-disclosure-button{grid-column:1;grid-row:1;min-inline-size:0}
.chat-workspace .chat-details .persona-member-row>.persona-member-ask{grid-column:2;grid-row:1}
.chat-workspace .chat-details .persona-member-row>[data-chat-disclosure]>[data-chat-disclosure-body]{grid-column:1/-1;grid-row:2;min-inline-size:0}
.chat-workspace .chat-details .persona-member-row>.persona-member-purpose{grid-column:1/-1;grid-row:3}
.chat-workspace .chat-details .persona-member-row>.persona-member-reads{grid-column:1/-1;grid-row:4}
.chat-workspace .chat-details .persona-member-row:has([data-chat-disclosure-body]:not([hidden]))>.persona-member-reads{display:none}
.chat-workspace .chat-details .agent-summary{margin:0;padding:4px 0 8px;border:0;gap:8px}
.chat-workspace .chat-details .agent-facts{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:8px 16px;margin:0;padding:0}
.chat-workspace .chat-details .agent-facts .agent-fact-label{margin:0;padding:0;font-size:.75rem;font-weight:500;line-height:1.5;color:var(--hcm-color-text-muted)}
.chat-workspace .chat-details .agent-facts .agent-fact-value{margin:0;padding:0;min-inline-size:0;font-size:.8125rem;line-height:1.5;color:var(--hcm-color-text);overflow-wrap:anywhere}
.chat-workspace .chat-details .agent-facts .agent-fact-value ul{display:flex;flex-direction:column;gap:4px;margin:0;padding:0;list-style:none}
.chat-workspace .chat-details .agent-summary .agent-summary-person{color:var(--accent);text-decoration:none}
.chat-workspace .chat-details .agent-summary .agent-summary-person:hover{text-decoration:underline}
.chat-workspace .chat-details .agent-summary-details,.chat-workspace .chat-details .details-agents-manage{color:var(--accent);text-decoration:none}
.chat-workspace .chat-details .agent-summary-details:hover,.chat-workspace .chat-details .details-agents-manage:hover{text-decoration:underline}
.chat-workspace .chat-details .manage-sec-ack{display:flex;flex-direction:column;gap:12px}
.chat-workspace .chat-details .chatstate-ack{display:flex;align-items:flex-start;gap:8px;margin:0;font-size:.8125rem;font-weight:400;color:var(--hcm-color-text)}
.chat-workspace .chat-details .chatstate-ack>input{flex:none;inline-size:16px;block-size:16px;min-inline-size:0;min-block-size:0;margin:2px 0 0;padding:0}
.chat-workspace .chat-details .chatstate-ack>span{flex:1 1 auto;min-inline-size:0}
.chat-workspace .chat-details .details-section>a[data-gate-open] .manage-sec-value{flex:0 1 auto;max-inline-size:60%}
`
