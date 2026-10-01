package edge

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
)

func deprecationMiddleware(next http.Handler, policies map[string]manifest.DeprecationPolicy, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policy, ok := policies[r.URL.Path]
		if !ok {
			for _, rule := range resourceAliasRules() {
				if _, matches := matchResourcePath(rule.template, r.URL.Path); matches {
					if candidate, found := policies[rule.procedure]; found {
						policy, ok = candidate, true
					}
					break
				}
			}
		}
		if ok {
			at := time.Now().UTC()
			if now != nil {
				at = now().UTC()
			}
			headers, err := policy.Headers(at)
			if err != nil {
				http.Error(w, "invalid deprecation policy", http.StatusInternalServerError)
				return
			}
			for key, values := range headers {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type resourceAliasRule struct {
	method, template, procedure string
}

func resourceAliasRules() []resourceAliasRule {
	m, err := manifest.Build()
	if err != nil {
		return nil
	}
	routes := manifest.PublicHTTPRoutes(m)
	out := make([]resourceAliasRule, 0, len(routes))
	for _, route := range routes {
		out = append(out, resourceAliasRule{method: route.Method, template: route.ResourcePath, procedure: route.Procedure})
	}
	return out
}

// resourceAliasMiddleware rewrites a resource-shaped alias into the
// canonical Connect procedure before admission and decoding. The rewrite is
// deliberately transport-local: authorization and business behavior remain
// owned by the canonical handler.
func resourceAliasMiddleware(next http.Handler, rules []resourceAliasRule) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rule := range rules {
			if !strings.EqualFold(rule.method, r.Method) {
				continue
			}
			params, ok := matchResourcePath(rule.template, r.URL.Path)
			if !ok {
				continue
			}
			if r.Method == http.MethodGet {
				body, err := aliasRequestBody(params, r.URL.Query())
				if err != nil {
					http.Error(w, "invalid resource alias", http.StatusBadRequest)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				r.ContentLength = int64(len(body))
				r.Header.Set("Content-Type", "application/json")
				r.Method = http.MethodPost
			}
			r.URL.Path = rule.procedure
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func matchResourcePath(template, actual string) (map[string]string, bool) {
	want := strings.Split(strings.Trim(template, "/"), "/")
	got := strings.Split(strings.Trim(actual, "/"), "/")
	if len(want) != len(got) {
		return nil, false
	}
	params := make(map[string]string)
	for i := range want {
		part := want[i]
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			value, err := url.PathUnescape(got[i])
			if err != nil || value == "" {
				return nil, false
			}
			params[strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")] = value
			continue
		}
		if part != got[i] {
			return nil, false
		}
	}
	return params, true
}

func aliasRequestBody(params map[string]string, query url.Values) ([]byte, error) {
	body := make(map[string]string, len(params)+len(query))
	for key, value := range params {
		body[toJSONFieldName(key)] = value
	}
	for key, values := range query {
		if len(values) > 0 {
			body[key] = values[0]
		}
	}
	return json.Marshal(body)
}

func toJSONFieldName(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		if r == '_' || r == '-' {
			upper = true
			continue
		}
		if upper {
			if r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			upper = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
