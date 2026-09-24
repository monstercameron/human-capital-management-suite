package workspace

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace/gwc"
)

// Route paths this workspace serves. They are constants because the
// discovery document, the rendered form targets and the mux all have to agree
// on them, and three literals that agree today are three literals that can
// disagree tomorrow.
const (
	// RoutePrefix is the subtree the cell's edge mux mounts this handler on.
	RoutePrefix = "/workspace/"
	// PathPromotion renders the workspace for the worker named by the
	// "worker" query parameter.
	PathPromotion = "/workspace/promotion"
	// PathSimulate accepts the submitted request form and re-renders.
	PathSimulate = "/workspace/promotion/simulate"
	// PathReceiptPrefix addresses one zero-effect receipt by its digest.
	PathReceiptPrefix = "/workspace/promotion/receipt/"
	// PathAssetPrefix serves the progressive-enhancement bundle, when one has
	// been built into this package.
	PathAssetPrefix = "/workspace/assets/"
	// PathChatMediaPrefix is the protected chat media boundary the edge mounts
	// at the origin root. The product shell's connect-src names it so the wasm
	// client can mint grants and read bytes with its bearer.
	PathChatMediaPrefix = "/v1/chat/media/"
	// PathDocumentMediaPrefix is the document attachment and export boundary
	// (internal/transport/documentmedia); it rides the same media allowance.
	PathDocumentMediaPrefix = "/v1/documents/media/"
	// PathWasm is the GWC/WASM bundle.
	PathWasm = PathAssetPrefix + assetWasm
	// PathWasmExec is the Go WASM runtime shim the bundle needs.
	PathWasmExec = PathAssetPrefix + assetWasmExec
	// PathAssetManifest describes the exact static bytes this binary can serve.
	PathAssetManifest = PathAssetPrefix + AssetIntegrityManifestName
	// PathLogin renders (GET) and accepts (POST) the dev-only pasted-token
	// sign-in form. It is registered only when Options.DevBrowserLogin is
	// set; otherwise this cell serves no route here at all.
	PathLogin        = "/workspace/login"
	PathOIDCLogin    = "/workspace/login/oidc"
	PathOIDCCallback = "/workspace/login/oidc/callback"
	// PathLogout clears the session cookie PathLogin set. Registered under
	// the same condition as PathLogin.
	PathLogout = "/workspace/logout"
)

// formID is the id the request form is given so that the action buttons -
// which every renderer emits in their own sibling forms - can name it as
// their form owner. It is the HTML5 form-owner attribute, not script: the
// page still submits with JavaScript disabled.
const formID = "promotion-request"

// Form field names the submitted request carries beyond the contract's own
// field ids.
const (
	// ParamWorker names the worker on both the query string and the form.
	ParamWorker = "worker"
	// ParamCSRF carries the per-session token.
	ParamCSRF = "csrf_token"
	// ParamTransition carries which declared action was invoked.
	ParamTransition = "transition"
	// ParamLocale preserves a presentation choice across a normal HTML form
	// submission. It has no authority beyond formatting and translations.
	ParamLocale = "locale"
)

// Render produces the served HTML document for one page.
//
// The document is produced by the GWC renderer's native SSR path
// (workspace/gwc.Document) - the same component tree its wasm
// build mounts live in the browser - and then bound to this workspace's
// routes by [bindForms]. When the progressive-enhancement bundle is served,
// [contractIsland] is embedded too, so the live renderer has to mount exactly
// the contract the page already shows and nothing else.
//
// Binding is a post-processing step rather than a renderer change on purpose:
// the renderer is a finished, qualified artifact this package must not edit,
// and it deliberately knows nothing about URLs - its forms post to "#". What
// is added here is exactly the routing and the session inputs, never content.
// The frozen template renderer (tools/uxqual/render/ssr) remains the
// qualified named fallback: the decision record
// (definitions/ux/workspace-renderer-decision.yaml) scores both renderers in
// the same fixture, and swapping this file's renderer import is the only
// edit that re-enables it.
func Render(c contract.WorkspaceContract, csrf, worker string, enhanced bool) (string, error) {
	doc, err := gwc.Document(c)
	if err != nil {
		return "", fmt.Errorf("workspace: render the workspace document: %w", err)
	}
	doc = bindForms(doc, csrf, worker)
	if enhanced {
		doc = strings.Replace(doc, "</body>",
			contractIsland(c, csrf, worker, "")+loaderScript()+"\n</body>", 1)
	}
	return doc, nil
}

