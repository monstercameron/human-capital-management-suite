package workspace

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pageledger"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/tokens"
	"github.com/monstercameron/human-capital-management-suite/internal/forms/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace/gwc"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// sessionCookie carries the workspace session the CSRF token is bound to.
//
// It is not a credential and it authenticates nothing: the request is
// admitted by the same bearer credential the API uses, and this cookie exists
// only so a submitted form can prove it was rendered for the session that is
// submitting it.
const sessionCookie = "hcmnext_workspace_session"

// EnvGiphyAPIKey configures the provider's public browser key when the
// embedding application does not set Options.GiphyAPIKey directly.
const EnvGiphyAPIKey = "HCMNEXT_GIPHY_API_KEY"

// maxFormBytes bounds a submitted request form.
const maxFormBytes = 64 << 10

// effectClassReadOnly marks workspace routes that only read or render data.
const effectClassReadOnly = "READ_ONLY"

// Options configures [NewHandler].
type Options struct {
	// Cell is the live cell every route reads through. Required.
	Cell Cell
	// Config is the same transport.Config the cell's RPC surfaces admit
	// under, so a workspace request and an API request derive identical
	// trusted context. Required: a surface that cannot authenticate must not
	// be served.
	Config transport.Config
	// Now supplies the session and cookie clock. Nil means time.Now in UTC.
	Now func() time.Time
	// SessionKey signs the per-session CSRF token. Nil means a key minted for
	// this process, which is the right default: the token is meaningful only
	// for the lifetime of the sessions this process issued.
	SessionKey []byte
	// Secure marks the session cookie Secure. It is a deployment fact (is
	// this cell behind TLS?) rather than something the handler can observe on
	// a request that may have been terminated upstream.
	Secure bool
	// DevBrowserLogin enables the dev-only pasted-token sign-in flow at
	// PathLogin/PathLogout. Off by default: a workspace that cannot be
	// reached without a bearer credential in an Authorization header must not
	// grow a second, cookie-based way in unless an operator says so
	// explicitly. When on, the session cookie PathLogin sets is also accepted
	// as the bearer source for every other workspace route.
	DevBrowserLogin bool
	// DevPersonas are server-issued local identities offered by the dev-only
	// sign-in page. Their credentials never enter HTML; the form submits only
	// an opaque ID and the handler selects the credential from this immutable
	// server-owned collection. Empty preserves the pasted-token fallback.
	DevPersonas []DevPersona
	// DevDirectory supplies the seeded demo org chart the dev-only sign-in
	// page offers, so a tester can pick exactly the employee they need
	// (dev_directory.go, login_directory.go). Nil renders no directory at
	// all, which is what a production-shaped composition supplies: this is a
	// narrow, separately stubbable port precisely so the sign-in page - which
	// runs before any credential exists - never reads workforce data through
	// the Cell.
	DevDirectory DevDirectory
	// Journey is the live engine the Promotion journey page reads and acts
	// through (journey_port.go). Nil means the journey routes answer that
	// execution is not composed on this cell.
	Journey JourneyEngine
	// RoleAccess resolves durable employee roles and page/action grants for
	// the product shell. Nil retains the signed-role compatibility policy.
	RoleAccess roleaccess.Store
	// Preferences supplies the organization-scoped admitted appearance for the
	// initial CSP-pinned product document. Nil keeps the default presentation.
	Preferences preferences.Store
	// PublicOrigin is the canonical http(s) origin (for example
	// "https://hcm.example.com") this cell is publicly reached at. It is a
	// deployment fact the request cannot carry: a proxy that terminates TLS
	// or rewrites Host still hands the page the authority its browser must
	// dial. Empty derives everything from the request, which is correct for
	// a listener browsers reach directly.
	PublicOrigin string
	// GiphyAPIKey is the provider's browser client key. It is public by
	// design and is included only in authenticated product page configuration.
	// Empty disables GIPHY requests and keeps its origins out of the product CSP.
	GiphyAPIKey string
	// PageLedger is the tenant-scoped durable publication ledger. Nil keeps
	// the in-memory governance used by isolated previews and tests.
	PageLedger pageledger.Store
	// BrandAssets is the durable tenant-scoped content and revision library
	// used by Appearance. Nil disables upload and mutation actions.
	BrandAssets BrandAssetRepository
	// Catalogs resolves the durable active product catalog revision per tenant.
	// Nil retains the reviewed build-time product catalog.
	Catalogs i18n.ActivatedCatalogStore
	// OIDCFlow is the configured tenant authorization-code flow. When set,
	// OIDCTenant, OIDCIssuerURL, and OIDCSessionIssuer are also required.
	OIDCFlow          *oidc.Flow
	OIDCTenant        values.TenantId
	OIDCIssuerURL     string
	OIDCSessionIssuer OIDCSessionIssuer
}

// OIDCSessionIssuer binds a verified OIDC principal to a server-side session
// and mints a credential accepted by the shared transport verifier.
type OIDCSessionIssuer interface {
	Issue(context.Context, *trust.Principal) (string, error)
}

type OIDCSessionRevoker interface {
	Revoke(context.Context, string) error
}

// Handler serves the Promotion workspace over one live cell.
type Handler struct {
	cell       Cell
	config     transport.Config
	now        func() time.Time
	sessionKey []byte
	secure     bool
	receipts   *receiptStore
	mux        *http.ServeMux
	enhanced   bool
	// assetManifestJSON is generated once from the exact embedded files at
	// construction time. Keeping the immutable bytes on the handler avoids
	// hashing a large WASM module on every manifest request.
	assetManifestJSON []byte
	assetIndex        map[string]assetIntegrityMetadata
	assetManifestETag string
	// devBrowserLogin mirrors Options.DevBrowserLogin.
	devBrowserLogin bool
	devPersonas     map[string]DevPersona
	// directory mirrors Options.DevDirectory. It is read only by the
	// dev-only sign-in page, and only when devBrowserLogin registered that
	// route at all.
	directory   DevDirectory
	roleAccess  roleaccess.Store
	preferences preferences.Store
	catalogs    i18n.ActivatedCatalogStore
	giphyAPIKey string
	// pages is the governed page-revision and rollout ledger the live
	// page-serving handler resolves through (REV-067-01). Nil or empty
	// preserves the pre-ledger behavior: pages with no published rollout
	// serve the compiled registry definition.
	pages                  *PageGovernance
	pageLedger             pageledger.Store
	brandAssets            BrandAssetRepository
	brandAssetUploadSlots  chan struct{}
	pageGovernanceMu       sync.Mutex
	pageGovernanceByTenant map[string]*PageGovernance
	oidcFlow               *oidc.Flow
	oidcTenant             values.TenantId
	oidcIssuerURL          string
	oidcSessionIssuer      OIDCSessionIssuer
	loginEnabled           bool
	// publicScheme and publicAuthority are Options.PublicOrigin resolved:
	// its scheme and its sanitized host[:port]. Empty means the shell
	// derives both from each request.
	publicScheme    string
	publicAuthority string
}

