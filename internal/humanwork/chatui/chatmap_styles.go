package chatui

const ChatmapStyles = `
.chatmap-sheet,.chatmap-card{box-sizing:border-box;max-inline-size:100%;min-inline-size:0;padding:12px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text)}
.chatmap-sheet summary,.chatmap-sheet button,.chatmap-card button,.chatmap-card a,.chatmap-sheet input,.chatmap-sheet select{min-block-size:44px;box-sizing:border-box;max-inline-size:100%;border-radius:var(--hcm-radius-control);color:var(--hcm-color-text)}
.chatmap-sheet button,.chatmap-card button{border:1px solid var(--hcm-color-border);background:var(--hcm-color-surface);padding:8px}
.chatmap-sheet :focus-visible,.chatmap-card :focus-visible{outline:2px solid var(--hcm-color-brand-primary);outline-offset:2px}
.chatmap-sheet label{display:flex;flex-direction:column;gap:4px;margin-block:8px}
.chatmap-sheet input,.chatmap-sheet select{inline-size:100%;border:1px solid var(--hcm-color-border);background:var(--hcm-color-surface);padding:8px}
.chatmap-actions{display:flex;flex-wrap:wrap;gap:8px}
.chatmap-picture{display:block;inline-size:100%;aspect-ratio:2/1;object-fit:contain;background:var(--hcm-color-canvas)}
.chatmap-card p,.chatmap-sheet p{overflow-wrap:anywhere}
.chatmap-sheet{inline-size:auto;padding:0;flex:none}.chatmap-sheet summary{padding:8px}.chatmap-sheet[open]{position:fixed;z-index:40;inset-block-start:12px;inset-inline-start:max(12px,calc((100vw - 480px)/2));inline-size:min(calc(100vw - 24px),480px);max-block-size:calc(100dvh - 24px);overflow:auto;padding:12px;box-shadow:var(--hcm-shadow-raised)}.chatmap-large{inline-size:min(calc(100vw - 24px),900px);max-block-size:calc(100dvh - 24px);overflow:auto;background:var(--hcm-color-surface);color:var(--hcm-color-text);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface)}
.chatmap-sheet .chatmap-send{background:var(--hcm-color-brand-primary);color:var(--hcm-color-on-brand);inline-size:100%}
@media(prefers-reduced-motion:reduce){.chatmap-sheet *,.chatmap-card *{transition:none;animation:none}}
`
