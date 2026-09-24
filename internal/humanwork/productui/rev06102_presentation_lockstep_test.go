package productui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/presentation"
)

func TestTodo_REV_061_02(t *testing.T) {
	assertVocabularyInLockstep(t, "field_disposition.go", "FieldDisposition", "../../../tools/uxqual/presentation/contracts.go", "FieldDisposition")
	assertVocabularyInLockstep(t, "action_availability.go", "ActionAvailability", "../../../tools/uxqual/presentation/contracts.go", "ActionAvailability")

	hidden := presentation.SemanticAction{ID: "private.operation", RPC: knownPresentationRPC(t), Availability: presentation.ActionHidden}
	if err := hidden.Validate(); err != nil {
		t.Fatalf("governed hidden action with no presentation data was rejected: %v", err)
	}
	if hidden.Presentable() {
		t.Fatal("governed hidden action is presentable")
	}
}

func assertVocabularyInLockstep(t *testing.T, productFile, productType, governedFile, governedType string) {
	t.Helper()
	product := declaredStringConstants(t, productFile, productType)
	governed := declaredStringConstants(t, governedFile, governedType)
	if productType == "ActionAvailability" {
		for index, value := range product {
			product[index] = strings.ToUpper(value)
			if value == string(ActionUnavailable) {
				product[index] = string(presentation.ActionUnavailableSafe)
			}
		}
		sort.Strings(product)
	}
	if !reflect.DeepEqual(product, governed) {
		t.Errorf("%s vocabulary drift: productui %v, presentation %v", productType, product, governed)
	}
}

func declaredStringConstants(t *testing.T, file, typeName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	values := []string{}
	for _, declaration := range parsed.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec := spec.(*ast.ValueSpec)
			if valueSpec.Type == nil || valueSpec.Type.(*ast.Ident).Name != typeName {
				continue
			}
			for _, expression := range valueSpec.Values {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s.%s has a non-string constant", file, typeName)
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("unquote %s.%s: %v", file, typeName, err)
				}
				values = append(values, value)
			}
		}
	}
	sort.Strings(values)
	return values
}

func TestTodo_REV_061_02_Golden(t *testing.T) {
	wantDispositions := []string{"DERIVED_ONLY", "HIDE", "MASK", "REDACT", "SHOW", "SUMMARY_ONLY"}
	for _, file := range []string{"field_disposition.go", "../../../tools/uxqual/presentation/contracts.go"} {
		if got := declaredStringConstants(t, file, "FieldDisposition"); !reflect.DeepEqual(got, wantDispositions) {
			t.Errorf("%s FieldDisposition bytes = %q, want %q", file, got, wantDispositions)
		}
	}
	wantFrontendActions := []string{"available", "hidden", "unavailable"}
	if got := declaredStringConstants(t, "action_availability.go", "ActionAvailability"); !reflect.DeepEqual(got, wantFrontendActions) {
		t.Errorf("productui action availability bytes = %q, want %q", got, wantFrontendActions)
	}
	wantGovernedActions := []string{"AVAILABLE", "HIDDEN", "UNAVAILABLE_SAFE_TO_DISCLOSE"}
	if got := declaredStringConstants(t, "../../../tools/uxqual/presentation/contracts.go", "ActionAvailability"); !reflect.DeepEqual(got, wantGovernedActions) {
		t.Errorf("presentation action availability bytes = %q, want %q", got, wantGovernedActions)
	}
}

func TestTodo_REV_061_02_Security(t *testing.T) {
	// The item contains deliberately sensitive text to prove the rendered UI
	// applies HIDDEN as a disclosure boundary, even if a caller supplies data
	// that a valid SemanticAction would reject.
	item := ActionLauncherItem{
		ID: "private.operation", Kind: ActionLauncherAction,
		Label: "Secret operation", Description: "Secret description", Href: "/private/operation",
		Reason: "Secret reason", Availability: ActionState{Availability: ActionHidden},
	}
	if got := presentableActionLauncherItems([]ActionLauncherItem{item}); len(got) != 0 {
		t.Fatalf("hidden action survived launcher admission: %+v", got)
	}
	doc, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{
		Items: []ActionLauncherItem{item}, InitialQuery: "secret",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Secret operation", "Secret description", "Secret reason", "/private/operation"} {
		if strings.Contains(doc, secret) {
			t.Errorf("hidden action leaked %q into launcher markup", secret)
		}
	}
	if err := (presentation.SemanticAction{
		ID: "private.operation", RPC: knownPresentationRPC(t), Availability: presentation.ActionHidden,
		Label: "Secret operation", ActionToken: "secret-token", UnavailableReason: "Secret reason",
	}).Validate(); err == nil {
		t.Fatal("governed hidden action accepted presentation text or a token")
	}
}

func knownPresentationRPC(t *testing.T) string {
	t.Helper()
	for rpc := range presentationKnownRPCs() {
		return rpc
	}
	t.Fatal("presentation has no registered RPCs")
	return ""
}

func presentationKnownRPCs() map[string]bool {
	// Use an RPC registered by the governed page definition without pinning a
	// service name in this conformance test.
	return pagedef.KnownRPCs()
}
