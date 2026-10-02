// Package chatfilter evaluates versioned workplace content rules without I/O.
package chatfilter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid     = errors.New("chatfilter: invalid definition")
	ErrDenied      = errors.New("chatfilter: permission denied")
	ErrBlocked     = errors.New("chatfilter: blocked")
	ErrUnavailable = errors.New("chatfilter: unavailable")
	ErrConflict    = errors.New("chatfilter: version conflict")
)

const APIVersion = "1.0.0"

type Definition struct {
	ID, Name, Version, Language, Kind, Action string
	Match                                     []string
	Channels, ExemptRoles, ExemptAgents       []string
	Target                                    string
	Hard, Product                             bool
	// Authority says whose filter this is: AuthorityWorkspace when the person
	// who saved the version held the permission for the whole workspace,
	// AuthorityChannel when they held it for the one channel only. The service
	// writes it; what a caller sends is ignored (chatmod003_authority.go).
	Authority string `json:",omitempty"`
}

type Input struct {
	Tenant, Channel, Subject, Language, Body string
	Roles, AttachmentTypes                   []string
	Agent, Direct                            bool
	foldedText                               *folded
}

// Span uses UTF-8 byte offsets in the original draft, not normalized text.
type Span struct{ Start, End int }
type Hit struct {
	RuleID, RuleName, Version, Action, Digest, Masked, Target string
	Span                                                      Span
	DryRun                                                    bool
}
type Result struct {
	Action, Masked string
	Hits           []Hit
	blocked        *BlockedError
}

// BlockedError says a message was refused. Span is the first span that must
// change and Spans is every span of every blocking hit (sorted, without
// duplicates, at most 16); Span is always Spans[0] when Spans is not empty.
type BlockedError struct {
	RuleName string
	Span     Span
	Spans    []Span
}

func (e *BlockedError) Error() string { return "chatfilter: blocked by " + e.RuleName }
func (e *BlockedError) Unwrap() error { return ErrBlocked }

type Kind struct {
	Schema  string
	Compile func(Definition) (Matcher, error)
}
type Matcher func(Input) []Span
type Action struct {
	Schema                               string
	Rank                                 int
	Block, Mask, Deliver, RequiresTarget bool
}
type Registry struct {
	kinds   map[string]Kind
	actions map[string]Action
	// EvaluationBudget is how long one message may take to judge, and Clock the
	// clock it is measured on (chatmod003_authority.go). Zero is the default
	// budget and the wall clock.
	EvaluationBudget time.Duration
	Clock            func() time.Time
}

func NewRegistry() *Registry {
	r := &Registry{kinds: map[string]Kind{}, actions: map[string]Action{}}
	for name, compile := range map[string]func(Definition) (Matcher, error){"words": compileWords, "pattern": compilePattern, "detector": compileDetector, "attachment": compileAttachment} {
		_ = r.RegisterKind(name, Kind{Schema: "match: array of strings", Compile: compile})
	}
	for name, rank := range map[string]int{"notify": 1, "flag": 2, "reword": 2, "mask": 3, "block": 4} {
		_ = r.RegisterAction(name, Action{Schema: "target: optional destination", Rank: rank, Block: name == "block", Mask: name == "mask", Deliver: name == "flag" || name == "notify", RequiresTarget: name == "notify"})
	}
	return r
}
func (r *Registry) RegisterKind(name string, k Kind) error {
	if name == "" || k.Schema == "" || k.Compile == nil {
		return ErrInvalid
	}
	if _, ok := r.kinds[name]; ok {
		return ErrConflict
	}
	r.kinds[name] = k
	return nil
}
func (r *Registry) RegisterAction(name string, a Action) error {
	if name == "" || a.Schema == "" || a.Rank < 1 {
		return ErrInvalid
	}
	if _, ok := r.actions[name]; ok {
		return ErrConflict
	}
	r.actions[name] = a
	return nil
}

type compiledRule struct {
	definition Definition
	matcher    Matcher
	rank       int
	action     Action
}
type Evaluator struct {
	rules  []compiledRule
	budget time.Duration
	clock  func() time.Time
}

var semanticVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func ValidVersion(version string) bool {
	if !semanticVersion.MatchString(version) {
		return false
	}
	for _, part := range strings.Split(version, ".") {
		if len(part) > 7 {
			return false
		}
	}
	return true
}

// maxActiveRules is how many rules one compiled set may hold.
const maxActiveRules = 128

// compilable reports whether a set of rules passes the checks Compile makes on
// the set as a whole, without compiling a matcher: its size, and every rule's
// action being one the registry knows with the target it needs. Each rule's
// own terms were compiled when its version was saved.
func (r *Registry) compilable(defs []Definition) bool {
	if len(defs) > maxActiveRules {
		return false
	}
	for _, d := range defs {
		if a, ok := r.actions[d.Action]; !ok || a.RequiresTarget && d.Target == "" {
			return false
		}
	}
	return true
}

