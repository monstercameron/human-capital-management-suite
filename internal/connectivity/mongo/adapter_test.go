package mongo

import (
	"context"
	"crypto/tls"
	"errors"
	"math"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/datasource"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	testUsername = "tenant-user-must-not-leak"
	testPassword = "tenant-password-must-not-leak"
)

type stubSecrets struct {
	cred datasource.Credential
	err  error
}

func (s stubSecrets) ResolveSecret(_ context.Context, _ string, _ datasource.SecretRef) (datasource.Credential, error) {
	return s.cred, s.err
}

type stubEgress struct {
	denied     bool
	host       string
	port       int
	calls      int
	deniedHost string
}

func (s *stubEgress) AllowHost(_ context.Context, host string, port int) error {
	s.calls++
	s.host, s.port = host, port
	if s.denied {
		return errors.New("egress stub denies all hosts")
	}
	return nil
}

func testSource() datasource.Source {
	return datasource.Source{
		Tenant:              "tenant-a",
		ID:                  "ledger",
		AdapterID:           AdapterID,
		Host:                "mongo.internal",
		Port:                0,
		Database:            "ledgerdb",
		Secret:              datasource.SecretRef{Store: "platform", Key: "ledger-ro"},
		TLSRequired:         true,
		ReadOnly:            true,
		WritableCollections: nil,
	}
}

// TestBuildOptionsRequiresTLS pins the dial shape: TLS 1.2+ with SNI,
// tenant-database auth, single allow-listed host, and bounded timeouts.
func TestBuildOptionsRequiresTLS(t *testing.T) {
	src := testSource()
	opts := buildOptions(src, DefaultPort, datasource.Credential{Username: testUsername, Password: testPassword})

	if len(opts.Hosts) != 1 || opts.Hosts[0] != "mongo.internal:27017" {
		t.Fatalf("hosts = %v, want [mongo.internal:27017]", opts.Hosts)
	}
	if opts.Auth == nil || opts.Auth.Username != testUsername || opts.Auth.Password != testPassword {
		t.Fatalf("auth = %+v, want the resolved material", opts.Auth)
	}
	if opts.Auth.AuthSource != "ledgerdb" {
		t.Fatalf("auth source = %q, want the tenant database", opts.Auth.AuthSource)
	}
	if opts.TLSConfig == nil {
		t.Fatalf("TLS config is nil")
	}
	if opts.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS min version = %x, want TLS 1.2", opts.TLSConfig.MinVersion)
	}
	if opts.TLSConfig.ServerName != "mongo.internal" {
		t.Fatalf("TLS server name = %q, want SNI for the host", opts.TLSConfig.ServerName)
	}
	if opts.AppName == nil || *opts.AppName != appName {
		t.Fatalf("app name = %v, want %q", opts.AppName, appName)
	}

	if opts.ConnectTimeout == nil || *opts.ConnectTimeout != connectTimeout {
		t.Fatalf("connect timeout = %v, want %v", opts.ConnectTimeout, connectTimeout)
	}
	if opts.ServerSelectionTimeout == nil || *opts.ServerSelectionTimeout != serverSelectionTimeout {
		t.Fatalf("server selection timeout = %v, want %v", opts.ServerSelectionTimeout, serverSelectionTimeout)
	}
	if opts.SocketTimeout == nil || *opts.SocketTimeout != socketTimeout {
		t.Fatalf("socket timeout = %v, want %v", opts.SocketTimeout, socketTimeout)
	}

	ipSrc := testSource()
	ipSrc.Host = "127.0.0.1"
	ipOpts := buildOptions(ipSrc, 27018, datasource.Credential{Username: "u", Password: "p"})
	if len(ipOpts.Hosts) != 1 || ipOpts.Hosts[0] != "127.0.0.1:27018" {
		t.Fatalf("IP hosts = %v, want [127.0.0.1:27018]", ipOpts.Hosts)
	}
	if ipOpts.TLSConfig.ServerName != "" {
		t.Fatalf("IP server name = %q, want empty (no SNI for literals)", ipOpts.TLSConfig.ServerName)
	}
}

