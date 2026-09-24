package chatui

import "strings"

// ScopedStylesheet keeps chat's generic utility selectors (.button, .avatar,
// .icon-button) from leaking into other product surfaces while retaining one
// CSP-hashable stylesheet payload. Inside the @scope block the workspace root
// is addressed as :scope, so state attributes on the root keep working.
func ScopedStylesheet() string {
	return "@scope (.chat-workspace){" + strings.ReplaceAll(Stylesheet, ".chat-workspace", ":scope") + "}" +
		`.chat-image-viewer{box-sizing:border-box;position:fixed;inset:0;width:100vw;height:100vh;height:100dvh;z-index:2147483647;display:flex;align-items:center;justify-content:center;padding:56px 16px 16px;background:rgba(7,17,24,.96);overscroll-behavior:contain}` +
		`.chat-image-viewer-media{position:relative;width:100%;height:100%;min-width:0;min-height:0;overflow:hidden}` +
		`.chat-image-viewer img{display:block;position:absolute;top:50%;left:50%;width:auto;height:auto;max-width:100%;max-height:100%;object-fit:contain;transform:translate(-50%,-50%)}` +
		`.chat-image-viewer-media.chat-image-viewer-zoomed{display:grid;place-items:center;overflow:auto}` +
		`.chat-image-viewer-zoomed .chat-image-viewer-preview,.chat-image-viewer-zoomed .chat-image-viewer-full{visibility:hidden}` +
		`.chat-image-viewer-zoomed .chat-image-viewer-original{position:relative;inset:auto;top:auto;left:auto;width:auto;height:auto;max-width:none;max-height:none;transform:none;justify-self:center;align-self:center}` +
		`.chat-image-viewer-full{opacity:0}` +
		`.chat-image-viewer-full.chat-image-display-ready{opacity:1}` +
		`.chat-image-viewer-original{opacity:0}` +
		`.chat-image-viewer-original.chat-image-original-ready{opacity:1}` +
		`@media(prefers-reduced-motion:no-preference){:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-image-viewer-full,:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-image-viewer-original{transition:opacity var(--hcm-motion-normal) var(--hcm-motion-easing)}}` +
		`.chat-image-viewer-download,.chat-image-viewer-zoom{position:absolute;z-index:2;padding:var(--hcm-space-2) var(--hcm-space-3);border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;box-shadow:var(--hcm-shadow-raised);cursor:pointer}` +
		`.chat-image-viewer-download{inset-inline-start:var(--hcm-space-3);bottom:var(--hcm-space-3)}` +
		`.chat-image-viewer-zoom{top:var(--hcm-space-2);inset-inline-start:var(--hcm-space-3);min-width:44px;min-height:44px}` +
		`.chat-image-viewer-download:disabled,.chat-image-viewer-zoom:disabled{cursor:wait;opacity:.65}` +
		`.chat-image-viewer-download:focus-visible,.chat-image-viewer-zoom:focus-visible,.chat-image-viewer-close:focus-visible{outline:3px solid var(--accent);outline-offset:2px}` +
		`.chat-image-viewer-close{position:absolute;top:var(--hcm-space-2);inset-inline-end:var(--hcm-space-3);width:44px;height:44px;border:1px solid var(--line);border-radius:50%;background:var(--surface);color:var(--ink);font:32px/1 sans-serif;box-shadow:var(--hcm-shadow-raised);cursor:pointer}` +
		`@media(prefers-reduced-motion:reduce){.chat-image-viewer-full,.chat-image-viewer-original{transition:none!important}}`
}