func (r *Registry) Compile(defs []Definition) (*Evaluator, error) {
	if len(defs) > maxActiveRules {
		return nil, ErrInvalid
	}
	e := &Evaluator{budget: r.EvaluationBudget, clock: r.Clock}
	seen := map[string]bool{}
	for _, d := range defs {
		k, ok := r.kinds[d.Kind]
		a, actionOK := r.actions[d.Action]
		if !ok || !actionOK || d.ID == "" || d.Name == "" || len(d.Name) > 100 || !ValidVersion(d.Version) || len(d.Match) == 0 || len(d.Match) > 128 || seen[d.ID] {
			return nil, ErrInvalid
		}
		if a.RequiresTarget && d.Target == "" {
			return nil, ErrInvalid
		}
		for _, term := range d.Match {
			if len(term) == 0 || len(term) > 512 {
				return nil, ErrInvalid
			}
		}
		m, err := k.Compile(d)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalid, d.ID)
		}
		seen[d.ID] = true
		e.rules = append(e.rules, compiledRule{definition: d, matcher: m, rank: a.Rank, action: a})
	}
	sort.Slice(e.rules, func(i, j int) bool { return e.rules[i].definition.ID < e.rules[j].definition.ID })
	return e, nil
}
func (e *Evaluator) Evaluate(in Input) (Result, error) {
	all, _, err := e.evaluate(in, nil)
	return all, err
}

// outcome accumulates the strictest action (block > mask > flag > notify), the
// spans a mask action covers and the spans every blocking hit covers.
type outcome struct {
	rank         int
	action, rule string
	winnerBlocks bool
	masks, block []Span
}

func (o *outcome) add(rule compiledRule, span Span) {
	if rule.rank > o.rank {
		o.rank, o.action, o.rule, o.winnerBlocks = rule.rank, rule.definition.Action, rule.definition.Name, rule.action.Block
	}
	if rule.action.Mask {
		o.masks = append(o.masks, span)
	}
	if rule.action.Block {
		o.block = append(o.block, span)
	}
}

func (o *outcome) result(body string) Result {
	out := Result{Action: o.action, Masked: Mask(body, o.masks, "[removed word]")}
	if o.winnerBlocks {
		if spans := normalizeSpans(o.block, maxBlockedSpans); len(spans) > 0 {
			out.blocked = &BlockedError{RuleName: o.rule, Span: spans[0], Spans: spans}
		}
	}
	return out
}

// evaluate judges the input against every rule once. all is the outcome with
// every rule enforced; enforced is the outcome when the rules dry names are
// only recorded, so a dry-run rule never acts. With no dry set they are equal.
func (e *Evaluator) evaluate(in Input, dry func(ruleID string) bool) (all, enforced Result, err error) {
	if len(in.Body) > 32768 || !utf8.ValidString(in.Body) {
		return Result{}, Result{}, ErrInvalid
	}
	var every, acting outcome
	var hits []Hit
	deadline := e.deadline()
	for _, rule := range e.rules {
		// The budget is checked before each rule: a matcher cannot be stopped
		// part-way, but none of them is unbounded, so the overrun is at most one
		// rule's time.
		if e.expired(deadline) {
			return Result{}, Result{}, ErrDeadline
		}
		d := rule.definition
		if in.Direct && !d.Hard || len(d.Channels) > 0 && !contains(d.Channels, in.Channel) || d.Language != "" && in.Language != "" && language(d.Language) != language(in.Language) {
			continue
		}
		exempt := in.Agent && contains(d.ExemptAgents, in.Subject)
		for _, role := range in.Roles {
			exempt = exempt || contains(d.ExemptRoles, role)
		}
		if exempt {
			continue
		}
		if d.Kind == "words" && in.foldedText == nil {
			f := fold(in.Body)
			in.foldedText = &f
		}
		spans := rule.matcher(in)
		for _, span := range spans {
			if span.Start < 0 || span.End < span.Start || span.End > len(in.Body) {
				return Result{}, Result{}, ErrInvalid
			}
		}
		for _, span := range normalizeSpans(spans, 0) {
			sum := sha256.Sum256([]byte(in.Body[span.Start:span.End]))
			hits = append(hits, Hit{RuleID: d.ID, RuleName: d.Name, Version: d.Version, Action: d.Action, Digest: "sha256:" + hex.EncodeToString(sum[:]), Masked: "[removed word]", Span: span, Target: d.Target})
			every.add(rule, span)
			if dry == nil || !dry(d.ID) {
				acting.add(rule, span)
			}
		}
	}
	all = every.result(in.Body)
	all.Hits = hits
	if dry == nil {
		return all, all, nil
	}
	enforced = acting.result(in.Body)
	enforced.Hits = hits
	return all, enforced, nil
}

// maxBlockedSpans bounds the spans a refusal carries.
const maxBlockedSpans = 16

// normalizeSpans returns the spans sorted by position without duplicates, at
// most limit of them (no limit when limit is 0).
func normalizeSpans(spans []Span, limit int) []Span {
	out := append([]Span(nil), spans...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].End < out[j].End
	})
	n := 0
	for _, s := range out {
		if n > 0 && out[n-1] == s {
			continue
		}
		out[n] = s
		n++
	}
	out = out[:n]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
func Mask(body string, spans []Span, label string) string {
	spans = append([]Span(nil), spans...)
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	var b strings.Builder
	end := 0
	for _, s := range spans {
		if s.Start < 0 || s.End > len(body) || s.End <= end || s.Start >= s.End {
			continue
		}
		if s.Start < end {
			end = s.End
			continue
		}
		b.WriteString(body[end:s.Start])
		b.WriteString(label)
		end = s.End
	}
	b.WriteString(body[end:])
	return b.String()
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func language(s string) string { return strings.ToLower(strings.Split(s, "-")[0]) }

func compileAttachment(d Definition) (Matcher, error) {
	return func(in Input) []Span {
		for _, typ := range in.AttachmentTypes {
			if contains(d.Match, typ) {
				return []Span{{0, 0}}
			}
		}
		return nil
	}, nil
}
