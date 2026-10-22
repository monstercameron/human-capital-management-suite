package pgxadapter

import (
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// DSNMetadata is the driver-independent connection identity used by
// composition code to enforce database and login-role separation.
type DSNMetadata struct {
	Host     string
	Port     uint16
	Database string
	User     string
}

// ParseDSNMetadata parses a PostgreSQL DSN without exposing a pgx config type
// to callers. Both URL and keyword DSNs use pgx's canonical parser.
func ParseDSNMetadata(dsn string) (DSNMetadata, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return DSNMetadata{}, err
	}
	return DSNMetadata{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User}, nil
}

// SameDatabaseIdentity compares logical database identity, ignoring
// credentials and connection options. Malformed DSNs fall back to a trimmed
// exact comparison so isolation checks remain fail-closed.
func SameDatabaseIdentity(a, b string) bool {
	left, leftErr := ParseDSNMetadata(strings.TrimSpace(a))
	right, rightErr := ParseDSNMetadata(strings.TrimSpace(b))
	if leftErr != nil || rightErr != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return strings.EqualFold(left.Host, right.Host) && left.Port == right.Port && left.Database == right.Database
}

// SameLoginRole reports whether two DSNs authenticate as the same PostgreSQL
// role. A malformed DSN falls back to trimmed exact comparison.
func SameLoginRole(a, b string) bool {
	left, leftErr := ParseDSNMetadata(a)
	right, rightErr := ParseDSNMetadata(b)
	if leftErr != nil || rightErr != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return left.User == right.User
}
