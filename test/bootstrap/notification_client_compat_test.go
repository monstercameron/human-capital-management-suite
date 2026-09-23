package bootstrap_test

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
)

// Preserve this harness's Connect request convention while exercising the
// canonical generated clients. There is no second wire implementation here.
type bootstrapIntentClient struct{ inner clients.IntentClient }
type bootstrapRegistryClient struct{ inner clients.RegistryClient }

func newBootstrapIntentClient(client *http.Client, url string) *bootstrapIntentClient {
	return &bootstrapIntentClient{inner: clients.NewIntentClientConnect(client, url)}
}

func newBootstrapRegistryClient(client *http.Client, url string) *bootstrapRegistryClient {
	return &bootstrapRegistryClient{inner: clients.NewRegistryClientConnect(client, url)}
}

func bootstrapClientCall[Req, Res any](ctx context.Context, req *connect.Request[Req], call func(context.Context, *Req, ...clients.CallOption) (*Res, error)) (*connect.Response[Res], error) {
	var opts []clients.CallOption
	for key, values := range req.Header() {
		for _, value := range values {
			opts = append(opts, clients.WithHeader(key, value))
		}
	}
	res, err := call(ctx, req.Msg, opts...)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(res), nil
}

func (c *bootstrapIntentClient) CreateIntent(ctx context.Context, req *connect.Request[intentsv1.CreateIntentRequest]) (*connect.Response[intentsv1.CreateIntentResponse], error) {
	return bootstrapClientCall(ctx, req, c.inner.CreateIntent)
}
func (c *bootstrapIntentClient) SimulateIntent(ctx context.Context, req *connect.Request[intentsv1.SimulateIntentRequest]) (*connect.Response[intentsv1.SimulateIntentResponse], error) {
	return bootstrapClientCall(ctx, req, c.inner.SimulateIntent)
}
func (c *bootstrapIntentClient) ExecuteIntent(ctx context.Context, req *connect.Request[intentsv1.ExecuteIntentRequest]) (*connect.Response[intentsv1.ExecuteIntentResponse], error) {
	return bootstrapClientCall(ctx, req, c.inner.ExecuteIntent)
}
func (c *bootstrapRegistryClient) ListIntentDefinitions(ctx context.Context, req *connect.Request[registryv1.ListIntentDefinitionsRequest]) (*connect.Response[registryv1.ListIntentDefinitionsResponse], error) {
	return bootstrapClientCall(ctx, req, c.inner.ListIntentDefinitions)
}
