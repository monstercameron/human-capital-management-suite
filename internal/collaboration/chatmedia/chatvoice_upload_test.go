package chatmedia

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

func oggFixturePage(serial, seq uint32, kind byte, granule int64, body []byte) []byte {
	var table []byte
	for n := len(body); ; n -= 255 {
		if n >= 255 {
			table = append(table, 255)
			continue
		}
		table = append(table, byte(n))
		break
	}
	page := make([]byte, 27, 27+len(table)+len(body))
	copy(page, "OggS")
	page[5] = kind
	binary.LittleEndian.PutUint64(page[6:14], uint64(granule))
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], seq)
	page[26] = byte(len(table))
	page = append(page, table...)
	page = append(page, body...)
	binary.LittleEndian.PutUint32(page[22:26], oggChecksum(page))
	return page
}

// oggFixture is Ogg Opus lasting seconds of 48 kHz audio after the pre-skip.
func oggFixture(seconds int) []byte {
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8], head[9] = 1, 1
	binary.LittleEndian.PutUint16(head[10:12], 312)
	binary.LittleEndian.PutUint32(head[12:16], 48000)
	tags := append([]byte("OpusTags"), 0, 0, 0, 0, 0, 0, 0, 0)
	out := oggFixturePage(7, 0, 0x02, 0, head)
	out = append(out, oggFixturePage(7, 1, 0, 0, tags)...)
	out = append(out, oggFixturePage(7, 2, 0x04, int64(seconds)*48000+312, []byte{0xf8, 1, 2, 3})...)
	return out
}

func ebmlFixture(id uint32, unknown bool, body ...[]byte) []byte {
	var content []byte
	for _, part := range body {
		content = append(content, part...)
	}
	var out []byte
	for shift := 24; shift >= 0; shift -= 8 {
		if b := byte(id >> shift); b != 0 || len(out) > 0 {
			out = append(out, b)
		}
	}
	if unknown {
		return append(append(out, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff), content...)
	}
	size := make([]byte, 8)
	binary.BigEndian.PutUint64(size, uint64(len(content)))
	size[0] = 0x01
	return append(append(out, size...), content...)
}

func ebmlNumber(v uint64) []byte {
	out := []byte{}
	for ; v > 0; v >>= 8 {
		out = append([]byte{byte(v)}, out...)
	}
	if len(out) == 0 {
		out = []byte{0}
	}
	return out
}

// webmFixture is a MediaRecorder-shaped recording: unknown-size Segment and
// Clusters, one SimpleBlock of one 20 ms packet each.
func webmFixture(blocks int, codec string, kind uint64, live bool) []byte {
	header := ebmlFixture(ebmlHeader, false, ebmlFixture(ebmlDocType, false, []byte("webm")))
	info := ebmlFixture(ebmlInfo, false, ebmlFixture(ebmlTimeScale, false, ebmlNumber(1_000_000)))
	tracks := ebmlFixture(ebmlTracks, false, ebmlFixture(ebmlTrack, false, ebmlFixture(ebmlTrackType, false, ebmlNumber(kind)), ebmlFixture(ebmlCodecID, false, []byte(codec))))
	var clusters []byte
	for first := 0; first < blocks; first += 50 {
		parts := [][]byte{ebmlFixture(ebmlTimecode, false, ebmlNumber(uint64(first*20)))}
		for i := first; i < blocks && i < first+50; i++ {
			parts = append(parts, ebmlFixture(ebmlSimple, false, []byte{0x81, byte((i - first) * 20 >> 8), byte((i - first) * 20), 0x80, 0xf8, 1, 2}))
		}
		clusters = append(clusters, ebmlFixture(ebmlCluster, live, parts...)...)
	}
	return append(header, ebmlFixture(ebmlSegment, live, info, tracks, clusters)...)
}

func TestChatvoiceInspect(t *testing.T) {
	cases := map[string]struct {
		data []byte
		kind MediaType
		want time.Duration
	}{
		"ogg opus":                    {oggFixture(3), MediaOgg, 3 * time.Second},
		"webm opus, sized":            {webmFixture(100, "A_OPUS", 2, false), MediaWebM, 2 * time.Second},
		"webm opus, live (unknown)":   {webmFixture(160, "A_OPUS", 2, true), MediaWebM, 3200 * time.Millisecond},
		"webm opus, one short packet": {webmFixture(1, "A_OPUS", 2, true), MediaWebM, 20 * time.Millisecond},
	}
	for name, c := range cases {
		facts, err := InspectVoice(c.data)
		if err != nil || facts.MediaType != c.kind || facts.Duration != c.want {
			t.Fatalf("%s: %+v err=%v", name, facts, err)
		}
	}
}

