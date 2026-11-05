// Package mongo is the WF-DATA-015 MongoDB adapter: the only package that
// may import the mongo driver. It turns a validated datasource.Source
// into the datasource.Connector contract. No mongo.* type appears in an
// exported signature; credentials travel struct-field to struct-field
// (never through a URI string) and never enter an error message.
package mongo

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/datasource"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AdapterID is the datasource registry name for this adapter.
const AdapterID = "mongodb"

// DefaultPort is the wire default when Source.Port is zero.
const DefaultPort = 27017

const (
	// appName identifies driver connections in server logs.
	appName = "hcm-next-datasource"
	// connectTimeout caps the initial handshake.
	connectTimeout = 5 * time.Second
	// serverSelectionTimeout caps waiting for a usable server.
	serverSelectionTimeout = 5 * time.Second
	// socketTimeout caps a single idle socket operation.
	socketTimeout = 30 * time.Second
)

// Factory returns the datasource adapter factory. Open order is fixed:
// validate, egress allow-list, secret resolution, dial with mandatory
// TLS, then ping before the connector is handed out.
func Factory() datasource.AdapterFactory {
	return func(ctx context.Context, src datasource.Source, deps datasource.OpenDeps) (datasource.Connector, error) {
		if err := src.Validate(); err != nil {
			return nil, err
		}
		port := src.Port
		if port == 0 {
			port = DefaultPort
		}
		if err := deps.Egress.AllowHost(ctx, src.Host, port); err != nil {
			return nil, fmt.Errorf("mongo: source %s/%s: %w", src.Tenant, src.ID, err)
		}
		cred, err := deps.Secrets.ResolveSecret(ctx, src.Tenant, src.Secret)
		if err != nil {
			return nil, fmt.Errorf("%w: source %s/%s: %v", datasource.ErrSecretResolution, src.Tenant, src.ID, err)
		}
		if cred.Username == "" || cred.Password == "" {
			return nil, fmt.Errorf("%w: source %s/%s carries incomplete material", datasource.ErrSecretResolution, src.Tenant, src.ID)
		}
		client, err := mongo.Connect(ctx, buildOptions(src, port, cred))
		if err != nil {
			return nil, fmt.Errorf("%w: source %s/%s dial: %v", datasource.ErrUnavailable, src.Tenant, src.ID, err)
		}
		if err := client.Ping(ctx, nil); err != nil {
			_ = client.Disconnect(ctx)
			return nil, fmt.Errorf("%w: source %s/%s ping: %v", datasource.ErrUnavailable, src.Tenant, src.ID, err)
		}
		return newConnector(client, src), nil
	}
}

// buildOptions renders the dial configuration. TLS is unconditional:
// Source.Validate already refuses TLSRequired=false, and this builder
// offers no knob to turn it back off. Authentication binds to the
// tenant database so tenant-scoped SCRAM users stay scoped.
func buildOptions(src datasource.Source, port int, cred datasource.Credential) *options.ClientOptions {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if ip := net.ParseIP(src.Host); ip == nil {
		tlsConfig.ServerName = src.Host
	}
	return options.Client().
		SetHosts([]string{net.JoinHostPort(src.Host, fmt.Sprint(port))}).
		SetAuth(options.Credential{
			Username:   cred.Username,
			Password:   cred.Password,
			AuthSource: src.Database,
		}).
		SetTLSConfig(tlsConfig).
		SetAppName(appName).
		SetConnectTimeout(connectTimeout).
		SetServerSelectionTimeout(serverSelectionTimeout).
		SetSocketTimeout(socketTimeout)
}
