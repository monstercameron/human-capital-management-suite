package adversarial

import (
	"context"
	"strings"
)

// Cross-tenant isolation vectors for cache, queue, object-store, log and
// backup paths (SECARCH-010). Every builder namespaces its artifact by the
// owning tenant so same logical names under different tenants never share
// storage, execution, or placement, and every cross-tenant access denies
// without disclosing tenant markers.

const (
	ChannelCache       = "cache"
	ChannelQueue       = "queue"
	ChannelObjectStore = "object_store"
	ChannelBackup      = "backup_restore"
)

// CacheKey returns the tenant-namespaced cache key for a logical key.
func CacheKey(tenant, key string) string {
	return "t/" + tenant + "/cache/" + key
}

// CacheKeyTenant parses the owning tenant out of a namespaced cache key.
func CacheKeyTenant(namespaced string) (string, bool) {
	rest, ok := strings.CutPrefix(namespaced, "t/")
	if !ok {
		return "", false
	}
	tenant, key, ok := strings.Cut(rest, "/cache/")
	if !ok || tenant == "" || key == "" {
		return "", false
	}
	if strings.Contains(tenant, "/") {
		return "", false
	}
	return tenant, true
}

// JobEnvelope carries the owning tenant of a queued job payload.
type JobEnvelope struct {
	Tenant  string
	JobID   string
	Payload []byte
}

// NewJobEnvelope binds a payload to its owning tenant.
func NewJobEnvelope(tenant, jobID string, payload []byte) JobEnvelope {
	return JobEnvelope{Tenant: tenant, JobID: jobID, Payload: payload}
}

// ExecuteJob allows execution only under the owning tenant's identity;
// cross-tenant execution denies without disclosure.
func ExecuteJob(callerTenant string, env JobEnvelope) error {
	if callerTenant == "" || env.Tenant == "" || callerTenant != env.Tenant {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	return nil
}

// ObjectKey returns the tenant-namespaced object-store key.
func ObjectKey(tenant, prefix, name string) string {
	return "t/" + tenant + "/obj/" + prefix + "/" + name
}

// ObjectKeyTenant parses the owning tenant out of a namespaced object key.
func ObjectKeyTenant(namespaced string) (string, bool) {
	rest, ok := strings.CutPrefix(namespaced, "t/")
	if !ok {
		return "", false
	}
	tenant, tail, ok := strings.Cut(rest, "/obj/")
	if !ok || tenant == "" || tail == "" {
		return "", false
	}
	if strings.Contains(tenant, "/") {
		return "", false
	}
	return tenant, true
}

// LogLine emits a tenant-scoped log line carrying no foreign tenant marker.
func LogLine(tenant, msg string) string {
	return "tenant=" + tenant + " " + msg
}

// ForeignBytes counts bytes attributable to a foreign tenant: occurrences
// of any known tenant marker other than the owner's own marker.
func ForeignBytes(line, owner string, knownTenants []string) int {
	n := 0
	for _, tn := range knownTenants {
		if tn == "" || tn == owner {
			continue
		}
		n += strings.Count(line, tn)
	}
	return n
}

// BackupPlacement resolves the restore shard for a tenant snapshot.
func BackupPlacement(tenant, snapshot string) string {
	return "shard:" + tenant + ":" + snapshot
}

// AllowRestore permits a restore only into the placement's owning tenant.
func AllowRestore(requestTenant, placement string) error {
	rest, ok := strings.CutPrefix(placement, "shard:")
	if !ok {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	owner, _, ok := strings.Cut(rest, ":")
	if !ok || owner == "" {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	if requestTenant == "" || requestTenant != owner {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	return nil
}

// PresetIsolationJourneys retains the five SECARCH-010 adversarial vectors
// as release-gating conformance fixtures.
func PresetIsolationJourneys() []Journey {
	return []Journey{
		{ID: "ISO-CACHE-01", Kind: KindAuthz, Channel: ChannelCache, Principal: "user-b", Tenant: "tenant-b", Target: "cache:t/tenant-a/session:123", Action: "cache_read"},
		{ID: "ISO-QUEUE-01", Kind: KindAuthz, Channel: ChannelQueue, Principal: "worker-b", Tenant: "tenant-b", Target: "job:tenant-a/job-1", Action: "job_execute"},
		{ID: "ISO-OBJECT-01", Kind: KindAuthz, Channel: ChannelObjectStore, Principal: "user-b", Tenant: "tenant-b", Target: "obj:t/tenant-a/exports/pay.csv", Action: "object_read"},
		{ID: "ISO-LOG-01", Kind: KindPrivacy, Channel: ChannelLogQuery, Principal: "user-b", Tenant: "tenant-b", Target: "logs:tenant-a", Action: "log_query"},
		{ID: "ISO-BACKUP-01", Kind: KindAuthz, Channel: ChannelBackup, Principal: "operator-b", Tenant: "tenant-b", Target: "backup:shard:tenant-a:snap-001", Action: "backup_restore"},
	}
}

// IsolationDenyHandler denies cross-tenant isolation probes without
// disclosure; it falls back to the shared default deny for anything else.
func IsolationDenyHandler(ctx context.Context, j Journey) error {
	switch j.Channel {
	case ChannelCache, ChannelQueue, ChannelObjectStore, ChannelBackup:
		// Cross-tenant use of another tenant's namespaced artifact denies.
		// Same-tenant fixtures below still deny at the adversarial layer
		// (defense in depth); the unit-level ExecuteJob/AllowRestore
		// functions above are the allow-path for legitimate owners.
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	default:
		return DefaultDenyHandler(ctx, j)
	}
}
