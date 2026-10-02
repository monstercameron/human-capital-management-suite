// Package chatgate owns versioned channel admission forms and their personal data.
package chatgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("gate: invalid argument")
	ErrDenied      = errors.New("gate: permission denied")
	ErrConflict    = errors.New("gate: revision or idempotency conflict")
	ErrNotFound    = errors.New("gate: not found")
	ErrUnavailable = errors.New("gate: unavailable")
	ErrRequired    = errors.New("gate: answers required")
	ErrHeld        = errors.New("gate: legal hold suspends deletion")
)

const APIVersion = "hcmnext.chat.v1.ChannelGateService"

type Version struct{ Major, Minor, Patch uint32 }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }
func ParseVersion(s string) (Version, error) {
	p := strings.Split(s, ".")
	if len(p) != 3 {
		return Version{}, ErrInvalid
	}
	var n [3]uint32
	for i, x := range p {
		u, e := strconv.ParseUint(x, 10, 32)
		if e != nil || strconv.FormatUint(u, 10) != x {
			return Version{}, ErrInvalid
		}
		n[i] = uint32(u)
	}
	if n[0] == 0 {
		return Version{}, ErrInvalid
	}
	return Version{n[0], n[1], n[2]}, nil
}
func (v Version) Less(w Version) bool {
	if v.Major != w.Major {
		return v.Major < w.Major
	}
	if v.Minor != w.Minor {
		return v.Minor < w.Minor
	}
	return v.Patch < w.Patch
}

type Visibility struct {
	Administrators, Members bool
	Consumers               []string
}
type Field struct {
	ID, Kind, KindVersion, Label, Help, Purpose, DataClass string
	Required                                               bool
	Options                                                []string
	Visibility                                             Visibility
	RetentionDays                                          int
	DocumentID, DocumentVersion                            string
}
type Expression struct {
	Operator, Field, Fact string
	Values                []string
	Children              []Expression
}
type Rule struct {
	When            Expression
	Outcome, Reason string
}
type Definition struct {
	Version    Version
	Digest     string
	Fields     []Field
	Mode       string
	Rules      []Rule
	Purpose    string
	AnswerBy   time.Time
	Extensions map[string]json.RawMessage
}
type Gate struct {
	Revision      uint64
	Draft         *Definition
	Versions      []Definition
	Current       string
	State         string
	Installations []Installation
}
type Submission struct {
	ID, Person, Version, Status, Reason, Reviewer string
	Revision                                      uint64
	SubmittedAt                                   time.Time
	Fields                                        []Field
	Digest                                        string
}
type Answer struct {
	SubmissionID, Person, FieldID string
	Value                         json.RawMessage
	ExpiresAt                     time.Time
	Held                          bool
}
type ReadAudit struct {
	Person, Reader, Consumer, Purpose string
	Fields                            []string
	At                                time.Time
	Export                            bool
}
type Event struct {
	ID, Kind, Conversation, Tenant, Person, SubmissionID, Version string
	At                                                            time.Time
}
type Receipt struct {
	Fingerprint string
	Result      json.RawMessage
}

// State excludes answer values. Answers are persisted in a separate personal-data store.
type State struct {
	Gate        Gate
	Submissions []Submission
	Receipts    map[string]Receipt
	Audits      []ReadAudit
	Events      []Event
	Overrides   []AdmissionOverride
}
type AdmissionOverride struct {
	Person, Administrator, Version, Reason string
	At                                     time.Time
}
type Scope struct{ Tenant, Conversation string }
type Actor struct{ Tenant, Person string }
type Command struct {
	Scope            Scope
	Actor            Actor
	Key              string
	ExpectedRevision uint64
}
type Consumer struct {
	Names                           map[string]string
	ID, Version, Effect, Permission string
	Fields, Kinds                   []string
}
type Installation struct {
	ConsumerID, Version string
	Mapping             map[string]string
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func ContentDigest(d Definition) string { d.Digest = ""; return digest(d) }

// RequiredBump is total: removing or changing a field's contract is major;
// additions which cannot invalidate old answers are minor; wording is patch.
func RequiredBump(old, next Definition) Version {
	level := 0
	byID := map[string]Field{}
	for _, f := range next.Fields {
		byID[f.ID] = f
	}
	for _, f := range old.Fields {
		n, ok := byID[f.ID]
		if !ok || f.Kind != n.Kind || f.KindVersion != n.KindVersion || (!f.Required && n.Required) || f.Purpose != n.Purpose || f.DataClass != n.DataClass || digest(f.Visibility) != digest(n.Visibility) || f.RetentionDays != n.RetentionDays || f.DocumentID != n.DocumentID || f.DocumentVersion != n.DocumentVersion {
			level = 2
		}
		for _, o := range f.Options {
			if !contains(n.Options, o) {
				level = 2
			}
		}
		if len(n.Options) > len(f.Options) && level < 1 {
			level = 1
		}
		delete(byID, f.ID)
	}
	for _, f := range byID {
		if f.Required {
			level = 2
		} else if level < 1 {
			level = 1
		}
	}
	if old.Mode != next.Mode || digest(old.Rules) != digest(next.Rules) {
		level = 2
	}
	v := old.Version
	switch level {
	case 2:
		if v.Major == ^uint32(0) {
			return Version{^uint32(0), ^uint32(0), ^uint32(0)}
		}
		v.Major++
		v.Minor = 0
		v.Patch = 0
	case 1:
		if v.Minor == ^uint32(0) {
			if v.Major == ^uint32(0) {
				return Version{^uint32(0), ^uint32(0), ^uint32(0)}
			}
			return Version{v.Major + 1, 0, 0}
		}
		v.Minor++
		v.Patch = 0
	default:
		if v.Patch == ^uint32(0) {
			if v.Minor < ^uint32(0) {
				return Version{v.Major, v.Minor + 1, 0}
			}
			if v.Major < ^uint32(0) {
				return Version{v.Major + 1, 0, 0}
			}
			return v
		}
		v.Patch++
	}
	return v
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func current(g Gate) (Definition, error) {
	for _, d := range g.Versions {
		if d.Version.String() == g.Current {
			return d, nil
		}
	}
	return Definition{}, ErrNotFound
}
