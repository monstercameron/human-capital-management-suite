package forms_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
)

func progressiveFixture() forms.ProgressiveForm {
	c := testdata.PromotionFixture()
	fields := append([]contract.RequestField(nil), c.Request.Fields...)
	for i := range fields {
		switch fields[i].ID {
		case workspace.FieldProposedComp:
			fields[i].Value = "98000.00"
		case workspace.FieldBusinessReason:
			fields[i].Validation.Message = "Explain the business reason."
		}
	}
	return forms.ProgressiveForm{
		ID:      "promotion-request",
		Action:  workspace.PathSimulate,
		Method:  http.MethodPost,
		Locale:  "en-US",
		FocusID: workspace.FieldBusinessReason,
		Hidden: map[string]string{
			forms.ProgressiveCSRFField:        "csrf-1",
			forms.ProgressiveIdempotencyField: "idem-1",
			forms.ProgressiveLocaleField:      "en-US",
			forms.ProgressiveTransitionField:  workspace.TransitionRunSimulation,
			workspace.ParamWorker:             c.Request.WorkerID,
		},
		Fields:            fields,
		SubmitLabel:       "Propose and simulate",
		ErrorSummaryLabel: "Correct the highlighted fields",
	}
}

func submittedValues(f forms.ProgressiveForm) map[string]string {
	values := make(map[string]string, len(f.Hidden)+len(f.Fields))
	for name, value := range f.Hidden {
		values[name] = value
	}
	for _, field := range f.Fields {
		if field.Kind != contract.FieldKindReadOnly {
			values[field.ID] = field.Value
		}
	}
	return values
}

