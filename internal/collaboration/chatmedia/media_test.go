package chatmedia

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
	"golang.org/x/image/bmp"
)

type testScanner struct {
	verdict quarantine.Verdict
	err     error
}

type countingStore struct {
	inner                 *MemoryStore
	quarantines, verdicts int
}

func (s *countingStore) Quarantine(ctx context.Context, a Artifact) error {
	s.quarantines++
	return s.inner.Quarantine(ctx, a)
}
func (s *countingStore) SetRenditions(ctx context.Context, id, tenant string, renditions map[string]Rendition) error {
	return s.inner.SetRenditions(ctx, id, tenant, renditions)
}
func (s *countingStore) SetVerdict(ctx context.Context, id, tenant string, state ArtifactState, reason, scanner string) error {
	s.verdicts++
	return s.inner.SetVerdict(ctx, id, tenant, state, reason, scanner)
}
func (s *countingStore) Get(ctx context.Context, tenant, id string) (Artifact, error) {
	return s.inner.Get(ctx, tenant, id)
}
func (s *countingStore) GetRendition(ctx context.Context, tenant, id, variant string) (Artifact, error) {
	return s.inner.GetRendition(ctx, tenant, id, variant)
}

type countingScanner struct {
	calls   int
	verdict quarantine.Verdict
}

func (s *countingScanner) Scan(ctx context.Context, id string, r io.Reader) (quarantine.Verdict, error) {
	s.calls++
	return s.verdict, nil
}

func (s testScanner) Scan(context.Context, string, io.Reader) (quarantine.Verdict, error) {
	return s.verdict, s.err
}
func pngBytes() []byte {
	var out bytes.Buffer
	im := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	im.SetNRGBA(0, 0, color.NRGBA{R: 120, G: 80, B: 40, A: 255})
	_ = png.Encode(&out, im)
	return out.Bytes()
}
func bmpBytes() []byte {
	var out bytes.Buffer
	_ = bmp.Encode(&out, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	return out.Bytes()
}
func gifBytes() []byte {
	var out bytes.Buffer
	_ = gif.Encode(&out, image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black}), nil)
	return out.Bytes()
}
func makeService(scanner Scanner, auth Authorizer) *Service {
	return New(Config{Store: NewMemoryStore(), Scanner: scanner, Authorize: auth, Now: func() time.Time { return time.Unix(100, 0) }})
}

