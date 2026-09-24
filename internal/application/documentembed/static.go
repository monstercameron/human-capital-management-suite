package documentembed

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Static is a model2vec-style static embedding model: every token has one
// vector, a text's embedding is the mean of its token vectors, L2
// normalized. It runs in process, so text never leaves the deployment.
type Static struct {
	name      string
	tokenizer *tokenizer
	vectors   []float32
	dim       int
	normalize bool
	maxTokens int
}

// LoadStatic loads tokenizer.json, model.safetensors (one "embeddings"
// tensor of shape [vocab, dim] in F32, F16 or BF16) and the optional
// config.json from dir, the layout of minishlab/potion-base-8M.
func LoadStatic(dir string) (*Static, error) {
	tokRaw, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, fmt.Errorf("document embedding: read tokenizer: %w", err)
	}
	tok, err := parseTokenizer(tokRaw)
	if err != nil {
		return nil, err
	}
	modelRaw, err := os.ReadFile(filepath.Join(dir, "model.safetensors"))
	if err != nil {
		return nil, fmt.Errorf("document embedding: read model: %w", err)
	}
	vectors, rows, dim, err := readEmbeddingsTensor(modelRaw)
	if err != nil {
		return nil, err
	}
	if rows < tok.size() {
		return nil, fmt.Errorf("document embedding: tokenizer has %d tokens but the model has %d rows", tok.size(), rows)
	}
	model := &Static{name: "static:" + filepath.Base(filepath.Clean(dir)), tokenizer: tok, vectors: vectors, dim: dim, normalize: true, maxTokens: 512}
	if cfgRaw, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		var cfg struct {
			Normalize *bool `json:"normalize"`
		}
		if json.Unmarshal(cfgRaw, &cfg) == nil && cfg.Normalize != nil {
			model.normalize = *cfg.Normalize
		}
	}
	return model, nil
}

// Model names the model after its directory, e.g. static:potion-base-8M.
func (m *Static) Model() string { return m.name }

// Local is always true: the model runs in process.
func (m *Static) Local() bool { return true }

// Dim is the vector width.
func (m *Static) Dim() int { return m.dim }

// Embed returns one vector per text. Unknown tokens are skipped; a text
// with no known tokens embeds as the zero vector.
func (m *Static) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		vec := make([]float32, m.dim)
		ids := m.tokenizer.encode(text)
		if len(ids) > m.maxTokens {
			ids = ids[:m.maxTokens]
		}
		for _, id := range ids {
			row := m.vectors[id*m.dim : (id+1)*m.dim]
			for j, v := range row {
				vec[j] += v
			}
		}
		if len(ids) > 0 {
			for j := range vec {
				vec[j] /= float32(len(ids))
			}
		}
		if m.normalize {
			var norm float64
			for _, v := range vec {
				norm += float64(v) * float64(v)
			}
			if norm > 0 {
				scale := float32(1 / math.Sqrt(norm))
				for j := range vec {
					vec[j] *= scale
				}
			}
		}
		out[i] = vec
	}
	return out, nil
}

// readEmbeddingsTensor decodes the "embeddings" tensor of a safetensors
// file into row-major float32.
func readEmbeddingsTensor(raw []byte) ([]float32, int, int, error) {
	if len(raw) < 8 {
		return nil, 0, 0, errors.New("document embedding: model file is too short")
	}
	headerLen := binary.LittleEndian.Uint64(raw[:8])
	if headerLen > uint64(len(raw)-8) {
		return nil, 0, 0, errors.New("document embedding: model header is truncated")
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw[8:8+headerLen], &header); err != nil {
		return nil, 0, 0, fmt.Errorf("document embedding: model header: %w", err)
	}
	entry, ok := header["embeddings"]
	if !ok {
		return nil, 0, 0, errors.New(`document embedding: model has no "embeddings" tensor`)
	}
	var tensor struct {
		Dtype   string   `json:"dtype"`
		Shape   []int    `json:"shape"`
		Offsets []uint64 `json:"data_offsets"`
	}
	if err := json.Unmarshal(entry, &tensor); err != nil {
		return nil, 0, 0, err
	}
	if len(tensor.Shape) != 2 || tensor.Shape[0] <= 0 || tensor.Shape[1] <= 0 || len(tensor.Offsets) != 2 {
		return nil, 0, 0, errors.New("document embedding: embeddings tensor must be two-dimensional")
	}
	width := map[string]int{"F32": 4, "F16": 2, "BF16": 2}[tensor.Dtype]
	if width == 0 {
		return nil, 0, 0, fmt.Errorf("document embedding: unsupported dtype %s", tensor.Dtype)
	}
	rows, dim := tensor.Shape[0], tensor.Shape[1]
	data := raw[8+headerLen:]
	start, end := tensor.Offsets[0], tensor.Offsets[1]
	if end > uint64(len(data)) || start > end || end-start != uint64(rows*dim*width) {
		return nil, 0, 0, errors.New("document embedding: embeddings tensor offsets are inconsistent")
	}
	data = data[start:end]
	out := make([]float32, rows*dim)
	for i := range out {
		switch tensor.Dtype {
		case "F32":
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
		case "F16":
			out[i] = halfToFloat(binary.LittleEndian.Uint16(data[i*2:]))
		case "BF16":
			out[i] = math.Float32frombits(uint32(binary.LittleEndian.Uint16(data[i*2:])) << 16)
		}
	}
	return out, rows, dim, nil
}

