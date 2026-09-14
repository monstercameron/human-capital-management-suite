// Package threatregister implements THREAT-001: the signed trust-boundary
// threat register for every Phase 1 vertical slice. See doc.go for the
// "slice graph edge" definition, the threat-identity dedup rule, the
// shared-mitigation retention rule and why one residual-risk owner field is
// checked-in deliberately empty.
package threatregister

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AttackClass is the closed, named vocabulary of abuse classes RED requires
// every slice's threat register to cover. The nine values below are copied
// verbatim from planning/todos.md's THREAT-001 RED line ("ignores confused
// deputy, tenant crossover, stale authorization, insider abuse, metadata
// leakage, replay, ambiguous effect, supply-chain or human-social attack").
type AttackClass string

const (
	AttackConfusedDeputy     AttackClass = "CONFUSED_DEPUTY"
	AttackTenantCrossover    AttackClass = "TENANT_CROSSOVER"
	AttackStaleAuthorization AttackClass = "STALE_AUTHORIZATION"
	AttackInsiderAbuse       AttackClass = "INSIDER_ABUSE"
	AttackMetadataLeakage    AttackClass = "METADATA_LEAKAGE"
	AttackReplay             AttackClass = "REPLAY"
	AttackAmbiguousEffect    AttackClass = "AMBIGUOUS_EFFECT"
	AttackSupplyChain        AttackClass = "SUPPLY_CHAIN"
	AttackHumanSocial        AttackClass = "HUMAN_SOCIAL"
)

// AllAttackClasses is the exact, closed set every slice's threats must cover
// at least once each. Validate and every test that proves "total" attack
// coverage iterate this function rather than a second hardcoded list, so
// the taxonomy has exactly one source of truth and cannot drift out of sync
// with itself.
func AllAttackClasses() []AttackClass {
	return []AttackClass{
		AttackConfusedDeputy, AttackTenantCrossover, AttackStaleAuthorization,
		AttackInsiderAbuse, AttackMetadataLeakage, AttackReplay,
		AttackAmbiguousEffect, AttackSupplyChain, AttackHumanSocial,
	}
}

var validAttackClasses = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range AllAttackClasses() {
		m[string(c)] = true
	}
	return m
}()

// Closed data-classification vocabulary, taken verbatim from
// planning/workflows/vertical-slices/maximal-configuration-profile.md's
// Privacy/classification axis ("public/internal/personal/sensitive/highly
// restricted/medical/privileged").
const (
	DataPublic           = "PUBLIC"
	DataInternal         = "INTERNAL"
	DataPersonal         = "PERSONAL"
	DataSensitive        = "SENSITIVE"
	DataHighlyRestricted = "HIGHLY_RESTRICTED"
	DataMedical          = "MEDICAL"
	DataPrivileged       = "PRIVILEGED"
)

var validDataClasses = map[string]bool{
	DataPublic: true, DataInternal: true, DataPersonal: true, DataSensitive: true,
	DataHighlyRestricted: true, DataMedical: true, DataPrivileged: true,
}

// Closed severity vocabulary.
const (
	SeverityCritical = "CRITICAL"
	SeverityHigh     = "HIGH"
	SeverityMedium   = "MEDIUM"
	SeverityLow      = "LOW"
)

var validSeverities = map[string]bool{
	SeverityCritical: true, SeverityHigh: true, SeverityMedium: true, SeverityLow: true,
}

// Closed test-kind vocabulary. GREEN requires the register "produce[] exact
// negative/security/fault tests"; AllTestKinds is this package's taxonomy
// anchor for that requirement, exactly like AllAttackClasses is for attack
// coverage.
const (
	TestKindNegative = "NEGATIVE"
	TestKindSecurity = "SECURITY"
	TestKindFault    = "FAULT"
)

func AllTestKinds() []string { return []string{TestKindNegative, TestKindSecurity, TestKindFault} }

var validTestKinds = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range AllTestKinds() {
		m[k] = true
	}
	return m
}()

