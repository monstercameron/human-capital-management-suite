package chatui

const ChatSearchStyles = `
#chatsearch-results{flex:1 1 100%;min-width:0;width:100%;color:var(--hcm-color-text);background:var(--hcm-color-surface)}
.search-filter-bar:has(#chatsearch-results){flex-wrap:wrap}
.chat-search-results:has(#chatsearch-results[data-active=true])>.search-status,.chat-search-results:has(#chatsearch-results[data-active=true])>.search-result-group,.chat-search-results:has(#chatsearch-results[data-active=true]) .search-head-count{display:none}
.chatsearch-view{display:grid;gap:1rem;min-width:0;overflow-wrap:anywhere}
.chatsearch-controls{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,12rem),1fr));gap:.75rem}
.chatsearch-view label{display:grid;gap:.25rem;min-width:0}
.chatsearch-view :is(input,select,button):not(.mention-chip){min-height:44px;min-width:0;max-width:100%;font:inherit;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);padding:.5rem .75rem}
.chatsearch-view :is(button,a,input,select,summary):focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}
.chatsearch-view summary{min-height:44px;display:flex;align-items:center;cursor:pointer}
.chatsearch-check{display:flex!important;align-items:center;gap:.5rem;min-height:44px}
.chatsearch-check input{min-height:0}
.chatsearch-chips{display:flex;flex-wrap:wrap;gap:.5rem}
.chatsearch-result{display:grid;text-align:start;width:100%;gap:.5rem;margin-block:.5rem;white-space:normal;overflow-wrap:anywhere}
.chatsearch-view mark{background:var(--hcm-color-brand-primary);color:var(--hcm-color-surface)}
.chatsearch-primary{background:var(--hcm-color-brand-primary)!important;color:var(--hcm-color-surface)!important}
.chatsearch-return{flex:0 0 auto;padding:.5rem 1rem;border-bottom:1px solid var(--hcm-color-border);background:var(--hcm-color-surface)}
.chatsearch-return .chatsearch-view{display:flex}
.chatsearch-count{margin:0;color:var(--hcm-color-text)}
@media(prefers-reduced-motion:reduce){.chatsearch-view *{animation:none;transition:none}}
` + chatsearch003Styles
