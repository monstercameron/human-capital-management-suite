package chatfilter

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Facts about one folded character, decided from the ORIGINAL character so a
// symbol that is only read as a letter for matching never becomes part of a word.
const (
	flagWord uint8 = 1 << iota // letter, digit or underscore in the original text
	flagBang                   // an exclamation mark read as the letter i
	flagSep                    // space . _ - : may sit between the letters of a term
	flagMark                   // censor mark (* # _ % ^ ~ bullet): may stand for a letter
)

// folded is a message (or a listed term) reduced to the form the matchers
// compare: case folded, compatibility and accent folded, look-alike letters
// mapped to Latin, letter substitutions (0 1 3 4 5 7 @ $ !) read as letters.
// starts and ends are UTF-8 byte offsets of each folded character in the
// original text; ends covers the zero width and combining characters that
// follow, so a span always reaches the end of the visible word.
type folded struct {
	text         []rune
	flags        []uint8
	starts, ends []int
	cand         []int // indexes where a word may start: not a separator, not after a word character
}

func (f *folded) extend(end int) {
	if n := len(f.ends); n > 0 {
		f.ends[n-1] = end
	}
}

func (f *folded) push(r rune, word bool, start, end int) {
	var fl uint8
	if word {
		fl = flagWord
	}
	switch r {
	case '0':
		r = 'o'
	case '1':
		r = 'i'
	case '3':
		r = 'e'
	case '4':
		r = 'a'
	case '5':
		r = 's'
	case '7':
		r = 't'
	case '@':
		r = 'a'
	case '$':
		r = 's'
	case '!':
		r, fl = 'i', fl|flagBang
	case '.', '-':
		fl |= flagSep
	case '_':
		fl |= flagSep | flagMark
	case '*', '#', '%', '^', '~', 0x2022:
		fl |= flagMark
	default:
		if unicode.IsSpace(r) {
			r, fl = ' ', fl|flagSep
		}
	}
	f.text = append(f.text, r)
	f.flags = append(f.flags, fl)
	f.starts = append(f.starts, start)
	f.ends = append(f.ends, end)
}

// apostrophe is transparent inside a word (I'll, don't, d'amn) and ordinary
// punctuation anywhere else.
func (f *folded) apostrophe(s string, start, end int) {
	var next rune
	for rest := s[end:]; rest != ""; {
		r, width := utf8.DecodeRuneInString(rest)
		if rest = rest[width:]; !ignorable(r) {
			next = r
			break
		}
	}
	if n := len(f.flags); n > 0 && f.flags[n-1]&flagWord != 0 && (unicode.IsLetter(next) || unicode.IsDigit(next) || strings.ContainsRune("*#_%^~", next)) {
		f.extend(end)
		return
	}
	f.push('\'', false, start, end)
}

func isApostrophe(r rune) bool {
	return r == '\'' || r == '\u2019' || r == '\u2018' || r == '\u02bc' || r == '\uff07'
}

// ignorable characters are invisible or only decorate the character before
// them: zero width and format characters, bidi marks, soft hyphen, combining
// marks, control characters, Arabic tatweel and the invisible Hangul fillers.
func ignorable(r rune) bool {
	switch r {
	case 0x0640, 0x115f, 0x1160, 0x3164, 0xffa0, 0x2800:
		return true
	}
	return unicode.Is(unicode.Cf, r) || unicode.Is(unicode.M, r) || unicode.IsControl(r)
}

// confusable maps the common look-alike letters of other scripts to Latin and
// folds the Arabic alef maqsura to yeh.
func confusable(r rune) rune {
	switch r {
	case '\u0430': // Cyrillic a
		return 'a'
	case '\u0435': // Cyrillic ie
		return 'e'
	case '\u043e': // Cyrillic o
		return 'o'
	case '\u0440': // Cyrillic er
		return 'p'
	case '\u0441': // Cyrillic es
		return 'c'
	case '\u0445': // Cyrillic ha
		return 'x'
	case '\u0443': // Cyrillic u
		return 'y'
	case '\u0456': // Cyrillic byelorussian-ukrainian i
		return 'i'
	case '\u0455': // Cyrillic dze
		return 's'
	case '\u0458': // Cyrillic je
		return 'j'
	case '\u03bf': // Greek omicron
		return 'o'
	case '\u03b1': // Greek alpha
		return 'a'
	case '\u03b5': // Greek epsilon
		return 'e'
	case '\u03b9': // Greek iota
		return 'i'
	case '\u03ba': // Greek kappa
		return 'k'
	case '\u03bd': // Greek nu
		return 'v'
	case '\u03c1': // Greek rho
		return 'p'
	case '\u03c4': // Greek tau
		return 't'
	case '\u03c5': // Greek upsilon
		return 'u'
	case 'ı': // dotless i
		return 'i'
	case 'ø': // o with stroke
		return 'o'
	case 'đ': // d with stroke
		return 'd'
	case 'ł': // l with stroke
		return 'l'
	case 'ħ': // h with stroke
		return 'h'
	case 'ى': // Arabic alef maqsura
		return 'ي'
	}
	return r
}

