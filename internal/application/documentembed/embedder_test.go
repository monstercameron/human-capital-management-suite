package documentembed

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSafetensors writes one "embeddings" tensor in the given dtype.
func writeSafetensors(t *testing.T, path, dtype string, rows [][]float32) {
	t.Helper()
	dim := len(rows[0])
	var data []byte
	for _, row := range rows {
		for _, v := range row {
			switch dtype {
			case "F32":
				data = binary.LittleEndian.AppendUint32(data, math.Float32bits(v))
			case "BF16":
				data = binary.LittleEndian.AppendUint16(data, uint16(math.Float32bits(v)>>16))
			case "F16":
				data = binary.LittleEndian.AppendUint16(data, floatToHalf(v))
			}
		}
	}
	header, err := json.Marshal(map[string]any{
		"__metadata__": map[string]string{"format": "pt"},
		"embeddings":   map[string]any{"dtype": dtype, "shape": []int{len(rows), dim}, "data_offsets": []int{0, len(data)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := binary.LittleEndian.AppendUint64(nil, uint64(len(header)))
	out = append(append(out, header...), data...)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// floatToHalf handles the exact small values the fixtures use.
func floatToHalf(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits>>16) & 0x8000
	if f == 0 {
		return sign
	}
	exp := int((bits>>23)&0xff) - 127 + 15
	return sign | uint16(exp)<<10 | uint16((bits>>13)&0x3ff)
}

// wordPieceDir builds a potion-style model directory: Bert normalizer and
// pre-tokenizer, WordPiece vocabulary, and a 4-wide embeddings tensor.
func wordPieceDir(t *testing.T, dtype string, config string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "potion-mini")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tok := `{
	  "normalizer": {"type": "BertNormalizer", "clean_text": true, "lowercase": true, "strip_accents": null},
	  "pre_tokenizer": {"type": "BertPreTokenizer"},
	  "model": {"type": "WordPiece", "unk_token": "[UNK]", "continuing_subword_prefix": "##", "max_input_chars_per_word": 100,
	    "vocab": {"[UNK]": 0, "[CLS]": 1, "leave": 2, "policy": 3, "on": 4, "##board": 5, "##ing": 6, "cafe": 7, ".": 8}}
	}`
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSafetensors(t, filepath.Join(dir, "model.safetensors"), dtype, [][]float32{
		{9, 9, 9, 9}, {8, 8, 8, 8},
		{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0.5, 0}, {0, 0, 0.25, 0}, {0, 0, 0, 1}, {0.5, 0.5, 0.5, 0.5},
	})
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestStaticWordPieceMeanPoolAndNormalize(t *testing.T) {
	for _, dtype := range []string{"F32", "F16", "BF16"} {
		m, err := LoadStatic(wordPieceDir(t, dtype, ""))
		if err != nil {
			t.Fatalf("%s: %v", dtype, err)
		}
		if m.Model() != "static:potion-mini" || m.Dim() != 4 || !IsLocal(m) {
			t.Fatalf("%s model = %q dim %d", dtype, m.Model(), m.Dim())
		}
		vecs, err := m.Embed(context.Background(), []string{"Leave POLICY", "Onboarding", "Café", "zzz [UNK]", ""})
		if err != nil {
			t.Fatal(err)
		}
		// "leave policy" -> mean of e2,e3 = (0.5,0.5,0,0) -> normalized.
		if s := float32(1 / math.Sqrt2); !near(vecs[0][0], s) || !near(vecs[0][1], s) || vecs[0][2] != 0 {
			t.Fatalf("%s leave policy = %v", dtype, vecs[0])
		}
		// "onboarding" -> on, ##board, ##ing: only the third axis.
		if !near(vecs[1][2], 1) || vecs[1][0] != 0 {
			t.Fatalf("%s onboarding = %v", dtype, vecs[1])
		}
		// Accents are stripped under the Bert normalizer.
		if !near(vecs[2][3], 1) {
			t.Fatalf("%s cafe = %v", dtype, vecs[2])
		}
		// Unknown words and the unknown token contribute nothing; the
		// unknown row (all nines) must never leak in.
		for _, i := range []int{3, 4} {
			for _, v := range vecs[i] {
				if v != 0 {
					t.Fatalf("%s text %d = %v", dtype, i, vecs[i])
				}
			}
		}
	}
	m, err := LoadStatic(wordPieceDir(t, "F32", `{"normalize": false}`))
	if err != nil {
		t.Fatal(err)
	}
	vecs, _ := m.Embed(context.Background(), []string{"leave policy"})
	if !near(vecs[0][0], 0.5) {
		t.Fatalf("unnormalized = %v", vecs[0])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Embed(ctx, []string{"x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled embed = %v", err)
	}
}

func TestStaticUnigramViterbi(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uni")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tok := `{"normalizer": {"type": "Sequence", "normalizers": [{"type": "Lowercase"}]},
	  "pre_tokenizer": {"type": "Metaspace", "replacement": "▁"},
	  "model": {"type": "Unigram", "unk_id": 0, "vocab": [["<unk>", 0], ["▁pay", -1], ["▁payroll", -1.5], ["roll", -2], ["▁", -3], ["x", -9]]}}`
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSafetensors(t, filepath.Join(dir, "model.safetensors"), "F32", [][]float32{{9, 9}, {1, 0}, {0, 1}, {1, 1}, {5, 5}, {0, 0}})
	m, err := LoadStatic(dir)
	if err != nil {
		t.Fatal(err)
	}
	// "▁payroll" (one piece, -1.5) beats "▁pay"+"roll" (-3).
	if ids := m.tokenizer.encode("PAYROLL"); len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("payroll ids = %v", ids)
	}
	// Uncovered characters become unknown and are dropped.
	if ids := m.tokenizer.encode("pay?"); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("pay? ids = %v", ids)
	}
}

func TestStaticRejectsMalformedModels(t *testing.T) {
	good := wordPieceDir(t, "F32", "")
	if _, err := LoadStatic(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory loaded")
	}
	bad := func(name, content string) string {
		dir := t.TempDir()
		raw, _ := os.ReadFile(filepath.Join(good, "tokenizer.json"))
		model, _ := os.ReadFile(filepath.Join(good, "model.safetensors"))
		_ = os.WriteFile(filepath.Join(dir, "tokenizer.json"), raw, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "model.safetensors"), model, 0o600)
		_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
		return dir
	}
	for name, dir := range map[string]string{
		"bpe tokenizer":   bad("tokenizer.json", `{"model": {"type": "BPE", "vocab": {}}}`),
		"bad tokenizer":   bad("tokenizer.json", `{`),
		"short model":     bad("model.safetensors", "abc"),
		"truncated model": bad("model.safetensors", "\xff\x00\x00\x00\x00\x00\x00\x00{}"),
	} {
		if _, err := LoadStatic(dir); err == nil {
			t.Fatalf("%s loaded", name)
		}
	}
	small := t.TempDir()
	raw, _ := os.ReadFile(filepath.Join(good, "tokenizer.json"))
	_ = os.WriteFile(filepath.Join(small, "tokenizer.json"), raw, 0o600)
	writeSafetensors(t, filepath.Join(small, "model.safetensors"), "F32", [][]float32{{1, 0}})
	if _, err := LoadStatic(small); err == nil || !strings.Contains(err.Error(), "rows") {
		t.Fatalf("vocab larger than tensor = %v", err)
	}
}

func TestHTTPEmbedderAndEnv(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var data []item
		for i := len(req.Input) - 1; i >= 0; i-- {
			data = append(data, item{Index: i, Embedding: []float32{float32(len(req.Input[i])), 1}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "model": req.Model})
	}))
	defer srv.Close()
	env := map[string]string{EnvURL: srv.URL, EnvModel: "nomic-embed", EnvAPIKey: "secret"}
	e, err := FromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if e.Model() != "nomic-embed" || !IsLocal(e) {
		t.Fatalf("model %q local %v", e.Model(), IsLocal(e))
	}
	texts := make([]string, httpBatch+3)
	for i := range texts {
		texts[i] = strings.Repeat("a", i+1)
	}
	vecs, err := e.Embed(context.Background(), texts)
	if err != nil || len(vecs) != len(texts) || vecs[0][0] != 1 || vecs[len(texts)-1][0] != float32(len(texts)) || calls != 2 {
		t.Fatalf("embed = %d vectors, calls %d, %v", len(vecs), calls, err)
	}
	env[EnvAPIKey] = "wrong"
	e, _ = FromEnv(func(k string) string { return env[k] })
	if _, err := e.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("rejected request accepted")
	}
	if _, err := FromEnv(func(string) string { return "" }); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured = %v", err)
	}
	if _, err := FromEnv(func(k string) string { return map[string]string{EnvURL: srv.URL}[k] }); err == nil {
		t.Fatal("URL without model accepted")
	}
	if _, err := NewHTTP("ftp://x", "m", "", nil); err == nil {
		t.Fatal("ftp URL accepted")
	}
	remote, err := NewHTTP("https://api.example.com/v1/embeddings", "m", "", nil)
	if err != nil || IsLocal(remote) || remote.endpoint != "https://api.example.com/v1/embeddings" {
		t.Fatalf("remote = %+v, %v", remote, err)
	}
	static, err := FromEnv(func(k string) string {
		return map[string]string{EnvDir: wordPieceDir(t, "F32", ""), EnvURL: srv.URL}[k]
	})
	if err != nil || static.Model() != "static:potion-mini" {
		t.Fatalf("directory should win: %v, %v", static, err)
	}
}

// TestStaticPotionModel runs against the real minishlab/potion-base-8M
// files when a developer has placed them under .artifacts/models; the
// repository never downloads them.
func TestStaticPotionModel(t *testing.T) {
	dir := filepath.Join("..", "..", "..", ".artifacts", "models", "potion-base-8M")
	if _, err := os.Stat(filepath.Join(dir, "model.safetensors")); err != nil {
		t.Skip("potion-base-8M is not present under .artifacts/models")
	}
	m, err := LoadStatic(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Model() != "static:potion-base-8M" || m.Dim() != 256 {
		t.Fatalf("model %q dim %d", m.Model(), m.Dim())
	}
	vecs, err := m.Embed(context.Background(), []string{
		"How do I request paid time off for a vacation?",
		"Submitting a leave request for holiday days away from work",
		"Rotate the TLS certificate on the database server",
		"Renewing expired SSL certificates for backend services",
	})
	if err != nil {
		t.Fatal(err)
	}
	cos := func(a, b []float32) float64 {
		var dot float64
		for i := range a {
			dot += float64(a[i]) * float64(b[i])
		}
		return dot
	}
	var norm float64
	for _, v := range vecs[0] {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1) > 1e-3 {
		t.Fatalf("vector is not unit length: %v", norm)
	}
	leave, certs, crossA, crossB := cos(vecs[0], vecs[1]), cos(vecs[2], vecs[3]), cos(vecs[0], vecs[2]), cos(vecs[1], vecs[3])
	t.Logf("leave~leave %.3f, cert~cert %.3f, cross %.3f / %.3f", leave, certs, crossA, crossB)
	if leave <= crossA || leave <= crossB || certs <= crossA || certs <= crossB {
		t.Fatalf("related sentences do not score higher: leave %.3f certs %.3f cross %.3f %.3f", leave, certs, crossA, crossB)
	}
}
