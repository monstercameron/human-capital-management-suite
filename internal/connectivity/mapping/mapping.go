package mapping

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrInvalidIR     = errors.New("mapping: invalid IR")
	ErrMissingSource = errors.New("mapping: source field is missing")
	ErrUnsupportedOp = errors.New("mapping: unsupported operation")
	ErrTransform     = errors.New("mapping: transform failed")
)

type Op string

const (
	OpIdentity Op = "IDENTITY"
	OpTrim     Op = "TRIM"
	OpUpper    Op = "UPPER"
	OpLower    Op = "LOWER"
	OpConstant Op = "CONSTANT"
	OpLookup   Op = "LOOKUP"
	OpDate     Op = "DATE"
	OpMoney    Op = "MONEY"
	OpCompose  Op = "COMPOSE"
)

type NullPolicy string

const (
	NullError  NullPolicy = "ERROR"
	NullOmit   NullPolicy = "OMIT"
	NullDelete NullPolicy = "DELETE"
)

type Rule struct {
	Source    string
	Target    string
	Op        Op
	Argument  string
	MoneyMode string
	Lookup    map[string]string
	Null      NullPolicy
}

type IR struct {
	Version string
	Rules   []Rule
}
type Diagnostic struct{ Target, Code, Detail string }
type Field struct {
	Target, Value string
	Deleted       bool
}
type Result struct {
	Fields        []Field
	Diagnostics   []Diagnostic
	PayloadDigest string
}

func (ir IR) Validate() error {
	if ir.Version == "" || len(ir.Rules) == 0 {
		return ErrInvalidIR
	}
	seen := map[string]bool{}
	for _, r := range ir.Rules {
		if r.Target == "" || seen[r.Target] {
			return fmt.Errorf("%w: duplicate or empty target", ErrInvalidIR)
		}
		seen[r.Target] = true
		if r.Null == "" {
			r.Null = NullError
		}
		switch r.Op {
		case OpIdentity, OpTrim, OpUpper, OpLower:
			if r.Argument != "" || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: extraneous arguments", ErrInvalidIR)
			}
		case OpConstant:
			if r.Argument == "" || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: constant", ErrInvalidIR)
			}
		case OpLookup:
			if len(r.Lookup) == 0 || r.Argument != "" {
				return fmt.Errorf("%w: lookup", ErrInvalidIR)
			}
		case OpDate:
			if !allowedLayouts[r.Argument] || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: date layout", ErrInvalidIR)
			}
		case OpMoney:
			if !currencyPattern.MatchString(r.Argument) || len(r.Lookup) > 0 || (r.MoneyMode != "" && r.MoneyMode != "PROFILE") {
				return fmt.Errorf("%w: money currency", ErrInvalidIR)
			}
		case OpCompose:
			if r.Argument == "" || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: compose", ErrInvalidIR)
			}
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedOp, r.Op)
		}
		if r.Null != NullError && r.Null != NullOmit && r.Null != NullDelete {
			return fmt.Errorf("%w: null policy", ErrInvalidIR)
		}
	}
	return nil
}

var allowedLayouts = map[string]bool{"2006-01-02": true, time.RFC3339: true, "2006/01/02": true, "01/02/2006": true, "20060102": true}
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func digest(ir IR, fs []Field) string {
	h := sha256.New()
	put := func(s string) { fmt.Fprintf(h, "%d:", len(s)); h.Write([]byte(s)) }
	put("hcmnext.connectivity.mapping")
	put(ir.Version)
	for _, f := range fs {
		put(f.Target)
		put(f.Value)
		if f.Deleted {
			put("DELETE")
		} else {
			put("VALUE")
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