// DevPersona is one server-owned local-development sign-in identity. Token is
// deliberately never rendered; only ID crosses the browser boundary.
type DevPersona struct {
	ID, Name, Access, Description, Token string
	// IssueToken mints a fresh server-owned credential when the persona signs
	// in. Local development servers may run longer than a credential's
	// lifetime, so startup credentials are only a preview fallback.
	IssueToken func() (string, error)
	// Roles are the exact roles signed into Token. loginPersonaDescription
	// derives the persona's sign-in copy from these against the live
	// productui page registry (UXAUDIT-014), so the rendered promise can
	// never name a destination the persona's own credential does not admit.
	// Description is kept only for callers that construct a DevPersona
	// without roles (e.g. a componentized preview); the sign-in page never
	// reads it directly.
	Roles []string
	// WorkerRef is an optional server-owned binding checked against the verified
	// credential before its persona card advertises any access.
	WorkerRef string
	// Company is the tenant key of the demo company the persona belongs to,
	// and Slot the quick-pick slot ("admin", "hiring-manager", ...) a
	// company's persona fills. Both are empty for a composition that serves
	// one company and names neither, which renders exactly as before.
	Company string
	Slot    string
}

// NewHandler builds the workspace HTTP surface.
func NewHandler(opts Options) (*Handler, error) {
	switch {
	case opts.Cell == nil:
		return nil, errors.New("workspace: a Cell is required")
	case opts.Config.Verifier == nil:
		return nil, errors.New("workspace: a credential verifier is required; an unauthenticated workspace must not be served")
	case opts.OIDCFlow != nil && (opts.OIDCTenant == "" || strings.TrimSpace(opts.OIDCIssuerURL) == "" || opts.OIDCSessionIssuer == nil):
		return nil, errors.New("workspace: OIDC login requires a tenant, issuer URL, and session issuer")
	case opts.OIDCFlow == nil && (opts.OIDCTenant != "" || strings.TrimSpace(opts.OIDCIssuerURL) != "" || opts.OIDCSessionIssuer != nil):
		return nil, errors.New("workspace: incomplete OIDC login configuration")
	case opts.OIDCFlow != nil && func() bool { _, ok := opts.OIDCSessionIssuer.(trust.Verifier); return !ok }():
		return nil, errors.New("workspace: OIDC session issuer must verify issued credentials")
	}
	key := opts.SessionKey
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("workspace: mint a session key: %w", err)
		}
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	assetManifest, err := LoadEmbeddedAssetIntegrityManifest()
	if err != nil {
		return nil, fmt.Errorf("workspace: build asset integrity manifest: %w", err)
	}
	assetManifestJSON, err := assetManifest.CanonicalJSON()
	if err != nil {
		return nil, fmt.Errorf("workspace: encode asset integrity manifest: %w", err)
	}
	assetManifestDigest, err := assetManifest.Digest()
	if err != nil {
		return nil, fmt.Errorf("workspace: digest asset integrity manifest: %w", err)
	}

	personas := make(map[string]DevPersona, len(opts.DevPersonas))
	for _, persona := range opts.DevPersonas {
		persona.ID = strings.TrimSpace(persona.ID)
		persona.Token = normalizeBearerInput(persona.Token)
		if persona.ID != "" && persona.Token != "" {
			personas[persona.ID] = persona
		}
	}
	// The directory is a dev-only affordance of the dev-only sign-in page.
	// Refusing it outright when the browser login is off means the surface
	// cannot exist on a production-shaped cell even if a composition supplies
	// one by mistake, rather than merely going unrendered because no route
	// happens to reach it.
	directory := opts.DevDirectory
	if !opts.DevBrowserLogin {
		directory = nil
	}
	loginEnabled := opts.DevBrowserLogin || opts.OIDCFlow != nil
	publicScheme, publicAuthority, err := parsePublicOrigin(opts.PublicOrigin)
	if err != nil {
		return nil, err
	}
	giphyAPIKey := strings.TrimSpace(opts.GiphyAPIKey)
	if giphyAPIKey == "" {
		giphyAPIKey = strings.TrimSpace(os.Getenv(EnvGiphyAPIKey))
	}
	h := &Handler{
		cell:                   opts.Cell,
		config:                 opts.Config,
		now:                    now,
		sessionKey:             key,
		secure:                 opts.Secure,
		receipts:               newReceiptStore(),
		enhanced:               BundleBuilt(),
		assetManifestJSON:      assetManifestJSON,
		assetIndex:             indexAssetIntegrityManifest(assetManifest),
		assetManifestETag:      `"` + assetManifestDigest + `"`,
		devBrowserLogin:        opts.DevBrowserLogin,
		devPersonas:            personas,
		directory:              directory,
		roleAccess:             opts.RoleAccess,
		preferences:            opts.Preferences,
		catalogs:               opts.Catalogs,
		giphyAPIKey:            giphyAPIKey,
		publicScheme:           publicScheme,
		publicAuthority:        publicAuthority,
		pages:                  nil,
		pageLedger:             opts.PageLedger,
		brandAssets:            opts.BrandAssets,
		brandAssetUploadSlots:  make(chan struct{}, 2),
		pageGovernanceByTenant: make(map[string]*PageGovernance),
		oidcFlow:               opts.OIDCFlow, oidcTenant: opts.OIDCTenant, oidcIssuerURL: strings.TrimSpace(opts.OIDCIssuerURL),
		oidcSessionIssuer: opts.OIDCSessionIssuer, loginEnabled: loginEnabled,
	}
	if verifier, ok := opts.OIDCSessionIssuer.(trust.Verifier); ok {
		h.config.Verifier = verifier
	}
	if opts.PageLedger == nil {
		h.pages = NewPageGovernance()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathPromotion, h.servePromotion)
	mux.HandleFunc("GET "+PathJourney, h.serveJourney)
	mux.HandleFunc("POST "+PathCatalogPublication, h.publishCatalog)
	mux.HandleFunc("POST "+PathBrandAssets, h.uploadBrandAsset)
	mux.HandleFunc("GET "+PathBrandAssets, h.listBrandAssets)
	mux.HandleFunc("POST "+PathBrandAssetLifecycle, h.changeBrandAsset)
	mux.HandleFunc("GET "+PathBrandAssetPrefix+"{digest}", h.serveBrandAsset)
	// Product routes may be nested (for example, Admin-owned configuration
	// pages). Capture the complete suffix so a cold reload reaches the same
	// registered route as client-side navigation.
	mux.HandleFunc("GET "+PathProductPrefix+"{page...}", h.serveProduct)
	mux.HandleFunc("POST "+PathSimulate, h.serveSimulate)
	mux.HandleFunc("GET "+PathReceiptPrefix+"{digest}", h.serveReceipt)
	mux.HandleFunc("GET "+PathAssetPrefix+"{name}", h.serveAsset)
	if h.loginEnabled {
		mux.HandleFunc("GET "+PathLogin, h.serveLoginForm)
		if h.devBrowserLogin {
			mux.HandleFunc("POST "+PathLogin, h.serveLoginSubmit)
		}
		if h.oidcFlow != nil {
			mux.HandleFunc("GET "+PathOIDCLogin, h.serveOIDCLogin)
			mux.HandleFunc("GET "+PathOIDCCallback, h.serveOIDCCallback)
		}
		mux.HandleFunc("GET "+PathLogout, h.serveLogout)
	}
	mux.HandleFunc("/", h.serveNotFound)
	h.mux = mux
	return h, nil
}

