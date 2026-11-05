package simulate

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestPurpose is the invocation purpose every test run presents. Evidence
// stamped with it is test evidence: it lives in the expiring store under
// the TEST_EVIDENCE retention class and must never become permanent history.
const TestPurpose = "workflow.test"

// GrantsFunc is real scope enforcement: whether principal holds scope. The
// deployment injects the same function it uses for direct reads, so a test
// run authorizes exactly what the author could authorize outside it.
type GrantsFunc func(principal, scope string) bool

// TestRunOptions carries the author's real identity into a test run: the
// principal every node scope is authorized against, the real grants to check
// them with, and the expiring store the run's evidence lands in. All three
// are required: an unattributed run, an unenforced run, or a run whose
// evidence has nowhere to expire to is refused outright.
type TestRunOptions struct {
	Principal string
	Grants    GrantsFunc
	Store     *ExpiringEvidenceStore
	Tenant    string
}

// check refuses an option set that cannot run honestly.
func (o *TestRunOptions) check() error {
	if o == nil {
		return nil
	}
	if o.Principal == "" {
		return refuse(CodeInvalidOptions, "", "a test run names the author's real principal")
	}
	if o.Grants == nil {
		return refuse(CodeInvalidOptions, "", "a test run authorizes through real grants, not declared scopes")
	}
	if o.Store == nil {
		return refuse(CodeInvalidOptions, "", "a test run stores its evidence in the expiring store")
	}
	return nil
}

// authorizer returns the per-node authorization for a test run: every
// required scope passes the real grants, and the author's principal is the
// subject of record. A node the author could not read directly is denied
// here, before the gateway is ever consulted.
func (o *TestRunOptions) authorizer() Authorizer {
	return func(node workflow.CompiledNode) capability.Authorization {
		var scopes []string
		if node.Capability != nil {
			scopes = node.Capability.AuthorityScopes
		}
		for _, scope := range scopes {
			if !o.Grants(o.Principal, scope) {
				return capability.Authorization{
					Decision:   capability.Deny,
					SubjectRef: o.Principal,
					Reason:     "test run: " + o.Principal + " holds no grant for " + scope,
				}
			}
		}
		return capability.Authorization{
			Decision:   capability.Allow,
			Scopes:     append([]string(nil), scopes...),
			SubjectRef: o.Principal,
			Reason:     "test run: every required scope granted to " + o.Principal,
		}
	}
}

// EvidenceRecord is one test-run invocation's evidence: who ran it, under
// what purpose, what the gateway decided, and when the record expires. The
// retention class is always TEST_EVIDENCE; the store refuses records whose
// purpose is not the test purpose.
type EvidenceRecord struct {
	ID                string    `json:"id"`
	Purpose           string    `json:"purpose"`
	Principal         string    `json:"principal"`
	Tenant            string    `json:"tenant,omitempty"`
	CapabilityID      string    `json:"capability_id"`
	CapabilityVersion uint32    `json:"capability_version"`
	Decision          string    `json:"decision"`
	ReasonCode        string    `json:"reason_code,omitempty"`
	RetentionClass    string    `json:"retention_class"`
	StoredAt          time.Time `json:"stored_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// ExpiringEvidenceStore keeps test-run evidence for a fixed TTL and purges
// it on expiry. Expired records read back nothing even before a purge runs:
// expiry is enforced on read, and the purge reclaims the rows. It is safe
// for concurrent use.
type ExpiringEvidenceStore struct {
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	seq     int
	records map[string]EvidenceRecord
}

// NewExpiringEvidenceStore returns a store whose records expire ttl after
// they are stored. A non-positive ttl is refused at Put time, never
// silently: evidence without an expiry is permanent history by accident.
func NewExpiringEvidenceStore(ttl time.Duration, now func() time.Time) *ExpiringEvidenceStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ExpiringEvidenceStore{ttl: ttl, now: now, records: map[string]EvidenceRecord{}}
}

// RetentionClass is the registered class every record in this store carries.
func (s *ExpiringEvidenceStore) RetentionClass() string {
	return storagedisposition.RetentionTestEvidence
}

// Put stores one record, stamping its id, retention class and expiry. It
// returns the stored id. Records whose purpose is not the test purpose are
// refused: this store holds test evidence and nothing else.
func (s *ExpiringEvidenceStore) Put(record EvidenceRecord) (string, error) {
	if record.Purpose != TestPurpose {
		return "", fmt.Errorf("simulate: evidence purpose %q does not belong in the test-evidence store", record.Purpose)
	}
	if s.ttl <= 0 {
		return "", fmt.Errorf("simulate: test-evidence store has no positive TTL")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	stored := s.now().UTC()
	record.ID = fmt.Sprintf("test-evidence-%d", s.seq)
	record.RetentionClass = s.RetentionClass()
	record.StoredAt = stored
	record.ExpiresAt = stored.Add(s.ttl)
	s.records[record.ID] = record
	return record.ID, nil
}

// Get reads one record back. An expired or missing record reads back
// nothing: expiry is enforced here, not only by the purge.
func (s *ExpiringEvidenceStore) Get(id string) (EvidenceRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return EvidenceRecord{}, false
	}
	if !s.now().UTC().Before(record.ExpiresAt) {
		return EvidenceRecord{}, false
	}
	return record, true
}

// PurgeExpired deletes every record expired as of now and returns how many
// went. Purged records are gone: no tombstone, no history.
func (s *ExpiringEvidenceStore) PurgeExpired(now time.Time) int {
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	purged := 0
	for id, record := range s.records {
		if !now.Before(record.ExpiresAt) {
			delete(s.records, id)
			purged++
		}
	}
	return purged
}

// Len counts the live records. Expired-but-unpurged rows do not count: they
// already read back nothing.
func (s *ExpiringEvidenceStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := 0
	now := s.now().UTC()
	for _, record := range s.records {
		if now.Before(record.ExpiresAt) {
			live++
		}
	}
	return live
}

// Records returns the live records in id order. It exists for tests and for
// operators draining a run's evidence before it expires.
func (s *ExpiringEvidenceStore) Records() []EvidenceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	var out []EvidenceRecord
	for _, record := range s.records {
		if now.Before(record.ExpiresAt) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
