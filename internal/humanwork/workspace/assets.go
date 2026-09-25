package workspace

import (
	"embed"
	"errors"
	"io/fs"
	"strconv"
	"strings"
)

// Asset file names the progressive-enhancement bundle is looked for under.
const (
	assetWasm = "uxqual.wasm"
	// assetJourneyWasm is the Promotion journey page's own client. It is a
	// second wasm module rather than a second entrypoint in the first one
	// because the two pages are different products: uxqual.wasm re-renders
	// an already-complete server-rendered document, journey.wasm is the
	// whole page and reaches the cell over the gRPC tunnel.
	assetJourneyWasm    = "journey.wasm"
	assetWasmExec       = "wasm_exec.js"
	assetHarborcareLogo = "harborcare-logo.svg"
	assetIronridgeLogo  = "ironridge-logo.svg"
)

// assetsFS is the embedded bundle directory.
//
// It is embedded with the "all:" prefix so the directory can be committed
// containing only its marker file: a stock checkout carries no bundle, and
// the absence is a served 404 rather than a build failure. See this package's
// doc comment for the two commands that populate it.
//
//go:embed all:assets
var assetsFS embed.FS

// asset returns one embedded bundle file.
func asset(name string) ([]byte, bool) {
	// uxqual.wasm is the legacy fixture-only mount. It rebuilds the request
	// form with action="#" and without this workspace's CSRF, worker, locale,
	// and form-owner binding. Never advertise or serve it as an enhancement
	// until its entrypoint consumes the bound contract island. The complete
	// server-rendered POST form remains the production path.
	if name == assetWasm {
		return nil, false
	}
	if _, ok := FrontendAssetContentType(name); !ok {
		return nil, false
	}
	return embeddedAsset(name)
}

func isSeedPhotoProxy(name string) bool {
	const prefix, suffix = "person-hc-", "-small.jpg"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	return isSeedPhotoIndex(strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix))
}

func isSeedPhotoIndex(raw string) bool {
	if len(raw) != 3 {
		return false
	}
	index, err := strconv.Atoi(raw)
	return err == nil && index >= 1 && index <= 59 && index%4 != 0
}

func embeddedAsset(name string) ([]byte, bool) {
	body, err := fs.ReadFile(assetsFS, "assets/"+name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, false
		}
		return nil, false
	}
	return body, true
}

// compressedAsset returns only build-produced transfer representations for
// compressible executable assets. The .gz files are never directly routable.
func compressedAsset(name string) ([]byte, bool) {
	switch name {
	case assetJourneyWasm, assetWasmExec:
		if _, identityExists := embeddedAsset(name); !identityExists {
			return nil, false
		}
		return embeddedAsset(name + ".gz")
	default:
		return nil, false
	}
}

// FrontendAssetContentType returns the fixed media type for a publicly
// routable frontend asset. Packaging uses this same allowlist as serving so
// source originals and legacy fixtures cannot enter the release manifest.
func FrontendAssetContentType(name string) (string, bool) {
	switch name {
	case assetJourneyWasm, assetWasmExec, assetHarborcareLogo, assetIronridgeLogo:
	default:
		if !isSeedPhotoProxy(name) {
			return "", false
		}
	}
	if name == assetWasm || name == assetJourneyWasm {
		return "application/wasm", true
	}
	if strings.HasSuffix(name, ".png") {
		return "image/png", true
	}
	if strings.HasSuffix(name, ".jpg") {
		return "image/jpeg", true
	}
	if strings.HasSuffix(name, ".svg") {
		return "image/svg+xml", true
	}
	return "text/javascript; charset=utf-8", true
}

func assetContentType(name string) string {
	if name == assetWasm || name == assetJourneyWasm {
		return "application/wasm"
	}
	if strings.HasSuffix(name, ".png") {
		return "image/png"
	}
	if strings.HasSuffix(name, ".jpg") {
		return "image/jpeg"
	}
	if strings.HasSuffix(name, ".svg") {
		return "image/svg+xml"
	}
	return "text/javascript; charset=utf-8"
}

// BundleBuilt reports whether both halves of the progressive-enhancement
// bundle are embedded in this build. When it is false the workspace serves
// the server-rendered document only, which is fully functional on its own.
func BundleBuilt() bool {
	_, wasm := asset(assetWasm)
	_, shim := asset(assetWasmExec)
	return wasm && shim
}

// JourneyBundleBuilt reports whether both halves of the Promotion journey
// page's client are embedded in this build. It is [BundleBuilt] for the other
// wasm module and shares the shim with it.
//
// Unlike the workspace, the journey page has no server-rendered equivalent to
// fall back to, so a false answer here is not "serve less enhancement": it is
// a page that cannot do anything until the bundle is built. The shell says so
// in the document rather than 404ing, because the person who needs to read
// that sentence is whoever just opened the page in a build that has no
// bundle.
func JourneyBundleBuilt() bool {
	_, wasm := asset(assetJourneyWasm)
	_, shim := asset(assetWasmExec)
	return wasm && shim
}