// RenderPage renders a Page with its presentation context. Its locale is
// applied after the already-masked contract is built, so this function can
// never unmask a field or change a governed simulation answer.
func RenderPage(page Page, csrf, worker string, enhanced bool) (string, error) {
	doc, err := gwc.Document(page.Contract)
	if err != nil {
		return "", fmt.Errorf("workspace: render the workspace document: %w", err)
	}
	doc = bindFormsForLocale(doc, csrf, worker, page.Locale.Resolved)
	doc = localizeDocument(doc, page.Locale, page.Query, page.TranslationDiagnostics)
	if enhanced {
		doc = strings.Replace(doc, "</body>",
			contractIsland(page.Contract, csrf, worker, page.Locale.Resolved)+loaderScript()+"\n</body>", 1)
	}
	return doc, nil
}

// bindForms retargets the GWC renderer's forms at this workspace's routes
// and injects the hidden inputs a submission needs.
//
// The renderer puts the request fields in one form and each action's button
// in its own sibling form, which is correct for a renderer that knows nothing
// about where it is served but would submit an empty request here. The
// binding gives the request form an id and makes every action control name
// it as its form owner, so pressing an action submits the fields the person
// filled in. That is a plain HTML5 mechanism; nothing here needs script.
func bindForms(doc, csrf, worker string) string {
	return bindFormsForLocale(doc, csrf, worker, "")
}

func bindFormsForLocale(doc, csrf, worker, locale string) string {
	hidden := hiddenInput(ParamCSRF, csrf) + hiddenInput(ParamWorker, worker)
	if locale != "" {
		hidden += hiddenInput(ParamLocale, locale)
	}

	replacements := []struct{ from, to string }{
		// The action forms first: GWC emits the request form and the action
		// forms with the same opening tag, so they are only told apart by
		// their first child - every action form begins with its hidden
		// transition input. Rewriting that input rewrites the tag it sits in
		// in the same step, so by the time the plain form literal is walked
		// only the request form still matches it.
		{
			`<form action="#" method="post"><input name="transition" type="hidden" value="`,
			`<form action="` + PathSimulate + `" method="post"><input form="` + formID + `" name="transition" type="hidden" value="`,
		},
		{
			`<form action="#" method="post">`,
			`<form action="` + PathSimulate + `" id="` + formID + `" method="post">` + hidden,
		},
		{
			`<input aria-required="true" id="reason-`,
			`<input form="` + formID + `" aria-required="true" id="reason-`,
		},
		{
			`<button data-variant="`,
			`<button form="` + formID + `" data-variant="`,
		},
	}
	for _, r := range replacements {
		doc = strings.ReplaceAll(doc, r.from, r.to)
	}
	return doc
}

