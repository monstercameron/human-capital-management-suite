package hydration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/contract"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
)

// workspaceSemantic is the small, renderer-neutral surface that must survive
// a switch from the server fallback to the mounted GWC/WASM tree. It excludes
// stylesheet bytes and widget interiors, but includes everything that changes
// what a keyboard, screen reader, or form submission can mean.
type workspaceSemantic struct {
	Landmarks []workspaceLandmark `json:"landmarks"`
	Forms     []workspaceForm     `json:"forms"`
	Statuses  []workspaceStatus   `json:"statuses"`
	Errors    []workspaceError    `json:"errors"`
	Links     []string            `json:"links"`
}

type workspaceLandmark struct {
	Tag        string `json:"tag"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	LabelledBy string `json:"labelled_by,omitempty"`
}

type workspaceForm struct {
	Action     string             `json:"action"`
	Transition string             `json:"transition,omitempty"`
	Controls   []workspaceControl `json:"controls"`
}

type workspaceControl struct {
	Tag         string `json:"tag"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	Required    bool   `json:"required"`
	Invalid     string `json:"invalid,omitempty"`
	DescribedBy string `json:"described_by,omitempty"`
	Text        string `json:"text,omitempty"`
}

type workspaceStatus struct {
	Status string `json:"status"`
	Role   string `json:"role"`
	Live   string `json:"live"`
	Text   string `json:"text"`
}

type workspaceError struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Text string `json:"text"`
}

func renderWorkspacePair(t *testing.T, c contract.WorkspaceContract) (string, string) {
	t.Helper()
	server, err := ssr.Render(c)
	if err != nil {
		t.Fatalf("SSR render: %v", err)
	}
	client, err := gwc.Document(c)
	if err != nil {
		t.Fatalf("GWC render: %v", err)
	}
	return server, client
}

func parseWorkspaceSemantic(source string) (workspaceSemantic, error) {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return workspaceSemantic{}, err
	}
	var out workspaceSemantic
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "header", "nav", "main", "section", "footer":
				out.Landmarks = append(out.Landmarks, workspaceLandmark{Tag: n.Data, ID: attrValue(n, "id"), Name: attrValue(n, "aria-label"), LabelledBy: attrValue(n, "aria-labelledby")})
			case "a":
				out.Links = append(out.Links, attrValue(n, "href"))
			case "form":
				out.Forms = append(out.Forms, semanticForm(n))
			case "div":
				if attrValue(n, "class") == "status-banner" {
					out.Statuses = append(out.Statuses, workspaceStatus{Status: attrValue(n, "data-status"), Role: attrValue(n, "role"), Live: attrValue(n, "aria-live"), Text: strings.TrimSpace(nodeText(n))})
				}
			case "p":
				if attrValue(n, "role") == "alert" && strings.HasSuffix(attrValue(n, "id"), "-error") {
					out.Errors = append(out.Errors, workspaceError{ID: attrValue(n, "id"), Role: attrValue(n, "role"), Text: strings.TrimSpace(nodeText(n))})
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out, nil
}