// TestFactoryResolvesDefaultPort proves the open order through the two
// seams: egress sees the resolved port before secrets resolve, and a
// denial stops the open before any dial.
func TestFactoryResolvesDefaultPort(t *testing.T) {
	ctx := context.Background()
	egress := &stubEgress{denied: true}
	_, err := Factory()(ctx, testSource(), datasource.OpenDeps{Secrets: stubSecrets{}, Egress: egress})
	if err == nil {
		t.Fatalf("denied egress: Factory opened anyway")
	}
	if egress.calls != 1 || egress.host != "mongo.internal" || egress.port != DefaultPort {
		t.Fatalf("egress saw (%s,%d) in %d calls, want (mongo.internal,%d) once", egress.host, egress.port, egress.calls, DefaultPort)
	}

	explicit := testSource()
	explicit.Port = 27018
	egress2 := &stubEgress{denied: true}
	if _, err := Factory()(ctx, explicit, datasource.OpenDeps{Secrets: stubSecrets{}, Egress: egress2}); err == nil {
		t.Fatalf("denied egress on explicit port: Factory opened anyway")
	}
	if egress2.port != 27018 {
		t.Fatalf("egress saw port %d, want the explicit 27018", egress2.port)
	}
}

// TestFactoryRefusals covers every pre-dial refusal: invalid sources,
// secret failures, incomplete material, and unreachable servers.
func TestFactoryRefusals(t *testing.T) {
	ctx := context.Background()
	open := Factory()

	if _, err := open(ctx, datasource.Source{}, datasource.OpenDeps{Secrets: stubSecrets{}, Egress: &stubEgress{}}); !errors.Is(err, datasource.ErrInvalidSource) {
		t.Errorf("zero source: %v, want ErrInvalidSource", err)
	}
	deps := datasource.OpenDeps{Secrets: stubSecrets{err: errors.New("vault sealed")}, Egress: &stubEgress{}}
	if _, err := open(ctx, testSource(), deps); !errors.Is(err, datasource.ErrSecretResolution) {
		t.Errorf("sealed vault: %v, want ErrSecretResolution", err)
	}
	deps = datasource.OpenDeps{Secrets: stubSecrets{cred: datasource.Credential{Username: "u"}}, Egress: &stubEgress{}}
	if _, err := open(ctx, testSource(), deps); !errors.Is(err, datasource.ErrSecretResolution) {
		t.Errorf("passwordless material: %v, want ErrSecretResolution", err)
	}

	// A port that was open a moment ago is deterministically refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	closedPort := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("closing the listener: %v", err)
	}
	dark := testSource()
	dark.Host = "127.0.0.1"
	dark.Port = closedPort
	deps = datasource.OpenDeps{
		Secrets: stubSecrets{cred: datasource.Credential{Username: testUsername, Password: testPassword}},
		Egress:  &stubEgress{},
	}
	_, err = open(ctx, dark, deps)
	if !errors.Is(err, datasource.ErrUnavailable) {
		t.Fatalf("closed port: %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("closed-port error %q names no host", err)
	}
}