// localizeDocument annotates a frozen SSR document with presentation-only
// facts. The canonical date remains in its native date control, while a
// named-month label makes the localized display and the submitted ISO value
// independently visible. This keeps the browser's date semantics intact and
// makes format conversion auditable without JavaScript.
func localizeDocument(doc string, locale LocaleContext, query Query, diagnostics []TranslationDiagnostic) string {
	if locale.Resolved == "" {
		return doc
	}
	doc = strings.Replace(doc, `<html lang="en">`, `<html lang="`+html.EscapeString(locale.Resolved)+`">`, 1)

	var notice strings.Builder
	notice.WriteString(`<section class="locale-context" data-locale="`)
	notice.WriteString(html.EscapeString(locale.Resolved))
	notice.WriteString(`" role="status"><p>Display locale: `)
	notice.WriteString(html.EscapeString(locale.Resolved))
	notice.WriteString(`.</p>`)
	if locale.Fallback == LocaleFallbackUnsupported {
		notice.WriteString(`<p data-locale-fallback="`)
		notice.WriteString(html.EscapeString(string(locale.Fallback)))
		notice.WriteString(`">Requested locale `)
		notice.WriteString(html.EscapeString(locale.Requested))
		notice.WriteString(` is unsupported; using `)
		notice.WriteString(html.EscapeString(locale.Resolved))
		notice.WriteString(`.</p>`)
	}
	if formattedDate, err := FormatDate(locale, query.EffectiveDate); err == nil {
		notice.WriteString(`<p>Effective date display: <time datetime="`)
		notice.WriteString(html.EscapeString(query.EffectiveDate))
		notice.WriteString(`">`)
		notice.WriteString(html.EscapeString(formattedDate))
		notice.WriteString(`</time>. Submitted as canonical ISO `)
		notice.WriteString(html.EscapeString(query.EffectiveDate))
		notice.WriteString(`.</p>`)
	}
	for _, diagnostic := range diagnostics {
		notice.WriteString(`<p class="error" data-l10n-missing="`)
		notice.WriteString(html.EscapeString(diagnostic.Key))
		notice.WriteString(`">Missing translation for `)
		notice.WriteString(html.EscapeString(diagnostic.Key))
		notice.WriteString(`; English fallback: `)
		notice.WriteString(html.EscapeString(diagnostic.Fallback))
		notice.WriteString(`.</p>`)
	}
	notice.WriteString(`</section>`)
	return strings.Replace(doc, `<main id="main-content">`, `<main id="main-content">`+notice.String(), 1)
}

func hiddenInput(name, value string) string {
	return `<input type="hidden" name="` + html.EscapeString(name) +
		`" value="` + html.EscapeString(value) + `">`
}

// liveBinding is the routing half of the data island the enhanced document
// carries: everything the live renderer needs to re-bind the tree it mounts
// to this workspace's routes. It mirrors - and is pinned to - the values
// [bindFormsForLocale] writes into the bound document, so the live tree and
// the server-rendered tree always submit identically.
type liveBinding struct {
	Action string            `json:"action"`
	FormID string            `json:"form_id"`
	Hidden map[string]string `json:"hidden"`
}

// contractIsland is the data island the enhanced document carries: the very
// contract the rendered tree shows, and the binding the live renderer
// live build applies to the tree it mounts over it.
// The document remains complete without the island; the island only tells the
// browser how to re-bind what it already has in front of it.
//
// The island is a script element on purpose, and it is CSP-clean: json.Marshal
// HTML-escapes every character a string value could use to terminate the tag,
// so its body can neither close the script early nor inject markup.
func contractIsland(c contract.WorkspaceContract, csrf, worker, locale string) string {
	hidden := map[string]string{
		ParamCSRF:   csrf,
		ParamWorker: worker,
	}
	if locale != "" {
		hidden[ParamLocale] = locale
	}
	island := struct {
		Contract contract.WorkspaceContract `json:"contract"`
		Binding  liveBinding                `json:"binding"`
	}{
		Contract: c,
		Binding:  liveBinding{Action: PathSimulate, FormID: formID, Hidden: hidden},
	}
	body, err := json.Marshal(island)
	if err != nil { // a contract of strings and times cannot fail to marshal;
		return "" //  treat a failure as "no enhancement" rather than a broken page
	}
	return `<script type="application/json" id="gwc-contract">` + string(body) + `</script>` + "\n"
}

// loaderScript is the one inline script the content-security-policy admits: a
// progressive-enhancement loader that starts the GWC/WASM renderer over an
// already-complete document.
//
// It is emitted only when both bundle halves are embedded, and it never
// removes anything: if instantiation fails, the server-rendered page is still
// the page. The script is a constant so its hash can be pinned in the policy;
// a loader assembled at run time could not be.
func loaderScript() string {
	return "<script>" + loaderSource + "</script>"
}

