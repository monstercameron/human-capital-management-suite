package chatui

const ChatremoveStyles = `
.chatremove-overlay{position:fixed;color:var(--hcm-color-text);background:var(--hcm-color-surface);overflow:auto}
.chatremove-overlay-page{z-index:1200;inset-block-start:var(--chatmod-top,0);inset-block-end:var(--chatmod-bottom,0);left:var(--chatmod-left,0);right:var(--chatmod-right,0);border-inline-start:1px solid var(--hcm-color-border)}
.chatremove {color:var(--hcm-color-text);background:var(--hcm-color-surface);max-inline-size:100%;min-inline-size:0;padding:1rem;overflow-wrap:anywhere}
.chatremove * {box-sizing:border-box;min-inline-size:0}
.chatremove form,.chatremove .chatmod005-item,.chatremove fieldset {display:grid;gap:.75rem;margin-block:1rem;padding:1rem;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}
.chatremove input,.chatremove select,.chatremove textarea {inline-size:100%;max-inline-size:100%;min-block-size:44px;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);padding:.5rem}
.chatremove input[type=checkbox] {inline-size:44px;block-size:44px;flex-shrink:0}
.chatremove button,.chatremove a {min-block-size:44px;display:inline-flex;align-items:center;justify-content:center;padding:.5rem .75rem;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);color:var(--hcm-color-text);background:var(--hcm-color-surface);white-space:normal;text-align:start;text-decoration:none}
.chatremove .primary {background:var(--hcm-color-brand-primary);color:var(--hcm-color-on-brand)}
.chatremove button:disabled {opacity:.6;cursor:default}
.chatremove :focus-visible {outline:2px solid var(--hcm-color-brand-primary);outline-offset:3px}
.chatremove-actions {display:flex;flex-wrap:wrap;gap:.5rem}
.chatremove-choice {display:flex;align-items:center;gap:.5rem}
.chatremove blockquote {margin:0;padding:.75rem;border-inline-start:3px solid var(--hcm-color-border);white-space:pre-wrap}
.chatremove-tombstone {color:var(--hcm-color-text-muted);overflow-wrap:anywhere;min-inline-size:0;padding:.75rem}
.chatremove-tombstone button {min-block-size:44px;border-radius:var(--hcm-radius-control);color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border)}
.chatremove-tombstone :focus-visible {outline:2px solid var(--hcm-color-focus);outline-offset:3px}
@media(max-width:400px){.chatremove{padding:.5rem}.chatremove form,.chatremove .chatmod005-item,.chatremove fieldset{padding:.5rem}.chatremove-actions>*{flex:1 1 100%}}
@media(prefers-reduced-motion:reduce){.chatremove *{scroll-behavior:auto;transition:none;animation:none}}
.chatremove-reasons{border:0;padding:0;margin:0}.chatremove-reasons legend{font-weight:650;padding:0;margin-block-end:.25rem}
.chatremove-reason{display:flex;align-items:center;gap:.5rem;min-block-size:44px}.chatremove .chatremove-reason input[type=radio]{inline-size:24px;block-size:24px;min-block-size:24px;flex-shrink:0;padding:0;accent-color:var(--hcm-color-brand-primary)}.chatremove-reason label{flex:1;padding-block:.5rem;cursor:pointer}
.chatremove-quote{margin:0;display:grid;gap:.25rem}.chatremove-quote figcaption{font-weight:650}
.chatremove blockquote.chatremove-target{border-inline-start-color:var(--hcm-color-brand-primary);background:var(--hcm-color-brand-soft)}
.chatremove .chatremove-several{margin-block-start:1rem;border-block-start:1px solid var(--hcm-color-border);padding-block-start:.5rem}.chatremove-several>summary{min-block-size:44px;display:flex;align-items:center;cursor:pointer}
.chatremove .chatremove-link{border:0;background:transparent;color:var(--hcm-color-brand-primary);text-decoration:underline;min-block-size:44px;padding-inline:.25rem}
.chatremove-tombstone .chatremove-appeal{border:0;padding:0;margin:0;display:inline}.chatremove-own-reason{margin:.25rem 0;color:var(--hcm-color-text)}
.chatremove-notice{display:grid;gap:.5rem;margin-block:.75rem;padding:.75rem;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}.chatremove-notice form{border:0;padding:0;margin:0}.chatremove-notice h3,.chatremove-notice p{margin:0}.chatremove-date{color:var(--hcm-color-text-muted);font-size:.875rem}
.chatmod005-row{inline-size:100%;display:flex;align-items:center;gap:8px;border:0;background:transparent;font:inherit;text-align:start;min-block-size:44px}.chat-workspace .chatmod005-row>.chat-count{position:static;inset:auto;transform:none;width:auto;height:auto;min-width:18px;padding:1px 5px}.chat-workspace [data-moderation-count][hidden]{display:none}
.chatremove-tombstone .chatremove-actions{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem}
.chatremove.chatmod005-page{padding:0;display:block}.chatmod005-page>*{margin-inline:16px}.chatmod005-page .chatmod005-heading{margin-inline:0;padding-inline:16px;border-block-end:1px solid var(--hcm-color-border);background:var(--hcm-color-surface)}
.chatmod005-heading h2{font-size:1rem;margin:0}.chatremove .chatmod005-heading .icon-button{min-inline-size:44px;padding:0;justify-content:center;border-color:transparent;background:transparent}
.chatmod005-subheading{font-size:.9375rem;margin-block:16px 4px}
.chatmod005-tabs{display:flex;flex-wrap:wrap;gap:4px;margin-block:12px 4px}.chatremove .chatmod005-tab{min-block-size:44px;padding:8px 14px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);font:inherit;cursor:pointer}.chatremove .chatmod005-tab[aria-selected=true]{border-color:var(--hcm-color-brand-primary);color:var(--hcm-color-brand-primary);font-weight:650}
.chatremove .chatmod005-search{border:0;padding:0;margin-block:8px}.chatmod005-search .member-filter{position:relative;display:flex;align-items:center}.chatmod005-search .member-filter>svg{position:absolute;inset-inline-start:10px;inline-size:16px;block-size:16px;pointer-events:none}.chatremove .chatmod005-search input{padding-inline-start:34px}
.chatmod005-items{display:grid;gap:12px;padding-block:12px}.chatremove .chatmod005-item{margin:0;padding:12px;gap:8px}.chatmod005-item[hidden]{display:none}
.chatmod005-item-head{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.chatmod005-kind{font-size:.75rem;font-weight:650;padding:2px 8px;border-radius:var(--hcm-radius-control);background:var(--hcm-color-brand-soft);color:var(--hcm-color-text)}.chatmod005-kind.chatmod005-removed{background:var(--hcm-color-danger-surface);color:var(--hcm-color-danger)}
.chatremove .chatmod005-channel{min-block-size:0;display:inline;padding:0;border:0;background:transparent;color:var(--hcm-color-brand-primary);text-decoration:underline}
.chatmod005-muted{color:var(--hcm-color-text-muted);margin:0;font-size:.875rem}.chatmod005-empty{padding-block:32px;text-align:center}.chatmod005-empty p{margin:0 0 4px}
.chatmod005-message .message-actions,.chatmod005-message .message-stats{display:none}.chatmod005-message .message{padding-inline:0}
.chatremove .chatmod005-context{border:0;padding:0;margin:0}.chatmod005-context>summary{min-block-size:44px;display:flex;align-items:center;cursor:pointer;color:var(--hcm-color-brand-primary)}.chatmod005-context .chatmod005-message{border-inline-start:3px solid var(--hcm-color-border);padding-inline-start:8px;margin-block:4px}
.chatmod005-actions{display:flex;flex-wrap:wrap;gap:8px}.chatremove .chatmod005-action{min-block-size:44px;padding:8px 14px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);font:inherit;cursor:pointer}.chatremove .chatmod005-action.primary{background:var(--hcm-color-brand-primary);color:var(--hcm-color-on-brand);border-color:var(--hcm-color-brand-primary)}.chatmod005-action:disabled{opacity:.6;cursor:default}
.chatmod005-error{color:var(--hcm-color-danger);margin:0}.chatmod005-nomatch{padding-block:16px}
.chatremove .chatmod005-message button{min-block-size:0;min-inline-size:0;padding:0;border:0;background:transparent;color:inherit;text-align:start;justify-content:flex-start;white-space:normal}.chatremove .chatmod005-message .person-avatar-button{flex:none}.chatremove .chatmod005-message .message-author{font-weight:650;white-space:nowrap}.chatmod005-message .message-meta{flex-wrap:wrap}
.chatremove .chatremove-reasons{border:0;padding:0;margin:0;gap:0}.chatremove-reason{min-block-size:40px}.chatremove-reason label{padding-block:.25rem}
` + chatmodSelectedStyles + chatmod004RestoredStyles + chatmod005PermissionsStyles