// ServeHTTP implements [http.Handler].
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// Routes returns the routes this handler publishes, for the discovery
// document. It is derived from the same constants the mux is built from, so
// a published route is always a route that exists.
func Routes() []Route {
	return []Route{
		{Path: PathBrandAssets, Method: http.MethodPost, Description: "Upload and validate a tenant-owned brand image revision.", EffectClass: "CONTROLLED_WRITE"},
		{Path: PathBrandAssets, Method: http.MethodGet, Description: "List tenant-owned brand asset revisions without exposing stored image bytes.", EffectClass: effectClassReadOnly},
		{Path: PathBrandAssetLifecycle, Method: http.MethodPost, Description: "Remove or roll back a tenant-owned brand image revision.", EffectClass: "CONTROLLED_WRITE"},
		{Path: PathBrandAssetPrefix + "{digest}", Method: http.MethodGet, Description: "Serve a tenant-owned approved brand image by content digest.", EffectClass: effectClassReadOnly},
		{
			Path: PathCatalogPublication, Method: http.MethodPost,
			Description: "Publish and activate one immutable reviewed product catalog revision for the authenticated tenant.",
			EffectClass: "CONTROLLED_WRITE",
		},
		{
			Path: PathProductPrefix + "{page}", Method: http.MethodGet,
			Description: "Serve the signed-in workspace shell and the workforce data it is authorized to show.",
			EffectClass: effectClassReadOnly,
		},
		{
			Path: PathPromotion, Method: http.MethodGet,
			Description: "Render the Promotion workspace for ?worker=<ref> from live cell data.",
			EffectClass: effectClassReadOnly,
		},
		{
			Path: PathJourney, Method: http.MethodGet,
			Description: "Serve the Promotion journey page shell; its client reaches the cell's gRPC services over the WebSocket tunnel.",
			EffectClass: effectClassReadOnly,
		},
		{
			Path: PathSimulate, Method: http.MethodPost,
			Description: "Submit the Promotion request form and re-render with preflight findings and the zero-effect receipt.",
			EffectClass: effectClassReadOnly,
		},
		{
			Path: PathReceiptPrefix + "{digest}", Method: http.MethodGet,
			Description: "Show one zero-effect simulation receipt by its result digest.",
			EffectClass: effectClassReadOnly,
		},
		{
			Path: PathAssetPrefix + "{name}", Method: http.MethodGet,
			Description: "Serve the authenticated frontend asset or its generated integrity manifest; 404 when an optional bundle is not built into this binary.",
			EffectClass: effectClassReadOnly,
		},
	}
}

// Route is one published workspace route.
type Route struct {
	Path        string `json:"path"`
	Method      string `json:"method"`
	Description string `json:"description"`
	// EffectClass declares whether the route reads or writes governed state.
	EffectClass string `json:"effect_class"`
}

// ---------------------------------------------------------------------------
// Admission
// ---------------------------------------------------------------------------

