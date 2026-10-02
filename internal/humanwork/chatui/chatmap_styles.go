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
.chatmap-live-banner{position:fixed;z-index:60;inset-block-start:0;inset-inline:0;display:flex;flex-wrap:wrap;align-items:center;justify-content:center;gap:12px;padding:8px 12px;background:var(--hcm-color-brand-primary);color:var(--hcm-color-on-brand)}
.chatmap-live-banner button{min-block-size:44px;min-inline-size:44px;border:1px solid currentColor;border-radius:var(--hcm-radius-control);background:transparent;color:inherit;padding:8px}
.chatmap-live-banner :focus-visible{outline:2px solid currentColor;outline-offset:2px}
.chatmap-live{font-weight:600}
.chatmap-sharing{display:flex;flex-direction:column;gap:8px;margin-block-start:12px}
.chatmap-crew-view ol{padding-inline-start:20px}.chatmap-crew-view li button{min-block-size:44px}
@media(prefers-reduced-motion:reduce){.chatmap-sheet *,.chatmap-card *{transition:none;animation:none}}
`
