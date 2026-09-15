// Package journeyclient is the browser client's brain for the Promotion
// journey page: everything the page does between "an RPC answered" and "the
// renderer has a new Page", with none of the browser in it.
//
// The page itself is three packages. The shell
// (internal/humanwork/workspace.serveJourney) is a document that carries a
// JSON island and loads a wasm module. The module
// (tools/uxqual/cmd/journeywasm) is fifty lines of syscall/js: read the
// island, dial the tunnel, mount the renderer, forward hash changes. This
// package is everything else -- the configuration, the routes, the
// projection from Protobuf answers onto tools/uxqual/render/journey's
// contract, and the state machine that decides which RPC to make and what
// the reader sees while it is in flight.
//
// It is deliberately free of syscall/js so all of that is exercised by
// ordinary `go test` on the native toolchain: the only thing the wasm build
// adds is a DOM and a socket.
//
// # What it may import
//
// definitions/architecture/dependency-roles.yaml admits google.golang.org/grpc
// and github.com/monstercameron/GoGRPCBridge into exactly two roots,
// tools/uxqual/journeyclient and tools/uxqual/cmd/journeywasm. This package
// therefore imports grpc (for the client connection interface, per-RPC
// metadata and the status codes it projects into notices) and the generated
// journey client, and nothing else from the transport stack. The dialer
// itself is the command's business, not this package's: a Service is built
// from any grpc.ClientConnInterface, which is what lets every test here run
// against a fake.
package journeyclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Config is the JSON island the shell writes into the document, parsed.
//
// The field names are a compile-time contract with
// internal/humanwork/workspace.JourneyConfig: the two structs are separate
// declarations (the server may not import a tools/ package, and this package
// may not import internal/) with identical json tags, and
// TestConfigMatchesTheShellIsland pins them to each other by parsing a
// document the shell actually produced.
type Config struct {
	// Locale is resolved by the browser from the product route or document.
	// It is presentation state, never a credential or authorization input.
	Locale string `json:"-"`
	// TunnelURL is the absolute ws:// or wss:// address of the cell's gRPC
	// tunnel.
	TunnelURL string `json:"tunnel_url"`
	// Bearer is the credential every RPC this page makes must carry as
	// "authorization: Bearer <bearer>" metadata. The tunnel forwards no
	// Authorization header from the upgrade request, so a call without it is
	// admitted anonymously and refused UNAUTHENTICATED.
	Bearer string `json:"bearer"`
	// Tenant, Subject, Roles and Purpose are display facts about the admitted
	// principal. Nothing is authorized from them: every RPC is authorized
	// server-side from the credential.
	Tenant  string   `json:"tenant"`
	Subject string   `json:"subject"`
	Roles   []string `json:"roles"`
	// PagePermissions is the effective, server-derived union of the signed-in
	// worker's durable role grants. It only controls browser discoverability
	// and affordances; every mutation is authorized again by the service.
	PagePermissions    []PagePermission    `json:"page_permissions,omitempty"`
	FeaturePermissions []FeaturePermission `json:"feature_permissions"`
	LauncherActions    []LauncherAction    `json:"launcher_actions,omitempty"`
	Purpose            string              `json:"purpose"`
	// JourneysPath is the page's own address, used for the masthead link back
	// to itself.
	JourneysPath string `json:"journeys_path"`
	// LogoutPath is present only for the explicitly enabled local browser
	// session. It is presentation metadata, never authentication input.
	LogoutPath string `json:"logout_path,omitempty"`
}

// LauncherAction mirrors the server-resolved presentation-safe action
// verdict carried by the authenticated shell island.
type LauncherAction struct {
	ID            string `json:"id"`
	Availability  string `json:"availability"`
	Reason        string `json:"reason,omitempty"`
	RecoveryLabel string `json:"recovery_label,omitempty"`
	RecoveryHref  string `json:"recovery_href,omitempty"`
	Priority      int64  `json:"priority,omitempty"`
}

// PagePermission mirrors the presentation-only page/action projection in
// the shell island without importing the server's internal policy package.
type PagePermission struct {
	Version int64  `json:"version"`
	RoleID  string `json:"RoleID"`
	PageID  string `json:"PageID"`
	View    bool   `json:"View"`
	Create  bool   `json:"Create"`
	Update  bool   `json:"Update"`
	Delete  bool   `json:"Delete"`
}

// FeaturePermission mirrors one effective page-feature CRUD grant from the
// server-owned role policy.
type FeaturePermission struct {
	Version   int64  `json:"version"`
	RoleID    string `json:"RoleID"`
	PageID    string `json:"PageID"`
	FeatureID string `json:"FeatureID"`
	View      bool   `json:"View"`
	Create    bool   `json:"Create"`
	Update    bool   `json:"Update"`
	Delete    bool   `json:"Delete"`
}