// admit authenticates one workspace request under the same admission the RPC
// surfaces run, and returns the request context the principal travels in.
func (h *Handler) admit(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	principal, _, ownedErr := transport.PreAdmit(r.Context(), h.config,
		h.admissionMetadata(r), r.URL.Path)
	if ownedErr != nil {
		status := ownedErr.HTTPStatus()
		if status == http.StatusUnauthorized && h.loginEnabled && r.Method == http.MethodGet && strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			writeRedirect(w, r, PathLogin, http.StatusSeeOther)
			return nil, false
		}
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="human-capital-management-suite"`)
		}
		h.writeProblem(w, status, "Not authenticated",
			"This workspace is served to an authenticated caller only. Present the same bearer credential the API accepts.")
		return nil, false
	}
	return r.WithContext(trust.WithPrincipal(r.Context(), principal)), true
}

// admissionMetadata returns the metadata admission reads for r.
//
// It is the request's own headers, unmodified, unless dev browser sign-in is
// enabled and the request carries no Authorization header of its own - only
// then does the session cookie PathLogin set stand in for it. This is the one
// and only place a cookie ever substitutes for the header the API requires,
// it is entirely gated behind the operator's own explicit flag, and it never
// changes what admission itself does with the resulting metadata.
//
// The rule itself lives in [AdmissionMetadata] (tunnel_admission.go) because
// the cell's gRPC-over-WebSocket tunnel admits the very same browser session
// on its upgrade request and must apply the identical rule; one exported
// function is how "the one and only place" stays true across two surfaces.
func (h *Handler) admissionMetadata(r *http.Request) transport.Metadata {
	return AdmissionMetadata(r, h.loginEnabled)
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

func (h *Handler) servePromotion(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	seed, err := DefaultQuery()
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	locale := ResolveLocale(r.URL.Query().Get(ParamLocale))
	h.renderWorkspace(w, admitted, seed.WithWorker(r.URL.Query().Get(ParamWorker)), locale)
}

func (h *Handler) serveSimulate(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Unreadable submission", err.Error())
		return
	}
	if !h.csrfValid(r) {
		h.writeProblem(w, http.StatusForbidden, "Stale submission",
			"This form was not issued to the current workspace session. Reload the workspace and submit again.")
		return
	}
	if transition := r.PostFormValue(ParamTransition); transition != TransitionRunSimulation {
		h.writeProblem(w, http.StatusBadRequest, "Unavailable action",
			fmt.Sprintf("%q is not an action this release offers; the only released transition is %q, and it simulates.",
				transition, TransitionRunSimulation))
		return
	}

	seed, err := DefaultQuery()
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	answers := make(map[string]string, len(r.PostForm))
	for name := range r.PostForm {
		answers[name] = r.PostFormValue(name)
	}
	locale := ResolveLocale(r.PostFormValue(ParamLocale))
	answers, err = CanonicalizePresentationAnswers(locale, seed.Currency, answers)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Unreadable localized submission", err.Error())
		return
	}
	query := seed.WithWorker(r.PostFormValue(ParamWorker)).WithFormAnswers(answers)

	// The submitted form and a direct governed capability call must produce
	// the same intent identity. Checking it here is FORM-004's equivalence
	// clause enforced on the live path rather than only in a fixture: a form
	// that produced a different intent than the API route would be a second,
	// ungoverned way to ask for a promotion.
	submitted, err := forms.FromFormSubmission(query.WorkerRef, answers)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Incomplete request", err.Error())
		return
	}
	direct, err := query.Intent()
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Incomplete request", err.Error())
		return
	}
	if submitted.Digest != direct.Digest {
		h.writeProblem(w, http.StatusInternalServerError, "Route divergence",
			"the submitted form and the equivalent capability call produced different intent digests")
		return
	}

	h.renderWorkspace(w, admitted, query, locale)
}

func (h *Handler) serveReceipt(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admit(w, r); !ok {
		return
	}
	receipt, found := h.receipts.get(r.PathValue("digest"))
	if !found {
		h.writeProblem(w, http.StatusNotFound, "No such receipt",
			"A simulation receipt is held only for as long as this process has room for it. Re-run the simulation to mint it again.")
		return
	}
	doc, err := Render(ReceiptContract(receipt), "", receipt.WorkerID, false)
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	h.writeDocument(w, http.StatusOK, doc, h.policyHost(r), false)
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admit(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if name == AssetIntegrityManifestName {
		h.serveAssetIntegrityManifest(w, r)
		return
	}
	metadata, catalogued := h.assetIndex[PathAssetPrefix+name]
	if !catalogued {
		h.writeProblem(w, http.StatusNotFound, "No enhancement bundle",
			"This build carries no GWC/WASM bundle; the server-rendered workspace is the whole page.")
		return
	}
	body, found := asset(name)
	if !found {
		h.writeProblem(w, http.StatusInternalServerError, "Enhancement unavailable",
			"the generated frontend asset manifest names bytes unavailable to this build")
		return
	}
	encoding := ""
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		if compressed, ok := compressedAsset(name); ok {
			body = compressed
			encoding = "gzip"
		}
	}
	w.Header().Set("Content-Type", metadata.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A request that names the exact bytes it wants can be answered once and
	// kept: the address changes whenever the bytes do, so a stored copy can
	// never be stale. Without this the bundle was re-transferred on every
	// navigation (UXLIVE-013). The cache stays private: this is an
	// authenticated asset and no shared cache may keep it.
	if contentAddressed(r, metadata.SHA256) {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	}
	w.Header().Set("Vary", "Accept-Encoding")
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}
	variant := encodingOrIdentity(encoding)
	etag, represented := metadata.ETags[variant]
	if !represented {
		h.writeProblem(w, http.StatusInternalServerError, "Enhancement unavailable",
			"the requested transfer representation is not in the generated manifest")
		return
	}
	w.Header().Set("ETag", etag)
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	// The bytes are already whole in memory, so the length is known. Writing
	// the header before the body suppresses Go's own sniffing and the
	// response would otherwise go out chunked, which leaves a browser
	// deciding whether to store a multi-megabyte entry without knowing how
	// large it will be.
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// contentAddressed reports whether the request names the asset's own digest,
// which makes its address unique to these bytes.
func contentAddressed(r *http.Request, sha256Hex string) bool {
	if strings.TrimSpace(sha256Hex) == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get(assetVersionQueryKey)), sha256Hex)
}

// assetVersionQueryKey is the parameter the loader stamps an asset address
// with. It is presentation only: the bytes served never depend on it, and a
// wrong or absent value simply falls back to revalidation.
const assetVersionQueryKey = "v"

func encodingOrIdentity(encoding string) string {
	if encoding == "" {
		return "identity"
	}
	return encoding
}

func (h *Handler) serveAssetIntegrityManifest(w http.ResponseWriter, r *http.Request) {
	// The manifest is an authenticated release description. It is never a
	// source of browser authority and is deliberately not publicly cacheable.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", h.assetManifestETag)
	if matchesETag(r.Header.Get("If-None-Match"), h.assetManifestETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.assetManifestJSON)
}

func acceptsGzip(value string) bool {
	for _, part := range strings.Split(strings.ToLower(value), ",") {
		fields := strings.Split(part, ";")
		encoding := strings.TrimSpace(fields[0])
		if encoding != "gzip" && encoding != "*" {
			continue
		}
		enabled := true
		for _, parameter := range fields[1:] {
			parameter = strings.TrimSpace(parameter)
			if strings.HasPrefix(parameter, "q=") && strings.TrimSpace(strings.TrimPrefix(parameter, "q=")) == "0" {
				enabled = false
			}
		}
		if enabled {
			return true
		}
	}
	return false
}

func matchesETag(value, etag string) bool {
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func (h *Handler) serveNotFound(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admit(w, r); !ok {
		return
	}
	h.writeProblem(w, http.StatusNotFound, "No such workspace route",
		"This cell serves the Promotion workspace at "+PathPromotion+"?worker=<ref>.")
}

// ---------------------------------------------------------------------------
// Dev browser sign-in
// ---------------------------------------------------------------------------
//
// PathLogin and PathLogout are the only workspace routes that do not run
// through admit(): reaching them is the point of not having a credential yet.
// They are registered at all only when Options.DevBrowserLogin is set, so a
// deployment that never turns the flag on serves no route here whatsoever -
// PathLogin 404s the same as any other address this cell does not answer.

// loginSessionCookie carries the actual bearer credential a person pasted at
// PathLogin. Unlike sessionCookie (a CSRF-binding value that authenticates
// nothing on its own), this cookie's value is the credential itself, so it is
// never logged and is cleared outright by PathLogout rather than rotated.
const loginSessionCookie = "hcmnext_session"
const oidcStateCookie = "hcmnext_oidc_state"

// maxLoginFormBytes bounds the pasted-token submission.
const maxLoginFormBytes = 8 << 10

// paramLoginToken names the sign-in form's one field.
const paramLoginToken = "token"
const paramLoginPersona = "persona"

// serveLoginForm renders the plain, accessible sign-in form. The employee
// search is a native GET, so its whole state is the request's own query.
func (h *Handler) serveLoginForm(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	company := ""
	if companies := h.devCompanies(); len(companies) > 0 {
		company = selectedCompany(companies, requestedCompany(r)).Key
		if query.Get(paramLoginCompany) != "" {
			h.rememberCompany(w, company)
		}
	}
	h.writeLoginPageFor(w, http.StatusOK, "", query.Get(paramDirectoryQuery), query.Get(paramDirectoryRole), company, query.Get("locale"))
}

func (h *Handler) serveOIDCLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: PathOIDCCallback,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	request, err := h.oidcFlow.BeginAuthorization(r.Context(), h.oidcTenant, h.oidcIssuerURL, h.now())
	if err != nil {
		h.writeLoginPage(w, http.StatusServiceUnavailable, "Enterprise sign-in is temporarily unavailable. Try again later.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: request.State, Path: PathOIDCCallback,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
		Expires: request.ExpiresAt, MaxAge: int(request.ExpiresAt.Sub(h.now()).Seconds())})
	http.Redirect(w, r, request.URL, http.StatusFound)
}

func (h *Handler) serveOIDCCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	stateCookie, cookieErr := r.Cookie(oidcStateCookie)
	queryState := r.URL.Query().Get("state")
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: PathOIDCCallback,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if cookieErr != nil || stateCookie == nil || queryState == "" || !hmac.Equal([]byte(stateCookie.Value), []byte(queryState)) {
		h.writeLoginPage(w, http.StatusUnauthorized, "We couldn't sign you in. Try again or contact the workspace administrator.")
		return
	}
	principal, _, err := h.oidcFlow.HandleCallback(r.Context(), oidc.CallbackParams{
		State: queryState, Code: r.URL.Query().Get("code"),
		Error: r.URL.Query().Get("error"), ErrorDescription: r.URL.Query().Get("error_description"), Now: h.now(),
	})
	if err != nil || principal == nil || principal.Tenant() != h.oidcTenant {
		h.writeLoginPage(w, http.StatusUnauthorized, "We couldn't sign you in. Try again or contact the workspace administrator.")
		return
	}
	token, err := h.oidcSessionIssuer.Issue(r.Context(), principal)
	if err != nil || strings.TrimSpace(token) == "" {
		h.writeLoginPage(w, http.StatusServiceUnavailable, "A workspace session couldn't be created. Try again later.")
		return
	}
	verified, err := h.config.Verifier.Verify(r.Context(), trust.Credential{Scheme: "Bearer", Token: token, Audience: h.config.Audience})
	if err != nil || verified == nil || verified.Tenant() != principal.Tenant() || verified.Subject() != principal.Subject() || verified.Assurance() != principal.Assurance() || verified.SessionRef() == "" {
		h.writeLoginPage(w, http.StatusServiceUnavailable, "A workspace session couldn't be validated. Try again later.")
		return
	}
	h.setLoginSessionCookie(w, token, 10*time.Minute)
	writeRedirect(w, r, PathProductHome, http.StatusSeeOther)
}

func (h *Handler) setLoginSessionCookie(w http.ResponseWriter, token string, lifetime time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: loginSessionCookie, Value: token, Path: RoutePrefix, HttpOnly: true,
		Secure: h.secure, SameSite: http.SameSiteStrictMode, Expires: h.now().Add(lifetime), MaxAge: int(lifetime.Seconds())})
}

// serveLoginSubmit verifies a pasted credential with the same trust.Verifier
// the API authenticates every other request against, and on success sets the
// session cookie admissionMetadata reads. The presented text is never echoed
// back, redirected with, or logged; a rejected credential gets a generic
// refusal and the form again, nothing that could confirm which part of it was
// wrong.
func (h *Handler) serveLoginSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormBytes)
	if err := r.ParseForm(); err != nil {
		h.writeLoginPage(w, http.StatusBadRequest, "Unreadable submission.")
		return
	}
	token := ""
	var selected *DevPersona
	if personaID := strings.TrimSpace(r.PostFormValue(paramLoginPersona)); personaID != "" {
		persona, ok := h.devPersonas[personaID]
		if !ok {
			h.writeLoginPage(w, http.StatusUnauthorized, "We couldn't find that workspace persona. Try again, or use a bearer credential or contact the workspace administrator.")
			return
		}
		issuedToken, issueErr := devPersonaToken(persona)
		if issueErr != nil {
			h.writeLoginPage(w, http.StatusServiceUnavailable, "A workspace session couldn't be created. Try again later.")
			return
		}
		token = issuedToken
		selected = &persona
	} else {
		token = normalizeBearerInput(r.PostFormValue(paramLoginToken))
	}
	if token == "" {
		h.writeLoginPage(w, http.StatusBadRequest, "Choose a development persona or paste a bearer credential.")
		return
	}
	principal, err := h.config.Verifier.Verify(r.Context(), trust.Credential{
		Token: token, Audience: h.config.Audience,
	})
	if err != nil || principal == nil {
		h.writeLoginPage(w, http.StatusUnauthorized, "We couldn't sign you in right now. Try again; if the problem continues, use a bearer credential or contact the workspace administrator.")
		return
	}
	destination := PathProductHome
	if selected != nil {
		if worker := strings.TrimSpace(selected.WorkerRef); worker != "" && worker != principal.Subject() {
			h.writeLoginPage(w, http.StatusUnauthorized, "This workspace persona is unavailable. Choose another account or contact the workspace administrator.")
			return
		}
		access, loadErr := h.resolveProductAccess(r.Context(), principal)
		if loadErr != nil {
			h.writeLoginPage(w, http.StatusServiceUnavailable, "Workspace access is temporarily unavailable. Try again later.")
			return
		}
		destination = loginPersonaLanding(access)
		if destination == "" {
			h.writeLoginPage(w, http.StatusForbidden, "This account has no available workspace pages. Contact the workspace administrator.")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     loginSessionCookie,
		Value:    token,
		Path:     RoutePrefix,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  h.now().Add(12 * time.Hour),
	})
	if selected != nil && selected.Company != "" && len(h.devCompanies()) > 0 {
		h.rememberCompany(w, selected.Company)
	}
	writeRedirect(w, r, destination, http.StatusSeeOther)
}

func loginPersonaLanding(access productAccess) string {
	candidates := []productui.PageID{productui.PageHome}
	for _, role := range access.roles {
		switch role {
		case "hiring_manager":
			candidates = append([]productui.PageID{productui.PagePeople}, candidates...)
		case "payroll_manager":
			candidates = append([]productui.PageID{productui.PageWork}, candidates...)
		case "worker_self":
			candidates = append([]productui.PageID{productui.PageMyself}, candidates...)
		}
	}
	for _, page := range candidates {
		definition, ok := productui.LookupPage(page)
		if ok && definition.NavigationPublished && access.can(page, roleaccess.ActionView) {
			return definition.Route
		}
	}
	for _, definition := range productui.PageDefinitions() {
		if definition.NavigationPublished && access.can(definition.ID, roleaccess.ActionView) {
			return definition.Route
		}
	}
	return ""
}

// serveLogout clears the session cookie PathLogin set.
func (h *Handler) serveLogout(w http.ResponseWriter, r *http.Request) {
	if revoker, ok := h.oidcSessionIssuer.(OIDCSessionRevoker); ok {
		if cookie, err := r.Cookie(loginSessionCookie); err == nil && cookie.Value != "" {
			_ = revoker.Revoke(r.Context(), cookie.Value)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     loginSessionCookie,
		Value:    "",
		Path:     RoutePrefix,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeRedirect(w, r, PathLogin, http.StatusSeeOther)
}

// normalizeBearerInput trims a pasted credential and its optional "Bearer "
// scheme prefix, so a person may paste either the raw token or the whole
// Authorization header value.
func normalizeBearerInput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	const scheme = "bearer "
	if len(trimmed) > len(scheme) && strings.EqualFold(trimmed[:len(scheme)], scheme) {
		trimmed = strings.TrimSpace(trimmed[len(scheme):])
	}
	return trimmed
}

// writeLoginPage renders the sign-in form, optionally over a refusal banner.
// It never includes the value that was submitted.
func (h *Handler) writeLoginPage(w http.ResponseWriter, status int, problem string) {
	h.writeLoginPageQuery(w, status, problem, "", "")
}

// writeLoginPageQuery is writeLoginPage with the employee directory's two
// filters. A refusal re-renders the page with neither set: the submitted
// persona is never echoed, and neither is anything else the failed request
// carried.
func (h *Handler) writeLoginPageQuery(w http.ResponseWriter, status int, problem, directoryQuery, directoryRole string) {
	h.writeLoginPageFor(w, status, problem, directoryQuery, directoryRole, "", "")
}

// writeLoginPageFor is writeLoginPageQuery for one company of a
// multi-company composition. company "" is the default company; a
// single-company composition ignores both company and locale and renders
// byte for byte what it always rendered.
func (h *Handler) writeLoginPageFor(w http.ResponseWriter, status int, problem, directoryQuery, directoryRole, companyKey, locale string) {
	companies := h.devCompanies()
	company := selectedCompany(companies, companyKey)
	defaultCompany := ""
	if len(companies) > 0 {
		defaultCompany = companies[0].Key
	}
	var banner string
	if problem != "" {
		banner = `<div class="status-banner" data-status="failed" role="alert">` + html.EscapeString(problem) + ` <a href="#credential-sign-in">Use a bearer credential</a></div>`
	}
	var personaForms strings.Builder
	for _, set := range DevPersonaRoleSets() {
		persona, ok := h.devPersonas[set.ID]
		if len(companies) > 0 {
			persona, ok = h.companyPersona(company, defaultCompany, set.ID)
		}
		if !ok {
			continue
		}
		personaForms.WriteString(`<form class="persona" method="post" action="` + PathLogin + `">`)
		access, description := h.loginPersonaCopy(persona)
		personaForms.WriteString(`<span class="persona-access">` + html.EscapeString(access) + `</span>`)
		personaForms.WriteString(`<strong>` + html.EscapeString(persona.Name) + `</strong>`)
		personaForms.WriteString(`<span>` + html.EscapeString(description) + `</span>`)
		// Put the selected persona on the successful submit control itself.
		// Besides making the association explicit to assistive technology, this
		// avoids depending on a hidden input surviving browser form mediation.
		personaForms.WriteString(`<button type="submit" name="` + paramLoginPersona + `" value="` + html.EscapeString(persona.ID) + `">Continue as ` + html.EscapeString(persona.Name) + `</button></form>`)
	}
	credentialDisclosure := ""
	if problem != "" {
		credentialDisclosure = " open"
	}
	credentialForm := ""
	if h.devBrowserLogin {
		credentialForm = `<details class="advanced" id="credential-sign-in"` + credentialDisclosure + `><summary>Use a bearer credential</summary><p>Development fallback: paste the bearer credential provided by your workspace administrator.</p><form method="post" action="` + PathLogin + `"><label for="` + paramLoginToken + `">Bearer credential</label><input type="password" id="` + paramLoginToken + `" name="` + paramLoginToken + `" autocomplete="off"><button type="submit">Sign in</button></form></details>`
	}
	if h.devBrowserLogin && len(h.devPersonas) == 0 {
		credentialForm = `<form id="credential-sign-in" method="post" action="` + PathLogin + `"><label for="` + paramLoginToken + `">Bearer credential</label><input type="password" id="` + paramLoginToken + `" name="` + paramLoginToken + `" autocomplete="off" required><button type="submit">Sign in</button></form>`
	}
	oidcLink := ""
	if h.oidcFlow != nil {
		oidcLink = `<p><a class="oidc-sign-in" href="` + PathOIDCLogin + `">Sign in with your organization</a></p>`
	}
	heading := "Sign in to your workspace"
	if h.devBrowserLogin && len(h.devPersonas) > 0 {
		heading = "Choose a workspace persona"
	}
	stylesheet := loginStylesheet()
	brand := `<div class="login-brand"><span class="login-mark" aria-hidden="true">H</span><strong>HarborCare</strong></div>`
	selector, directory := "", h.loginDirectorySection(directoryQuery, directoryRole)
	if len(companies) > 0 {
		stylesheet += loginCompanyStylesheet()
		brand = companyBrand(company)
		selector = companySelector(companies, company, locale)
		directory = h.loginCompanyDirectorySection(directoryQuery, directoryRole, company.Key)
	}
	doc := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in</title>
<style>` + stylesheet + `</style>
</head>
<body>
<main id="main-content" class="login-shell"><section class="login-card">
` + brand + `
<p class="persona-access">` + map[bool]string{true: "Local development", false: "Organization sign-in"}[h.devBrowserLogin] + `</p><h1>` + heading + `</h1>
<p class="login-intro">Each persona starts a signed server session with different permissions. Production deployments use the configured enterprise identity provider.</p>
` + oidcLink + selector + `<h2 id="quick-pick-heading">Quick picks</h2><p class="login-intro">The fixed identities the reference promotion is wired to: a proposer, a manager approver, a finance approver, and the employee.</p>
<div class="persona-grid" role="group" aria-labelledby="quick-pick-heading">` + personaForms.String() + `</div>
` + banner + directory + credentialForm + `
</section></main>
</body>
</html>
`
	if len(companies) > 0 {
		h.writeCompanyLoginDocument(w, status, doc, stylesheet)
		return
	}
	h.writeLoginDocument(w, status, doc, stylesheet)
}

