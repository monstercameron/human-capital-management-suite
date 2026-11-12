// Package doctemplate publishes and renders immutable, governed document templates.
//
// It deliberately accepts only typed, field-masked bindings. A published
// template is immutable: changing source, schema, or governance metadata
// changes its digest and requires a new version and publication.
package doctemplate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"sort"
	"strings"
)

type State string

const (
	Draft     State = "DRAFT"
	Published State = "PUBLISHED"
	Retired   State = "RETIRED"
)

type ValueKind string

const (
	String   ValueKind = "STRING"
	Integer  ValueKind = "INTEGER"
	Decimal  ValueKind = "DECIMAL"
	Boolean  ValueKind = "BOOLEAN"
	Date     ValueKind = "DATE"
	DateTime ValueKind = "DATETIME"
)

type Placeholder struct {
	Name     string    `json:"name"`
	Kind     ValueKind `json:"kind"`
	Required bool      `json:"required"`
}

// Binding is intentionally scalar. Callers must resolve and mask data before
// handing it to the renderer; arbitrary objects cannot be traversed.
type Binding struct {
	Kind  ValueKind `json:"kind"`
	Value string    `json:"value"`
}

type Approval struct {
	ApprovedBy string `json:"approved_by"`
	ApprovalID string `json:"approval_id"`
}

type Fixture struct {
	Name           string             `json:"name"`
	Bindings       map[string]Binding `json:"bindings"`
	ExpectedDigest string             `json:"expected_digest,omitempty"`
}

type Applicability struct {
	Locales         []string `json:"locales"`
	Jurisdictions   []string `json:"jurisdictions"`
	Classifications []string `json:"classifications"`
}

type Definition struct {
	ID                   string        `json:"id"`
	Version              string        `json:"version"`
	Type                 string        `json:"type"`
	Purpose              string        `json:"purpose"`
	SourceLocale         string        `json:"source_locale"`
	Locale               string        `json:"locale"`
	Jurisdiction         string        `json:"jurisdiction"`
	Classification       string        `json:"classification"`
	AccessibilityVersion string        `json:"accessibility_version"`
	Format               string        `json:"format"`
	Source               string        `json:"source"`
	Placeholders         []Placeholder `json:"placeholders"`
}

type Template struct {
	Definition   Definition   `json:"definition"`
	State        State        `json:"state"`
	SourceDigest string       `json:"source_digest"`
	Publication  *Publication `json:"publication,omitempty"`
}

type Publication struct {
	TemplateDigest string        `json:"template_digest"`
	Fixtures       []Fixture     `json:"fixtures"`
	Approval       Approval      `json:"approval"`
	Applicability  Applicability `json:"applicability"`
	Successor      string        `json:"successor,omitempty"`
	RetiredReason  string        `json:"retired_reason,omitempty"`
}

type Artifact struct {
	TemplateID           string
	TemplateVersion      string
	Locale               string
	Jurisdiction         string
	Classification       string
	AccessibilityVersion string
	Content              []byte
	Digest               string
}

var (
	ErrInvalid              = errors.New("invalid document template")
	ErrNotPublished         = errors.New("document template is not published")
	ErrChangedAfterApproval = errors.New("document template changed after approval")
)

func New(d Definition) (Template, error) {
	if err := validateDefinition(d); err != nil {
		return Template{}, err
	}
	digest, err := digestDefinition(d)
	if err != nil {
		return Template{}, err
	}
	return Template{Definition: cloneDefinition(d), State: Draft, SourceDigest: digest}, nil
}

func (t Template) Digest() string { return t.SourceDigest }

// Descriptor returns the generated protobuf full name for the template's
// document type: the type lowered under the doc package (WF-DATA-003). It
// is tenant-relative like every registry descriptor; the registry row
// binds it to its tenant, and the template version and digest pin the
// shape. A type that is not one protobuf identifier is refused rather
// than mangled.
func (d Definition) Descriptor() (string, error) {
	if !validDocType(d.Type) {
		return "", fmt.Errorf("%w: document type %q is not one identifier", ErrInvalid, d.Type)
	}
	return "doc." + strings.ToLower(d.Type), nil
}