func TestChatvoiceInspectRefuses(t *testing.T) {
	corrupt := oggFixture(2)
	corrupt[len(corrupt)-1] ^= 0xff
	twoStreams := append(oggFixture(1), oggFixturePage(9, 0, 0, 48000, []byte{1})...)
	for name, data := range map[string][]byte{
		"bad ogg checksum":  corrupt,
		"ogg second stream": twoStreams,
		"ogg trailing junk": append(oggFixture(1), 1, 2, 3),
		"video track":       webmFixture(10, "A_OPUS", 1, false),
		"vorbis":            webmFixture(10, "A_VORBIS", 2, false),
		"webm no blocks":    webmFixture(0, "A_OPUS", 2, false),
		"mp3":               append([]byte("ID3"), make([]byte, 40)...),
		"empty":             nil,
		"truncated webm":    webmFixture(10, "A_OPUS", 2, false)[:60],
	} {
		if _, err := InspectVoice(data); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

type voiceScannerFake struct {
	safe  bool
	calls int
}

func (s *voiceScannerFake) Scan(context.Context, string, io.Reader) (quarantine.Verdict, error) {
	s.calls++
	return quarantine.Verdict{Safe: s.safe, Reason: "fixture"}, nil
}

func voiceService(scanner Scanner, authorize Authorizer) *Service {
	return New(Config{Store: NewMemoryStore(), Scanner: scanner, Authorize: authorize})
}

func allowAll(context.Context, AccessRequest) error { return nil }

// TestTodo_CHATVOICE_002_Integration takes a recording through quarantine,
// scanning and admission, then reads it back through a grant for playback.
func TestTodo_CHATVOICE_002_Integration(t *testing.T) {
	scanner := &voiceScannerFake{safe: true}
	svc := voiceService(scanner, allowAll)
	recording := webmFixture(100, "A_OPUS", 2, true)
	ref, err := svc.UploadVoice(context.Background(), UploadRequest{TenantID: "t", ConversationID: "room", PrincipalID: "alice", DeclaredType: "audio/webm;codecs=opus", Content: recording})
	if err != nil || ref.State != StateAdmitted || ref.MediaType != MediaWebM || ref.Size != int64(len(recording)) || scanner.calls != 1 {
		t.Fatalf("upload %+v err=%v scans=%d", ref, err, scanner.calls)
	}
	grant, err := svc.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "room", PrincipalID: "bob", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	body, played, err := svc.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "room", PrincipalID: "bob", ArtifactID: ref.ArtifactID, Grant: grant.Token}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	if string(got) != string(recording) || played.MediaType != MediaWebM {
		t.Fatal("playback did not return the recording")
	}
	bytes, kind, err := svc.ReadVoice(context.Background(), "t", "room", ref.ArtifactID)
	if err != nil || kind != MediaWebM || string(bytes) != string(recording) {
		t.Fatalf("worker read err=%v", err)
	}
	if _, _, err = svc.ReadVoice(context.Background(), "t", "other-room", ref.ArtifactID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("worker read of another conversation: %v", err)
	}
	if _, _, err = svc.ReadVoice(context.Background(), "other-tenant", "room", ref.ArtifactID); err == nil {
		t.Fatal("worker read of another tenant")
	}
}

func TestTodo_CHATVOICE_002_Security(t *testing.T) {
	good := webmFixture(100, "A_OPUS", 2, true)
	long := webmFixture(6100, "A_OPUS", 2, true) // 122 seconds
	scanner := &voiceScannerFake{safe: true}
	svc := voiceService(scanner, allowAll)
	upload := func(declared string, content []byte) error {
		_, err := svc.UploadVoice(context.Background(), UploadRequest{TenantID: "t", ConversationID: "room", PrincipalID: "alice", DeclaredType: declared, Content: content})
		return err
	}
	if err := upload("audio/webm", []byte("MZ not audio at all")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("not audio: %v", err)
	}
	if err := upload("audio/webm", long); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("over-long recording: %v", err)
	}
	if err := upload("audio/ogg", good); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("mismatched content type: %v", err)
	}
	if err := upload("audio/mpeg", good); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("other audio type: %v", err)
	}
	if err := upload("audio/webm", append(good, make([]byte, VoiceMaxBytes)...)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("over-size recording: %v", err)
	}
	if scanner.calls != 0 {
		t.Fatal("a refused file reached the scanner")
	}
	denied := voiceService(scanner, func(context.Context, AccessRequest) error { return errors.New("not a member") })
	if _, err := denied.UploadVoice(context.Background(), UploadRequest{TenantID: "t", ConversationID: "room", PrincipalID: "mallory", DeclaredType: "audio/webm", Content: good}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("sender who cannot post: %v", err)
	}
	unsafe := voiceService(&voiceScannerFake{safe: false}, allowAll)
	if _, err := unsafe.UploadVoice(context.Background(), UploadRequest{TenantID: "t", ConversationID: "room", PrincipalID: "alice", DeclaredType: "audio/webm", Content: good}); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("unsafe verdict: %v", err)
	}
	if _, err := voiceService(nil, allowAll).UploadVoice(context.Background(), UploadRequest{TenantID: "t", ConversationID: "room", PrincipalID: "alice", DeclaredType: "audio/webm", Content: good}); !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("no scanner must fail closed: %v", err)
	}
}

// The voice scanner admits a real recording, refuses what is not one, and defers
// to the deployment's scanner when there is one.
func TestChatvoiceScanner(t *testing.T) {
	recording := webmFixture(100, "A_OPUS", 2, true)
	svc := voiceService(VoiceScanner{}, allowAll)
	ref, err := svc.UploadVoice(context.Background(), UploadRequest{TenantID: "t", ConversationID: "room", PrincipalID: "alice", DeclaredType: "audio/webm", Content: recording})
	if err != nil || ref.State != StateAdmitted {
		t.Fatalf("a real recording was refused: %+v err=%v", ref, err)
	}
	verdict, err := VoiceScanner{}.Scan(context.Background(), "x", bytesReader([]byte("MZ\x90 not audio")))
	if err != nil || verdict.Safe {
		t.Fatalf("not audio admitted: %+v err=%v", verdict, err)
	}
	external := &voiceScannerFake{safe: false}
	verdict, err = VoiceScanner{External: external}.Scan(context.Background(), "x", bytesReader(recording))
	if err != nil || verdict.Safe || external.calls != 1 {
		t.Fatalf("the deployment's scanner was not consulted: %+v calls=%d", verdict, external.calls)
	}
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