// loginPersonaDescription derives the persona's sign-in copy from the same
// effective-capability projection that decides its rendered navigation menu
// and its route admission (UXAUDIT-014 REFACTOR). A promise here is
// therefore never able to outrun what the persona's own signed roles admit:
// naming a page this function did not compute from that projection is not
// possible, so a future page that loses its admission, or a persona whose
// roles no longer reach it, silently correct the copy instead of leaving it
// to drift into an overpromise that only a manual review would catch.
//
// UXAUDIT-014 REFACTOR (two projections disagreeing): this used to reflect
// productui.PageVisible on the theory that a tenant-configured
// roleaccess.Store override can only ever widen a role's reach beyond that
// floor, never narrow it, so describing the floor could understate but never
// overstate real capability. That theory was live-verified false: for
// worker_self on PageInsights, productui.PageVisible's workforce bucket
// denies (it omits "worker_self" from the role list PageInsights checks),
// while roleaccess.DefaultPagePermissions grants worker_self View on
// "insights" explicitly. serveProduct (product_shell.go) always prefers the
// roleaccess-derived allowance whenever a snapshot carries any page
// permissions at all, and productShellDocumentForRouteQuery's loading shell
// builds its navigation the same way (ApplyPagePermissions, when
// config.PagePermissions is non-empty, replaces ApplyRoleVisibility's
// productui.PageVisible projection). Every real deployment bootstraps a
// roleaccessstore.Store and seeds it from roleaccess.DefaultPagePermissions
// the first time a tenant has none (internal/data/roleaccessstore.Store.
// Bootstrap), so roleaccess's effective permissions -- not
// productui.PageVisible -- are what a signed-in worker_self persona actually
// sees on the live menu and can actually open. Describing the productui
// floor therefore understated a real, always-on capability instead of
// merely describing a conservative one. This function now derives from
// roleaccess.DefaultPagePermissions (through roleaccess.EffectivePagePermissions
// and roleaccess.CanPageAction), the same primitives serveProduct calls,
// evaluated against the registry's own default state -- these dev personas
// carry no per-tenant roleaccess.Assignment or PagePermission override, so
// DefaultPagePermissions is exactly the snapshot they resolve against.
func loginPersonaDescription(persona DevPersona) string {
	labels := admittedDestinationLabels(persona.Roles)
	if len(labels) == 0 {
		return "No workspace product area beyond Help and Settings is available to this role yet."
	}
	return "Reaches " + joinWithAnd(labels) + "."
}

