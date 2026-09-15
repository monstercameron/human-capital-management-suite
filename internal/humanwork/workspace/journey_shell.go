package workspace

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// The Promotion journey page is served as a shell, not as a rendered page.
//
// Every other route in this package answers with a complete server-rendered
// document that works with no JavaScript at all. This one does not, and the
// difference is deliberate rather than a regression: the journey page is a
// live view of a running workflow (proposed, awaiting approval, approved,
// recorded) that its client keeps current by calling the cell's canonical
// gRPC services over the WebSocket tunnel. A server-rendered snapshot of a
// workflow is stale the moment it is written, and projecting those same
// services onto a second HTTP/JSON surface just to render it would create
// exactly the parallel business surface this repository does not have. So
// the shell is a document that carries the client, the client's
// configuration, and an honest statement of what the page needs; the page
// itself is rendered in the browser by tools/uxqual/render/journey against
// live RPC answers.
//
// # The client-side contract
//
// This is the whole agreement between this file and the wasm client
// (tools/uxqual/cmd/journeywasm, built into assets/journey.wasm):
//
//  1. The client reads the JSON island at id [JourneyConfigElementID]
//     ("journey-config"). Its schema is [JourneyConfig]: tunnel_url, bearer,
//     tenant, subject, roles, purpose, journeys_path.
//
//  2. It dials the tunnel with GoGRPCBridge's browser dialer -
//     pkg/wasm/dialer.New(cfg.TunnelURL) - passed to grpc.NewClient together
//     with grpc.WithTransportCredentials(insecure.NewCredentials()). The
//     credentials are insecure at the gRPC layer on purpose: transport
//     security is the browser's wss:// connection, which gRPC inside a
//     WASM page cannot see and must not try to negotiate.
//
//  3. It attaches "authorization: Bearer " + cfg.Bearer as per-RPC metadata
//     to every call it makes, on every service, for the life of the page.
//     This is not optional and it is not covered by having connected: the
//     tunnel forwards only diagnostic headers (x-request-id,
//     x-correlation-id, traceparent) from the upgrade request into metadata,
//     never Authorization, so a call without that metadata is admitted as
//     an anonymous call and refused UNAUTHENTICATED.
//
//  4. It renders into the element with id "app", replacing the no-script
//     fallback this shell put there, and it injects
//     journey.Stylesheet() as the text content of a <style> element. That
//     exact string - not a variant of it - is what
//     [JourneyContentSecurityPolicy] pins by hash in style-src.
//
// # Why the credential is in the document
//
// The bearer the request was admitted with is written into the island so the
// client can present it per RPC. It is the same credential the browser
// already holds and already sends (as the Authorization header, or as the
// dev sign-in cookie it set for itself), so the document discloses nothing
// to its reader that its reader did not present; what it does is make the
// credential reachable from WASM, which cannot read an HttpOnly cookie and
// does not see the request's own headers. The document is served
// Cache-Control: no-store under a policy with no img-src, no connect-src
// beyond this origin's own tunnel and no form-action at all, so there is no
// mechanism in the page for that value to leave the page.

// Route paths and element ids the journey page shell publishes.
const (
	// PathJourney renders the Promotion journey page shell. It is written
	// out in full, like [PathPromotion], rather than composed from
	// [RoutePrefix]: RoutePrefix carries a trailing slash because it names a
	// mounted subtree, and gluing a second one onto it would name
	// "/workspace//journey".
	PathJourney = "/workspace/journey"
	// PathJourneyWasm is the journey client bundle.
	PathJourneyWasm = PathAssetPrefix + assetJourneyWasm
	// PathTunnel is the gRPC-over-WebSocket route the shell hands the client.
	//
	// It mirrors internal/transport/cell.TunnelPath, which is where the
	// route is actually mounted. It is restated here rather than imported
	// because that package composes this one: importing it back would be a
	// cycle. The two constants are pinned to each other by an assertion in
	// internal/transport/cell's own test, so the mirror cannot drift.
	PathTunnel = "/workspace/grpc"
	// JourneyConfigElementID is the id of the JSON island the client reads
	// its configuration from.
	JourneyConfigElementID = "journey-config"
	// JourneyRootElementID is the element the client renders into.
	JourneyRootElementID = "app"
)

