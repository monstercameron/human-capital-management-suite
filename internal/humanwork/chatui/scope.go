package chatui

import "strings"

// ScopedStylesheet keeps chat's generic utility selectors (.button, .avatar,
// .icon-button) from leaking into other product surfaces while retaining one
// CSP-hashable stylesheet payload. Inside the @scope block the workspace root
// is addressed as :scope, so state attributes on the root keep working.
func ScopedStylesheet() string {
	return "@scope (.chat-workspace){" + strings.ReplaceAll(Stylesheet, ".chat-workspace", ":scope") + "}" +
		`.chat-image-viewer{box-sizing:border-box;position:fixed;inset:0;width:100vw;height:100vh;height:100dvh;z-index:2147483647;display:flex;align-items:center;justify-content:center;padding:56px 16px 16px;background:rgba(7,17,24,.96);overscroll-behavior:contain}` +
		`.chat-image-viewer-media{position:relative;width:100%;height:100%;min-width:0;min-height:0}` +
		`.chat-image-viewer img{display:block;position:absolute;inset:0;width:100%;height:100%;object-fit:contain}` +
		`.chat-image-viewer-full{opacity:0}` +
		`.chat-image-viewer-full.chat-image-display-ready{opacity:1}` +
		`.chat-image-viewer-original{opacity:0}` +
		`.chat-image-viewer-original.chat-image-original-ready{opacity:1}` +
		`@media(prefers-reduced-motion:no-preference){:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-image-viewer-full,:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-image-viewer-original{transition:opacity 160ms ease}}` +
		`.chat-image-viewer-download{position:absolute;z-index:2;inset-inline-start:12px;bottom:12px;padding:10px 14px;border:1px solid rgba(255,255,255,.5);border-radius:6px;background:rgba(0,0,0,.7);color:#fff;font:inherit}` +
		`.chat-image-viewer-download:focus-visible{outline:3px solid #fff;outline-offset:2px}` +
		`.chat-image-viewer-close{position:absolute;top:8px;inset-inline-end:12px;width:44px;height:44px;border:1px solid rgba(255,255,255,.5);border-radius:50%;background:rgba(0,0,0,.55);color:#fff;font:32px/1 sans-serif;cursor:pointer}` +
		`.chat-image-viewer-close:focus-visible{outline:3px solid #fff;outline-offset:2px}`
}
