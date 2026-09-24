package recovery

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTodo_RECOVERY_001(t *testing.T) {
	matrix := DefaultMatrix()
	if err := matrix.Validate(); err != nil {
		t.Fatalf("default recovery matrix is invalid: %v", err)
	}
	if got, want := len(matrix.Contracts), 10; got != want {
		t.Fatalf("contract count = %d, want %d", got, want)
	}
	want := []Contract{
		{Plane: Keys, Store: "key-reference-store", Owner: "platform-security", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 30}, DependencyOrder: 1, SemanticChecks: []string{"key references resolve to the pinned version", "decryptability check passes"}},
		{Plane: Config, Store: "configuration-store", Owner: "platform-configuration", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 60}, DependencyOrder: 2, Dependencies: []Plane{Keys}, SemanticChecks: []string{"schema and policy versions are pinned", "configuration digest matches the approved revision"}},
		{Plane: Ledger, Store: "canonical-ledger", Owner: "data-ledger", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 60}, DependencyOrder: 3, Dependencies: []Plane{Keys, Config}, SemanticChecks: []string{"event chain and stream heads are contiguous", "ledger invariants and tenant boundaries pass"}},
		{Plane: Artifacts, Store: "content-addressed-artifacts", Owner: "data-artifacts", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOB, RPO: Target{Minutes: 15}, RTO: Target{Minutes: 120}, DependencyOrder: 4, Dependencies: []Plane{Keys, Config}, SemanticChecks: []string{"content digests and retention metadata match", "classification and tenant references resolve"}},
		{Plane: Runtime, Store: "workflow-runtime-state", Owner: "workflow-runtime", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 60}, DependencyOrder: 5, Dependencies: []Plane{Keys, Config, Ledger}, SemanticChecks: []string{"frontiers, timers and leases satisfy epoch fences", "workflow definitions and instance references resolve"}},
		{Plane: Outbox, Store: "transactional-outbox", Owner: "event-delivery", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 30}, DependencyOrder: 6, Dependencies: []Plane{Keys, Config, Ledger}, SemanticChecks: []string{"outbox rows remain idempotent against ledger events", "delivery leases are safe to resume"}},
		{Plane: Projection, Store: "critical-projections", Owner: "projection-platform", Authority: Rebuildable, Method: ModeReplay, RPOClass: RPOC, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 90}, Replayable: true, DependencyOrder: 7, Dependencies: []Plane{Ledger, Config}, SemanticChecks: []string{"replay watermark reaches the ledger head", "projection digest matches the reference conformance check"}},
		{Plane: Search, Store: "search-index", Owner: "search-platform", Authority: Rebuildable, Method: ModeRebuild, RPOClass: RPOC, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 180}, Replayable: true, DependencyOrder: 8, Dependencies: []Plane{Ledger, Artifacts, Config}, SemanticChecks: []string{"index is rebuilt only from authorized source rows", "document and field digests match the source snapshot"}},
		{Plane: Analytics, Store: "analytics-views", Owner: "analytics-platform", Authority: Rebuildable, Method: ModeRebuild, RPOClass: RPOC, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 240}, Replayable: true, DependencyOrder: 9, Dependencies: []Plane{Ledger, Projection}, SemanticChecks: []string{"source watermark and schema version are recorded", "aggregate counts and sample values reconcile"}},
		{Plane: Cache, Store: "runtime-cache", Owner: "runtime-platform", Authority: Rebuildable, Method: ModeRebuild, RPOClass: RPONone, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 30}, Replayable: false, DependencyOrder: 10, Dependencies: []Plane{Projection}, SemanticChecks: []string{"cache is empty or derived after the source is healthy", "tenant and authorization keys are scoped"}},
	}
	if got := matrix.Ordered(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recovery matrix differs from the declared source-of-truth contract:\n got=%+v\nwant=%+v", got, want)
	}
	for _, plane := range []Plane{Ledger, Artifacts, Config, Keys, Runtime, Outbox, Projection, Search, Analytics, Cache} {
		contract, ok := matrix.Contract(plane)
		if !ok {
			t.Fatalf("missing contract for %s", plane)
		}
		if contract.Owner == "" || contract.RTO.Minutes <= 0 || len(contract.SemanticChecks) == 0 {
			t.Fatalf("incomplete contract for %s: %+v", plane, contract)
		}
		if contract.RPO.NotApplicable && (contract.Authority != Rebuildable || (contract.RPOClass == RPOC && !contract.Replayable) || (contract.RPOClass == RPONone && contract.Replayable)) {
			t.Fatalf("RPO=N/A is not justified for %s: %+v", plane, contract)
		}
	}
	if explanation := matrix.Explain(); !strings.Contains(explanation, "ledger") || !strings.Contains(explanation, "class=RPO-A rpo=0m") || !strings.Contains(explanation, "class=RPO-C rpo=N/A") || !strings.Contains(explanation, "class=NONE rpo=N/A") {
		t.Fatalf("matrix explanation is incomplete: %s", explanation)
	}
}