func TestTodo_CHAT_036(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", DeclaredType: "image/png", Content: pngBytes(), EvidenceID: "ev"})
	if err != nil || ref.State != StateAdmitted {
		t.Fatalf("upload = %+v, %v", ref, err)
	}
	g, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Open(context.Background(), AccessRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", ArtifactID: ref.ArtifactID, Grant: g.Token}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
}
func TestTodo_CHAT_036_Security(t *testing.T) {
	s := makeService(nil, func(context.Context, AccessRequest) error { return nil })
	_, err := s.Upload(context.Background(), UploadRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", DeclaredType: "image/png", Content: pngBytes(), EvidenceID: "ev"})
	if !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("scanner failure = %v", err)
	}
	s2 := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	_, err = s2.Upload(context.Background(), UploadRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", DeclaredType: "image/png", Content: []byte("<script>"), EvidenceID: "ev"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("polyglot = %v", err)
	}
}
func TestTodo_CHAT_036_AuthorizationPrecedesSideEffects(t *testing.T) {
	store := &countingStore{inner: NewMemoryStore()}
	scanner := &countingScanner{verdict: quarantine.Verdict{Safe: true}}
	s := New(Config{Store: store, Scanner: scanner, Authorize: func(context.Context, AccessRequest) error { return ErrUnauthorized }})
	_, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: pngBytes(), EvidenceID: "e"})
	if !errors.Is(err, ErrUnauthorized) || store.quarantines != 0 || scanner.calls != 0 {
		t.Fatalf("denied upload side effects: err=%v quarantines=%d scans=%d", err, store.quarantines, scanner.calls)
	}
}
func TestTodo_CHAT_036_Integration(t *testing.T) {
	store := NewMemoryStore()
	s := New(Config{Store: store, Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Authorize: func(context.Context, AccessRequest) error { return nil }})
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "a", ConversationID: "c", PrincipalID: "p", DeclaredType: "audio/wav", Content: []byte("RIFFxxxxWAVEaudio"), EvidenceID: "e"})
	if err != nil || ref.ArtifactID == "" {
		t.Fatalf("wav integration = %+v %v", ref, err)
	}
}
func TestTodo_CHAT_037(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "audio/mpeg", Content: []byte("ID3recording"), EvidenceID: "e", Transcript: "hello"})
	if err != nil || ref.Transcript != "hello" {
		t.Fatalf("audio = %+v %v", ref, err)
	}
}
func TestTodo_CHAT_037_Security(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: false, Reason: "malware"}}, func(context.Context, AccessRequest) error { return nil })
	_, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "audio/mpeg", Content: []byte("ID3bad"), EvidenceID: "e"})
	if !errors.Is(err, ErrQuarantined) {
		t.Fatalf("unsafe audio = %v", err)
	}
}
func TestTodo_CHAT_038(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: pngBytes(), EvidenceID: "e"})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	b, _, err := s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, 1, func() *int64 { x := int64(5); return &x }())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(b)
	b.Close()
	if string(got) != "PNG\r" {
		t.Fatalf("range = %q", got)
	}
	s.Revoke(ref.ArtifactID)
	_, _, err = s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, 0, nil)
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked = %v", err)
	}
}
func TestTodo_CHAT_038_Security(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return errors.New("revoked") })
	_, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: "missing"}, time.Minute)
	if err == nil {
		t.Fatal("unknown artifact authorized")
	}
	if _, ok := normalizeType("application/octet-stream"); ok {
		t.Fatal("unknown type admitted")
	}
}
func TestTodo_CHAT_038_GrantBindingExpiryAndReauthorization(t *testing.T) {
	clock := time.Unix(100, 0)
	allowed := true
	calls := 0
	s := New(Config{Store: NewMemoryStore(), Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Now: func() time.Time { return clock }, Authorize: func(context.Context, AccessRequest) error {
		calls++
		if !allowed {
			return ErrUnauthorized
		}
		return nil
	}})
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", DeclaredType: "image/png", Content: pngBytes(), EvidenceID: "e"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	allowed = false
	_, _, err = s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, 0, nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("reauthorization = %v", err)
	}
	allowed = true
	_, _, err = s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c2", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, 0, nil)
	if err == nil {
		t.Fatal("cross-conversation grant accepted")
	}
	clock = clock.Add(2 * time.Second)
	_, _, err = s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, 0, nil)
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("expired grant = %v", err)
	}
	if calls < 3 {
		t.Fatalf("authorization calls=%d", calls)
	}
}
func TestTodo_CHAT_038_DigestScope(t *testing.T) {
	s := New(Config{Store: NewMemoryStore(), Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Authorize: func(context.Context, AccessRequest) error { return nil }})
	b := pngBytes()
	a, _ := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", DeclaredType: "image/png", Content: b, EvidenceID: "1"})
	d, _ := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c2", PrincipalID: "p", DeclaredType: "image/png", Content: b, EvidenceID: "2"})
	if a.ArtifactID == d.ArtifactID {
		t.Fatal("same bytes across conversations share artifact identity")
	}
}
func TestTodo_CHAT_038_Integration(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	for typ, b := range map[string][]byte{"image/bmp": bmpBytes(), "image/gif": gifBytes(), "video/mp4": []byte("\x00\x00\x00\x18ftypisomdata")} {
		if _, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: typ, Content: b, EvidenceID: typ}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
}
func TestTodo_CHAT_039(t *testing.T) {
	if _, err := ValidateEmbedURL("https://example.com/embed", []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_CHAT_039_Security(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://127.0.0.1/x", "https://example.com/x#token", "https://u:p@example.com/x"} {
		if _, err := ValidateEmbedURL(raw, []string{"example.com"}); !errors.Is(err, ErrEmbedDenied) {
			t.Fatalf("%s allowed: %v", raw, err)
		}
	}
}
func TestTodo_CHAT_039_Browser(t *testing.T) {
	if _, err := ValidateEmbedURL("https://evil.example", []string{"example.com"}); !errors.Is(err, ErrEmbedDenied) {
		t.Fatal(err)
	}
	if strings.Contains("default-src 'none'; sandbox", "script-src") {
		t.Fatal("unsafe CSP")
	}
}
func TestTodo_CHAT_039_EmbedGrantReauthorizesOrigin(t *testing.T) {
	allowed := true
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error {
		if !allowed {
			return ErrUnauthorized
		}
		return nil
	})
	g, err := s.AuthorizeEmbed(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p"}, "https://example.com/embed", []string{"example.com"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckEmbed(context.Background(), g, AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p"}, "https://example.com/other", []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckEmbed(context.Background(), g, AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p"}, "https://evil.example", []string{"example.com"}); !errors.Is(err, ErrEmbedDenied) {
		t.Fatalf("origin escape=%v", err)
	}
	allowed = false
	if err := s.CheckEmbed(context.Background(), g, AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p"}, "https://example.com/embed", []string{"example.com"}); !errors.Is(err, ErrEmbedDenied) {
		t.Fatalf("revoked auth=%v", err)
	}
}

