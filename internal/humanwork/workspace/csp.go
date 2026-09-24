package workspace

import (
	"crypto/sha256"
	"encoding/base64"
	"net"
	"sort"
	"strconv"
	"strings"
)

// cspPolicy is the small, typed boundary between a rendered shell and the
// policy that authorizes it.  Shells may opt into the few capabilities they
// need, but every capability is deny-by-default and every inline source is
// accepted only when its hash is a well-formed SHA-256 CSP source.
type cspPolicy struct {
	styleHashes           []string
	scriptHash            string
	formActionSelf        bool
	connectHost           string
	allowAssetConnections bool
	allowTunnelConnection bool
	// allowMediaConnections lets the page fetch protected chat media from
	// PathChatMediaPrefix with its bearer and a grant header. The bytes are
	// shown through blob URLs, so blobImages accompanies it.
	allowMediaConnections bool
	// allowGiphy enables direct browser requests for the optional GIF picker.
	allowGiphy       bool
	sameOriginImages bool
	blobImages       bool
	allowBlobScript  bool
	allowWASM        bool
}

func (p cspPolicy) header() string {
	directives := []string{
		"default-src 'none'",
		"base-uri 'none'",
		"object-src 'none'",
		"frame-src 'none'",
		"child-src 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
	}
	if p.formActionSelf {
		directives[5] = "form-action 'self'"
	}

	scriptSources := cspHashSource(p.scriptHash)
	if scriptSources != "'none'" {
		// The authenticated loader imports wasm_exec.js through an SRI-checked
		// blob URL, so no general same-origin script capability is needed.
		// script-src remains the CSP2 fallback; script-src-elem below is the
		// stricter element boundary for browsers that implement CSP3.
		if p.allowBlobScript {
			scriptSources += " blob:"
		}
		if p.allowWASM {
			scriptSources += " 'wasm-unsafe-eval'"
		}
	}
	directives = append(directives,
		"script-src "+scriptSources,
		"script-src-elem "+cspScriptElementSources(p.scriptHash, p.allowBlobScript),
		"script-src-attr 'none'",
		"style-src "+cspStyleSources(p.styleHashes),
		"style-src-elem "+cspStyleSources(p.styleHashes),
		"style-src-attr 'none'",
	)

	connectSources := cspConnectSources(p.connectHost, p.allowAssetConnections, p.allowTunnelConnection, p.allowMediaConnections)
	if p.allowGiphy {
		if connectSources == "'none'" {
			connectSources = "https://api.giphy.com"
		} else {
			connectSources += " https://api.giphy.com"
		}
	}
	directives = append(directives, "connect-src "+connectSources)
	switch {
	case p.sameOriginImages && p.blobImages && p.allowGiphy:
		directives = append(directives, "img-src 'self' blob: https://*.giphy.com")
	case p.sameOriginImages && p.blobImages:
		directives = append(directives, "img-src 'self' blob:")
	case p.sameOriginImages:
		directives = append(directives, "img-src 'self'")
	case p.blobImages:
		directives = append(directives, "img-src blob:")
	default:
		directives = append(directives, "img-src 'none'")
	}
	directives = append(directives,
		"font-src 'none'",
		"media-src 'none'",
		"worker-src 'none'",
		"manifest-src 'none'",
	)
	return strings.Join(directives, "; ")
}

func cspHashSource(value string) string {
	if !validCSPHashSource(value) {
		return "'none'"
	}
	return "'" + value + "'"
}

func cspScriptElementSources(scriptHash string, allowBlob bool) string {
	sources := cspHashSource(scriptHash)
	if sources == "'none'" {
		return sources
	}
	if allowBlob {
		sources += " blob:"
	}
	return sources
}

func cspStyleSources(values []string) string {
	valid := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validCSPHashSource(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		valid = append(valid, value)
	}
	if len(valid) == 0 {
		return "'none'"
	}
	sort.Strings(valid)
	quoted := make([]string, len(valid))
	for i, value := range valid {
		quoted[i] = "'" + value + "'"
	}
	return strings.Join(quoted, " ")
}

func cspConnectSources(rawHost string, allowAssets, allowTunnel, allowMedia bool) string {
	if !allowAssets && !allowTunnel && !allowMedia {
		return "'none'"
	}
	authority := sanitizeHostAuthority(rawHost)
	if authority == "" {
		return "'none'"
	}
	sources := make([]string, 0, 6)
	if allowAssets {
		sources = append(sources,
			"http://"+authority+PathAssetPrefix,
			"https://"+authority+PathAssetPrefix,
		)
	}
	if allowMedia {
		sources = append(sources,
			"http://"+authority+PathChatMediaPrefix,
			"https://"+authority+PathChatMediaPrefix,
			"http://"+authority+PathDocumentMediaPrefix,
			"https://"+authority+PathDocumentMediaPrefix,
		)
	}
	if allowTunnel {
		sources = append(sources,
			"ws://"+authority+PathTunnel,
			"wss://"+authority+PathTunnel,
		)
	}
	return strings.Join(sources, " ")
}

func validCSPHashSource(value string) bool {
	const prefix = "sha256-"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != base64.StdEncoding.EncodedLen(sha256.Size) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && len(raw) == sha256.Size && base64.StdEncoding.EncodeToString(raw) == encoded
}

// sanitizeHostAuthority returns a canonical CSP host[:port] authority, or an
// empty string. Host is request text, so malformed input must not become a
// source expression or an endpoint embedded in the shell's JSON island.
//
// CSP host-source matching does not define useful production semantics for
// literal IPv6 addresses or non-loopback IPv4 addresses. Refuse both instead
// of emitting a syntactically plausible source browsers will ignore. DNS
// deployments and the CSP-defined 127.0.0.1 development exception remain
// representable.
func sanitizeHostAuthority(raw string) string {
	host := raw
	if host == "" || host != strings.TrimSpace(host) || len(host) > 255 || strings.ContainsAny(host, "\x00\r\n\t /\\\"';") {
		return ""
	}

	if strings.HasPrefix(host, "[") {
		return ""
	}
	if strings.ContainsAny(host, "[]") {
		return ""
	}

	name := host
	port := ""
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
		if strings.Contains(host[:colon], ":") {
			return ""
		}
		name = host[:colon]
		var ok bool
		port, ok = normalizeCSPPort(host[colon+1:])
		if !ok {
			return ""
		}
	}
	if name == "" || len(name) > 253 {
		return ""
	}
	if parsed := net.ParseIP(name); parsed != nil {
		if parsed.To4() == nil || name != "127.0.0.1" {
			return ""
		}
	} else if looksLikeIPAddress(name) {
		return ""
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return ""
			}
		}
	}
	authority := strings.ToLower(name)
	if port != "" {
		authority += ":" + port
	}
	return authority
}

func normalizeCSPPort(value string) (string, bool) {
	if value == "" || len(value) > 5 {
		return "", false
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "", false
	}
	return strconv.Itoa(port), true
}

func looksLikeIPAddress(value string) bool {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "0x") {
		return true
	}
	for _, r := range lower {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}