// CanPageAction checks one effective browser affordance. It is not an
// authorization decision; the server repeats the check from trusted state.
func (c Config) CanPageAction(pageID, action string) bool {
	for _, permission := range c.PagePermissions {
		if permission.PageID != pageID {
			continue
		}
		switch action {
		case "view":
			return permission.View
		case "create":
			return permission.Create
		case "update":
			return permission.Update
		case "delete":
			return permission.Delete
		}
	}
	return false
}

func (c Config) CanFeatureAction(pageID, featureID, action string) bool {
	if !c.CanPageAction(pageID, action) {
		return false
	}
	for _, permission := range c.FeaturePermissions {
		if permission.PageID != pageID || permission.FeatureID != featureID {
			continue
		}
		switch action {
		case "view":
			return permission.View
		case "create":
			return permission.Create
		case "update":
			return permission.Update
		case "delete":
			return permission.Delete
		}
	}
	return false
}

// Refusals ParseConfig reports. They are distinguished because the client
// shows a different sentence for each: a page with no tunnel cannot be
// fixed by signing in again, and a page with no credential can.
var (
	// ErrConfigMalformed means the island was not the JSON object this
	// client's contract with the shell says it is.
	ErrConfigMalformed = errors.New("journeyclient: the journey-config island is not a JSON object")
	// ErrConfigNoTunnel means the island carried no usable ws:// or wss://
	// tunnel address.
	ErrConfigNoTunnel = errors.New("journeyclient: the journey-config island carries no ws:// or wss:// tunnel_url")
	// ErrConfigNoBearer means the island carried no credential. Every RPC
	// needs one, so this is fatal to the page rather than a degraded mode.
	ErrConfigNoBearer = errors.New("journeyclient: the journey-config island carries no bearer")
)

// ParseConfig reads the island.
//
// It refuses rather than degrading: a client that dialled nothing, or that
// dialled and then made anonymous calls, would show the reader a page full
// of UNAUTHENTICATED refusals whose cause is not on the page. Failing at
// parse time puts the cause where the reader is looking.
func ParseConfig(data []byte) (Config, error) {
	var c Config
	// Unknown fields are allowed on purpose: the shell may carry a new
	// display fact to a client that predates it, and a page that refused to
	// start over a field it does not read would make adding one a lockstep
	// deployment.
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrConfigMalformed, err)
	}
	if !validTunnelURL(c.TunnelURL) {
		return Config{}, ErrConfigNoTunnel
	}
	if strings.TrimSpace(c.Bearer) == "" {
		return Config{}, ErrConfigNoBearer
	}
	c.Bearer = strings.TrimSpace(c.Bearer)
	if c.JourneysPath == "" {
		c.JourneysPath = DefaultJourneysPath
	}
	return c, nil
}

// DefaultJourneysPath is the page's own address when the island did not name
// one. It mirrors internal/humanwork/workspace.PathJourney.
// diagnosticsPageID names PROMOUX-008's authorized diagnostics disclosure to
// CanPageAction. It shares its wire id with roleaccess.PageJourneyDiagnostics
// and productui.PageJourneyDiagnostics so one role grant governs every
// surface that shows it.
const diagnosticsPageID = "journey-diagnostics"

const DefaultJourneysPath = "/workspace/journey"

// WorkspacePath is the server-rendered Promotion workspace, which the
// masthead links to as an ordinary document link.
const WorkspacePath = "/workspace/promotion"

func validTunnelURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "ws" || u.Scheme == "wss"
}

// DialTarget is the string to hand grpc.NewClient for this configuration.
//
// The WebSocket dialer ignores the target entirely -- it dials
// [Config.TunnelURL] itself -- but grpc still parses it, resolves it and
// derives the :authority header from it. Two things follow. It must carry a
// registered resolver scheme, because grpc.NewClient's default is "dns" and
// a DNS resolver in a browser has no resolver to reach; "passthrough:///"
// hands the endpoint straight through with no lookup. And its endpoint
// should be the tunnel's host authority rather than the whole ws:// URL, so
// the :authority the cell sees is the host the page was served from rather
// than a URL with a scheme and a path in it.
func (c Config) DialTarget() string {
	u, err := url.Parse(strings.TrimSpace(c.TunnelURL))
	if err != nil || u.Host == "" {
		return "passthrough:///" + strings.TrimSpace(c.TunnelURL)
	}
	return "passthrough:///" + u.Host
}