// journeyBuildCommand is the command that produces the missing half of this
// page. It is named in the served document because the person looking at a
// shell with no client is the person who needs to run it.
const journeyBuildCommand = "go run ./tools/uxqual/cmd/journeywasm -out internal/humanwork/workspace/assets"

// JourneyConfig is the schema of the JSON island the journey page carries.
//
// It is a struct rather than a map so the field names are a compile-time
// contract with the client that reads them.
type JourneyConfig struct {
	// TunnelURL is the absolute ws:// or wss:// address of the cell's gRPC
	// tunnel, derived from the request that asked for this page so that a
	// page served through a proxy dials the proxy and not the origin.
	TunnelURL string `json:"tunnel_url"`
	// Bearer is the credential this request was admitted with, without its
	// scheme. See this file's doc comment for why it is here.
	Bearer string `json:"bearer"`
	// Tenant, Subject, Roles and Purpose are the admitted principal's own
	// derived facts, carried so the client can render who it is acting as
	// without a round trip. They are display facts only: every RPC the
	// client makes is authorized server-side from the credential, never from
	// anything in this island.
	Tenant  string   `json:"tenant"`
	Subject string   `json:"subject"`
	Roles   []string `json:"roles"`
	// PagePermissions is the effective union of durable role grants. It is
	// presentation metadata only; RPC handlers enforce the same policy.
	PagePermissions []roleaccess.PagePermission `json:"page_permissions,omitempty"`
	// FeaturePermissions is the effective union of durable page-feature grants.
	// A missing projection is the rolling-upgrade compatibility path; a
	// configured empty projection denies every feature.
	FeaturePermissions []roleaccess.FeaturePermission `json:"feature_permissions"`
	// LauncherActions is a server-resolved semantic-action projection. The
	// browser may render these entries, but every RPC still authorizes again.
	LauncherActions []LauncherActionConfig `json:"launcher_actions,omitempty"`
	Purpose         string                 `json:"purpose"`
	// JourneysPath is this page's own address, so the client can build
	// links back to itself.
	JourneysPath string `json:"journeys_path"`
	// LogoutPath is populated only for the explicitly enabled local browser
	// session. Enterprise deployments leave sign-out to their identity edge.
	LogoutPath string `json:"logout_path,omitempty"`
}

// LauncherActionConfig is the JSON-island form of one launcher verdict. Copy
// remains localized in the client; the server owns only identity and state.
type LauncherActionConfig struct {
	ID            string `json:"id"`
	Availability  string `json:"availability"`
	Reason        string `json:"reason,omitempty"`
	RecoveryLabel string `json:"recovery_label,omitempty"`
	RecoveryHref  string `json:"recovery_href,omitempty"`
	Priority      int64  `json:"priority,omitempty"`
}

// serveJourney renders the Promotion journey page shell.
//
// It admits the request exactly as [Handler.servePromotion] does - the same
// bearer credential, the same 401 with no credential - because the page is a
// view of the same tenant's governed data and the client it carries is
// handed that same credential.
func (h *Handler) serveJourney(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	config := JourneyConfig{
		TunnelURL:    h.tunnelURL(r),
		Bearer:       normalizeBearerInput(BearerFromRequest(r, h.devBrowserLogin)),
		Roles:        []string{},
		JourneysPath: PathJourney,
	}
	if h.devBrowserLogin {
		config.LogoutPath = PathLogout
	}
	if principal != nil {
		config.Tenant = string(principal.Tenant())
		config.Subject = principal.Subject()
		if roles := principal.Roles(); len(roles) > 0 {
			config.Roles = roles
		}
		config.Purpose = principal.DefaultPurpose()
	}

	doc, err := journeyShellDocument(config, JourneyBundleBuilt())
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	writeHTMLDocument(w, http.StatusOK, doc, JourneyContentSecurityPolicy(h.policyHost(r)))
}