// TestFactoryErrorsCarryNoCredentials collects every locally generated
// failure and asserts none of them repeats the credential material.
func TestFactoryErrorsCarryNoCredentials(t *testing.T) {
	ctx := context.Background()
	open := Factory()
	var failures []error

	_, err := open(ctx, testSource(), datasource.OpenDeps{Secrets: stubSecrets{}, Egress: &stubEgress{denied: true}})
	failures = append(failures, err)
	_, err = open(ctx, testSource(), datasource.OpenDeps{Secrets: stubSecrets{err: errors.New("vault sealed")}, Egress: &stubEgress{}})
	failures = append(failures, err)
	_, err = open(ctx, testSource(), datasource.OpenDeps{Secrets: stubSecrets{cred: datasource.Credential{Username: testUsername}}, Egress: &stubEgress{}})
	failures = append(failures, err)

	bare := &connector{src: testSource()}
	_, err = bare.Query(ctx, datasource.Query{Collection: "c", Predicate: `{"broken":}`})
	failures = append(failures, err)
	_, err = bare.Query(ctx, datasource.Query{Collection: "c", Limit: datasource.MaxLimit + 1})
	failures = append(failures, err)
	err = bare.Write(ctx, datasource.Write{Collection: "c", Documents: []datasource.Document{[]byte(`{"a":1}`)}})
	failures = append(failures, err)

	writable := testSource()
	writable.ReadOnly = false
	writable.WritableCollections = []string{"allowed"}
	wbare := &connector{src: writable}
	err = wbare.Write(ctx, datasource.Write{Collection: "other", Documents: []datasource.Document{[]byte(`{"a":1}`)}})
	failures = append(failures, err)
	err = wbare.Write(ctx, datasource.Write{Collection: "allowed", Documents: []datasource.Document{[]byte(`[1,2]`)}})
	failures = append(failures, err)

	for i, failure := range failures {
		if failure == nil {
			t.Fatalf("failure %d is nil, want a refusal", i)
		}
		if strings.Contains(failure.Error(), testUsername) || strings.Contains(failure.Error(), testPassword) {
			t.Errorf("failure %d %q carries credential material", i, failure)
		}
	}
	if got := bare.Source(); got.Tenant != "tenant-a" || got.ID != "ledger" {
		t.Errorf("connector source = %+v, want tenant-a/ledger", got)
	}
	if bare.AdapterID() != AdapterID {
		t.Errorf("adapter id = %q, want %q", bare.AdapterID(), AdapterID)
	}
}

func mustObjectID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		t.Fatalf("bad fixture oid: %v", err)
	}
	return id
}

func mustDecimal(t *testing.T, s string) primitive.Decimal128 {
	t.Helper()
	d, err := primitive.ParseDecimal128(s)
	if err != nil {
		t.Fatalf("bad fixture decimal: %v", err)
	}
	return d
}

// TestCanonicalJSONExact pins the canonical byte layout: sorted keys,
// integer-preserving numbers, and marker objects for BSON-only types.
func TestCanonicalJSONExact(t *testing.T) {
	row := bson.M{
		"s":   "x",
		"b":   true,
		"z":   nil,
		"n":   int32(7),
		"big": int64(-42),
		"f":   1.5,
		"id":  mustObjectID(t, "507f1f77bcf86cd799439011"),
		"d":   primitive.NewDateTimeFromTime(time.Date(2026, time.January, 2, 3, 4, 5, 123000000, time.UTC)),
		"dec": mustDecimal(t, "12.50"),
		"bin": primitive.Binary{Subtype: 0, Data: []byte{1, 2, 3}},
		"ts":  primitive.Timestamp{T: 9, I: 8},
		"min": primitive.MinKey{},
		"max": primitive.MaxKey{},
		"re":  primitive.Regex{Pattern: "^a", Options: "i"},
		"sub": bson.M{"k": "v"},
		"arr": bson.A{int32(1), "two"},
	}
	doc, err := canonicalJSON(row)
	if err != nil {
		t.Fatalf("canonicalizing: %v", err)
	}
	want := `{"arr":[1,"two"],"b":true,"big":-42,"bin":{"$binary":{"base64":"AQID","subType":"00"}},"d":{"$date":"2026-01-02T03:04:05.123Z"},"dec":{"$numberDecimal":"12.50"},"f":1.5,"id":{"$oid":"507f1f77bcf86cd799439011"},"max":{"$maxKey":1},"min":{"$minKey":1},"n":7,"re":{"$regularExpression":{"options":"i","pattern":"^a"}},"s":"x","sub":{"k":"v"},"ts":{"$timestamp":{"i":8,"t":9}},"z":null}`
	if string(doc) != want {
		t.Fatalf("canonical bytes differ:\n got: %s\nwant: %s", doc, want)
	}

	back, err := parseDocument(doc)
	if err != nil {
		t.Fatalf("parsing the canonical bytes back: %v", err)
	}
	if back["n"] != int64(7) || back["big"] != int64(-42) || back["f"] != 1.5 {
		t.Fatalf("numbers round-tripped to n=%v(%T) big=%v(%T) f=%v(%T)", back["n"], back["n"], back["big"], back["big"], back["f"], back["f"])
	}
	if back["id"] != row["id"] || back["d"] != row["d"] || back["dec"] != row["dec"] {
		t.Fatalf("typed markers changed: id=%v d=%v dec=%v", back["id"], back["d"], back["dec"])
	}
	if !reflect.DeepEqual(back["bin"], row["bin"]) || !reflect.DeepEqual(back["ts"], row["ts"]) || !reflect.DeepEqual(back["re"], row["re"]) {
		t.Fatalf("compound markers changed: bin=%v ts=%v re=%v", back["bin"], back["ts"], back["re"])
	}
	if _, ok := back["min"].(primitive.MinKey); !ok {
		t.Fatalf("minKey round-tripped to %T", back["min"])
	}
	if _, ok := back["max"].(primitive.MaxKey); !ok {
		t.Fatalf("maxKey round-tripped to %T", back["max"])
	}
}