func validDocType(t string) bool {
	if t == "" {
		return false
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		letter := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
		if c != '_' && !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func (t Template) Publish(p Publication) (Template, error) {
	if err := t.Validate(); err != nil {
		return Template{}, err
	}
	if t.State != Draft {
		return Template{}, fmt.Errorf("%w: state must be DRAFT", ErrInvalid)
	}
	if p.TemplateDigest == "" || p.TemplateDigest != t.SourceDigest {
		return Template{}, ErrChangedAfterApproval
	}
	if p.Approval.ApprovalID == "" || p.Approval.ApprovedBy == "" {
		return Template{}, fmt.Errorf("%w: approval is required", ErrInvalid)
	}
	if len(p.Fixtures) == 0 {
		return Template{}, fmt.Errorf("%w: at least one fixture is required", ErrInvalid)
	}
	if err := validateApplicability(p.Applicability); err != nil {
		return Template{}, err
	}
	if !contains(p.Applicability.Locales, t.Definition.Locale) || !contains(p.Applicability.Jurisdictions, t.Definition.Jurisdiction) || !contains(p.Applicability.Classifications, t.Definition.Classification) {
		return Template{}, fmt.Errorf("%w: applicability does not cover template metadata", ErrInvalid)
	}
	for i := range p.Fixtures {
		if p.Fixtures[i].Name == "" {
			return Template{}, fmt.Errorf("%w: fixture name is required", ErrInvalid)
		}
		artifact, err := t.render(p.Fixtures[i].Bindings)
		if err != nil {
			return Template{}, fmt.Errorf("%w: fixture %s: %v", ErrInvalid, p.Fixtures[i].Name, err)
		}
		if p.Fixtures[i].ExpectedDigest != "" && p.Fixtures[i].ExpectedDigest != artifact.Digest {
			return Template{}, fmt.Errorf("%w: fixture %s digest mismatch", ErrInvalid, p.Fixtures[i].Name)
		}
	}
	p.Fixtures = cloneFixtures(p.Fixtures)
	p.Applicability = cloneApplicability(p.Applicability)
	return Template{Definition: cloneDefinition(t.Definition), State: Published, SourceDigest: t.SourceDigest, Publication: &p}, nil
}

func (t Template) Retire(reason, successor string) (Template, error) {
	if t.State != Published || t.Publication == nil {
		return Template{}, ErrNotPublished
	}
	if strings.TrimSpace(reason) == "" {
		return Template{}, fmt.Errorf("%w: retirement reason is required", ErrInvalid)
	}
	p := *t.Publication
	p.Fixtures = cloneFixtures(p.Fixtures)
	p.Applicability = cloneApplicability(p.Applicability)
	p.RetiredReason, p.Successor = reason, successor
	return Template{Definition: cloneDefinition(t.Definition), State: Retired, SourceDigest: t.SourceDigest, Publication: &p}, nil
}

func (t Template) Render(bindings map[string]Binding) (Artifact, error) {
	if t.State != Published || t.Publication == nil {
		return Artifact{}, ErrNotPublished
	}
	current, err := digestDefinition(t.Definition)
	if err != nil || current != t.SourceDigest || t.Publication.TemplateDigest != t.SourceDigest {
		return Artifact{}, ErrChangedAfterApproval
	}
	return t.render(bindings)
}

// render executes the template source against typed bindings without the
// publication guards. Publish uses it to verify fixtures while the template
// is still a draft; Render applies the guards before delegating here.
func (t Template) render(bindings map[string]Binding) (Artifact, error) {
	if err := validateBindings(t.Definition.Placeholders, bindings); err != nil {
		return Artifact{}, err
	}
	data := make(map[string]string, len(bindings))
	for k, v := range bindings {
		data[k] = v.Value
	}
	tpl, err := template.New(t.Definition.ID).Option("missingkey=error").Parse(t.Definition.Source)
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: source: %v", ErrInvalid, err)
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return Artifact{}, fmt.Errorf("%w: render: %v", ErrInvalid, err)
	}
	content := out.Bytes()
	sum := sha256.Sum256(content)
	return Artifact{TemplateID: t.Definition.ID, TemplateVersion: t.Definition.Version, Locale: t.Definition.Locale, Jurisdiction: t.Definition.Jurisdiction, Classification: t.Definition.Classification, AccessibilityVersion: t.Definition.AccessibilityVersion, Content: append([]byte(nil), content...), Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

func (t Template) Validate() error { return validateDefinition(t.Definition) }

func validateDefinition(d Definition) error {
	if d.ID == "" || d.Version == "" || d.Type == "" || d.Purpose == "" || d.SourceLocale == "" || d.Locale == "" || d.Jurisdiction == "" || d.Classification == "" || d.AccessibilityVersion == "" || d.Source == "" {
		return fmt.Errorf("%w: required metadata missing", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, p := range d.Placeholders {
		if p.Name == "" || seen[p.Name] {
			return fmt.Errorf("%w: duplicate or empty placeholder", ErrInvalid)
		}
		seen[p.Name] = true
		if !validKind(p.Kind) {
			return fmt.Errorf("%w: unsupported placeholder kind %q", ErrInvalid, p.Kind)
		}
	}
	return nil
}
func validateBindings(ps []Placeholder, bs map[string]Binding) error {
	want := map[string]Placeholder{}
	for _, p := range ps {
		want[p.Name] = p
	}
	for name, b := range bs {
		p, ok := want[name]
		if !ok {
			return fmt.Errorf("%w: unknown binding %q", ErrInvalid, name)
		}
		if b.Kind != p.Kind {
			return fmt.Errorf("%w: binding %q has kind %s, want %s", ErrInvalid, name, b.Kind, p.Kind)
		}
		if b.Value == "" && p.Required {
			return fmt.Errorf("%w: required binding %q is empty", ErrInvalid, name)
		}
	}
	for _, p := range ps {
		if p.Required && bs[p.Name].Value == "" {
			return fmt.Errorf("%w: required binding %q is missing", ErrInvalid, p.Name)
		}
	}
	return nil
}
func validKind(k ValueKind) bool {
	switch k {
	case String, Integer, Decimal, Boolean, Date, DateTime:
		return true
	}
	return false
}
func validateApplicability(a Applicability) error {
	if len(a.Locales) == 0 || len(a.Jurisdictions) == 0 || len(a.Classifications) == 0 {
		return fmt.Errorf("%w: applicability must include locale, jurisdiction and classification", ErrInvalid)
	}
	return nil
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func digestDefinition(d Definition) (string, error) {
	b, e := json.Marshal(d)
	if e != nil {
		return "", e
	}
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:]), nil
}
func cloneDefinition(d Definition) Definition {
	d.Placeholders = append([]Placeholder(nil), d.Placeholders...)
	return d
}
func cloneFixtures(in []Fixture) []Fixture {
	out := make([]Fixture, len(in))
	for i, f := range in {
		out[i] = f
		out[i].Bindings = map[string]Binding{}
		keys := make([]string, 0, len(f.Bindings))
		for k := range f.Bindings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out[i].Bindings[k] = f.Bindings[k]
		}
	}
	return out
}

func cloneApplicability(in Applicability) Applicability {
	return Applicability{
		Locales:         append([]string(nil), in.Locales...),
		Jurisdictions:   append([]string(nil), in.Jurisdictions...),
		Classifications: append([]string(nil), in.Classifications...),
	}
}