func halfToFloat(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h) & 0x3ff
	switch {
	case exp == 0 && frac == 0:
		return math.Float32frombits(sign)
	case exp == 0:
		v := float32(frac) / (1 << 24)
		if sign != 0 {
			v = -v
		}
		return v
	case exp == 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | frac<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | frac<<13)
}

// tokenizer is the subset of a Hugging Face tokenizer.json that static
// models use: a Bert-style or lowercase normalizer, whitespace and
// punctuation (or Metaspace) pre-tokenization, and a WordPiece or Unigram
// model. Special tokens are never emitted and unknown tokens are dropped,
// as model2vec does.
type tokenizer struct {
	lowercase, stripAccents bool
	metaspace               bool
	replacement             string
	kind                    string
	vocab                   map[string]int
	unk                     int
	prefix                  string
	maxWordChars            int
	scores                  map[string]float64
	maxPiece                int
	count                   int
}

func (t *tokenizer) size() int { return t.count }

type normalizerSpec struct {
	Type         string           `json:"type"`
	Lowercase    *bool            `json:"lowercase"`
	StripAccents *bool            `json:"strip_accents"`
	Normalizers  []normalizerSpec `json:"normalizers"`
}

type preTokenizerSpec struct {
	Type          string             `json:"type"`
	Replacement   string             `json:"replacement"`
	PreTokenizers []preTokenizerSpec `json:"pretokenizers"`
}