const loaderSource = `(function(){` +
	`if(!window.WebAssembly){return}` +
	`var c=document.getElementById("` + JourneyConfigElementID + `"),j=JSON.parse(c?c.textContent||"{}":"{}");` +
	`function o(i){return{credentials:"same-origin",headers:{authorization:"Bearer "+(j.bearer||"")},integrity:i||""}}` +
	// v stamps an asset address with its own digest so a stored copy can
	// never be stale and never has to be revalidated (UXLIVE-013).
	`function v(a){return a&&a.sha256?"?v="+encodeURIComponent(a.sha256):""}` +
	// k answers an addressed asset from the origin's own cache storage
	// when it has been seen before. The HTTP cache is the right place for
	// this and the immutable policy above is what asks it to do the job,
	// but a browser caps how large a single entry it will store -- measured
	// here, the multi-megabyte bundle is refused while every smaller asset
	// with the identical policy is kept -- so the bundle alone would still
	// be transferred on every navigation. Cache storage has no such cap.
	//
	// The key always carries the asset's own digest, so a stored copy can
	// never answer for different bytes, and entries under any other digest
	// are dropped as soon as a new one is stored. What is kept was verified
	// by subresource integrity when it was fetched; the cache is
	// origin-private, so anything able to write it already runs here.
	`function k(u,i){var f=function(){return fetch(u,o(i))};if(!window.caches){return f()}return caches.open("hcmnext-assets").then(function(c){return c.match(u).then(function(r){if(r){return r}return f().then(function(r){if(r.ok){c.put(u,r.clone()).then(function(){return c.keys().then(function(ks){for(var n=0;n<ks.length;n++){var q=new URL(ks[n].url);if(q.pathname==new URL(u,location.href).pathname&&ks[n].url.indexOf(u)<0){c.delete(ks[n])}}})},function(){})}return r})})},f)}` +
	`fetch("` + PathAssetManifest + `",o())` +
	`.then(function(r){if(!r.ok){throw new Error("asset manifest unavailable")}return r.json()})` +
	`.then(function(m){var a,s;for(var i=0;i<m.assets.length;i++){if(m.assets[i].path=="` + PathWasm + `"){a=m.assets[i]}else if(m.assets[i].path=="` + PathWasmExec + `"){s=m.assets[i]}}` +
	`if(!a||!s||typeof a.integrity!=="string"||typeof s.integrity!=="string"){throw new Error("asset integrity unavailable")}` +
	`return k("` + PathWasmExec + `"+v(s),s.integrity).then(function(r){if(!r.ok){throw new Error("wasm runtime unavailable")}return r.blob()}).then(function(b){var u=URL.createObjectURL(b);return import(u).then(function(){URL.revokeObjectURL(u)},function(e){URL.revokeObjectURL(u);throw e})}).then(function(){if(!window.Go){throw new Error("wasm runtime unavailable")}var g=new window.Go();return window.WebAssembly.instantiateStreaming(k("` + PathWasm + `"+v(a),a.integrity),g.importObject).then(function(r){g.run(r.instance)})})})` +
	`.catch(function(){});` +
	`})();`

// loaderHash pins the exact inline enhancement loader emitted by
// [loaderScript]. Keeping the source and its hash adjacent prevents a policy
// change from silently authorizing different bytes.
var loaderHash = sha256Source(loaderSource)

// ContentSecurityPolicy returns the policy header value for a workspace
// document served on host. The shared builder keeps the no-JavaScript
// response and the authenticated enhancement response on the same explicit
// deny boundary.
func ContentSecurityPolicy(host string, enhanced bool) string {
	policy := cspPolicy{
		styleHashes:    []string{stylesheetHash},
		formActionSelf: true,
	}
	if enhanced {
		policy.scriptHash = loaderHash
		policy.connectHost = host
		policy.allowAssetConnections = true
		policy.allowBlobScript = true
		policy.allowWASM = true
	}
	return policy.header()
}

// stylesheetHash pins the exact stylesheet emitted by the selected GWC
// document renderer, including its shared page-layout layer. Hashing the
// renderer's complete stylesheet keeps the policy aligned with the document.
var stylesheetHash = sha256Source(gwc.Stylesheet())

// sha256Source renders one CSP source expression for an inline block.
func sha256Source(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}
