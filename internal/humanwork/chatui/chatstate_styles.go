package chatui

const ChannelStatusStyles = `
.chatstate-badge{display:inline-flex;align-items:center;gap:.35rem;max-inline-size:100%;overflow-wrap:anywhere;color:var(--hcm-color-text-muted)}
.chatstate-section,.chatstate-notice{min-inline-size:0;max-inline-size:100%;overflow-wrap:anywhere;color:var(--hcm-color-text);background:var(--hcm-color-surface);padding:1rem}
.chatstate-section dl{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,2fr);gap:.5rem}
.chatstate-section dd{margin:0;min-inline-size:0;overflow-wrap:anywhere}
.chatstate-section label{display:block;margin-block:.6rem .3rem}
.chatstate-section input,.chatstate-section select,.chatstate-section textarea,.chatstate-section button,.chatstate-section summary{box-sizing:border-box;min-block-size:44px;max-inline-size:100%;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}
.chatstate-section input,.chatstate-section select,.chatstate-section textarea{inline-size:100%;padding:.5rem}
.chatstate-section button{padding:.5rem 1rem;cursor:pointer}
.chatstate-section summary{padding:.6rem;cursor:pointer}
.chatstate-section :focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}
.chatstate-effects{padding-inline-start:1.2rem}
.chatstate-directory-row{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem;min-inline-size:0;margin-block:.5rem}.chatstate-directory-row strong{overflow-wrap:anywhere;max-inline-size:100%}
@media(max-width:390px){.chatstate-section dl{grid-template-columns:minmax(0,1fr)}.chatstate-section{padding:.75rem}}
@media(prefers-reduced-motion:reduce){.chatstate-section *{animation:none;transition:none}}
.conversation-header>.chatstate-badge,.chat-row>.chatstate-badge{flex:0 1 auto;min-inline-size:0;overflow:hidden;white-space:nowrap;overflow-wrap:normal}
.chat-row>.chatstate-badge{max-inline-size:40%}.chatstate-badge .chatstate-label{min-inline-size:0;overflow:hidden;text-overflow:ellipsis}
@container chatmain (max-width:560px){.conversation-header>.chatstate-badge .chatstate-label{position:absolute;clip:rect(0 0 0 0);clip-path:inset(50%);inline-size:1px;block-size:1px;overflow:hidden;white-space:nowrap}}
@media(max-width:560px){.conversation-header>.chatstate-badge .chatstate-label{position:absolute;clip:rect(0 0 0 0);clip-path:inset(50%);inline-size:1px;block-size:1px;overflow:hidden;white-space:nowrap}}
`