// Actor is one initiator that can reach a slice's assets (RED: "actor").
type Actor struct {
	ID          string `yaml:"id" json:"id"`
	Kind        string `yaml:"kind" json:"kind"`
	Description string `yaml:"description" json:"description"`
}

// Asset is one protected value or capability a slice's threats target
// (RED: "asset", "data class").
type Asset struct {
	ID          string `yaml:"id" json:"id"`
	DataClass   string `yaml:"data_class" json:"data_class"`
	Description string `yaml:"description" json:"description"`
}

// TrustBoundary is one change in authority or trust a slice's operations
// cross (RED: "trust boundary").
type TrustBoundary struct {
	ID          string `yaml:"id" json:"id"`
	From        string `yaml:"from" json:"from"`
	To          string `yaml:"to" json:"to"`
	Description string `yaml:"description" json:"description"`
}

// EntryPoint is one channel through which an actor reaches a trust boundary
// (RED: "entry point").
type EntryPoint struct {
	ID          string `yaml:"id" json:"id"`
	Channel     string `yaml:"channel" json:"channel"`
	Description string `yaml:"description" json:"description"`
}

// SliceEdge is one slice graph edge: a concrete (actor, entry point, trust
// boundary, asset) tuple describing one way an actor can reach one asset
// across one trust boundary through one entry point. GREEN requires the
// register "maps each slice graph edge to reviewed threats and controls" -
// see doc.go for why this tuple, and not something coarser or finer, is
// this package's definition of "edge".
type SliceEdge struct {
	ID            string `yaml:"id" json:"id"`
	Actor         string `yaml:"actor" json:"actor"`
	EntryPoint    string `yaml:"entry_point" json:"entry_point"`
	TrustBoundary string `yaml:"trust_boundary" json:"trust_boundary"`
	Asset         string `yaml:"asset" json:"asset"`
	Description   string `yaml:"description" json:"description"`
}

// ThreatTest names one exact test identifier a threat's mitigation and
// detection evidence depends on (GREEN: "produces exact negative/security/
// fault tests"). Landing the named test in its owning package is tracked
// through the threat's Owner domain; this register names the obligation,
// it does not itself implement every downstream test (see doc.go).
type ThreatTest struct {
	Kind string `yaml:"kind" json:"kind"`
	Name string `yaml:"name" json:"name"`
}

// Threat is one reviewed abuse scenario bound to an asset, a trust boundary
// and a named attack class.
type Threat struct {
	ID             string       `yaml:"id" json:"id"`
	Asset          string       `yaml:"asset" json:"asset"`
	TrustBoundary  string       `yaml:"trust_boundary" json:"trust_boundary"`
	AttackClass    string       `yaml:"attack_class" json:"attack_class"`
	Scenario       string       `yaml:"scenario" json:"scenario"`
	Severity       string       `yaml:"severity" json:"severity"`
	ConsumingEdges []string     `yaml:"consuming_edges" json:"consuming_edges"`
	Detection      string       `yaml:"detection" json:"detection"`
	Recovery       string       `yaml:"recovery" json:"recovery"`
	Owner          string       `yaml:"owner" json:"owner"`
	Mitigations    []string     `yaml:"mitigations" json:"mitigations"`
	Tests          []ThreatTest `yaml:"tests" json:"tests"`
}

// ThreatIdentity is REFACTOR's stable dedup key: a threat's identity is
// exactly its (slice, asset, trust boundary, attack class) tuple, never its
// free-text ID or scenario prose. Two Threat entries that resolve to the
// same ThreatIdentity are the same threat recorded twice - Validate flags
// this as a duplicate rather than silently accepting both.
func ThreatIdentity(sliceID, asset, trustBoundary string, attack string) string {
	return sliceID + "|" + asset + "|" + trustBoundary + "|" + attack
}

// Mitigation is one named control. When more than one Threat names the same
// Mitigation ID, REFACTOR requires the mitigation retain every one of those
// threats' consuming edges - ConsumingEdges below is that retained set, not
// merely the edges of whichever threat happened to define the mitigation
// first.
type Mitigation struct {
	ID             string   `yaml:"id" json:"id"`
	Description    string   `yaml:"description" json:"description"`
	ConsumingEdges []string `yaml:"consuming_edges" json:"consuming_edges"`
}

