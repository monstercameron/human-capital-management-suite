// Package evidenceexport owns the ADMIN-007 operator contract.  It is
// deliberately independent of transports and persistence: adapters provide
// already-authorized, redacted records and persist the returned checkpoint.
package evidenceexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	RegistryID      = "ADMIN-007"
	ContractVersion = "hcmnext.admincenter.evidence-export/1"
	PurposeEvidence = "evidence_export"
	DefaultTTL      = 24 * time.Hour
	MaxTTL          = 7 * 24 * time.Hour
)

var (
	ErrUnauthorized   = errors.New("evidence export: unauthorized")
	ErrExpired        = errors.New("evidence export: expired")
	ErrInvalidRequest = errors.New("evidence export: invalid request")
	ErrNotFound       = errors.New("evidence export: operation not found")
	ErrConflict       = errors.New("evidence export: checkpoint conflict")
)

type Authorization struct {
	TenantID, SubjectID, Scope, Purpose                     string
	IssuedAt, ExpiresAt                                     time.Time
	AllowedFields                                           []string
	RedactionProfile, SourceProof, ConfigProof, PolicyProof string
}
type Request struct {
	Query         string
	From, To      time.Time
	Authorization Authorization
	Records       []Record
	ChunkSize     int
	Format        string
}
type Record struct {
	ID, Digest string
	Fields     map[string]string
}
type Manifest struct {
	Query                                      string
	From, To                                   time.Time
	TenantID, Scope, Purpose, RedactionProfile string
	AllowedFields                              []string
	SourceProof, ConfigProof, PolicyProof      string
	RecordCount                                int
	ChunkCount                                 int
	ContentDigest, ManifestDigest              string
	ExpiresAt                                  time.Time
}
type Status string

const (
	StatusPending  Status = "PENDING"
	StatusRunning  Status = "RUNNING"
	StatusFailed   Status = "FAILED"
	StatusComplete Status = "COMPLETE"
)

type Operation struct {
	ID               string
	Status           Status
	Completed, Total int
	NextChunk        int
	Manifest         Manifest
	Failure          string
}

type RegistryEntry struct {
	ID, Version, Purpose                                   string
	MaxTTL                                                 time.Duration
	SupportsResume, OfflineVerification, RedactionRequired bool
}

func Registry() RegistryEntry {
	return RegistryEntry{RegistryID, ContractVersion, PurposeEvidence, MaxTTL, true, true, true}
}

func (a Authorization) Validate(now time.Time) error {
	if strings.TrimSpace(a.TenantID) == "" || strings.TrimSpace(a.SubjectID) == "" || strings.TrimSpace(a.Scope) == "" || a.Purpose != PurposeEvidence || len(a.AllowedFields) == 0 || strings.TrimSpace(a.RedactionProfile) == "" || strings.TrimSpace(a.SourceProof) == "" || strings.TrimSpace(a.ConfigProof) == "" || strings.TrimSpace(a.PolicyProof) == "" {
		return ErrUnauthorized
	}
	if a.ExpiresAt.IsZero() || !now.Before(a.ExpiresAt) {
		return ErrExpired
	}
	if !a.IssuedAt.IsZero() && a.ExpiresAt.Sub(a.IssuedAt) > MaxTTL {
		return ErrUnauthorized
	}
	return nil
}
func (r Request) Validate(now time.Time) error {
	if strings.TrimSpace(r.Query) == "" || r.To.IsZero() || r.From.IsZero() || !r.To.After(r.From) || r.To.Sub(r.From) > MaxTTL || r.ChunkSize < 1 || len(r.Records) == 0 {
		return ErrInvalidRequest
	}
	if err := r.Authorization.Validate(now); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, f := range r.Authorization.AllowedFields {
		if strings.TrimSpace(f) == "" {
			return ErrUnauthorized
		}
		allowed[f] = true
	}
	for _, rec := range r.Records {
		for field := range rec.Fields {
			if !allowed[field] {
				return ErrUnauthorized
			}
		}
	}
	return nil
}

type Manager struct {
	mu   sync.Mutex
	next uint64
	ops  map[string]*job
}
type job struct {
	op     Operation
	req    Request
	chunks [][]Record
}