func semanticForm(n *html.Node) workspaceForm {
	form := workspaceForm{Action: attrValue(n, "action")}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "input", "textarea", "select", "button":
				control := workspaceControl{Tag: node.Data, ID: attrValue(node, "id"), Name: attrValue(node, "name"), Type: attrValue(node, "type"), Value: attrValue(node, "value"), Required: hasAttr(node, "required"), Invalid: attrValue(node, "aria-invalid"), DescribedBy: attrValue(node, "aria-describedby"), Text: strings.TrimSpace(nodeText(node))}
				form.Controls = append(form.Controls, control)
				if node.Data == "input" && control.Name == "transition" {
					form.Transition = control.Value
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return form
}

func attrValue(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func compareWorkspaceSemantics(ssrHTML, gwcHTML string) (workspaceSemantic, error) {
	server, err := parseWorkspaceSemantic(ssrHTML)
	if err != nil {
		return workspaceSemantic{}, fmt.Errorf("parse SSR: %w", err)
	}
	client, err := parseWorkspaceSemantic(gwcHTML)
	if err != nil {
		return workspaceSemantic{}, fmt.Errorf("parse GWC: %w", err)
	}
	if !semanticEqualValue(server, client) {
		return workspaceSemantic{}, fmt.Errorf("SSR/GWC semantic projection differs")
	}
	return server, nil
}

func semanticEqualValue(a, b workspaceSemantic) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func workspaceDigest(s workspaceSemantic) string {
	body, _ := json.Marshal(s)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func assertWorkspaceParity(t *testing.T, c contract.WorkspaceContract) workspaceSemantic {
	t.Helper()
	server, client := renderWorkspacePair(t, c)
	semantic, err := compareWorkspaceSemantics(server, client)
	if err != nil {
		t.Fatal(err)
	}
	return semantic
}

func TestTodo_ALIGN_057(t *testing.T) {
	semantic := assertWorkspaceParity(t, testdata.PromotionFixture())
	if len(semantic.Landmarks) != 9 {
		t.Fatalf("landmarks=%d, want header/nav/main, five sections, and footer", len(semantic.Landmarks))
	}
	if len(semantic.Forms) != 4 {
		t.Fatalf("forms=%d, want request plus three authorized actions", len(semantic.Forms))
	}
}

func TestTodo_ALIGN_057_Property(t *testing.T) {
	for _, status := range []contract.SimulationStatus{contract.SimulationPending, contract.SimulationReady, contract.SimulationNeedsReview, contract.SimulationFailed} {
		t.Run(string(status), func(t *testing.T) {
			c := testdata.PromotionFixture()
			c.Simulation.Status = status
			semantic := assertWorkspaceParity(t, c)
			if len(semantic.Statuses) != 1 || semantic.Statuses[0].Status != string(status) {
				t.Fatalf("status projection=%+v", semantic.Statuses)
			}
		})
	}
}

func TestTodo_ALIGN_057_Golden(t *testing.T) {
	server, client := renderWorkspacePair(t, testdata.PromotionFixture())
	one, err := compareWorkspaceSemantics(server, client)
	if err != nil {
		t.Fatal(err)
	}
	two, err := compareWorkspaceSemantics(server, client)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := workspaceDigest(one), workspaceDigest(two); got != want {
		t.Fatalf("semantic digest is nondeterministic: %q vs %q", got, want)
	}
	const wantDigest = "sha256:1e6651e17b2bf145424d5e3c1fe9073bd16e39810b53500ee855b3c91caae27a"
	if got := workspaceDigest(one); got != wantDigest {
		t.Fatalf("semantic digest=%q, want pinned ALIGN-057 golden %q", got, wantDigest)
	}
}

func TestTodo_ALIGN_057_Security(t *testing.T) {
	server, client := renderWorkspacePair(t, testdata.PromotionFixture())
	for name, document := range map[string]string{"SSR": server, "GWC": client} {
		for _, needle := range testdata.MaskedNeedles() {
			if strings.Contains(document, needle) {
				t.Errorf("%s rendered masked authorization value %q", name, needle)
			}
		}
	}
	serverSemantic, err := parseWorkspaceSemantic(server)
	if err != nil {
		t.Fatal(err)
	}
	for _, href := range serverSemantic.Links {
		if strings.Contains(strings.ToLower(href), "javascript:") {
			t.Fatalf("unsafe navigation route reached the SSR surface: %q", href)
		}
	}
	for _, form := range serverSemantic.Forms {
		if form.Transition == "force_execute" {
			t.Fatal("unauthorized force_execute action reached the form surface")
		}
	}

	// Authorization is resolved before either renderer receives the value. A
	// stricter decision must reduce both surfaces identically, never leave a
	// disabled or blank privileged control behind.
	restricted := contract.NewWorkspaceContract(testdata.SourceRecord(), contract.Allow("currentJobTitle", "proposedJobTitle"), contract.Allow("approve"))
	restrictedSSR, restrictedGWC := renderWorkspacePair(t, restricted)
	restrictedSemantic, err := compareWorkspaceSemantics(restrictedSSR, restrictedGWC)
	if err != nil {
		t.Fatal(err)
	}
	if len(restrictedSemantic.Forms) != 2 || restrictedSemantic.Forms[1].Transition != "approve" {
		t.Fatalf("restricted action projection=%+v", restrictedSemantic.Forms)
	}
	for _, needle := range testdata.MaskedNeedles() {
		if strings.Contains(restrictedSSR, needle) || strings.Contains(restrictedGWC, needle) {
			t.Errorf("restricted render leaked %q", needle)
		}
	}
}

func TestTodo_ALIGN_057_Integration(t *testing.T) {
	server, client := renderWorkspacePair(t, testdata.PromotionFixture())
	semantic, err := compareWorkspaceSemantics(server, client)
	if err != nil {
		t.Fatal(err)
	}
	for _, form := range semantic.Forms {
		if form.Action == "" || strings.Contains(strings.ToLower(form.Action), "javascript:") {
			t.Fatalf("unsafe or missing form route: %+v", form)
		}
	}
	if len(semantic.Links) != 5 {
		t.Fatalf("links=%d, want skip link plus four section routes", len(semantic.Links))
	}
	for i, want := range []string{"approve", "reject", "request_more_information"} {
		if got := semantic.Forms[i+1].Transition; got != want {
			t.Errorf("action[%d] transition=%q, want %q", i, got, want)
		}
	}
}

func TestTodo_ALIGN_057_Fault(t *testing.T) {
	server, client := renderWorkspacePair(t, testdata.PromotionFixture())
	erroneous := testdata.PromotionFixture()
	for i := range erroneous.Request.Fields {
		if erroneous.Request.Fields[i].ID == "proposedCompensation" {
			erroneous.Request.Fields[i].Validation.Message = "Enter an amount within the approved compensation band."
		}
	}
	errServer, errClient := renderWorkspacePair(t, erroneous)
	mutations := map[string]string{
		"landmark name": strings.Replace(client, `aria-label="Workspace sections"`, `aria-label="Different sections"`, 1),
		"action route":  strings.Replace(client, `action="#"`, `action="/wrong-route"`, 1),
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := compareWorkspaceSemantics(server, mutation); err == nil {
				t.Fatal("semantic drift was accepted")
			}
		})
	}
	if _, err := compareWorkspaceSemantics(errServer, strings.Replace(errClient, `aria-describedby="proposedCompensation-error"`, `aria-describedby="other-error"`, 1)); err == nil {
		t.Fatal("error relationship drift was accepted")
	}
}

func TestTodo_ALIGN_057_Conformance(t *testing.T) {
	semantic := assertWorkspaceParity(t, testdata.PromotionFixture())
	if got := semantic.Landmarks[0]; got.Tag != "header" {
		t.Fatalf("first landmark=%+v, want application header", got)
	}
	if got := semantic.Landmarks[2]; got.Tag != "main" || got.ID != "main-content" {
		t.Fatalf("primary landmark=%+v, want main#main-content", got)
	}
	if len(semantic.Errors) != 0 {
		t.Fatalf("clean fixture unexpectedly exposes errors: %+v", semantic.Errors)
	}
	if len(semantic.Statuses) != 1 || semantic.Statuses[0].Role != "status" || semantic.Statuses[0].Live != "polite" {
		t.Fatalf("live loading/status contract=%+v", semantic.Statuses)
	}
}

func TestTodo_ALIGN_057_ErrorState(t *testing.T) {
	c := testdata.PromotionFixture()
	for i := range c.Request.Fields {
		if c.Request.Fields[i].ID == "proposedCompensation" {
			c.Request.Fields[i].Validation.Message = "Enter an amount within the approved compensation band."
		}
	}
	semantic := assertWorkspaceParity(t, c)
	if len(semantic.Errors) != 1 || semantic.Errors[0].ID != "proposedCompensation-error" {
		t.Fatalf("error projection=%+v", semantic.Errors)
	}
	for _, form := range semantic.Forms {
		for _, control := range form.Controls {
			if control.ID == "proposedCompensation" && (control.Invalid != "true" || control.DescribedBy != "proposedCompensation-error") {
				t.Fatalf("errored control lost relationship: %+v", control)
			}
		}
	}
}