// admittedDestinationLabels lists the top-level product destinations
// (Admitted, PrimaryNav pages) that roles can reach, in registry order.
// Home, Help, and Settings are every signed-in identity's baseline -- naming
// them would not distinguish one persona's promise from another's, so they
// are left out of the list a description names explicitly.
//
// Visibility is decided by roleaccess's effective page permissions, not
// productui.PageVisible -- see loginPersonaDescription's doc comment for why
// that is the projection serveProduct and the rendered menu actually honor.
func admittedDestinationLabels(roles []string) []string {
	permissions := roleaccess.EffectivePagePermissions(roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, roles)
	var labels []string
	for _, definition := range productui.PageDefinitions() {
		if !definition.Admitted || !definition.PrimaryNav {
			continue
		}
		switch definition.ID {
		case productui.PageHome, productui.PageHelp, productui.PageSettings:
			continue
		}
		if roleaccess.CanPageAction(permissions, string(definition.ID), roleaccess.ActionView) {
			labels = append(labels, definition.Label)
		}
	}
	return labels
}

// joinWithAnd renders a label list as prose: "A", "A and B", or
// "A, B, and C".
func joinWithAnd(labels []string) string {
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	case 2:
		return labels[0] + " and " + labels[1]
	default:
		return strings.Join(labels[:len(labels)-1], ", ") + ", and " + labels[len(labels)-1]
	}
}