func parseTokenizer(raw []byte) (*tokenizer, error) {
	var spec struct {
		Normalizer   *normalizerSpec   `json:"normalizer"`
		PreTokenizer *preTokenizerSpec `json:"pre_tokenizer"`
		Model        struct {
			Type                 string          `json:"type"`
			UnkToken             string          `json:"unk_token"`
			UnkID                *int            `json:"unk_id"`
			Prefix               *string         `json:"continuing_subword_prefix"`
			MaxInputCharsPerWord int             `json:"max_input_chars_per_word"`
			Vocab                json.RawMessage `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("document embedding: tokenizer: %w", err)
	}
	t := &tokenizer{kind: spec.Model.Type, unk: -1, replacement: "▁"}
	var walkNorm func(n normalizerSpec)
	walkNorm = func(n normalizerSpec) {
		switch n.Type {
		case "BertNormalizer":
			t.lowercase = n.Lowercase == nil || *n.Lowercase
			t.stripAccents = t.lowercase
			if n.StripAccents != nil {
				t.stripAccents = *n.StripAccents
			}
		case "Lowercase":
			t.lowercase = true
		case "StripAccents":
			t.stripAccents = true
		case "Sequence":
			for _, c := range n.Normalizers {
				walkNorm(c)
			}
		}
	}
	if spec.Normalizer != nil {
		walkNorm(*spec.Normalizer)
	}
	var walkPre func(p preTokenizerSpec)
	walkPre = func(p preTokenizerSpec) {
		switch p.Type {
		case "Metaspace":
			t.metaspace = true
			if p.Replacement != "" {
				t.replacement = p.Replacement
			}
		case "Sequence":
			for _, c := range p.PreTokenizers {
				walkPre(c)
			}
		}
	}
	if spec.PreTokenizer != nil {
		walkPre(*spec.PreTokenizer)
	}
	switch spec.Model.Type {
	case "WordPiece":
		if err := json.Unmarshal(spec.Model.Vocab, &t.vocab); err != nil {
			return nil, fmt.Errorf("document embedding: WordPiece vocab: %w", err)
		}
		t.prefix = "##"
		if spec.Model.Prefix != nil {
			t.prefix = *spec.Model.Prefix
		}
		t.maxWordChars = spec.Model.MaxInputCharsPerWord
		if t.maxWordChars <= 0 {
			t.maxWordChars = 100
		}
		if id, ok := t.vocab[spec.Model.UnkToken]; ok {
			t.unk = id
		}
	case "Unigram":
		var pieces [][2]json.RawMessage
		if err := json.Unmarshal(spec.Model.Vocab, &pieces); err != nil {
			return nil, fmt.Errorf("document embedding: Unigram vocab: %w", err)
		}
		t.vocab = make(map[string]int, len(pieces))
		t.scores = make(map[string]float64, len(pieces))
		for i, p := range pieces {
			var piece string
			var score float64
			if json.Unmarshal(p[0], &piece) != nil || json.Unmarshal(p[1], &score) != nil {
				return nil, errors.New("document embedding: Unigram vocab entries must be [piece, score]")
			}
			t.vocab[piece], t.scores[piece] = i, score
			t.maxPiece = max(t.maxPiece, utf8.RuneCountInString(piece))
		}
		if spec.Model.UnkID != nil {
			t.unk = *spec.Model.UnkID
		}
		if !t.metaspace {
			t.metaspace = true
		}
	default:
		return nil, fmt.Errorf("document embedding: tokenizer model %q is not supported", spec.Model.Type)
	}
	for _, id := range t.vocab {
		t.count = max(t.count, id+1)
	}
	if t.count == 0 {
		return nil, errors.New("document embedding: tokenizer vocabulary is empty")
	}
	return t, nil
}

// accentFolds maps common precomposed Latin letters to their base letter,
// the effect of Bert's NFD-and-drop-marks accent stripping on them.
var accentFolds = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a', 'ā': 'a', 'ç': 'c', 'č': 'c', 'ć': 'c',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e', 'ě': 'e', 'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i',
	'ñ': 'n', 'ń': 'n', 'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o', 'ō': 'o', 'ř': 'r', 'š': 's', 'ś': 's',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u', 'ū': 'u', 'ý': 'y', 'ÿ': 'y', 'ž': 'z', 'ź': 'z', 'ż': 'z',
	'À': 'A', 'Á': 'A', 'Â': 'A', 'Ä': 'A', 'Ç': 'C', 'È': 'E', 'É': 'E', 'Ê': 'E', 'Í': 'I', 'Ñ': 'N', 'Ó': 'O', 'Ö': 'O', 'Ú': 'U', 'Ü': 'U',
}

func (t *tokenizer) normalize(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == 0 || r == utf8.RuneError || (unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r') {
			continue
		}
		if t.lowercase {
			r = unicode.ToLower(r)
		}
		if t.stripAccents {
			if unicode.Is(unicode.Mn, r) {
				continue
			}
			if base, ok := accentFolds[r]; ok {
				r = base
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// bertWords splits normalized text the Bert way: on whitespace, with every
// punctuation character its own word.
func bertWords(text string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flush()
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			flush()
			out = append(out, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func (t *tokenizer) encode(text string) []int {
	text = t.normalize(text)
	var ids []int
	if t.kind == "WordPiece" {
		for _, word := range bertWords(text) {
			ids = append(ids, t.wordPiece(word)...)
		}
		return ids
	}
	for _, word := range strings.Fields(text) {
		ids = append(ids, t.unigram(t.replacement+word)...)
	}
	return ids
}

// wordPiece is greedy longest-match-first; a word that cannot be fully
// segmented is one unknown token, which is dropped.
func (t *tokenizer) wordPiece(word string) []int {
	runes := []rune(word)
	if len(runes) > t.maxWordChars {
		return nil
	}
	var ids []int
	for start := 0; start < len(runes); {
		end := len(runes)
		found := -1
		for end > start {
			piece := string(runes[start:end])
			if start > 0 {
				piece = t.prefix + piece
			}
			if id, ok := t.vocab[piece]; ok {
				found = id
				break
			}
			end--
		}
		if found < 0 {
			return nil
		}
		if found != t.unk {
			ids = append(ids, found)
		}
		start = end
	}
	return ids
}

// unigram picks the highest-scoring segmentation with Viterbi; characters
// no piece covers become unknown and are dropped.
func (t *tokenizer) unigram(word string) []int {
	runes := []rune(word)
	n := len(runes)
	const unkPenalty = -100.0
	best := make([]float64, n+1)
	prev := make([]int, n+1)
	piece := make([]int, n+1)
	for i := 1; i <= n; i++ {
		best[i] = math.Inf(-1)
	}
	for end := 1; end <= n; end++ {
		for start := max(0, end-t.maxPiece); start < end; start++ {
			if math.IsInf(best[start], -1) {
				continue
			}
			if id, ok := t.vocab[string(runes[start:end])]; ok {
				if s := best[start] + t.scores[string(runes[start:end])]; s > best[end] {
					best[end], prev[end], piece[end] = s, start, id
				}
			}
		}
		if math.IsInf(best[end], -1) {
			best[end], prev[end], piece[end] = best[end-1]+unkPenalty, end-1, -1
		}
	}
	var ids []int
	for i := n; i > 0; i = prev[i] {
		if piece[i] >= 0 && piece[i] != t.unk {
			ids = append(ids, piece[i])
		}
	}
	for l, r := 0, len(ids)-1; l < r; l, r = l+1, r-1 {
		ids[l], ids[r] = ids[r], ids[l]
	}
	return ids
}