func NewManager() *Manager { return &Manager{ops: make(map[string]*job)} }
func (m *Manager) Start(req Request, now time.Time) (Operation, error) {
	if err := req.Validate(now); err != nil {
		return Operation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	id := fmt.Sprintf("evidence-%d", m.next)
	sorted := SortRecords(req.Records)
	chunks := make([][]Record, 0, (len(sorted)+req.ChunkSize-1)/req.ChunkSize)
	for i := 0; i < len(sorted); i += req.ChunkSize {
		e := i + req.ChunkSize
		if e > len(sorted) {
			e = len(sorted)
		}
		chunk := make([]Record, e-i)
		for k, r := range sorted[i:e] {
			chunk[k] = cloneRecord(r)
		}
		chunks = append(chunks, chunk)
	}
	man := Manifest{Query: req.Query, From: req.From, To: req.To, TenantID: req.Authorization.TenantID, Scope: req.Authorization.Scope, Purpose: req.Authorization.Purpose, RedactionProfile: req.Authorization.RedactionProfile, AllowedFields: append([]string(nil), req.Authorization.AllowedFields...), SourceProof: req.Authorization.SourceProof, ConfigProof: req.Authorization.ConfigProof, PolicyProof: req.Authorization.PolicyProof, RecordCount: len(sorted), ChunkCount: len(chunks), ExpiresAt: req.Authorization.ExpiresAt}
	reqCopy := req
	reqCopy.Records = sorted
	j := &job{op: Operation{ID: id, Status: StatusPending, Total: len(sorted), Manifest: man}, req: reqCopy, chunks: chunks}
	m.ops[id] = j
	return j.op, nil
}
func (m *Manager) Resume(id string, checkpoint int, now time.Time) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.ops[id]
	if !ok {
		return Operation{}, ErrNotFound
	}
	if err := j.req.Authorization.Validate(now); err != nil {
		return Operation{}, err
	}
	if checkpoint != j.op.NextChunk {
		return Operation{}, ErrConflict
	}
	j.op.Status = StatusRunning
	for j.op.NextChunk < len(j.chunks) {
		for _, r := range j.chunks[j.op.NextChunk] {
			j.op.Manifest.ContentDigest = hash(r.Digest + j.op.Manifest.ContentDigest)
			j.op.Completed++
		}
		j.op.NextChunk++
	}
	j.op.Manifest.ManifestDigest = manifestDigest(j.op.Manifest)
	j.op.Status = StatusComplete
	return j.op, nil
}
func (m *Manager) Fail(id string, reason string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.ops[id]
	if !ok {
		return Operation{}, ErrNotFound
	}
	j.op.Status = StatusFailed
	j.op.Failure = reason
	return j.op, nil
}
func (m *Manager) Get(id string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.ops[id]
	if !ok {
		return Operation{}, ErrNotFound
	}
	return j.op, nil
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func manifestDigest(m Manifest) string {
	fields := append([]string(nil), m.AllowedFields...)
	sort.Strings(fields)
	return hash(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%d|%d|%s|%s", m.Query, m.From.UTC().Format(time.RFC3339Nano), m.To.UTC().Format(time.RFC3339Nano), m.TenantID, m.Scope, m.Purpose, m.RedactionProfile, strings.Join(fields, ","), m.SourceProof, m.ConfigProof, m.PolicyProof, m.RecordCount, m.ChunkCount, m.ContentDigest, m.ExpiresAt.UTC().Format(time.RFC3339Nano)))
}
func (m Manifest) VerifyDigest() bool {
	return m.ManifestDigest != "" && m.ManifestDigest == manifestDigest(m)
}

// SortRecords returns the canonical export order: a copy sorted by ID with
// deterministic content tie-breakers. The caller's slice and nested field
// maps are never reordered or retained.
func SortRecords(rs []Record) []Record {
	if rs == nil {
		return nil
	}
	out := make([]Record, len(rs))
	for i, r := range rs {
		out[i] = cloneRecord(r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Digest != out[j].Digest {
			return out[i].Digest < out[j].Digest
		}
		return fieldsKey(out[i].Fields) < fieldsKey(out[j].Fields)
	})
	return out
}

func cloneRecord(r Record) Record {
	if r.Fields != nil {
		cp := make(map[string]string, len(r.Fields))
		for k, v := range r.Fields {
			cp[k] = v
		}
		r.Fields = cp
	}
	return r
}

// fieldsKey returns a collision-resistant canonical representation of the
// complete field map. encoding/json sorts string map keys and safely escapes
// control characters, so distinct maps cannot share a key the way NUL/SOH
// concatenation allowed (e.g. {"a":"b","c":"d"} vs {"a":"b\x01c\x00d"}).
func fieldsKey(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&sb, "%q:%q;", k, m[k])
		}
		return sb.String()
	}
	return string(b)
}