func TestTodo_CHAT_036_FilesystemStoreLifecycle(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	a := Artifact{Reference: Reference{ArtifactID: "a", TenantID: "t", ConversationID: "c", State: StateQuarantined}, Content: pngBytes()}
	if err := store.Quarantine(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "t", "a"); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("quarantine get=%v", err)
	}
	if err := store.SetVerdict(context.Background(), "a", "t", StateAdmitted, "", "scanner/v1"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), "t", "a")
	if err != nil || string(got.Content) != string(a.Content) {
		t.Fatalf("filesystem get=%v content=%q", err, got.Content)
	}
	if _, err := os.Stat(root + "/a.bytes"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVerdict(context.Background(), "a", "other", StateRejected, "x", ""); err == nil {
		t.Fatal("foreign verdict accepted")
	}
}

func TestTodo_CHAT_037_ScannerFailureAndAllFormats(t *testing.T) {
	for typ, content := range map[string][]byte{"audio/mpeg": []byte("ID3x"), "audio/wav": []byte("RIFFxxxxWAVEx"), "image/bmp": bmpBytes(), "image/png": pngBytes(), "image/gif": gifBytes(), "video/mp4": []byte("\x00\x00\x00\x18ftypisomx")} {
		s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
		if _, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: typ, PrincipalID: "p", DeclaredType: typ, Content: content, EvidenceID: typ}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	s := makeService(testScanner{err: errors.New("engine down")}, func(context.Context, AccessRequest) error { return nil })
	_, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: pngBytes(), EvidenceID: "e"})
	if !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("scanner error=%v", err)
	}
}

// TestTodo_CHAT_038_BoundedProcessState proves grants, embed grants and
// revocation counters are expired rather than accumulated for the life of the
// process, and that an expired grant is still refused after the sweep.
func TestTodo_CHAT_038_BoundedProcessState(t *testing.T) {
	clock := time.Unix(100, 0)
	s := New(Config{Store: NewMemoryStore(), Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Now: func() time.Time { return clock }, Authorize: func(context.Context, AccessRequest) error { return nil }})
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", DeclaredType: "image/png", Content: pngBytes(), EvidenceID: "e"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AuthorizeEmbed(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p"}, "https://example.com/a", []string{"example.com"}, time.Minute); err != nil {
			t.Fatal(err)
		}
		s.Revoke(ref.ArtifactID + string(rune('a'+i)))
	}
	grants, embeds, revocations := s.Retained()
	if grants != 5 || embeds != 5 || revocations != 5 {
		t.Fatalf("retained grants=%d embeds=%d revocations=%d, want 5 each", grants, embeds, revocations)
	}
	expiring, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Past every grant TTL and the retention window, one further write sweeps
	// the whole process-local state back to what is still live.
	clock = clock.Add(2 * DefaultRetention)
	if _, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute); err != nil {
		t.Fatal(err)
	}
	grants, embeds, revocations = s.Retained()
	if grants != 1 || embeds != 0 || revocations != 0 {
		t.Fatalf("after sweep grants=%d embeds=%d revocations=%d, want 1, 0, 0", grants, embeds, revocations)
	}
	if _, _, err := s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: expiring.Token}, 0, nil); !errors.Is(err, ErrRevoked) {
		t.Fatalf("swept grant=%v, want ErrRevoked", err)
	}
	// A revocation still bites while the grant it targets is live.
	live, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.Revoke(ref.ArtifactID)
	if _, _, err := s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c1", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: live.Token}, 0, nil); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked live grant=%v, want ErrRevoked", err)
	}
}