func fold(s string) folded {
	f := folded{text: make([]rune, 0, len(s)), flags: make([]uint8, 0, len(s)), starts: make([]int, 0, len(s)), ends: make([]int, 0, len(s))}
	var caser cases.Caser
	haveCaser := false
	for pos := 0; pos < len(s); {
		original, width := utf8.DecodeRuneInString(s[pos:])
		start, end := pos, pos+width
		pos = end
		switch {
		case original < utf8.RuneSelf:
			switch {
			case unicode.IsSpace(original):
				f.push(' ', false, start, end)
			case unicode.IsControl(original):
				f.extend(end)
			case original >= 'A' && original <= 'Z':
				f.push(original+'a'-'A', true, start, end)
			case original == '\'':
				f.apostrophe(s, start, end)
			default:
				f.push(original, original == '_' || original >= 'a' && original <= 'z' || original >= '0' && original <= '9', start, end)
			}
		// Fast paths for the characters of everyday Arabic and German text,
		// which have no compatibility decomposition to apply: the result is the
		// same as the general path in pushComplex.
		case original >= 0x0621 && original <= 0x064a && original != 0x0640 && (original < 0x0622 || original > 0x0626):
			f.push(confusable(original), true, start, end)
		case original == 0xe4 || original == 0xc4:
			f.push('a', true, start, end)
		case original == 0xf6 || original == 0xd6:
			f.push('o', true, start, end)
		case original == 0xfc || original == 0xdc:
			f.push('u', true, start, end)
		case original == 0xdf:
			f.push('s', true, start, end)
			f.push('s', true, start, end)
		case unicode.IsSpace(original):
			f.push(' ', false, start, end)
		case ignorable(original):
			f.extend(end)
		case isApostrophe(original):
			f.apostrophe(s, start, end)
		default:
			f.pushComplex(original, start, end, &caser, &haveCaser)
		}
	}
	for i, fl := range f.flags {
		if fl&flagSep == 0 && (i == 0 || f.flags[i-1]&flagWord == 0) {
			f.cand = append(f.cand, i)
		}
	}
	return f
}