func fallbackRequest(f forms.ProgressiveForm, values map[string]string) *http.Request {
	encoded := make(url.Values, len(values))
	for name, value := range values {
		encoded.Set(name, value)
	}
	r := httptest.NewRequest(http.MethodPost, f.Action, strings.NewReader(encoded.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func governedIntent(values map[string]string) (forms.IntentInstance, error) {
	return forms.FromFormSubmission(values[workspace.ParamWorker], values)
}

// TestTodo_WEB_029 proves enhanced collection and a normal browser POST feed
// the one existing Promotion form route with identical values, intent digest,
// and server-issued idempotency evidence.
func TestTodo_WEB_029(t *testing.T) {
	f := progressiveFixture()
	values := submittedValues(f)
	enhanced, err := f.NormalizeEnhanced(values)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := f.NormalizeHTTP(fallbackRequest(f, values))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(enhanced, fallback) {
		t.Fatalf("enhanced/fallback normalized values differ:\n enhanced=%#v\n fallback=%#v", enhanced, fallback)
	}

	formIntent, err := governedIntent(enhanced)
	if err != nil {
		t.Fatal(err)
	}
	queryIntent, err := (workspace.Query{
		WorkerRef:      enhanced[workspace.ParamWorker],
		TargetJobCode:  enhanced[workspace.FieldProposedJobTitle],
		TargetGrade:    enhanced[workspace.FieldProposedGrade],
		ProposedBase:   enhanced[workspace.FieldProposedComp],
		EffectiveDate:  enhanced[workspace.FieldEffectiveDate],
		BusinessReason: enhanced[workspace.FieldBusinessReason],
	}).Intent()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(formIntent, queryIntent) {
		t.Fatalf("form and workspace query produced different governed intents:\n form=%+v\nquery=%+v", formIntent, queryIntent)
	}
	if enhanced[forms.ProgressiveIdempotencyField] != values[forms.ProgressiveIdempotencyField] {
		t.Fatal("normalization replaced the server-issued idempotency key")
	}
}

func TestTodo_WEB_029_Golden(t *testing.T) {
	intent, err := governedIntent(submittedValues(progressiveFixture()))
	if err != nil {
		t.Fatal(err)
	}
	const want = "af864c60df5458da0d72aca7694b7b9c8831cb40b13d11da89437bb8685292ab"
	if intent.Digest != want {
		t.Fatalf("digest = %q, want %q", intent.Digest, want)
	}
	if intent.Inputs.ProposedGrade != "RN3" {
		t.Fatalf("canonical intent lost proposed grade: %+v", intent.Inputs)
	}
}

// TestTodo_WEB_029_Browser parses the script-free result and also inspects
// the production GWC binding. Native browser semantics are the fallback; no
// browser emulation or JavaScript mock is required to prove them.
func TestTodo_WEB_029_Browser(t *testing.T) {
	f := progressiveFixture()
	doc, err := f.NativeHTML()
	if err != nil {
		t.Fatal(err)
	}
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	var formsFound, submits int
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			formsFound++
		}
		if n.Type == html.ElementNode && n.Data == "button" && attribute(n, "type") == "submit" {
			submits++
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if formsFound != 1 || submits != 1 {
		t.Fatalf("native document has forms=%d submit controls=%d, want one each", formsFound, submits)
	}
	for _, want := range []string{
		`method="post"`, `action="` + workspace.PathSimulate + `"`, `lang="en-US"`,
		`name="csrf_token"`, `name="idempotency_key"`, `name="locale"`,
		`name="worker"`, `name="transition"`, `name="proposedGrade"`,
		`value="RN3"`, `required aria-required="true"`,
		`aria-invalid="true" aria-describedby="businessReason-error"`,
		`href="#businessReason"`, `data-hydration-focus="businessReason"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("native fallback missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(doc), "<script") || strings.Contains(strings.ToLower(doc), " onsubmit=") {
		t.Fatal("native fallback contains executable markup")
	}

	page := workspace.Page{
		Contract: testdata.PromotionFixture(),
		Query:    workspace.Query{EffectiveDate: "2026-10-01"},
		Locale:   workspace.ResolveLocale("de-DE"),
	}
	production, err := workspace.RenderPage(page, "csrf-production", "worker-1048", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<form action="` + workspace.PathSimulate + `" id="promotion-request" method="post">`,
		`name="csrf_token" value="csrf-production"`,
		`name="worker" value="worker-1048"`,
		`name="locale" value="de-DE"`,
		`name="transition"`, `name="proposedGrade"`, `form="promotion-request"`, `type="submit"`,
	} {
		if !strings.Contains(production, want) {
			t.Errorf("production native binding missing %q", want)
		}
	}
	if strings.Contains(production, `action="#"`) || strings.Contains(production, "<script") {
		t.Fatal("production native fallback contains the unbound legacy mount or a script")
	}
}

func TestTodo_WEB_029_Conformance(t *testing.T) {
	f := progressiveFixture()
	base := submittedValues(f)
	mutations := map[string]func(map[string]string){
		"unknown":            func(v map[string]string) { v["unexpected"] = "true" },
		"masked":             func(v map[string]string) { v["nationalId"] = "555-11-2222" },
		"authority":          func(v map[string]string) { v["tenant_id"] = "tenant-other" },
		"missing required":   func(v map[string]string) { delete(v, workspace.FieldProposedGrade) },
		"empty required":     func(v map[string]string) { v[workspace.FieldBusinessReason] = "" },
		"forged csrf":        func(v map[string]string) { v[forms.ProgressiveCSRFField] = "forged" },
		"forged idempotency": func(v map[string]string) { v[forms.ProgressiveIdempotencyField] = "forged" },
		"forged locale":      func(v map[string]string) { v[forms.ProgressiveLocaleField] = "de-DE" },
		"forged transition":  func(v map[string]string) { v[forms.ProgressiveTransitionField] = "force_execute" },
		"newline in text":    func(v map[string]string) { v[workspace.FieldProposedGrade] = "RN3\nadmin" },
		"noncanonical date":  func(v map[string]string) { v[workspace.FieldEffectiveDate] = "10/01/2026" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			values := cloneMap(base)
			mutate(values)
			_, err := f.NormalizeEnhanced(values)
			if !errors.Is(err, forms.ErrInvalidSubmission) {
				t.Fatalf("error = %v, want sanitized invalid-submission refusal", err)
			}
			if strings.Contains(err.Error(), "555-11-2222") || strings.Contains(err.Error(), "tenant-other") || strings.Contains(err.Error(), "force_execute") {
				t.Fatalf("refusal disclosed submitted data: %q", err)
			}
		})
	}

	duplicate := make(url.Values)
	for name, value := range base {
		duplicate.Set(name, value)
	}
	duplicate.Add(workspace.FieldBusinessReason, "duplicate")
	r := httptest.NewRequest(http.MethodPost, f.Action, strings.NewReader(duplicate.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := f.NormalizeHTTP(r); !errors.Is(err, forms.ErrInvalidSubmission) {
		t.Fatalf("duplicate control error = %v", err)
	}

	wrongRoute := fallbackRequest(f, base)
	wrongRoute.URL.Path = "/other"
	for name, request := range map[string]*http.Request{
		"wrong method":       httptest.NewRequest(http.MethodGet, f.Action, nil),
		"wrong route":        wrongRoute,
		"wrong content type": httptest.NewRequest(http.MethodPost, f.Action, strings.NewReader("x=y")),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.NormalizeHTTP(request); !errors.Is(err, forms.ErrInvalidSubmission) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTodo_WEB_029_Security(t *testing.T) {
	tests := map[string]func(*forms.ProgressiveForm){
		"non-POST":              func(f *forms.ProgressiveForm) { f.Method = http.MethodGet },
		"external action":       func(f *forms.ProgressiveForm) { f.Action = "https://evil.example/submit" },
		"missing csrf":          func(f *forms.ProgressiveForm) { delete(f.Hidden, forms.ProgressiveCSRFField) },
		"locale mismatch":       func(f *forms.ProgressiveForm) { f.Hidden[forms.ProgressiveLocaleField] = "de-DE" },
		"authority hidden":      func(f *forms.ProgressiveForm) { f.Hidden["principal"] = "alice" },
		"readonly focus":        func(f *forms.ProgressiveForm) { f.FocusID = "currentJobTitle" },
		"unlocalized submit":    func(f *forms.ProgressiveForm) { f.SubmitLabel = "" },
		"missing error heading": func(f *forms.ProgressiveForm) { f.ErrorSummaryLabel = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := cloneForm(progressiveFixture())
			mutate(&f)
			if err := f.Validate(); !errors.Is(err, forms.ErrInvalidForm) {
				t.Fatalf("Validate error = %v", err)
			}
		})
	}
}

func TestProgressiveLocaleIsPresentationOnly(t *testing.T) {
	en := progressiveFixture()
	enValues := submittedValues(en)
	enValues[workspace.FieldProposedComp] = "USD 98,000.00"
	de := cloneForm(en)
	de.Locale = "de-DE"
	de.Hidden[forms.ProgressiveLocaleField] = "de-DE"
	deValues := submittedValues(de)
	deValues[workspace.FieldProposedComp] = "98.000,00 USD"

	enNormalized, err := en.NormalizeEnhanced(enValues)
	if err != nil {
		t.Fatal(err)
	}
	deNormalized, err := de.NormalizeHTTP(fallbackRequest(de, deValues))
	if err != nil {
		t.Fatal(err)
	}
	enCanonical, err := workspace.CanonicalizePresentationAnswers(workspace.ResolveLocale("en-US"), "USD", enNormalized)
	if err != nil {
		t.Fatal(err)
	}
	deCanonical, err := workspace.CanonicalizePresentationAnswers(workspace.ResolveLocale("de-DE"), "USD", deNormalized)
	if err != nil {
		t.Fatal(err)
	}
	enIntent, err := governedIntent(enCanonical)
	if err != nil {
		t.Fatal(err)
	}
	deIntent, err := governedIntent(deCanonical)
	if err != nil {
		t.Fatal(err)
	}
	if enIntent.Digest != deIntent.Digest || !reflect.DeepEqual(enIntent.Inputs, deIntent.Inputs) {
		t.Fatalf("locale changed governed semantics:\n en=%+v\n de=%+v", enIntent, deIntent)
	}
	if enNormalized[forms.ProgressiveLocaleField] != "en-US" || deNormalized[forms.ProgressiveLocaleField] != "de-DE" {
		t.Fatal("transport normalization did not preserve presentation locale")
	}
	if enNormalized[forms.ProgressiveIdempotencyField] != deNormalized[forms.ProgressiveIdempotencyField] {
		t.Fatal("locale changed the logical-attempt idempotency key")
	}
}

func TestTodo_WEB_029_Integration(t *testing.T) {
	f := progressiveFixture()
	values := submittedValues(f)
	paths := make([]map[string]string, 0, 3)
	first, err := f.NormalizeEnhanced(values)
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, first)
	for i := 0; i < 2; i++ {
		got, err := f.NormalizeHTTP(fallbackRequest(f, values))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, got)
	}

	records := map[string]string{}
	effects := 0
	apply := func(values map[string]string) error {
		intent, err := governedIntent(values)
		if err != nil {
			return err
		}
		key := values[forms.ProgressiveIdempotencyField]
		if prior, exists := records[key]; exists {
			if prior != intent.Digest {
				return errors.New("idempotency conflict")
			}
			return nil
		}
		records[key] = intent.Digest
		effects++
		return nil
	}
	for _, path := range paths {
		if err := apply(path); err != nil {
			t.Fatal(err)
		}
	}
	if effects != 1 {
		t.Fatalf("refresh/back/double submit produced %d effects, want one", effects)
	}

	changed := cloneMap(values)
	changed[workspace.FieldProposedGrade] = "RN4"
	changedValues, err := f.NormalizeEnhanced(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply(changedValues); err == nil || effects != 1 {
		t.Fatalf("same key with changed grade was not refused: err=%v effects=%d", err, effects)
	}
}

func TestTodo_WEB_029_Fault(t *testing.T) {
	f := progressiveFixture()
	oversized := httptest.NewRequest(http.MethodPost, f.Action, strings.NewReader(strings.Repeat("x", 65537)))
	oversized.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := f.NormalizeHTTP(oversized); !errors.Is(err, forms.ErrSubmissionTooLarge) {
		t.Fatalf("oversized body error = %v", err)
	}

	many := make([]string, 129)
	for i := range many {
		many[i] = "x="
	}
	r := httptest.NewRequest(http.MethodPost, f.Action, strings.NewReader(strings.Join(many, "&")))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := f.NormalizeHTTP(r); !errors.Is(err, forms.ErrSubmissionTooLarge) {
		t.Fatalf("excess control count error = %v", err)
	}

	malformed := httptest.NewRequest(http.MethodPost, f.Action, strings.NewReader("secret=%zz"))
	malformed.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := f.NormalizeHTTP(malformed)
	if !errors.Is(err, forms.ErrInvalidSubmission) || strings.Contains(err.Error(), "%zz") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("malformed encoding error is not sanitized: %v", err)
	}
}

func FuzzProgressiveEnhancedFallbackParity(fuzz *testing.F) {
	fuzz.Add("Scope increase.\r\nRetention risk.", "RN3")
	fuzz.Add("", "")
	fuzz.Add("Gründe: äöü", "P4")
	fuzz.Fuzz(func(t *testing.T, reason, grade string) {
		f := progressiveFixture()
		values := submittedValues(f)
		values[workspace.FieldBusinessReason] = reason
		values[workspace.FieldProposedGrade] = grade
		enhanced, enhancedErr := f.NormalizeEnhanced(values)
		fallback, fallbackErr := f.NormalizeHTTP(fallbackRequest(f, values))
		if (enhancedErr == nil) != (fallbackErr == nil) {
			t.Fatalf("acceptance mismatch: enhanced=%v fallback=%v", enhancedErr, fallbackErr)
		}
		if enhancedErr == nil && !reflect.DeepEqual(enhanced, fallback) {
			t.Fatalf("normalization mismatch: enhanced=%#v fallback=%#v", enhanced, fallback)
		}
	})
}

func BenchmarkProgressiveFormNormalize(b *testing.B) {
	f := progressiveFixture()
	values := submittedValues(f)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.NormalizeEnhanced(values); err != nil {
			b.Fatal(err)
		}
	}
}

func TestProgressiveFormLatencyBudget(t *testing.T) {
	f := progressiveFixture()
	values := submittedValues(f)
	// The benchmark below is the allocation/CPU regression oracle. Keep this
	// wall-clock gate wide enough for Windows scheduler ticks under concurrent
	// package builds while still two orders of magnitude below the 100 ms
	// interaction budget.
	budget := latencygate.Budget{Name: "progressive form normalization", P95: time.Millisecond, Warmups: 3, Samples: 25}
	result, err := latencygate.Measure(budget, func() error {
		_, err := f.NormalizeEnhanced(values)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(result)
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatal(err)
	}
}

func attribute(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneForm(in forms.ProgressiveForm) forms.ProgressiveForm {
	in.Hidden = cloneMap(in.Hidden)
	in.Fields = append([]contract.RequestField(nil), in.Fields...)
	return in
}