// journeyShellDocument renders the shell for one configuration.
//
// bundleBuilt decides two things and only two: whether the client is loaded
// at all, and whether the document tells its reader how to build it. The
// island is written either way, so a page whose bundle arrives later needs
// no change here.
func journeyShellDocument(config JourneyConfig, bundleBuilt bool) (string, error) {
	island, err := json.Marshal(config)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString(`<html lang="en">` + "\n")
	b.WriteString("<head>\n")
	b.WriteString(`<meta charset="utf-8">` + "\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString("<title>Promotion journey</title>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString(`<div id="` + JourneyRootElementID + `">` + "\n")
	b.WriteString(`<p class="journey-fallback">This page needs WebAssembly and JavaScript enabled. `)
	b.WriteString(`The server-rendered Promotion workspace is at <a href="`)
	b.WriteString(html.EscapeString(PathPromotion))
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(PathPromotion))
	b.WriteString("</a>.</p>\n")
	if !bundleBuilt {
		b.WriteString(`<p class="journey-fallback" data-journey-bundle="missing">`)
		b.WriteString(`This build carries no Promotion journey client. Build it with: <code>`)
		b.WriteString(html.EscapeString(journeyBuildCommand))
		b.WriteString("</code></p>\n")
	}
	b.WriteString("</div>\n")
	// The island is a script element and it is CSP-clean: json.Marshal
	// escapes the three characters a string value could use to terminate the
	// tag as their JSON unicode escapes, so no value in it can close the
	// script element early or inject markup.
	b.WriteString(`<script type="application/json" id="` + JourneyConfigElementID + `">`)
	b.Write(island)
	b.WriteString("</script>\n")
	if bundleBuilt {
		b.WriteString("<script>" + journeyLoaderSource + "</script>\n")
	}
	b.WriteString("</body>\n</html>\n")
	return b.String(), nil
}

// journeyLoaderSource starts the journey client over the shell.
//
// It is a compile-time constant for one reason: [JourneyContentSecurityPolicy]
// pins its sha256, and a loader assembled at run time could not be hashed at
// build time. It fails silently and completely - no Go runtime, no start; a
// failed instantiation, no start - because the fallback paragraph the shell
// already rendered is the correct thing for the reader to be left looking at.
const journeyLoaderSource = `(function(){` +
	`if(!window.WebAssembly){return}` +
	`var c=document.getElementById("` + JourneyConfigElementID + `"),j=JSON.parse(c?c.textContent||"{}":"{}");` +
	`function o(i){return{credentials:"same-origin",headers:{authorization:"Bearer "+(j.bearer||"")},integrity:i||""}}` +
	`fetch("` + PathAssetManifest + `",o())` +
	`.then(function(r){if(!r.ok){throw new Error("asset manifest unavailable")}return r.json()})` +
	`.then(function(m){var a,s;for(var i=0;i<m.assets.length;i++){if(m.assets[i].path=="` + PathJourneyWasm + `"){a=m.assets[i]}else if(m.assets[i].path=="` + PathWasmExec + `"){s=m.assets[i]}}` +
	`if(!a||!s||typeof a.integrity!=="string"||typeof s.integrity!=="string"){throw new Error("asset integrity unavailable")}` +
	`return fetch("` + PathWasmExec + `",o(s.integrity)).then(function(r){if(!r.ok){throw new Error("wasm runtime unavailable")}return r.blob()}).then(function(b){var u=URL.createObjectURL(b);return import(u).then(function(){URL.revokeObjectURL(u)},function(e){URL.revokeObjectURL(u);throw e})}).then(function(){if(!window.Go){throw new Error("wasm runtime unavailable")}var g=new window.Go();return window.WebAssembly.instantiateStreaming(fetch("` + PathJourneyWasm + `",o(a.integrity)),g.importObject).then(function(r){g.run(r.instance)})})})` +
	`.catch(function(){});` +
	`})();`

// journeyStylesheetHash pins the exact stylesheet the journey client injects.
// It is computed once from journey.Stylesheet(), which is the same string the
// client sets as a <style> element's text content, so the policy cannot drift
// from the page.
var journeyStylesheetHash = sha256Source(journey.Stylesheet())

// journeyLoaderHash pins this shell's own inline loader.
var journeyLoaderHash = sha256Source(journeyLoaderSource)

