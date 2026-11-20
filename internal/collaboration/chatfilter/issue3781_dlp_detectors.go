package chatfilter

import (
	"bytes"
	"regexp"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// Issue 3781: the sensitive-data detector kind also runs the product's central
// data-classification inspector (internal/trust/dlp). A detector name that
// starts with "dlp-" selects one class; a hit yields the same warn/mask/block
// outcome as every other detector, with spans only. The inspector's findings
// carry locations and classes, never the matched text.

// DLPDetectorNames is the order of the classification-backed choices.
var DLPDetectorNames = []string{"dlp-compensation", "dlp-medical", "dlp-bank", "dlp-immigration", "dlp-pii"}

var dlpClassByDetector = map[string]dlp.DataClass{
	"dlp-compensation": dlp.ClassCompensation,
	"dlp-medical":      dlp.ClassMedical,
	"dlp-bank":         dlp.ClassBank,
	"dlp-immigration":  dlp.ClassImmigration,
	"dlp-pii":          dlp.ClassPII,
}

// DLPDetectorClass reports the data class a "dlp-" detector name stands for.
func DLPDetectorClass(name string) (string, bool) {
	c, ok := dlpClassByDetector[name]
	return string(c), ok
}

const (
	money          = `(?:[$€£]\s?[0-9][0-9.,]*\s?[kKmM]?|[0-9][0-9.,]*\s?(?:[kKmM]\b|usd|eur|gbp|dollars|euros|pounds))`
	compensationRE = `(?i)\b(?:salary|salaries|base pay|pay ?rate|bonus|raise|gehalt|lohn)\b[^.\n]{0,40}?` + money +
		`|` + money + `\s?(?:/|per\s)\s?(?:year|yr|annum|month|hour|jahr|monat)\b`
	medicalRE = `(?i)\b(?:diagnos(?:ed|is)|prescription|prescribed|chemotherapy|sick note|medical (?:note|record|certificate)|hiv|diagnose|krankschreibung|arztbrief)\b`
	passRE    = `(?i)\b(?:passport|visa|work permit|residence permit|green card|reisepass|aufenthaltstitel)\b[^0-9\n]{0,24}[A-Z]{0,2}[0-9][A-Z0-9]{5,11}\b`
	piiRE     = `\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b|(?i)\b(?:date of birth|dob|geburtsdatum)\b\s*[:.]?\s*[0-9]{1,4}[-/.][0-9]{1,2}[-/.][0-9]{1,4}\b`
	ibanRE    = `\b[A-Z]{2}[0-9]{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?\b`
)

var (
	dlpOnce      sync.Once
	dlpInspector map[string]*dlp.Inspector
)

func dlpInspectors() map[string]*dlp.Inspector {
	dlpOnce.Do(func() {
		dlpInspector = map[string]*dlp.Inspector{}
		specs := []struct {
			name, pattern string
			sev           dlp.Severity
		}{
			{"dlp-compensation", compensationRE, dlp.SeverityHigh},
			{"dlp-medical", medicalRE, dlp.SeverityHigh},
			{"dlp-immigration", passRE, dlp.SeverityHigh},
			{"dlp-pii", piiRE, dlp.SeverityHigh},
		}
		for _, s := range specs {
			d, err := dlp.NewDetector(s.name, dlpClassByDetector[s.name], s.sev, s.pattern)
			if err == nil {
				dlpInspector[s.name], _ = dlp.NewInspector(d)
			}
		}
		bank := dlp.Detector{ID: "dlp-bank", Class: dlp.ClassBank, Severity: dlp.SeverityHigh, Matcher: ibanLocations}
		dlpInspector["dlp-bank"], _ = dlp.NewInspector(bank)
	})
	return dlpInspector
}

// dlpSpans returns the byte spans of one class in body.
func dlpSpans(name, body string) []Span {
	in := dlpInspectors()[name]
	if in == nil {
		return nil
	}
	res, err := in.Inspect([]byte(body))
	if err != nil {
		return nil
	}
	out := make([]Span, 0, len(res.Findings))
	for _, f := range res.Findings {
		out = append(out, Span{f.Location.Start, f.Location.End})
	}
	return out
}

// ibanLocations finds IBAN-shaped text whose mod-97 check passes, so ordinary
// codes are not caught.
func ibanLocations(payload []byte) []dlp.Location {
	var out []dlp.Location
	for _, m := range ibanPattern.FindAllIndex(payload, -1) {
		// The pattern is greedy and may swallow an upper-case word after the
		// account number; give back trailing groups until the check passes.
		for end := m[1]; end > m[0]; {
			if ibanValid(string(payload[m[0]:end])) {
				out = append(out, dlp.Location{Start: m[0], End: end})
				break
			}
			cut := bytes.LastIndexByte(payload[m[0]:end], ' ')
			if cut < 0 {
				break
			}
			end = m[0] + cut
		}
	}
	return out
}

func ibanValid(s string) bool {
	s = strings.ReplaceAll(strings.ToUpper(s), " ", "")
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	s = s[4:] + s[:4]
	rem := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			rem = (rem*10 + int(r-'0')) % 97
		case r >= 'A' && r <= 'Z':
			rem = (rem*100 + int(r-'A') + 10) % 97
		default:
			return false
		}
	}
	return rem == 1
}

var ibanPattern = regexp.MustCompile(ibanRE)