// TestParsePredicateOperators proves operator objects pass through as
// ordinary documents while known markers convert to typed values.
func TestParsePredicateOperators(t *testing.T) {
	filter, err := parsePredicate(`{"age":{"$gt":5},"name":"ann"}`)
	if err != nil {
		t.Fatalf("parsing operator predicate: %v", err)
	}
	op, ok := filter["age"].(map[string]any)
	if !ok {
		t.Fatalf("operator object decoded to %T, want a document", filter["age"])
	}
	if op["$gt"] != int64(5) {
		t.Fatalf("operator operand = %v(%T), want int64 5", op["$gt"], op["$gt"])
	}
	empty, err := parsePredicate("  ")
	if err != nil {
		t.Fatalf("parsing blank predicate: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("blank predicate decoded to %v, want an empty filter", empty)
	}
}

// TestParsePredicateRejects pins every malformed-marker refusal: each
// input fails with ErrInvalidQuery rather than passing corruption on.
func TestParsePredicateRejects(t *testing.T) {
	cases := map[string]string{
		"malformed JSON":     `{"a":}`,
		"top-level array":    `[1,2]`,
		"top-level scalar":   `7`,
		"short oid":          `{"x":{"$oid":"zzz"}}`,
		"numeric oid":        `{"x":{"$oid":5}}`,
		"prose date":         `{"x":{"$date":"yesterday"}}`,
		"numeric date":       `{"x":{"$date":7}}`,
		"bad decimal":        `{"x":{"$numberDecimal":"abc"}}`,
		"bad base64":         `{"x":{"$binary":{"base64":"!!!","subType":"00"}}}`,
		"bad subtype":        `{"x":{"$binary":{"base64":"AQID","subType":"zz"}}}`,
		"flat binary":        `{"x":{"$binary":"AQID"}}`,
		"timestamp missing":  `{"x":{"$timestamp":{"t":1}}}`,
		"flat timestamp":     `{"x":{"$timestamp":9}}`,
		"minKey two":         `{"x":{"$minKey":2}}`,
		"maxKey string":      `{"x":{"$maxKey":"1"}}`,
		"regex missing opts": `{"x":{"$regularExpression":{"pattern":"a"}}}`,
		"flat regex":         `{"x":{"$regularExpression":"a"}}`,
		"float overflow":     `{"x":1e999999}`,
	}
	for name, raw := range cases {
		if _, err := parsePredicate(raw); !errors.Is(err, datasource.ErrInvalidQuery) {
			t.Errorf("%s: %v, want ErrInvalidQuery", name, err)
		}
	}
	if _, err := parseDocument(datasource.Document(`[1,2]`)); !errors.Is(err, datasource.ErrInvalidWrite) {
		t.Errorf("array document: %v, want ErrInvalidWrite", err)
	}
}

// TestCanonicalJSONRejects proves server values without a JSON spelling
// fail loudly instead of degrading.
func TestCanonicalJSONRejects(t *testing.T) {
	if _, err := canonicalJSON(bson.M{"x": math.NaN()}); err == nil {
		t.Errorf("NaN: canonicalized without an error")
	}
	if _, err := canonicalJSON(bson.M{"x": math.Inf(1)}); err == nil {
		t.Errorf("+Inf: canonicalized without an error")
	}
	if _, err := canonicalJSON(bson.M{"x": primitive.Symbol("s")}); err == nil {
		t.Errorf("BSON symbol: canonicalized without an error")
	}
}