func TestTodo_RECOVERY_001_Fault(t *testing.T) {
	matrix := DefaultMatrix()
	matrix.Contracts[0].Owner = ""
	if err := matrix.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("missing owner error = %v, want ErrInvalidContract", err)
	}

	matrix = DefaultMatrix()
	matrix.Contracts[6].Replayable = false
	if err := matrix.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unjustified N/A error = %v, want ErrInvalidContract", err)
	}

	matrix = DefaultMatrix()
	matrix.Contracts[2].Dependencies = append(matrix.Contracts[2].Dependencies, Plane("missing"))
	if err := matrix.Validate(); !errors.Is(err, ErrUnknownPlane) {
		t.Fatalf("unknown dependency error = %v, want ErrUnknownPlane", err)
	}

	matrix = DefaultMatrix()
	matrix.Contracts[2].RPO.Minutes = 1
	if err := matrix.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("RPO-A data-loss target error = %v, want ErrInvalidContract", err)
	}

	matrix = DefaultMatrix()
	matrix.Contracts[3].RPO.NotApplicable = true
	if err := matrix.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("RPO-B N/A target error = %v, want ErrInvalidContract", err)
	}
}

func TestTodo_RECOVERY_001_Recovery(t *testing.T) {
	matrix := DefaultMatrix()
	ordered := matrix.Ordered()
	if len(ordered) != len(matrix.Contracts) {
		t.Fatalf("ordered contract count = %d, want %d", len(ordered), len(matrix.Contracts))
	}
	for i, contract := range ordered {
		if contract.DependencyOrder != i+1 {
			t.Fatalf("ordered contract %d has order %d", i, contract.DependencyOrder)
		}
		for _, dependency := range contract.Dependencies {
			dep, ok := matrix.Contract(dependency)
			if !ok || dep.DependencyOrder >= contract.DependencyOrder {
				t.Fatalf("dependency %s is not before %s", dependency, contract.Plane)
			}
		}
	}
	copyOfFirst, ok := matrix.Contract(Ledger)
	if !ok {
		t.Fatal("ledger contract missing")
	}
	copyOfFirst.Dependencies[0] = Plane("mutated")
	ledger, _ := matrix.Contract(Ledger)
	if ledger.Dependencies[0] == Plane("mutated") {
		t.Fatal("Contract returned the matrix's dependency backing slice")
	}
}

func TestTodo_RECOVERY_001_Mutation(t *testing.T) {
	mutations := []func(*Matrix){
		func(m *Matrix) { m.Contracts[0].DependencyOrder = 0 },
		func(m *Matrix) { m.Contracts[0].RTO.Minutes = 0 },
		func(m *Matrix) { m.Contracts[0].SemanticChecks = nil },
		func(m *Matrix) { m.Contracts[0].Method = ModeRebuild },
		func(m *Matrix) { m.Contracts[0].Dependencies = []Plane{Projection} },
		func(m *Matrix) { m.Contracts[0].RPOClass = RPOB },
		func(m *Matrix) { m.Contracts[0].RPO.Minutes = 1 },
	}
	for i, mutate := range mutations {
		matrix := DefaultMatrix()
		mutate(&matrix)
		if err := matrix.Validate(); err == nil {
			t.Errorf("mutation %d unexpectedly validated", i)
		}
	}
}

func TestTodo_RECOVERY_002(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	verification := Verify(set, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()})
	if !verification.Verified() {
		t.Fatalf("backup verification failed: %s", verification.Explain())
	}
	if len(verification.SampledIndices) == 0 || len(verification.Checks) < 5 {
		t.Fatalf("verification lacks sample/check evidence: %+v", verification)
	}
	for _, block := range set.Blocks {
		if len(block.Ciphertext) == 0 || bytes.Contains(block.Ciphertext, []byte("ledger event")) {
			t.Fatalf("backup contains plaintext or an empty ciphertext block: %+v", block)
		}
	}
	if got := Explain(verification); !strings.Contains(got, "status=VERIFIED") {
		t.Fatalf("verification explanation = %q", got)
	}
}

func TestTodo_RECOVERY_002_Golden(t *testing.T) {
	first, publicKey, encryptionKey := testBackup(t)
	second, _, _ := testBackup(t)
	if first.Manifest.Digest != second.Manifest.Digest || !bytes.Equal(first.Manifest.Signature, second.Manifest.Signature) {
		t.Fatalf("deterministic nonce input did not produce a stable manifest")
	}
	if first.Manifest.BlockCount != 3 || first.Manifest.ContentHash == "" || first.Manifest.Digest == "" {
		t.Fatalf("incomplete backup manifest: %+v", first.Manifest)
	}
	if _, err := VerifyReadable(first, publicKey, encryptionKey, VerifyOptions{Now: fixedNow(), SampleCount: 3}); err != nil {
		t.Fatalf("full sample verification failed: %v", err)
	}
}

