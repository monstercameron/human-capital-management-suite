package clockpartner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	timeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"gopkg.in/yaml.v3"
)

const maxOpenAPIBody = 1 << 20

// FetchClockOpenAPI fetches and validates the contract actually served by a
// partner endpoint. It never accepts caller-supplied document bytes.
func FetchClockOpenAPI(ctx context.Context, endpoint string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("%w: invalid endpoint", ErrConformance)
	}
	if !strings.HasSuffix(u.Path, timeclock.OpenAPIPath) {
		u.Path = strings.TrimRight(u.Path, "/") + timeclock.OpenAPIPath
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrConformance, err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch served contract: %v", ErrConformance, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: contract endpoint returned HTTP %d", ErrConformance, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenAPIBody+1))
	if err != nil || len(body) > maxOpenAPIBody {
		return nil, fmt.Errorf("%w: contract exceeds 1 MiB", ErrConformance)
	}
	if err := validateServedContract(body); err != nil {
		return nil, err
	}
	return body, nil
}

func validateServedContract(document []byte) error {
	var root yaml.Node
	if err := yaml.Unmarshal(document, &root); err != nil {
		return fmt.Errorf("%w: invalid served YAML: %v", ErrConformance, err)
	}
	if len(root.Content) != 1 {
		return fmt.Errorf("%w: empty OpenAPI document", ErrConformance)
	}
	rootMap := root.Content[0]
	if rootMap.Kind != yaml.MappingNode {
		return fmt.Errorf("%w: OpenAPI root is not a mapping", ErrConformance)
	}
	paths, components := mappingValue(rootMap, "paths"), mappingValue(rootMap, "components")
	if paths == nil || components == nil || mappingValue(components, "schemas") == nil {
		return fmt.Errorf("%w: missing paths or component schemas", ErrConformance)
	}
	for _, path := range []string{"/v1/time/clock-device/SubmitPunches", "/v1/time/self", "/v1/time/self/in", "/v1/time/self/out", "/v1/time/missing-punch/SubmitCorrection"} {
		item := mappingValue(paths, path)
		if item == nil || (mappingValue(item, "post") == nil && mappingValue(item, "get") == nil) {
			return fmt.Errorf("%w: missing served path %s", ErrConformance, path)
		}
	}
	return nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