// JourneyContentSecurityPolicy returns the policy header value for the
// Promotion journey page shell served on host.
//
// It is deny-by-default with three exact allowances and nothing else:
//
//   - script-src: the inline loader by hash, loader-created blob modules
//     (for the authenticated wasm_exec.js fetch), and
//     'wasm-unsafe-eval', which is what
//     WebAssembly.instantiateStreaming needs and is strictly narrower than
//     'unsafe-eval'.
//   - connect-src: this host's authenticated asset prefix and exact gRPC
//     tunnel path. Both transport schemes are listed because whether the page
//     was reached over TLS is a deployment fact the policy should not have
//     to predict; paths prevent unrelated same-origin endpoints or WebSocket
//     upgrades from inheriting either capability.
//   - style-src: the journey stylesheet by hash. No 'unsafe-inline': the
//     client injects that exact string and nothing else.
//
// Everything else - images, fonts, frames, form submissions, any other
// origin - is refused. img-src is stated explicitly rather than left to
// default-src because "this page loads no images" is a claim worth reading
// in the header.
func JourneyContentSecurityPolicy(host string) string {
	return cspPolicy{
		styleHashes:           []string{journeyStylesheetHash},
		scriptHash:            journeyLoaderHash,
		connectHost:           host,
		allowAssetConnections: true,
		allowTunnelConnection: true,
		allowBlobScript:       true,
		allowWASM:             true,
	}.header()
}

// JourneyTunnelURL derives the absolute tunnel address for one request.
//
// The scheme follows how the page itself was reached: wss:// when this
// listener terminated TLS, or when a trusted reverse proxy in front of it
// says the client's leg was https. The host is the request's own Host, so a
// page served through a proxy hands its client the proxy's address rather
// than an origin address the browser cannot reach.
func JourneyTunnelURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	authority := sanitizeHostAuthority(r.Host)
	if authority == "" {
		return ""
	}
	scheme := "ws"
	if r.TLS != nil || forwardedProtoIsHTTPS(r) {
		scheme = "wss"
	}
	return scheme + "://" + authority + PathTunnel
}

// forwardedProtoIsHTTPS reports whether X-Forwarded-Proto names https. The
// header may carry a comma-separated chain; the client's own leg is the
// first entry.
func forwardedProtoIsHTTPS(r *http.Request) bool {
	raw := r.Header.Get("X-Forwarded-Proto")
	if raw == "" {
		return false
	}
	first, _, _ := strings.Cut(raw, ",")
	return strings.EqualFold(strings.TrimSpace(first), "https")
}

// tunnelURL is [JourneyTunnelURL] except that a handler composed with a
// public origin answers for it: the page hands its browser the authority the
// deployment declared, not whatever Host an inner proxy hop rewrote the
// request to.
func (h *Handler) tunnelURL(r *http.Request) string {
	if h.publicAuthority == "" {
		return JourneyTunnelURL(r)
	}
	scheme := "ws"
	if h.publicScheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + h.publicAuthority + PathTunnel
}

// policyHost is the authority bound into a shell's connect-src. Under a
// declared public origin it is that origin's own: the browser's page origin
// is the public one whatever Host the request arrived with, so connect-src
// must name the public authority or the client cannot open its tunnel.
func (h *Handler) policyHost(r *http.Request) string {
	if h.publicAuthority != "" {
		return h.publicAuthority
	}
	return r.Host
}

// parsePublicOrigin resolves an Options.PublicOrigin value into its scheme
// and sanitized host[:port]. The configured origin is operator input rather
// than request text, but it is emitted into a page's JSON island and its
// CSP, so it is parsed exactly and the authority goes through the same
// sanitizer a request Host does. An origin the sanitizer cannot represent -
// a bare non-loopback IP, for example - is refused at construction rather
// than discovered as an empty tunnel address on every page.
func parsePublicOrigin(raw string) (scheme, authority string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	u, parseErr := url.Parse(raw)
	if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("workspace: public origin must be an absolute http(s) origin like https://hcm.example.com; got %q", raw)
	}
	authority = sanitizeHostAuthority(u.Host)
	if authority == "" {
		return "", "", fmt.Errorf("workspace: public origin %q has no usable authority", raw)
	}
	return u.Scheme, authority, nil
}