func (f *folded) pushComplex(original rune, start, end int, caser *cases.Caser, have *bool) {
	var buf [8]rune
	out := buf[:0]
	for _, r := range norm.NFKD.String(string(original)) {
		if unicode.Is(unicode.M, r) {
			continue
		}
		if unicode.Is(unicode.Arabic, r) {
			out = append(out, confusable(r))
			continue
		}
		if !*have {
			*caser, *have = cases.Fold(), true
		}
		for _, c := range caser.String(string(r)) {
			if !unicode.Is(unicode.M, c) {
				out = append(out, confusable(c))
			}
		}
	}
	if len(out) == 0 {
		f.extend(end)
		return
	}
	word := unicode.IsLetter(original) || unicode.IsNumber(original)
	for _, r := range out {
		word = word || unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	for _, r := range out {
		f.push(r, word, start, end)
	}
}

// wordTerm is one listed word or phrase, folded. breaks[k] reports that the
// term itself has a space or separator before its k-th letter.
type wordTerm struct {
	letters []rune
	breaks  []bool
}

func newWordTerm(term string) (wordTerm, bool) {
	f := fold(term)
	var t wordTerm
	pending := false
	for i, r := range f.text {
		if f.flags[i]&flagSep != 0 {
			pending = len(t.letters) > 0
			continue
		}
		t.breaks = append(t.breaks, pending)
		t.letters = append(t.letters, r)
		pending = false
	}
	return t, len(t.letters) > 0
}

func (t wordTerm) key() string {
	b := make([]rune, 0, len(t.letters)*2)
	for i, r := range t.letters {
		if t.breaks[i] {
			b = append(b, 0)
		}
		b = append(b, r)
	}
	return string(b)
}

// at reports where the term ends when it starts at folded index i. Letters
// may be separated by spaces and the separators . _ - ; the match must end on
// a word boundary of the original text. Spacing a term out is accepted when
// every letter is separated ("d a m n"), when the separators sit where the
// term itself has its spaces, or when the term has five or more letters; a
// short term split in one place ("s hit") is ordinary words, not a trick.
func (t wordTerm) at(f *folded, i int) (int, bool) {
	n := len(f.text)
	last := len(t.letters) - 1
	j, gaps, extra := i, 0, 0
	for k, l := range t.letters {
		if k > 0 {
			from := j
			for j < n && f.flags[j]&flagSep != 0 {
				j++
			}
			if j > from {
				gaps++
				if !t.breaks[k] {
					extra++
				}
			}
		}
		if j >= n || f.text[j] != l {
			return 0, false
		}
		if f.flags[j]&flagBang != 0 && (k == 0 || k == last) {
			return 0, false
		}
		j++
		// Each letter may be stretched ("fuuuck"), but a letter the term
		// itself doubles still needs both of its own letters ("ass" is not
		// "as", "hell" is not "hello"): only the last letter of a run stretches.
		// A term's word break lets the letter before it stretch too ("shuuut
		// the"), keeping one letter back when the words are written together.
		if k == last || t.letters[k+1] != l || t.breaks[k+1] {
			from := j
			for j < n && f.text[j] == l && f.flags[j]&flagBang == 0 {
				j++
			}
			if k < last && t.letters[k+1] == l && j > from && (j >= n || f.flags[j]&flagSep == 0) {
				j--
			}
		}
	}
	if j < n && f.flags[j]&flagWord != 0 {
		return 0, false
	}
	if extra > 0 && len(t.letters) < 5 && !(last >= 2 && gaps == last) {
		return 0, false
	}
	return j, true
}

// atMarks matches a term of four or more letters in which censor marks (* # _
// % ^ ~ and the bullet) stand in for letters: one mark for one letter, or a
// run of marks for as many letters as the run is long ("f*ck", "b**ch",
// "d_mn"). At least half the letters must be visible, and the first and last
// character must be real letters, so "*bold*", "5*3" and "a*b" are not hits.
func (t wordTerm) atMarks(f *folded, i int) (int, bool) {
	n, total := len(f.text), len(t.letters)
	if total < 4 {
		return 0, false
	}
	j, k, hidden := i, 0, 0
	for k < total {
		if k > 0 && t.breaks[k] { // the term's own spaces
			for j < n && f.flags[j]&flagSep != 0 && f.flags[j]&flagMark == 0 {
				j++
			}
		}
		if j >= n {
			return 0, false
		}
		if f.flags[j]&flagMark != 0 {
			if k == 0 {
				return 0, false
			}
			for j < n && f.flags[j]&flagMark != 0 {
				j++
				k++
				hidden++
			}
			if k >= total {
				return 0, false
			}
			continue
		}
		if f.text[j] != t.letters[k] || f.flags[j]&flagBang != 0 && (k == 0 || k == total-1) {
			return 0, false
		}
		j++
		k++
	}
	if hidden == 0 || hidden*2 > total || j < n && f.flags[j]&flagWord != 0 {
		return 0, false
	}
	return j, true
}

type wordSet struct {
	terms    []wordTerm
	first    map[rune][]int
	innocent map[string]bool
}

func (w *wordSet) spans(f *folded) []Span {
	var out []Span
	for _, i := range f.cand {
		for _, ti := range w.first[f.text[i]] {
			end, ok := w.terms[ti].at(f, i)
			if !ok {
				end, ok = w.terms[ti].atMarks(f, i)
			}
			if !ok || w.innocent != nil && w.innocent[string(f.text[i:end])] {
				continue
			}
			out = append(out, Span{f.starts[i], f.ends[end-1]})
		}
	}
	return out
}

func compileWords(d Definition) (Matcher, error) {
	set := &wordSet{first: map[rune][]int{}}
	seen := map[string]bool{}
	for _, term := range d.Match {
		t, ok := newWordTerm(term)
		if !ok {
			return nil, ErrInvalid
		}
		if key := t.key(); !seen[key] {
			seen[key] = true
			set.first[t.letters[0]] = append(set.first[t.letters[0]], len(set.terms))
			set.terms = append(set.terms, t)
		}
	}
	// The product's known false positives are a guard on the product lists only;
	// an administrator's own list says exactly what it means.
	if d.Product {
		set.innocent = map[string]bool{}
		for _, item := range knownFalsePositives(language(d.Language)) {
			set.innocent[string(fold(item).text)] = true
		}
	}
	return func(in Input) []Span {
		f := in.foldedText
		if f == nil {
			v := fold(in.Body)
			f = &v
		}
		return set.spans(f)
	}, nil
}

// falsePositive reports whether the folded text is a known innocent word of the
// locale's language.
func falsePositive(locale, term string) bool {
	for _, item := range knownFalsePositives(language(locale)) {
		if string(fold(item).text) == term {
			return true
		}
	}
	return false
}
