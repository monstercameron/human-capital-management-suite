package chatfilter

import (
	"net/url"
	"regexp"
	"strings"
)

func compileDetector(d Definition) (Matcher, error) {
	name := d.Match[0]
	var p *regexp.Regexp
	switch name {
	case "card":
		p = regexp.MustCompile(`\b(?:[0-9][ -]?){12,18}[0-9]\b`)
	case "national-id":
		p = regexp.MustCompile(`\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b|\b[0-9]{11}\b`)
	case "access-key":
		p = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	case "secret":
		p = regexp.MustCompile(`(?i)\b(?:password|secret|token|api[_-]?key)\s*[:=]\s*["']?[^\s"']{8,256}`)
	case "external-link":
		p = regexp.MustCompile(`https?://[^\s<>]+`)
	default:
		return nil, ErrInvalid
	}
	return func(in Input) []Span {
		var out []Span
		for _, m := range p.FindAllStringIndex(in.Body, -1) {
			text := in.Body[m[0]:m[1]]
			if name == "card" && !checksum(text) {
				continue
			}
			if name == "external-link" {
				u, err := url.Parse(text)
				if err == nil && contains(d.Match[1:], strings.ToLower(u.Hostname())) {
					continue
				}
			}
			out = append(out, Span{m[0], m[1]})
		}
		return out
	}, nil
}
func checksum(text string) bool {
	var digits []int
	for _, r := range text {
		if r >= '0' && r <= '9' {
			digits = append(digits, int(r-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	nonzero := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := digits[i]
		nonzero = nonzero || n != 0
		if (len(digits)-1-i)%2 == 1 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	return nonzero && sum%10 == 0
}