func TestTodo_RECOVERY_002_SampleSeed(t *testing.T) {
	first, publicKey, encryptionKey := testBackup(t)
	options := VerifyOptions{Now: fixedNow(), SampleCount: 2, SampleSeed: []byte("repeatable-check")}
	left := Verify(first, publicKey, encryptionKey, options)
	right := Verify(first, publicKey, encryptionKey, options)
	if !left.Verified() || !right.Verified() || !reflect.DeepEqual(left.SampledIndices, right.SampledIndices) {
		t.Fatalf("seeded sample was not repeatable: left=%+v right=%+v", left, right)
	}
	for _, index := range left.SampledIndices {
		if index < 0 || index >= first.Manifest.BlockCount {
			t.Fatalf("sample index %d is outside the backup inventory", index)
		}
	}
}

func TestTodo_RECOVERY_002_Fault(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)

	missing := cloneBackupSet(set)
	missing.Blocks = missing.Blocks[:2]
	if Verify(missing, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("missing block was accepted")
	}

	corrupt := cloneBackupSet(set)
	corrupt.Blocks[1].Ciphertext[0] ^= 0xff
	if Verify(corrupt, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("corrupt ciphertext was accepted")
	}

	badSignature := cloneBackupSet(set)
	badSignature.Manifest.Signature[0] ^= 0xff
	if Verify(badSignature, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("bad signature was accepted")
	}

	badRetention := cloneBackupSet(set)
	badRetention.Manifest.RetainUntil = fixedNow().Add(-time.Minute)
	if Verify(badRetention, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}).Verified() {
		t.Fatal("expired retention was accepted")
	}
}

func TestTodo_RECOVERY_002_Security(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	wrongKey := bytes.Repeat([]byte{0x99}, 32)
	verification := Verify(set, publicKey, wrongKey, VerifyOptions{Now: fixedNow()})
	if verification.Verified() || !strings.Contains(verification.Failure, "encryption key") {
		t.Fatalf("wrong key result = %+v", verification)
	}

	badRequest := testRequest()
	badRequest.TenantID = "tenant-a,tenant-b"
	if _, err := Create(badRequest, encryptionKey, testSigningKey(t)); !errors.Is(err, ErrUnboundedTenant) {
		t.Fatalf("unbounded tenant error = %v, want ErrUnboundedTenant", err)
	}
}

func TestTodo_RECOVERY_002_Recovery(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	plaintext, verification, err := Restore(set, publicKey, encryptionKey, VerifyOptions{Now: fixedNow(), SampleCount: 1})
	if err != nil || !verification.Verified() {
		t.Fatalf("restore verification failed: verification=%+v err=%v", verification, err)
	}
	if got, want := string(bytes.Join(plaintext, nil)), "ledger event 0ledger event 1ledger event 2"; got != want {
		t.Fatalf("restored plaintext = %q, want %q", got, want)
	}

	repository := NewRepository()
	if err := repository.Put(set); err != nil {
		t.Fatalf("put backup: %v", err)
	}
	stored, ok := repository.Get(set.Manifest.SetID)
	if !ok {
		t.Fatal("stored backup missing")
	}
	stored.Blocks[0].Ciphertext[0] ^= 0xff
	storedAgain, _ := repository.Get(set.Manifest.SetID)
	if bytes.Equal(stored.Blocks[0].Ciphertext, storedAgain.Blocks[0].Ciphertext) {
		t.Fatal("repository returned a mutable backing copy")
	}
	replacement := cloneBackupSet(set)
	replacement.Manifest.Digest = "sha256:replacement"
	if err := repository.Put(replacement); !errors.Is(err, ErrImmutable) {
		t.Fatalf("replacement error = %v, want ErrImmutable", err)
	}
}

func TestTodo_RECOVERY_002_Recovery_ManifestTraversal(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	set.Manifest.BlockHashes[2] = set.Manifest.BlockHashes[1]
	verification := Verify(set, publicKey, encryptionKey, VerifyOptions{Now: fixedNow(), SampleCount: 1})
	if verification.Verified() {
		t.Fatal("manifest traversal accepted mismatched block inventory")
	}
}

func testBackup(t *testing.T) (BackupSet, ed25519.PublicKey, []byte) {
	t.Helper()
	key := bytes.Repeat([]byte{0x21}, 32)
	signingKey := testSigningKey(t)
	request := testRequest()
	request.NonceReader = bytes.NewReader(bytes.Repeat([]byte{0x42}, 36))
	set, err := Create(request, key, signingKey)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	return set, signingKey.Public().(ed25519.PublicKey), key
}

func testRequest() CreateRequest {
	return CreateRequest{
		SetID: "backup-001", TenantID: "tenant-a", PolicyID: "daily-ledger", SourcePlane: "ledger", Watermark: "ledger:42",
		KeyReference: "kms://tenant-a/backup", KeyVersion: "key-v3", RetainUntil: fixedNow().Add(24 * time.Hour), Now: fixedNow(),
		Blocks: [][]byte{[]byte("ledger event 0"), []byte("ledger event 1"), []byte("ledger event 2")},
	}
}

func testSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seed := bytes.Repeat([]byte{0x31}, ed25519.SeedSize)
	return ed25519.NewKeyFromSeed(seed)
}

func fixedNow() time.Time { return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC) }