// loginPersonaCopy describes only capabilities admitted by the persona's
// verified credential. A persona ID is a display selector, not authority: two
// IDs may carry different roles and the card must never promise access merely
// because an ID happens to be named "admin".
func (h *Handler) loginPersonaCopy(persona DevPersona) (string, string) {
	token, err := devPersonaToken(persona)
	if err != nil {
		return "Workspace member", "Access is temporarily unavailable. Try again later."
	}
	principal, err := h.config.Verifier.Verify(context.Background(), trust.Credential{
		Token: token, Audience: h.config.Audience,
	})
	if err != nil || principal == nil {
		return "Workspace member", "Available pages and actions depend on your assigned access."
	}
	// The descriptor is only a display selector. A supplied worker binding
	// must agree with the verified subject, and role hints cannot add grants.
	if worker := strings.TrimSpace(persona.WorkerRef); worker != "" && worker != principal.Subject() {
		return "Workspace member", "Available pages and actions depend on your assigned access."
	}
	access, err := h.resolveProductAccess(context.Background(), principal)
	if err != nil {
		return "Workspace member", "Access is temporarily unavailable. Try again later."
	}
	labels := admittedDestinationLabelsForAccess(access)
	title := personaAccessTitle(access)
	if len(labels) == 0 {
		return title, "No workspace product area beyond Help and Settings is available to this role yet."
	}
	switch title {
	case "Individual contributor":
		return title, "View your employment profile. Reaches " + joinWithAnd(labels) + "."
	case "Finance partner":
		return title, "Decide the finance approvals routed to you. Reaches " + joinWithAnd(labels) + "."
	case "HR partner":
		return title, "Support your assigned units and the workers in them. Reaches " + joinWithAnd(labels) + "."
	}
	return title, "Reaches " + joinWithAnd(labels) + "."
}

func devPersonaToken(persona DevPersona) (string, error) {
	if persona.IssueToken != nil {
		return persona.IssueToken()
	}
	return persona.Token, nil
}

// personaAccessTitle names what a resolved credential IS, in one phrase.
//
// Capability alone cannot name it. The ladder used to be purely
// capability-shaped - Admin, else People, else Myself - and a finance partner
// (no Admin, no People, yes Myself) fell through to "Individual contributor",
// rendering the same label and the same "View your employment profile" copy as
// a worker_self persona holding a strictly smaller credential. Two different
// authorizations read as the same thing, which is the defect UXAUDIT-014
// named in different clothes. An HR partner would have collided the same way
// with "Hiring manager".
//
// So a role whose whole point is a distinct job names itself - but only when
// the resolved policy still admits the destination that name implies. The
// label is therefore never a claim the credential cannot back: an HR partner
// whose tenant has revoked People, or a finance partner without My Work,
// falls through to the capability ladder rather than keeping a title it can
// no longer act on. Role hints still add no grants; they only choose among
// names for grants access already holds.
//
// Roles come from access, not from the signed credential directly, so a
// durable per-worker assignment that narrows a broad token narrows the label
// with it (TestTodo_UXAUDIT_014_Security_DurablePolicy).
func personaAccessTitle(access productAccess) string {
	holds := func(role string) bool {
		for _, held := range access.roles {
			if held == role {
				return true
			}
		}
		return false
	}
	switch {
	case access.can(productui.PageAdmin, roleaccess.ActionView):
		return "HCM administrator"
	case holds("hr_partner") && access.can(productui.PagePeople, roleaccess.ActionView):
		return "HR partner"
	case holds("finance_partner") && access.can(productui.PageWork, roleaccess.ActionView):
		return "Finance partner"
	case access.can(productui.PagePeople, roleaccess.ActionView):
		return "Hiring manager"
	case access.can(productui.PageMyself, roleaccess.ActionView):
		return "Individual contributor"
	}
	return "Workspace member"
}

