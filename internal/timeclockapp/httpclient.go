package timeclockapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// maxResponseBytes bounds a device API answer. A SubmitPunches response for
// a full queue is a few kilobytes.
const maxResponseBytes = 1 << 20

// HTTPDoer is the part of *http.Client the device client needs.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// HTTPClient calls ClockDeviceService over the edge's HTTP JSON (protojson)
// projection. In the browser Base is the page's own origin: the kiosk
// server proxies the device routes, so the page stays same-origin.
type HTTPClient struct {
	Base string
	Doer HTTPDoer
}

var _ DeviceAPI = (*HTTPClient)(nil)

// NewHTTPClient returns a client for base using doer (http.DefaultClient
// when nil).
func NewHTTPClient(base string, doer HTTPDoer) *HTTPClient {
	if doer == nil {
		doer = http.DefaultClient
	}
	return &HTTPClient{Base: strings.TrimRight(base, "/"), Doer: doer}
}

func (c *HTTPClient) EnrollDevice(ctx context.Context, in *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	out := &timev1.EnrollDeviceResponse{}
	return out, c.call(ctx, MethodEnrollDevice, in, out)
}

func (c *HTTPClient) IdentifyWorker(ctx context.Context, in *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	out := &timev1.IdentifyWorkerResponse{}
	return out, c.call(ctx, MethodIdentifyWorker, in, out)
}

func (c *HTTPClient) SubmitPunches(ctx context.Context, in *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	out := &timev1.SubmitPunchesResponse{}
	return out, c.call(ctx, MethodSubmitPunches, in, out)
}

func (c *HTTPClient) Heartbeat(ctx context.Context, in *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	out := &timev1.HeartbeatResponse{}
	return out, c.call(ctx, MethodHeartbeat, in, out)
}

// SyncRoster retrieves the cursor-based device roster projection.
func (c *HTTPClient) SyncRoster(ctx context.Context, in *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	out := &timev1.SyncRosterResponse{}
	return out, c.call(ctx, MethodSyncRoster, in, out)
}

// GetWorkerStatus refreshes the identified worker's current session state.
func (c *HTTPClient) GetWorkerStatus(ctx context.Context, in *timev1.GetWorkerStatusRequest) (*timev1.GetWorkerStatusResponse, error) {
	out := &timev1.GetWorkerStatusResponse{}
	return out, c.call(ctx, MethodWorkerStatus, in, out)
}

// URL is the route one method is posted to. It refuses any method off the
// allow-list, so no code path in the client can reach another route.
func (c *HTTPClient) URL(method string) (string, error) {
	if !IsDeviceMethod(method) {
		return "", fmt.Errorf("timeclockapp: %q is not a kiosk device method", method)
	}
	return c.Base + ServicePath + method, nil
}

func (c *HTTPClient) call(ctx context.Context, method string, in, out proto.Message) error {
	url, err := c.URL(method)
	if err != nil {
		return err
	}
	body, err := protojson.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Doer.Do(req)
	if err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(raw, out)
	case resp.StatusCode == http.StatusBadGateway, resp.StatusCode == http.StatusServiceUnavailable, resp.StatusCode == http.StatusGatewayTimeout:
		return fmt.Errorf("%w: upstream status %d", ErrUnreachable, resp.StatusCode)
	default:
		return decodeAPIError(resp.StatusCode, raw)
	}
}

// decodeAPIError reads a connect-style {"code","message"} error body.
func decodeAPIError(status int, raw []byte) *APIError {
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &body)
	return &APIError{Status: status, Code: body.Code, Message: body.Message}
}
