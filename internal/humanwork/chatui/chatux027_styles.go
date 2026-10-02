package chatui

// ChatUX027Styles is the one disclosure row of Conversation details and the
// block under it. A row is 36 px high with its label at the start, the current
// value at the end in secondary text and a chevron in a fixed place; the only
// thing that marks a row as open is the chevron turned over and the block that
// follows it, so a row is never tinted while it is open. A block is inset from
// the row by one indent, with a hairline at its start, and everything in it is
// the panel's body size (13 px), with field labels, help text and group labels
// one step smaller (12 px). Manage channel is a header in the style of the
// panel's other section titles with its rows as a plain list under it, each
// group of rows under a quiet 12 px label with 16 px above and 6 px below. A
// block's main action is the one solid button, in a footer row at the end with
// Cancel beside it; every other button in a block is a text button. Everything
// is in logical properties, so the panel mirrors in Arabic, and nothing in it
// scrolls on its own: the panel is the one scroll area.
//
// The row rules name .chat-details as well, because the stylesheet gives every
// button a 24 px minimum height at a higher specificity than one class.
const ChatUX027Styles = `
.chat-details .details-manage .manage-caption{display:block;margin:16px 0 6px;padding:0 8px;font-size:.75rem;font-weight:500;line-height:1.3;letter-spacing:0;text-transform:none;color:var(--hcm-color-text-muted)}
.chat-details .details-manage .manage-caption:first-child{margin-block-start:4px}
.manage-sec{border-block-start:0}
.details-manage .manage-sec+.manage-sec{border-block-start:1px solid var(--hcm-color-border)}
.details-manage .manage-caption+.manage-sec{border-block-start:0}
.manage-sec-row{display:flex;align-items:center;gap:8px;inline-size:100%;min-block-size:36px;box-sizing:border-box;margin:0;padding:0 8px;border:0;border-radius:0;background:transparent;color:var(--hcm-color-text);font:inherit;font-size:.8125rem;font-weight:400;text-align:start;cursor:pointer}
.chat-details .manage-sec-row{min-block-size:36px;padding-block:4px}
.manage-sec-static>.manage-sec-row{cursor:default}
button.manage-sec-row:hover{background:var(--soft)}
.manage-sec-row[aria-expanded=true],.manage-sec-row[aria-expanded=true]:hover{background:transparent}
button.manage-sec-row:focus-visible{outline:2px solid var(--hcm-color-brand-primary);outline-offset:-2px}
.chat-details .details-manage>.details-group>.manage-sec-row{min-block-size:40px;padding:0;font-weight:600;color:var(--hcm-color-text-muted)}
.chat-details .details-manage>.details-group>.manage-sec-row:hover{background:transparent;color:var(--hcm-color-text)}
.details-manage .manage-sec-plain{padding-block-end:8px}
.manage-sec-label{flex:1 1 auto;min-inline-size:0;overflow-wrap:anywhere}
.manage-sec-value{flex:0 0 auto;min-inline-size:0;max-inline-size:55%;overflow:hidden;overflow-wrap:anywhere;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;line-height:1.3;color:var(--hcm-color-text-muted);font-weight:400;text-align:end}
.manage-sec-value:empty{display:none}
.manage-sec[data-failed=true] .manage-sec-value{color:var(--hcm-color-danger)}
.manage-sec-chevron{flex:none;display:inline-flex;align-items:center;justify-content:center;inline-size:16px;block-size:16px;color:var(--hcm-color-text-muted);transition:transform var(--hcm-motion-fast) var(--hcm-motion-easing)}
.manage-sec-chevron .chat-icon{inline-size:14px;block-size:14px}
.manage-sec-row[aria-expanded=true] .manage-sec-chevron{transform:rotate(180deg)}
.manage-sec-row:hover .manage-sec-chevron{color:var(--hcm-color-text)}
.manage-sec-static .manage-sec-value{margin-inline-end:24px}
.manage-sec-body{margin-inline-start:4px;padding:8px 0 12px;padding-inline-start:12px;border-inline-start:1px solid var(--hcm-color-border);font-size:.8125rem;line-height:1.45;color:var(--hcm-color-text);overflow:visible}
.manage-sec-body[hidden]{display:none}
.manage-sec-body.manage-sec-plain{margin:0;padding:0;border:0}
.manage-sec-body>*+*{margin-block-start:12px}
.manage-sec-body form,.manage-sec-body .manage-sec-form{display:flex;flex-direction:column;gap:12px;margin:0;padding:0}
.manage-sec-body form>*{margin:0}
.chat-details :is(.manage-sec-body,.details-purpose-form) :is(p,li,span,strong,b,em,a,dd,dt,input,select,textarea,button,h3,h4,h5){font-size:.8125rem;line-height:1.4}
.chat-details :is(.manage-sec-body,.details-purpose-form) :is(h3,h4,h5){margin:0;font-weight:500}
.chat-details :is(.manage-sec-body,.details-purpose-form) label{display:block;margin:0 0 4px;font-size:.75rem;font-weight:500;color:var(--hcm-color-text-muted)}
.chat-details .manage-sec-body input:not([type=checkbox]):not([type=radio]),.chat-details .manage-sec-body select,.chat-details .manage-sec-body textarea{inline-size:100%;box-sizing:border-box;min-block-size:36px}
.chat-details .manage-sec-body .manage-sec-help,.chat-details .manage-sec-body .manage-sec-hint,.chat-details .manage-sec-body :is(.field-hint,.chatmod-hint,.chatstate-effect,.chatstate-form-error){font-size:.75rem;line-height:1.4;color:var(--hcm-color-text-muted)}
.manage-sec-help,.manage-sec-hint{margin:0}
.manage-sec-actions{display:flex;flex-direction:row-reverse;flex-wrap:wrap;justify-content:flex-start;align-items:center;gap:8px}
.chat-details :is(.manage-sec-body,.details-purpose-form) .button{inline-size:auto;min-inline-size:0;block-size:36px;min-block-size:36px;padding-inline:16px;font-size:.8125rem}
.chat-details :is(.manage-sec-body,.details-purpose-form) .button.secondary{padding-inline:8px;border-color:transparent;background:transparent;color:var(--accent)}
.chat-details :is(.manage-sec-body,.details-purpose-form) .button.secondary:hover:not(:disabled){background:var(--soft)}
.chat-details :is(.manage-sec-body,.details-purpose-form) .button:disabled{opacity:.5;cursor:default}
.chat-details .manage-sec-body .chat-disclosure-button:hover{background:var(--soft)}
.chat-details .chat-disclosure-button[aria-expanded=true],.chat-details .chat-disclosure-button[aria-expanded=true]:hover{background:transparent}
.chat-details .manage-sec-body .chatstate-section{padding:0;border:0;background:transparent}
.manage-sec-error{display:flex;flex-direction:column;align-items:flex-start;gap:8px;padding:8px 12px;border-inline-start:2px solid var(--hcm-color-danger)}
.chat-details .manage-sec-error p{margin:0;overflow-wrap:anywhere;color:var(--hcm-color-danger)}
.manage-sec-toolbar{display:flex;align-items:center;justify-content:space-between;gap:8px}
.manage-sec-body .chatstate-effect:empty,.manage-sec-body .chatstate-form-error:empty,.manage-sec-body .chatlangadmin-channel-form p:empty{display:none}
.manage-sec-body .chatlangadmin-channel-form,.manage-sec-body .chatbug058-form{display:flex;flex-direction:column;gap:12px}
.manage-sec-body .chatlangadmin-channel-form .chatlangadmin-check,.manage-sec-body .chatbug058-switch{display:flex;align-items:center;gap:8px;margin:0;min-block-size:36px;color:var(--hcm-color-text)}
.manage-sec-body .channel-widget-list{margin:0;padding:0;list-style:none;display:flex;flex-direction:column;gap:12px}
.manage-sec-body .details-notify-options{padding:0}
.details-notify{padding-block:0}
.chat-details .manage-sec-body .details-notify-options{display:flex;flex-direction:column;gap:0}
.chat-details .manage-sec-body label.details-notify-option{display:flex;align-items:center;gap:8px;margin:0;min-block-size:32px;padding:0 8px;font-size:.8125rem;font-weight:400;line-height:1.4;color:var(--hcm-color-text)}
.chat-details .manage-sec-body label.details-notify-option.selected{font-weight:500}
.chat-details .manage-sec-body .details-notify-radio{flex:none;inline-size:16px;block-size:16px;min-block-size:0;margin:0}
.chat-details .manage-sec-body .details-notify-text{flex:1 1 auto;min-inline-size:0;font-size:.8125rem}
.chat-details .details-section:has(>a[data-gate-open]){padding-block:0}
.chat-details .details-section>a[data-gate-open]{display:flex;align-items:center;gap:8px;min-block-size:36px;padding:0 8px;font-size:.8125rem;font-weight:400;color:var(--hcm-color-text);text-decoration:none}
.chat-details .details-section>a[data-gate-open]:hover{background:var(--soft)}
.chat-details .details-section>a[data-gate-open]::after{content:"";flex:none;margin-inline-start:auto;inline-size:6px;block-size:6px;border-inline-end:1.5px solid var(--hcm-color-text-muted);border-block-end:1.5px solid var(--hcm-color-text-muted);transform:rotate(-45deg)}
[dir=rtl] .chat-details .details-section>a[data-gate-open]::after,.chat-details[dir=rtl] .details-section>a[data-gate-open]::after{transform:rotate(45deg)}
.manage-role-list{margin:0;padding:0;list-style:none;display:flex;flex-direction:column;gap:0}
.manage-role-list:empty{display:none}
.manage-role-row{display:flex;align-items:center;gap:8px;min-block-size:36px}
.manage-role-row .avatar{flex:none}
.manage-role-name{flex:1 1 auto;min-inline-size:0;overflow:hidden;white-space:normal;overflow-wrap:anywhere;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;line-height:1.25;font-size:.8125rem;font-weight:400}
.manage-role-field{flex:0 0 140px;inline-size:140px;display:flex;align-items:center;gap:4px;min-inline-size:0}
.chat-details .manage-sec-body .manage-role-field input:not([type=checkbox]):not([type=radio]){min-block-size:32px;block-size:32px;padding-inline:8px}
.manage-role-saved{flex:none;display:inline-flex;color:var(--hcm-color-text-muted)}
.manage-role-saved .chat-icon{inline-size:14px;block-size:14px}
.manage-milestone-add{display:flex;flex-direction:column;align-items:flex-start;gap:12px}
.manage-milestone-add>div{inline-size:100%}
.manage-milestone-add>div[hidden]{display:none}
.manage-milestone-add>.button.secondary{margin-inline-start:-8px}
.chat-details .manage-sec-body .chatmod-group>h5[dir]{direction:inherit;text-align:start}
.chat-details .manage-sec-body .chatmod-switch{position:relative}
.chat-details .manage-sec-body .chatmod-switch>.chatmod-switch-text{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.chat-details .chatmod{gap:8px}
.chat-details .chatmod .chatmod-intro{font-size:.75rem;line-height:1.4}
.chat-details .chatmod-section>h4{margin:8px 0 0;font-size:.8125rem;font-weight:600;color:var(--hcm-color-text-muted)}
.chat-details .chatmod-section>h4+.chatmod-hint,.chat-details .chatmod .chatmod-row>.chatmod-state{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.chat-details .chatmod-group{gap:0}
.chat-details .chatmod .chatmod-group>h5{margin:12px 0 2px;padding:0 8px;font-size:.75rem;font-weight:500;line-height:1.3;color:var(--hcm-color-text-muted)}
.chat-details .chatmod-list{gap:0}
.chat-details .chatmod .chatmod-row{display:block;padding:0;border:0}
.chat-details .chatmod .chatmod-row-main{flex-wrap:nowrap;justify-content:space-between;gap:8px;min-block-size:36px;padding-inline:8px}
.chat-details .chatmod .chatmod-row-main .chatmod-name{flex:1 1 auto;min-inline-size:0;font-size:.8125rem;font-weight:400}
.chat-details .chatmod .chatmod-row-more:empty{display:none}
.chat-details .chatmod .chatmod-row-more{padding:0 8px 8px}
.chat-details .chatmod .chatmod-switch{min-block-size:32px;min-inline-size:40px;padding:0}
.details-purpose-form{display:flex;flex-direction:column;gap:12px;padding:12px;border-inline-start:1px solid var(--hcm-color-border)}
.details-purpose-form>*{margin:0}
.details-purpose-form .chat-input{inline-size:100%;box-sizing:border-box;min-block-size:36px}
.details-purpose-form .details-purpose-hint{font-size:.75rem;color:var(--hcm-color-text-muted)}
.chat-details .details-summary{display:grid;grid-template-columns:auto minmax(0,1fr);grid-template-areas:"tile name" "tile kind";align-items:center;column-gap:12px;row-gap:0;min-block-size:48px;padding:8px 0 12px;text-align:start}
.chat-details .details-summary>:first-child{grid-area:tile;inline-size:40px;block-size:40px;margin:0;border-radius:10px;font-size:1.125rem}
.chat-details .details-summary>h3{grid-area:name;margin:0;font-size:.9375rem;line-height:1.3;overflow-wrap:anywhere}
.chat-details .details-summary>p{grid-area:kind;margin:0;font-size:.75rem;line-height:1.3;color:var(--hcm-color-text-muted)}
.chat-details .details-purpose-row{align-items:center;min-block-size:36px;padding:0 8px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}
.chat-details .details-purpose-row .details-purpose-cue{opacity:1}
.chat-details .details-purpose-row .details-purpose-value.muted{color:var(--hcm-color-text-muted)}
.chat-details .pinned-row .pinned-preview{white-space:normal;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;overflow:hidden;overflow-wrap:anywhere}
.chat-details .pin-actions [data-action=unpin]{border-color:transparent;background:transparent;color:var(--hcm-color-text-muted)}
.chat-details .pin-actions [data-action=unpin]:hover:not(:disabled){background:var(--soft);color:var(--hcm-color-text)}
.chat-details .manage-sec-value .chatstate-badge,.chat-details .details-about-status .chatstate-badge{display:inline-flex;align-items:center;gap:6px;margin:0;padding:2px 8px;border-radius:999px;background:var(--soft);color:var(--hcm-color-text);font-size:.75rem;line-height:1.4}
.chat-details .chatstate-badge>span[aria-hidden=true]{display:inline-block;inline-size:8px;block-size:8px;border-radius:50%;background:var(--hcm-color-text-muted);font-size:0;line-height:0;overflow:hidden}
.chat-details .details-members-more{margin-block-start:4px;margin-inline-start:-8px;border-color:transparent;background:transparent}
.chat-details .details-members-more:hover:not(:disabled){background:var(--soft)}
.chat-details .integrations-developers .manage-sec-row{padding-inline:0}
@media(prefers-reduced-motion:reduce){.manage-sec-chevron{transition:none}}
`