// admittedDestinationLabelsForAccess uses the same resolved tenant permission
// snapshot as the page shell, so local-dev cards cannot overpromise when an
// organization has narrowed a role's default grants.
func admittedDestinationLabelsForAccess(access productAccess) []string {
	var labels []string
	for _, definition := range productui.PageDefinitions() {
		if !definition.Admitted || !definition.PrimaryNav {
			continue
		}
		switch definition.ID {
		case productui.PageHome, productui.PageHelp, productui.PageSettings:
			continue
		}
		if access.can(definition.ID, roleaccess.ActionView) {
			labels = append(labels, definition.Label)
		}
	}
	return labels
}

func loginStylesheet() string {
	return tokens.WorkspaceCSS() + loginSpecificStylesheet()
}

func (h *Handler) writeLoginDocument(w http.ResponseWriter, status int, doc, stylesheet string) {
	writeHTMLDocument(w, status, doc, cspPolicy{
		styleHashes:    []string{sha256Source(stylesheet)},
		formActionSelf: true,
	}.header())
}

// writeCompanyLoginDocument is writeLoginDocument for the multi-company
// page, which additionally shows each company's logo inline as a data: image
// (the asset route admits only a signed-in request).
func (h *Handler) writeCompanyLoginDocument(w http.ResponseWriter, status int, doc, stylesheet string) {
	writeHTMLDocument(w, status, doc, cspPolicy{
		styleHashes:    []string{sha256Source(stylesheet)},
		formActionSelf: true,
		dataImages:     true,
	}.header())
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// renderWorkspace runs the whole read chain for one query and renders it.
func (h *Handler) renderWorkspace(w http.ResponseWriter, r *http.Request, query Query, locale LocaleContext) {
	req, err := query.Typed()
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Unanswerable request", err.Error())
		return
	}
	reading, err := h.cell.ReadPromotion(r.Context(), req)
	switch {
	case errors.Is(err, ErrDenied):
		h.writeProblem(w, http.StatusForbidden, "Not authorized",
			"The evaluated authorization policy does not disclose this worker to you under the resolved purpose.")
		return
	case errors.Is(err, ErrWorkerUnknown):
		h.writeProblem(w, http.StatusNotFound, "No such worker",
			"No worker resolves at "+query.WorkerRef+" in this tenant.")
		return
	case errors.Is(err, ErrQueryInvalid):
		h.writeProblem(w, http.StatusBadRequest, "Unanswerable request", err.Error())
		return
	case err != nil:
		h.writeProblem(w, http.StatusBadGateway, "Cell unavailable", err.Error())
		return
	}

	var approvals ApprovalTimeline
	if reading.CompensationDisclosed {
		approvals, err = approvalTimeline(r.Context(), req, reading.Worker, query.ProposedBase)
		if err != nil {
			h.writeProblem(w, http.StatusBadGateway, "Approval route unavailable", err.Error())
			return
		}
	}

	page := BuildPageLocalized(query, req, reading, approvals, locale)
	if receipt, ok := receiptOf(page); ok {
		h.receipts.put(receipt)
	}

	csrf := h.issueSession(w, r)
	doc, err := RenderPage(page, csrf, query.WorkerRef, h.enhanced)
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	w.Header().Set("X-HCM-Workspace-Locale", locale.Resolved)
	if locale.Fallback != LocaleFallbackNone {
		w.Header().Set("X-HCM-Workspace-Locale-Fallback", string(locale.Fallback))
	}
	h.writeDocument(w, http.StatusOK, doc, h.policyHost(r), h.enhanced)
}

// writeDocument writes one workspace document under the security headers
// every workspace response carries.
func (h *Handler) writeDocument(w http.ResponseWriter, status int, doc, host string, enhanced bool) {
	writeHTMLDocument(w, status, doc, ContentSecurityPolicy(host, enhanced))
}

func writeHTMLDocument(w http.ResponseWriter, status int, doc, policy string) {
	setHTMLSecurityHeaders(w, policy)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(doc))
}

func setHTMLSecurityHeaders(w http.ResponseWriter, policy string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", policy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

func writeRedirect(w http.ResponseWriter, r *http.Request, target string, status int) {
	setHTMLSecurityHeaders(w, (cspPolicy{}).header())
	http.Redirect(w, r, target, status)
}

// writeProblem renders a refusal as a page rather than a bare status line.
//
// It carries the same stylesheet the workspace does, so the policy's
// style-src hash covers it and a refusal does not arrive as unstyled text
// under a policy that then blocks its own page's CSS. That means the whole of
// gwc.Stylesheet(), which is what stylesheetHash hashes: it inlined only the
// token layer, the bytes never matched, and the browser blocked the page's
// only stylesheet -- every 404 and refusal rendered as bare serif text.
//
// The tone follows who can act on it. A refusal the reader can resolve --
// a page their role does not grant, a route that does not exist, a
// submission to correct -- is an amber notice; red is kept for the server
// failing. And every problem page offers the way back to the workspace:
// a refusal with no link out was a dead end the reader could only escape
// with the browser's Back button.
func (h *Handler) writeProblem(w http.ResponseWriter, status int, title, detail string) {
	tone := "failed"
	if status < http.StatusInternalServerError {
		tone = "needs_review"
	}
	doc := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + `</title>
<style>` + gwc.Stylesheet() + `</style>
</head>
<body>
<div class="workspace">
<header class="workspace-header"><h1>` + html.EscapeString(title) + `</h1></header>
<main id="main-content">
<section>
<div class="status-banner" data-status="` + tone + `" role="status">` + html.EscapeString(detail) + `</div>
<p class="actions"><a href="` + PathProductPrefix + `">Back to the workspace</a></p>
</section>
</main>
</div>
</body>
</html>
`
	writeHTMLDocument(w, status, doc, cspPolicy{styleHashes: []string{stylesheetHash}}.header())
}

// ---------------------------------------------------------------------------
// Session and CSRF
// ---------------------------------------------------------------------------

// issueSession returns the CSRF token for this request's session, setting the
// session cookie when the request carries none.
func (h *Handler) issueSession(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		return h.csrfToken(cookie.Value)
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	session := hex.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    session,
		Path:     RoutePrefix,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  h.now().Add(12 * time.Hour),
	})
	return h.csrfToken(session)
}

// csrfToken derives the token for one session. It is an HMAC rather than the
// session id itself so that the value in the form cannot be replayed as the
// cookie, and vice versa.
func (h *Handler) csrfToken(session string) string {
	mac := hmac.New(sha256.New, h.sessionKey)
	mac.Write([]byte(session))
	return hex.EncodeToString(mac.Sum(nil))
}

// csrfValid reports whether a submission carries the token issued to its own
// session.
func (h *Handler) csrfValid(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	presented := strings.TrimSpace(r.PostFormValue(ParamCSRF))
	if presented == "" {
		return false
	}
	return hmac.Equal([]byte(presented), []byte(h.csrfToken(cookie.Value)))
}