// ResidualRisk records that a threat's control is accepted rather than (or
// in addition to) mitigated. AcceptedBy is deliberately allowed to be
// empty - see doc.go for why the checked-in register carries exactly one
// residual risk with an honestly empty accepted_by rather than a fabricated
// accountable party - but Validate always reports it as a violation when
// empty, exactly like tools/planning/pilotjurisdiction.Reviewer.Name.
type ResidualRisk struct {
	ThreatID      string `yaml:"threat_id" json:"threat_id"`
	Justification string `yaml:"justification" json:"justification"`
	AcceptedBy    string `yaml:"accepted_by" json:"accepted_by"`
	AcceptedDate  string `yaml:"accepted_date" json:"accepted_date"`
	ExpiryDate    string `yaml:"expiry_date" json:"expiry_date"`
}

// Slice is one Phase 1 vertical slice's complete trust-boundary threat
// model.
type Slice struct {
	SliceID         string          `yaml:"slice_id" json:"slice_id"`
	Actors          []Actor         `yaml:"actors" json:"actors"`
	Assets          []Asset         `yaml:"assets" json:"assets"`
	TrustBoundaries []TrustBoundary `yaml:"trust_boundaries" json:"trust_boundaries"`
	EntryPoints     []EntryPoint    `yaml:"entry_points" json:"entry_points"`
	Edges           []SliceEdge     `yaml:"edges" json:"edges"`
	Threats         []Threat        `yaml:"threats" json:"threats"`
	Mitigations     []Mitigation    `yaml:"mitigations" json:"mitigations"`
	ResidualRisks   []ResidualRisk  `yaml:"residual_risks" json:"residual_risks"`
}

// Signature is an ed25519 signature over a Register's CanonicalDigest,
// structurally identical to tools/planning/pilotjurisdiction.Signature,
// tools/planning/pilotprovider.Signature and
// tools/planning/gateevidence.Signature.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// Register is THREAT-001's signed threat register
// (definitions/planning/gates/threat-001-register.yaml): one Slice entry
// per Phase 1 vertical slice named in definitions/planning/
// product-slices.yaml.
type Register struct {
	SchemaVersion int        `yaml:"schema_version" json:"schema_version"`
	TodoID        string     `yaml:"todo_id" json:"todo_id"`
	SignedDate    string     `yaml:"signed_date" json:"signed_date"`
	Slices        []Slice    `yaml:"slices" json:"slices"`
	Signature     *Signature `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// LoadRegister reads and parses a Register YAML file.
func LoadRegister(path string) (*Register, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var r Register
	if err := yaml.Unmarshal(content, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &r, nil
}

// Violation names one structural defect. Field is dotted for list entries
// so a test can name exactly what a mutation broke.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

func idSet[T any](items []T, id func(T) string) map[string]bool {
	m := map[string]bool{}
	for _, it := range items {
		m[id(it)] = true
	}
	return m
}

func stringSet(items []string) map[string]bool {
	m := map[string]bool{}
	for _, it := range items {
		m[it] = true
	}
	return m
}

// Validate returns every structural violation on r. It performs no file
// I/O. It checks, per slice: every RED element (actor, asset, trust
// boundary, data class, entry point, threat, mitigation, detection,
// recovery, owner, test) is present; every slice graph edge and every
// named attack class is covered by at least one reviewed threat; threat
// identity is deduplicated by (asset, trust boundary, attack class); a
// shared mitigation retains every edge consumed through it; and every
// residual-risk acceptance names a justification and an expiry (RED:
// "marks accepted risk without expiry").
func (r Register) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if r.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if r.TodoID != "THREAT-001" {
		add("todo_id", "must be THREAT-001")
	}
	if strings.TrimSpace(r.SignedDate) == "" {
		add("signed_date", "missing")
	}
	if len(r.Slices) == 0 {
		add("slices", "missing - the register covers no Phase 1 vertical slice")
	}

	seenSlice := map[string]bool{}
	for si, s := range r.Slices {
		sField := fmt.Sprintf("slices[%d]", si)
		if strings.TrimSpace(s.SliceID) == "" {
			add(sField+".slice_id", "missing")
		}
		if seenSlice[s.SliceID] {
			add(sField+".slice_id", "duplicate slice_id "+s.SliceID)
		}
		seenSlice[s.SliceID] = true

		if len(s.Actors) == 0 {
			add(sField+".actors", "missing - slice omits actor")
		}
		if len(s.Assets) == 0 {
			add(sField+".assets", "missing - slice omits asset")
		}
		if len(s.TrustBoundaries) == 0 {
			add(sField+".trust_boundaries", "missing - slice omits trust boundary")
		}
		if len(s.EntryPoints) == 0 {
			add(sField+".entry_points", "missing - slice omits entry point")
		}
		if len(s.Edges) == 0 {
			add(sField+".edges", "missing - slice has no slice graph edges")
		}
		if len(s.Threats) == 0 {
			add(sField+".threats", "missing - slice omits threat")
		}
		if len(s.Mitigations) == 0 {
			add(sField+".mitigations", "missing - slice omits mitigation")
		}

		actorIDs := idSet(s.Actors, func(a Actor) string { return a.ID })
		entryPointIDs := idSet(s.EntryPoints, func(e EntryPoint) string { return e.ID })
		boundaryIDs := idSet(s.TrustBoundaries, func(b TrustBoundary) string { return b.ID })
		assetIDs := idSet(s.Assets, func(a Asset) string { return a.ID })

		for ai, a := range s.Assets {
			field := fmt.Sprintf("%s.assets[%d]", sField, ai)
			if strings.TrimSpace(a.ID) == "" {
				add(field+".id", "missing")
			}
			if !validDataClasses[a.DataClass] {
				add(field+".data_class", fmt.Sprintf("unknown or missing data class %q - slice omits data class", a.DataClass))
			}
		}

		edgeIDs := map[string]bool{}
		for ei, e := range s.Edges {
			field := fmt.Sprintf("%s.edges[%d]", sField, ei)
			if strings.TrimSpace(e.ID) == "" {
				add(field+".id", "missing")
			}
			if edgeIDs[e.ID] {
				add(field+".id", "duplicate edge id "+e.ID)
			}
			edgeIDs[e.ID] = true
			if !actorIDs[e.Actor] {
				add(field+".actor", "references unknown actor "+e.Actor)
			}
			if !entryPointIDs[e.EntryPoint] {
				add(field+".entry_point", "references unknown entry point "+e.EntryPoint)
			}
			if !boundaryIDs[e.TrustBoundary] {
				add(field+".trust_boundary", "references unknown trust boundary "+e.TrustBoundary)
			}
			if !assetIDs[e.Asset] {
				add(field+".asset", "references unknown asset "+e.Asset)
			}
		}

		threatByID := map[string]Threat{}
		identityCount := map[string]int{}
		attackSeen := map[string]bool{}
		testKindSeen := map[string]bool{}
		mitigationIDs := idSet(s.Mitigations, func(m Mitigation) string { return m.ID })

		for ti, th := range s.Threats {
			field := fmt.Sprintf("%s.threats[%d]", sField, ti)
			if strings.TrimSpace(th.ID) == "" {
				add(field+".id", "missing")
			}
			if _, dup := threatByID[th.ID]; dup && th.ID != "" {
				add(field+".id", "duplicate threat id "+th.ID)
			}
			threatByID[th.ID] = th

			if !assetIDs[th.Asset] {
				add(field+".asset", "references unknown asset "+th.Asset)
			}
			if !boundaryIDs[th.TrustBoundary] {
				add(field+".trust_boundary", "references unknown trust boundary "+th.TrustBoundary)
			}
			if !validAttackClasses[th.AttackClass] {
				add(field+".attack_class", fmt.Sprintf("unknown attack class %q - RED requires a named attack class", th.AttackClass))
			} else {
				attackSeen[th.AttackClass] = true
			}
			if strings.TrimSpace(th.Scenario) == "" {
				add(field+".scenario", "missing")
			}
			if !validSeverities[th.Severity] {
				add(field+".severity", fmt.Sprintf("unknown severity %q", th.Severity))
			}
			if strings.TrimSpace(th.Detection) == "" {
				add(field+".detection", "missing - slice omits detection")
			}
			if strings.TrimSpace(th.Recovery) == "" {
				add(field+".recovery", "missing - slice omits recovery")
			}
			if strings.TrimSpace(th.Owner) == "" {
				add(field+".owner", "missing - slice omits owner")
			}
			if len(th.ConsumingEdges) == 0 {
				add(field+".consuming_edges", "missing - threat maps to no slice graph edge")
			}
			for _, eid := range th.ConsumingEdges {
				if !edgeIDs[eid] {
					add(field+".consuming_edges", "references unknown edge "+eid)
				}
			}
			if len(th.Tests) == 0 {
				add(field+".tests", "missing - slice omits test")
			}
			for tj, tst := range th.Tests {
				tf := fmt.Sprintf("%s.tests[%d]", field, tj)
				if !validTestKinds[tst.Kind] {
					add(tf+".kind", fmt.Sprintf("unknown test kind %q", tst.Kind))
				} else {
					testKindSeen[tst.Kind] = true
				}
				if strings.TrimSpace(tst.Name) == "" {
					add(tf+".name", "missing")
				}
			}
			for _, mid := range th.Mitigations {
				if !mitigationIDs[mid] {
					add(field+".mitigations", "references unknown mitigation "+mid)
				}
			}

			id := ThreatIdentity(s.SliceID, th.Asset, th.TrustBoundary, th.AttackClass)
			identityCount[id]++
		}

		for id, count := range identityCount {
			if count > 1 {
				add(sField+".threats", fmt.Sprintf("duplicate threat identity (asset+trust_boundary+attack_class) %q appears %d times", id, count))
			}
		}

		for _, class := range AllAttackClasses() {
			if !attackSeen[string(class)] {
				add(sField+".threats", "ignores attack class "+string(class))
			}
		}

		for _, kind := range AllTestKinds() {
			if !testKindSeen[kind] {
				add(sField+".threats", "no test of kind "+kind+" is present anywhere in this slice")
			}
		}

		edgeConsumed := map[string]bool{}
		for _, th := range s.Threats {
			for _, eid := range th.ConsumingEdges {
				edgeConsumed[eid] = true
			}
		}
		edgeOrder := make([]string, 0, len(edgeIDs))
		for eid := range edgeIDs {
			edgeOrder = append(edgeOrder, eid)
		}
		sort.Strings(edgeOrder)
		for _, eid := range edgeOrder {
			if !edgeConsumed[eid] {
				add(sField+".edges", "edge "+eid+" is not mapped to any reviewed threat")
			}
		}

		residualByThreat := map[string]ResidualRisk{}
		for _, rr := range s.ResidualRisks {
			residualByThreat[rr.ThreatID] = rr
		}
		for _, th := range s.Threats {
			if len(th.Mitigations) == 0 {
				if _, ok := residualByThreat[th.ID]; !ok {
					add(sField+".threats", "threat "+th.ID+" has no mitigation and no residual risk acceptance - slice omits mitigation")
				}
			}
		}

		mitigationConsuming := map[string]map[string]bool{}
		for _, th := range s.Threats {
			for _, mid := range th.Mitigations {
				if mitigationConsuming[mid] == nil {
					mitigationConsuming[mid] = map[string]bool{}
				}
				for _, eid := range th.ConsumingEdges {
					mitigationConsuming[mid][eid] = true
				}
			}
		}
		for mi, m := range s.Mitigations {
			field := fmt.Sprintf("%s.mitigations[%d]", sField, mi)
			if strings.TrimSpace(m.ID) == "" {
				add(field+".id", "missing")
			}
			if strings.TrimSpace(m.Description) == "" {
				add(field+".description", "missing")
			}
			want := mitigationConsuming[m.ID]
			got := stringSet(m.ConsumingEdges)
			wantOrder := make([]string, 0, len(want))
			for eid := range want {
				wantOrder = append(wantOrder, eid)
			}
			sort.Strings(wantOrder)
			for _, eid := range wantOrder {
				if !got[eid] {
					add(field+".consuming_edges", fmt.Sprintf("shared mitigation %s does not retain consuming edge %s", m.ID, eid))
				}
			}
			for _, eid := range m.ConsumingEdges {
				if !want[eid] {
					add(field+".consuming_edges", fmt.Sprintf("mitigation %s declares consuming edge %s that no threat actually references it for", m.ID, eid))
				}
			}
		}

		for ri, rr := range s.ResidualRisks {
			field := fmt.Sprintf("%s.residual_risks[%d]", sField, ri)
			if _, ok := threatByID[rr.ThreatID]; !ok {
				add(field+".threat_id", "references unknown threat "+rr.ThreatID)
			}
			if strings.TrimSpace(rr.Justification) == "" {
				add(field+".justification", "missing")
			}
			if strings.TrimSpace(rr.AcceptedBy) == "" {
				add(field+".accepted_by", "missing - no accountable owner has been designated to accept this residual risk")
			} else if strings.TrimSpace(rr.AcceptedDate) == "" {
				add(field+".accepted_date", "missing - accepted_by is populated but accepted_date is not")
			}
			if strings.TrimSpace(rr.ExpiryDate) == "" {
				add(field+".expiry_date", "missing - RED forbids marking accepted risk without expiry")
			} else if _, err := time.Parse("2006-01-02", rr.ExpiryDate); err != nil {
				add(field+".expiry_date", "not YYYY-MM-DD")
			}
		}
	}

	if r.Signature == nil {
		add("signature", "missing - a threat register must be signed")
	} else {
		if r.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", r.Signature.Algorithm))
		}
		if r.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if r.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

// ReleaseDecision reports whether r blocks release as of now, and names
// every blocking reason. GREEN requires the register "blocks release for
// unmitigated critical findings"; this is that gate, deliberately
// independent of Validate exactly the way
// tools/planning/pilotprovider.ProviderTopology.SatisfiesRealProviderSelectionGate
// is independent of Validate. A critical threat blocks release unless it
// carries at least one mitigation, OR an accountable (non-empty
// accepted_by), unexpired residual-risk acceptance - an expired or
// unaccountable acceptance waives nothing.
func (r Register) ReleaseDecision(now time.Time) (blocked bool, blockers []Violation) {
	for _, s := range r.Slices {
		residualByThreat := map[string]ResidualRisk{}
		for _, rr := range s.ResidualRisks {
			residualByThreat[rr.ThreatID] = rr
		}
		for _, th := range s.Threats {
			if th.Severity != SeverityCritical {
				continue
			}
			mitigated := len(th.Mitigations) > 0
			accepted := false
			if rr, ok := residualByThreat[th.ID]; ok && strings.TrimSpace(rr.AcceptedBy) != "" {
				if exp, err := time.Parse("2006-01-02", rr.ExpiryDate); err == nil {
					if !now.After(exp.Add(24*time.Hour - time.Nanosecond)) {
						accepted = true
					}
				}
			}
			if !mitigated && !accepted {
				blocked = true
				blockers = append(blockers, Violation{
					Field: fmt.Sprintf("slices[%s].threats[%s]", s.SliceID, th.ID),
					Issue: "unmitigated critical threat has no accountable, unexpired residual-risk acceptance - release blocked",
				})
			}
		}
	}
	return blocked, blockers
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every Register field except the signature itself.
type digestPayload struct {
	SchemaVersion int     `json:"schema_version"`
	TodoID        string  `json:"todo_id"`
	SignedDate    string  `json:"signed_date"`
	Slices        []Slice `json:"slices"`
}

func (r Register) payload() digestPayload {
	return digestPayload{
		SchemaVersion: r.SchemaVersion,
		TodoID:        r.TodoID,
		SignedDate:    r.SignedDate,
		Slices:        r.Slices,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of r's canonical
// JSON projection (every field except Signature).
func (r Register) CanonicalDigest() (string, error) {
	b, err := json.Marshal(r.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical register: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
