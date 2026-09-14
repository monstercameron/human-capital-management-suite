package closurewitness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrInvalidAsOf is returned when a snapshot's as-of date is not an exact
// YYYY-MM-DD date: without it no waiver can be judged current, and the
// compiler refuses rather than guessing a clock.
var ErrInvalidAsOf = errors.New("closurewitness: as-of must be a YYYY-MM-DD date")

// Endpoint disposition categories read from internal/transport/manifest.
const (
	categoryTyped      = "TYPED_PUBLIC_METHOD"
	categoryGeneric    = "GENERIC_INTENT_LIFECYCLE_ONLY"
	categoryInternal   = "INTERNAL_CAPABILITY_ONLY"
	categoryEvent      = "EVENT_OR_SCHEDULE_ONLY"
	categoryNoEndpoint = "NO_ENDPOINT_WITH_JUSTIFICATION"
)

// maturityDefined is the coverage-registry state that claims closed
// evidence (tools/planning/coveragematrix StateDefined).
const maturityDefined = "DEFINED"

// Compile builds exactly one witness per source-bound definition in snap
// plus every orphan edge no witness can own. It never fails on a closure
// gap; it fails only on an unusable as-of date.
func Compile(snap Snapshot) (Report, error) {
	if !validDate(snap.AsOf) {
		return Report{}, fmt.Errorf("%w: %q", ErrInvalidAsOf, snap.AsOf)
	}
	ix := newIndex(snap)

	builders := make([]*builder, 0, len(ix.acceptedList))
	for _, def := range ix.acceptedList {
		builders = append(builders, ix.witness(def))
	}
	applySharedTests(builders)

	report := Report{SchemaVersion: SchemaVersion, AsOf: snap.AsOf}
	for _, b := range builders {
		report.Witnesses = append(report.Witnesses, b.finalize())
	}
	report.Orphans = sortDefects(ix.orphans())
	report.Totals = summarize(report.Witnesses, report.Orphans)
	report.Result = ResultComplete
	if report.Totals.Incomplete > 0 || len(report.Orphans) > 0 {
		report.Result = ResultIncomplete
	}
	report.Digest = digestOf(report)
	return report, nil
}

// MarshalReport renders a report as canonical indented JSON. encoding/json
// sorts map keys, and every slice is already sorted by Compile, so the
// bytes are a pure function of the snapshot.
func MarshalReport(report Report) ([]byte, error) {
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("closurewitness: marshal report: %w", err)
	}
	return append(out, '\n'), nil
}

// Verify recompiles snap and returns one exact mismatch for every place a
// supplied report disagrees with fresh registry data or with its own
// digests: a hand-edited result, a dropped defect or a swapped witness is
// caught here even when the attacker recomputed nothing.
func Verify(snap Snapshot, report Report) []string {
	var mismatches []string
	fresh, err := Compile(snap)
	if err != nil {
		return []string{err.Error()}
	}
	if report.Digest != digestOf(report) {
		mismatches = append(mismatches, "report digest does not match its own content")
	}
	if report.Digest != fresh.Digest {
		mismatches = append(mismatches, fmt.Sprintf("report digest %s differs from the fresh compilation %s", report.Digest, fresh.Digest))
	}
	freshByDef := make(map[string]Witness, len(fresh.Witnesses))
	for _, w := range fresh.Witnesses {
		freshByDef[w.Definition] = w
	}
	seen := map[string]bool{}
	for _, w := range report.Witnesses {
		if seen[w.Definition] {
			mismatches = append(mismatches, "definition "+w.Definition+" has more than one witness")
			continue
		}
		seen[w.Definition] = true
		if w.Digest != witnessDigest(w) {
			mismatches = append(mismatches, "witness "+w.Definition+" digest does not match its own content")
		}
		want, ok := freshByDef[w.Definition]
		if !ok {
			mismatches = append(mismatches, "witness "+w.Definition+" is not a source-bound definition")
			continue
		}
		if w.Digest != want.Digest {
			mismatches = append(mismatches, "witness "+w.Definition+" differs from the fresh compilation")
		}
	}
	for def := range freshByDef {
		if !seen[def] {
			mismatches = append(mismatches, "source-bound definition "+def+" has no witness")
		}
	}
	sort.Strings(mismatches)
	return mismatches
}

// Summary renders the coverage view generated purely from witnesses: the
// per-class state counts and the incomplete definitions with their defect
// counts. No count in it is maintained anywhere else.
func Summary(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "closure witnesses as of %s: %s\n", report.AsOf, report.Result)
	fmt.Fprintf(&b, "  definitions=%d complete=%d incomplete=%d orphan_edges=%d digest=%s\n",
		report.Totals.Definitions, report.Totals.Complete, report.Totals.Incomplete, report.Totals.Orphans, report.Digest)
	for _, class := range Classes() {
		counts := report.Totals.ByClassState[class]
		states := make([]string, 0, len(counts))
		for state := range counts {
			states = append(states, string(state))
		}
		sort.Slice(states, func(i, j int) bool { return stateRank(EdgeState(states[i])) < stateRank(EdgeState(states[j])) })
		parts := make([]string, 0, len(states))
		for _, state := range states {
			parts = append(parts, fmt.Sprintf("%s=%d", state, counts[EdgeState(state)]))
		}
		fmt.Fprintf(&b, "  %-11s %s\n", class, strings.Join(parts, " "))
	}
	codes := make([]string, 0, len(report.Totals.ByDefectCode))
	for code := range report.Totals.ByDefectCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		fmt.Fprintf(&b, "  defect %s=%d\n", code, report.Totals.ByDefectCode[code])
	}
	for _, w := range report.Witnesses {
		fmt.Fprintf(&b, "  %s %s defects=%d expiry=%s\n", w.Result, w.Definition, len(w.Defects), w.Expiry)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// index

type index struct {
	snap         Snapshot
	acceptedList []string
	accepted     map[string]bool
	acceptedBare map[string]bool
	displayNames map[string]bool
	unsourced    map[EdgeClass]bool

	descriptors    map[string][]DescriptorRow
	catalog        map[string][]CatalogRow
	ceiling        map[string][]CeilingRow
	slices         map[string][]SliceRow
	models         map[string][]ModelBindingRow
	modelGaps      map[string][]ModelGapRow
	engines        map[string][]EngineRow
	published      map[string][]CapabilityRow
	claimsByDef    map[string][]ClaimRow
	claimsByCap    map[string][]ClaimRow
	entries        map[string][]BindingEntryRow
	gaps           map[string][]BindingGapRow
	dispositions   map[string][]IntentDispositionRow
	endpointsByDef map[string][]EndpointRow
	endpointByID   map[string][]EndpointRow
	scenarios      map[string][]ScenarioRow
	todos          map[string][]TodoRow
	todoClaims     map[string][]TodoClaimRow
	coverage       map[string][]CoverageRow
	waivers        map[string][]WaiverRow
}

func newIndex(snap Snapshot) *index {
	ix := &index{
		snap:           snap,
		accepted:       map[string]bool{},
		acceptedBare:   map[string]bool{},
		displayNames:   map[string]bool{},
		unsourced:      map[EdgeClass]bool{},
		descriptors:    map[string][]DescriptorRow{},
		catalog:        map[string][]CatalogRow{},
		ceiling:        map[string][]CeilingRow{},
		slices:         map[string][]SliceRow{},
		models:         map[string][]ModelBindingRow{},
		modelGaps:      map[string][]ModelGapRow{},
		engines:        map[string][]EngineRow{},
		published:      map[string][]CapabilityRow{},
		claimsByDef:    map[string][]ClaimRow{},
		claimsByCap:    map[string][]ClaimRow{},
		entries:        map[string][]BindingEntryRow{},
		gaps:           map[string][]BindingGapRow{},
		dispositions:   map[string][]IntentDispositionRow{},
		endpointsByDef: map[string][]EndpointRow{},
		endpointByID:   map[string][]EndpointRow{},
		scenarios:      map[string][]ScenarioRow{},
		todos:          map[string][]TodoRow{},
		todoClaims:     map[string][]TodoClaimRow{},
		coverage:       map[string][]CoverageRow{},
		waivers:        map[string][]WaiverRow{},
	}
	for _, class := range snap.Unsourced {
		ix.unsourced[class] = true
	}
	for _, d := range snap.Descriptors {
		if !ix.accepted[d.Definition] {
			ix.acceptedList = append(ix.acceptedList, d.Definition)
		}
		ix.accepted[d.Definition] = true
		ix.acceptedBare[bareID(d.Definition)] = true
		ix.displayNames[d.DisplayName] = true
		ix.descriptors[d.Definition] = append(ix.descriptors[d.Definition], d)
	}
	sort.Strings(ix.acceptedList)
	for _, c := range snap.Catalog {
		ix.catalog[c.Definition] = append(ix.catalog[c.Definition], c)
	}
	for _, c := range snap.Ceiling {
		ix.ceiling[c.Definition] = append(ix.ceiling[c.Definition], c)
	}
	for _, s := range snap.Slices {
		for _, def := range uniqueSorted(s.Intents) {
			ix.slices[def] = append(ix.slices[def], s)
		}
	}
	for _, m := range snap.ModelBindings {
		ix.models[m.Definition] = append(ix.models[m.Definition], m)
	}
	for _, g := range snap.ModelGaps {
		ix.modelGaps[g.Definition] = append(ix.modelGaps[g.Definition], g)
	}
	for _, e := range snap.Engines {
		ix.engines[e.Intent] = append(ix.engines[e.Intent], e)
	}
	for _, c := range snap.Capabilities {
		ix.published[c.CapabilityID] = append(ix.published[c.CapabilityID], c)
	}
	for _, c := range snap.Claims {
		ix.claimsByCap[c.CapabilityID] = append(ix.claimsByCap[c.CapabilityID], c)
		if c.DefinitionRef != "" {
			ix.claimsByDef[c.DefinitionRef] = append(ix.claimsByDef[c.DefinitionRef], c)
		}
	}
	for _, e := range snap.BindingEntries {
		ix.entries[e.CapabilityID] = append(ix.entries[e.CapabilityID], e)
	}
	for _, g := range snap.BindingGaps {
		ix.gaps[g.CapabilityID] = append(ix.gaps[g.CapabilityID], g)
	}
	for _, d := range snap.IntentDispositions {
		ix.dispositions[d.Definition] = append(ix.dispositions[d.Definition], d)
	}
	for _, e := range snap.Endpoints {
		ix.endpointByID[e.EndpointID] = append(ix.endpointByID[e.EndpointID], e)
		for _, def := range uniqueSorted(e.AcceptedDefinitions) {
			ix.endpointsByDef[def] = append(ix.endpointsByDef[def], e)
		}
	}
	for _, s := range snap.Scenarios {
		ix.scenarios[s.Definition] = append(ix.scenarios[s.Definition], s)
	}
	for _, t := range snap.Todos {
		ix.todos[t.ID] = append(ix.todos[t.ID], t)
	}
	for _, c := range snap.TodoClaims {
		ix.todoClaims[c.Intent] = append(ix.todoClaims[c.Intent], c)
	}
	for _, c := range snap.Coverage {
		ix.coverage[c.Intent] = append(ix.coverage[c.Intent], c)
	}
	for _, w := range snap.Waivers {
		ix.waivers[w.TodoID] = append(ix.waivers[w.TodoID], w)
	}
	return ix
}

// ---------------------------------------------------------------------------
// builder

type builder struct {
	w         Witness
	justified map[EdgeClass]bool
	edges     []Edge
	defects   []Defect
	tests     map[string]bool
}

func (b *builder) edge(class EdgeClass, dir Direction, from, to, registry string) {
	b.edges = append(b.edges, Edge{Class: class, Direction: dir, From: from, To: to, Registry: registry})
}

func (b *builder) defect(code string, class EdgeClass, identity, detail string) {
	b.defects = append(b.defects, Defect{Result: ResultIncomplete, Code: code, Class: class, Identity: identity, Detail: detail})
}

func (b *builder) finalize() Witness {
	if b.w.Phase.ClaimedMaturity == maturityDefined && len(b.defects) > 0 {
		b.defect(DefectMaturityOverclaim, ClassPhaseGate, ident(ClassPhaseGate, b.w.Definition, "maturity", maturityDefined),
			fmt.Sprintf("the coverage registry claims %s while %d closure defects remain", maturityDefined, len(b.defects)))
	}
	w := b.w
	w.Edges = sortEdges(b.edges)
	w.Defects = sortDefects(b.defects)
	w.Tests = sortedKeys(b.tests)
	sort.Slice(w.Evidence, func(i, j int) bool { return w.Evidence[i].Todo < w.Evidence[j].Todo })
	w.Expiry = NoWaiver
	for _, ref := range w.Evidence {
		if ref.Expiry != NoWaiver && (w.Expiry == NoWaiver || ref.Expiry < w.Expiry) {
			w.Expiry = ref.Expiry
		}
	}
	for _, class := range Classes() {
		state := StateBound
		if b.justified[class] {
			state = StateJustified
		}
		for _, d := range w.Defects {
			if d.Class == class && stateRank(defectState(d.Code)) < stateRank(state) {
				state = defectState(d.Code)
			}
		}
		w.Classes = append(w.Classes, ClassStatus{Class: class, State: state})
	}
	w.Result = ResultComplete
	if len(w.Defects) > 0 {
		w.Result = ResultIncomplete
	}
	if w.Tests == nil {
		w.Tests = []string{}
	}
	if w.Evidence == nil {
		w.Evidence = []EvidenceRef{}
	}
	if w.Edges == nil {
		w.Edges = []Edge{}
	}
	if w.Defects == nil {
		w.Defects = []Defect{}
	}
	if w.EndpointDisposition.ServingEndpoints == nil {
		w.EndpointDisposition.ServingEndpoints = []string{}
	}
	w.Digest = witnessDigest(w)
	return w
}

// ---------------------------------------------------------------------------
// per-definition rules

func (ix *index) witness(def string) *builder {
	b := &builder{justified: map[EdgeClass]bool{}, tests: map[string]bool{}}
	b.w.Definition = def
	for _, class := range Classes() {
		if ix.unsourced[class] {
			b.defect(DefectUnsourced, class, ident(class, def, "registry"),
				"no registry the loader could read supplies "+string(class)+" edges, so this class cannot be closed")
		}
	}
	ix.sourceRules(b, def)
	ix.phaseRules(b, def)
	ix.sliceRules(b, def)
	ix.modelRules(b, def)
	ix.engineRules(b, def)
	ix.capabilityRules(b, def)
	ix.endpointDispositionRules(b, def)
	ix.scenarioRules(b, def)
	ix.todoTestEvidenceRules(b, def)
	return b
}

func (ix *index) sourceRules(b *builder, def string) {
	descs := ix.descriptors[def]
	d := descs[0]
	b.w.DisplayName = d.DisplayName
	b.w.Source = SourceBinding{Registry: RegistryDescriptors, RowDigest: d.RowDigest}
	b.w.Phase.DescriptorPhase = d.Phase
	if ix.unsourced[ClassSource] {
		return
	}
	b.edge(ClassSource, Forward, "descriptor:"+def, def, RegistryDescriptors)
	if len(descs) > 1 {
		b.defect(DefectDuplicate, ClassSource, ident(ClassSource, def, "descriptor"),
			fmt.Sprintf("%d descriptor rows declare this definition; exactly one source row is required", len(descs)))
	}
	if strings.TrimSpace(d.RowDigest) == "" {
		b.defect(DefectStale, ClassSource, ident(ClassSource, def, "row_digest"), "the source row carries no content digest")
	}
	cats := ix.catalog[def]
	switch len(cats) {
	case 0:
		b.defect(DefectReverseAbsent, ClassSource, ident(ClassSource, def, "catalog"), "no compiled intent definition carries this definition ref")
	case 1:
		b.edge(ClassSource, Reverse, "catalog:"+def, def, RegistryCatalog)
		b.w.Phase.Release = cats[0].Release
		if cats[0].DisplayName != d.DisplayName {
			b.defect(DefectStale, ClassSource, ident(ClassSource, def, "display_name"),
				fmt.Sprintf("descriptor names %q but the compiled definition names %q", d.DisplayName, cats[0].DisplayName))
		}
	default:
		b.w.Phase.Release = cats[0].Release
		b.defect(DefectDuplicate, ClassSource, ident(ClassSource, def, "catalog"),
			fmt.Sprintf("%d compiled definitions carry this definition ref", len(cats)))
	}
}

// gateForRelease joins the compiled release vocabulary to the scope
// ceiling's gate vocabulary; both registries spell P1A/P1B identically and
// the ceiling records a conformance fixture as gate NONE.
func gateForRelease(release string) string {
	switch release {
	case "P1A":
		return "P1A"
	case "P1B":
		return "P1B"
	case "CONFORMANCE":
		return "NONE"
	}
	return ""
}

func (ix *index) phaseRules(b *builder, def string) {
	if ix.unsourced[ClassPhaseGate] {
		return
	}
	if strings.TrimSpace(b.w.Phase.DescriptorPhase) == "" {
		b.defect(DefectAbsent, ClassPhaseGate, ident(ClassPhaseGate, def, "descriptor_phase"), "the source descriptor names no phase")
	} else {
		b.edge(ClassPhaseGate, Forward, def, "phase:"+b.w.Phase.DescriptorPhase, RegistryDescriptors)
	}
	rows := ix.ceiling[def]
	switch len(rows) {
	case 0:
		b.defect(DefectReverseAbsent, ClassPhaseGate, ident(ClassPhaseGate, def, "ceiling"), "the Phase 1 scope ceiling has no row for this definition")
		return
	case 1:
	default:
		b.defect(DefectDuplicate, ClassPhaseGate, ident(ClassPhaseGate, def, "ceiling"),
			fmt.Sprintf("the Phase 1 scope ceiling carries %d rows for this definition", len(rows)))
	}
	row := rows[0]
	b.w.Phase.CeilingGate = row.Gate
	b.w.Phase.CeilingDisposition = row.Disposition
	b.edge(ClassPhaseGate, Reverse, "ceiling:gate:"+row.Gate, def, RegistryCeiling)
	if b.w.Phase.Release == "" {
		return
	}
	want := gateForRelease(b.w.Phase.Release)
	switch {
	case want == "":
		b.defect(DefectStale, ClassPhaseGate, ident(ClassPhaseGate, def, "release", b.w.Phase.Release),
			"the compiled release has no scope-ceiling gate it can join to")
	case want != row.Gate:
		b.defect(DefectStale, ClassPhaseGate, ident(ClassPhaseGate, def, "gate", row.Gate),
			fmt.Sprintf("the compiled release %s requires ceiling gate %s but the ceiling records %s", b.w.Phase.Release, want, row.Gate))
	}
}

func (ix *index) claimedCapabilities(def string) []ClaimRow {
	claims := append([]ClaimRow(nil), ix.claimsByDef[def]...)
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].CapabilityID != claims[j].CapabilityID {
			return claims[i].CapabilityID < claims[j].CapabilityID
		}
		return claims[i].Version < claims[j].Version
	})
	return claims
}

func (ix *index) sliceRules(b *builder, def string) {
	if ix.unsourced[ClassSlice] {
		return
	}
	slices := ix.slices[def]
	switch {
	case len(slices) == 0:
		b.defect(DefectAbsent, ClassSlice, ident(ClassSlice, def), "no product slice names this definition among its business intents")
	case len(slices) > 1:
		ids := make([]string, 0, len(slices))
		for _, s := range slices {
			ids = append(ids, s.SliceID)
		}
		sort.Strings(ids)
		b.defect(DefectDuplicate, ClassSlice, ident(ClassSlice, def, strings.Join(ids, ",")),
			"more than one product slice claims this definition; one reviewable slice is required")
	}
	for _, s := range slices {
		b.edge(ClassSlice, Reverse, fmt.Sprintf("slice:%s@v%d", s.SliceID, s.Version), def, RegistrySlices)
		if !s.DigestVerified {
			b.defect(DefectStale, ClassSlice, ident(ClassSlice, s.SliceID, "digest"), "the product-slice registry digest does not verify against its content")
		}
		for _, claim := range ix.claimedCapabilities(def) {
			if !contains(s.Capabilities, claim.CapabilityID) {
				b.defect(DefectStale, ClassSlice, ident(ClassSlice, s.SliceID, claim.CapabilityID),
					"the slice names this definition but not the capability that implements it")
			} else {
				b.edge(ClassSlice, Forward, fmt.Sprintf("slice:%s@v%d", s.SliceID, s.Version), "capability:"+claim.CapabilityID, RegistrySlices)
			}
		}
	}
}

func (ix *index) modelRules(b *builder, def string) {
	if ix.unsourced[ClassModel] {
		return
	}
	rows := ix.models[def]
	gaps := ix.modelGaps[def]
	for _, g := range gaps {
		b.defect(DefectStale, ClassModel, ident(ClassModel, def, g.Element), g.Detail)
	}
	if len(rows) == 0 && len(gaps) == 0 {
		b.defect(DefectAbsent, ClassModel, ident(ClassModel, def), "no model binding resolves this definition against the generated model registry")
	}
	if len(rows) > 1 {
		b.defect(DefectDuplicate, ClassModel, ident(ClassModel, def), fmt.Sprintf("%d model bindings resolve this definition", len(rows)))
	}
	for _, row := range rows {
		if len(row.Entities) == 0 {
			b.defect(DefectAbsent, ClassModel, ident(ClassModel, def, "entities"), "the model binding names no generated entity")
		}
		for _, entity := range uniqueSorted(row.Entities) {
			b.edge(ClassModel, Forward, def, "entity:"+entity, RegistryModel)
		}
	}
}

func (ix *index) engineRules(b *builder, def string) {
	if ix.unsourced[ClassEngine] {
		return
	}
	rows := ix.engines[bareID(def)]
	if len(rows) == 0 {
		b.defect(DefectAbsent, ClassEngine, ident(ClassEngine, def), "no reusable-computation responsibility names this definition")
	}
	for _, row := range rows {
		target := "engine:" + row.Package + "#" + row.Computation
		b.edge(ClassEngine, Forward, def, target, RegistryEngine)
		findings := uniqueSorted(row.Findings)
		for _, kind := range findings {
			id := ident(ClassEngine, def, row.Package, row.Computation)
			switch kind {
			case "IMPLICIT":
				b.defect(DefectReverseAbsent, ClassEngine, id, "the named engine is not imported by any intent definition, app or conformance package")
			case "DUPLICATE_RESPONSIBILITY":
				b.defect(DefectDuplicate, ClassEngine, id, "the computation has more than one engine owner")
			default:
				b.defect(DefectStale, ClassEngine, id, "engine coverage finding "+kind)
			}
		}
		if len(findings) == 0 {
			b.edge(ClassEngine, Reverse, target, def, RegistryEngine)
		}
	}
}

func (ix *index) capabilityRules(b *builder, def string) {
	claims := ix.claimedCapabilities(def)
	if !ix.unsourced[ClassCapability] {
		switch {
		case len(claims) == 0:
			b.defect(DefectAbsent, ClassCapability, ident(ClassCapability, def), "no reviewed capability claim binds this definition")
		case len(claims) > 1:
			b.defect(DefectDuplicate, ClassCapability, ident(ClassCapability, def),
				fmt.Sprintf("%d capability claims bind this definition; exactly one capability is required", len(claims)))
		}
	}
	if len(claims) == 0 && !ix.unsourced[ClassHandler] {
		b.defect(DefectAbsent, ClassHandler, ident(ClassHandler, def), "no capability binds this definition, so no typed handler can")
	}
	generic := false
	if rows := ix.dispositions[def]; len(rows) > 0 && rows[0].Category == categoryGeneric {
		generic = true
	}
	for _, claim := range claims {
		key := fmt.Sprintf("%s/v%d", claim.CapabilityID, claim.Version)
		if !ix.unsourced[ClassCapability] {
			ix.capabilityClaimRules(b, def, claim, key)
		}
		ix.bindingRules(b, def, claim, generic)
	}
}

func (ix *index) capabilityClaimRules(b *builder, def string, claim ClaimRow, key string) {
	b.edge(ClassCapability, Forward, def, "capability:"+key, RegistryClaims)
	published := ix.published[claim.CapabilityID]
	versionMatch := false
	for _, p := range published {
		if p.Version == claim.Version {
			versionMatch = true
		}
	}
	switch {
	case len(published) == 0:
		b.defect(DefectReverseAbsent, ClassCapability, ident(ClassCapability, def, key), "the claim names a capability the BOOTSTRAP registry does not publish")
	case !versionMatch:
		b.defect(DefectStale, ClassCapability, ident(ClassCapability, def, key), "the BOOTSTRAP registry publishes this capability only at another version")
	default:
		b.edge(ClassCapability, Reverse, "capability:"+key, def, RegistryBootstrap)
	}
	if len(ix.claimsByCap[claim.CapabilityID]) > 1 {
		b.defect(DefectDuplicate, ClassCapability, ident(ClassCapability, claim.CapabilityID, "claims"),
			fmt.Sprintf("%d reviewed claims name this capability", len(ix.claimsByCap[claim.CapabilityID])))
	}
}

func (ix *index) bindingRules(b *builder, def string, claim ClaimRow, generic bool) {
	capID := claim.CapabilityID
	handlerGap, wireGap := false, false
	for _, g := range sortGaps(ix.gaps[capID]) {
		detail := "binding gap " + g.Kind
		if g.OwnerTodo != "" {
			detail += " (allowlisted; closing owner " + g.OwnerTodo + ")"
		}
		switch g.Kind {
		case "NO_HANDLER":
			handlerGap = true
			ix.classDefect(b, DefectAbsent, ClassHandler, ident(ClassHandler, capID), detail)
		case "AMBIGUOUS_HANDLER":
			handlerGap = true
			ix.classDefect(b, DefectDuplicate, ClassHandler, ident(ClassHandler, capID, g.Subject), detail)
		case "MISSING_HANDLER_SYMBOL":
			handlerGap = true
			ix.classDefect(b, DefectStale, ClassHandler, ident(ClassHandler, capID, g.Subject), detail)
		case "NO_WIRE_METHOD":
			wireGap = true
			ix.classDefect(b, DefectAbsent, ClassEndpoint, ident(ClassEndpoint, capID), detail)
		case "AMBIGUOUS_WIRE_METHOD":
			wireGap = true
			if generic {
				ix.classDefect(b, DefectAggregateOnly, ClassEndpoint, ident(ClassEndpoint, capID, g.Subject),
					detail+": every candidate route is a generic lifecycle method shared across definitions")
			} else {
				ix.classDefect(b, DefectDuplicate, ClassEndpoint, ident(ClassEndpoint, capID, g.Subject), detail)
			}
		case "UNKNOWN_WIRE_METHOD", "STREAMING_WIRE_METHOD":
			wireGap = true
			ix.classDefect(b, DefectStale, ClassEndpoint, ident(ClassEndpoint, capID, g.Subject), detail)
		case "NO_MODEL_BINDING":
			ix.classDefect(b, DefectStale, ClassModel, ident(ClassModel, def, "capability:"+capID), detail)
		default:
			ix.classDefect(b, DefectStale, ClassCapability, ident(ClassCapability, capID, g.Kind, g.Subject), detail)
		}
	}
	entries := ix.entries[capID]
	switch len(entries) {
	case 0:
		if !handlerGap {
			ix.classDefect(b, DefectAbsent, ClassHandler, ident(ClassHandler, capID, "entry"),
				"the binding table produced no verified entry for this capability, so no single typed handler is proven")
		}
		if !wireGap {
			ix.classDefect(b, DefectAbsent, ClassEndpoint, ident(ClassEndpoint, capID, "entry"),
				"the binding table produced no verified entry for this capability, so no single wire descriptor is proven")
		}
	case 1:
		e := entries[0]
		if !ix.unsourced[ClassHandler] {
			b.edge(ClassHandler, Forward, "capability:"+capID, "handler:"+e.Handler, RegistryBinding)
		}
		if !ix.unsourced[ClassEndpoint] {
			b.edge(ClassEndpoint, Forward, "handler:"+e.Handler, "endpoint:"+e.Wire, RegistryBinding)
		}
	default:
		ix.classDefect(b, DefectDuplicate, ClassHandler, ident(ClassHandler, capID, "entries"),
			fmt.Sprintf("%d bound entries exist for this capability", len(entries)))
	}
}

// classDefect records a defect unless its class is unsourced (the
// UNSOURCED defect already names that class).
func (ix *index) classDefect(b *builder, code string, class EdgeClass, identity, detail string) {
	if ix.unsourced[class] {
		return
	}
	b.defect(code, class, identity, detail)
}

func (ix *index) endpointDispositionRules(b *builder, def string) {
	if ix.unsourced[ClassEndpoint] {
		return
	}
	rows := ix.dispositions[def]
	switch len(rows) {
	case 0:
		b.defect(DefectReverseAbsent, ClassEndpoint, ident(ClassEndpoint, def, "disposition"), "no endpoint disposition row names this definition")
		return
	case 1:
	default:
		b.defect(DefectDuplicate, ClassEndpoint, ident(ClassEndpoint, def, "disposition"),
			fmt.Sprintf("%d endpoint disposition rows name this definition", len(rows)))
	}
	d := rows[0]
	serving := uniqueSorted(d.ServingEndpoints)
	b.w.EndpointDisposition = EndpointDisposition{Category: d.Category, Justification: d.Justification, ServingEndpoints: serving}
	switch d.Category {
	case categoryNoEndpoint, categoryInternal, categoryEvent:
		if strings.TrimSpace(d.Justification) == "" {
			b.defect(DefectAbsent, ClassEndpoint, ident(ClassEndpoint, def, "justification"), d.Category+" carries no justification")
		} else {
			b.justified[ClassEndpoint] = true
		}
	case categoryGeneric:
		b.defect(DefectAggregateOnly, ClassEndpoint, ident(ClassEndpoint, def, "generic_lifecycle"),
			fmt.Sprintf("reachable only through %d generic lifecycle methods that dispatch every such definition; no endpoint is this definition's own", len(serving)))
	case categoryTyped:
		if len(serving) != 1 {
			code := DefectDuplicate
			if len(serving) == 0 {
				code = DefectAbsent
			}
			b.defect(code, ClassEndpoint, ident(ClassEndpoint, def, "typed"),
				fmt.Sprintf("a typed public method disposition names %d serving endpoints; exactly one is required", len(serving)))
		}
	default:
		b.defect(DefectStale, ClassEndpoint, ident(ClassEndpoint, def, "category", d.Category), "unknown endpoint disposition category")
	}
	for _, ep := range serving {
		manifestRows := ix.endpointByID[ep]
		switch {
		case len(manifestRows) == 0:
			b.defect(DefectStale, ClassEndpoint, ident(ClassEndpoint, def, ep), "the disposition names a serving endpoint absent from the endpoint manifest")
		case !contains(manifestRows[0].AcceptedDefinitions, def):
			b.defect(DefectStale, ClassEndpoint, ident(ClassEndpoint, def, ep), "the endpoint manifest row does not accept this definition")
		default:
			b.edge(ClassEndpoint, Forward, def, "endpoint:"+ep, RegistryDisposition)
		}
	}
	for _, row := range ix.endpointsByDef[def] {
		b.edge(ClassEndpoint, Reverse, "endpoint:"+row.EndpointID, def, RegistryEndpoints)
	}
}

func (ix *index) scenarioRules(b *builder, def string) {
	if ix.unsourced[ClassScenario] {
		return
	}
	rows := ix.scenarios[def]
	switch {
	case len(rows) == 0:
		b.defect(DefectAbsent, ClassScenario, ident(ClassScenario, def), "no adversarial scenario matrix is generated for this definition")
	case len(rows) > 1:
		b.defect(DefectDuplicate, ClassScenario, ident(ClassScenario, def), fmt.Sprintf("%d scenario matrices exist for this definition", len(rows)))
	}
	for _, row := range rows {
		for _, finding := range uniqueSorted(row.Findings) {
			b.defect(DefectStale, ClassScenario, ident(ClassScenario, def, finding), "scenario generation finding")
		}
		ids := uniqueSorted(row.ScenarioIDs)
		if len(ids) == 0 && row.NotApplicable == 0 {
			b.defect(DefectAbsent, ClassScenario, ident(ClassScenario, def, "scenarios"), "the scenario matrix carries zero scenarios and zero typed justifications")
		}
		for _, id := range ids {
			b.edge(ClassScenario, Forward, def, "scenario:"+id, RegistryScenarios)
		}
	}
}

func (ix *index) todoTestEvidenceRules(b *builder, def string) {
	bare := bareID(def)
	var direct []string
	domain := 0
	for _, c := range ix.todoClaims[bare] {
		switch c.Kind {
		case ClaimDirect:
			direct = append(direct, c.TodoID)
		default:
			domain++
		}
	}
	direct = uniqueSorted(direct)

	if !ix.unsourced[ClassTodo] {
		if len(direct) == 0 {
			if domain > 0 {
				b.defect(DefectAggregateOnly, ClassTodo, ident(ClassTodo, def, "domain_only"),
					fmt.Sprintf("%d todos reach this definition only through a SETS domain token; none names it exactly", domain))
			} else {
				b.defect(DefectAbsent, ClassTodo, ident(ClassTodo, def), "no todo names this definition")
			}
		}
	}

	var done, open []TodoRow
	for _, id := range direct {
		rows := ix.todos[id]
		if !ix.unsourced[ClassTodo] {
			b.edge(ClassTodo, Forward, def, "todo:"+id, RegistryTodos)
		}
		switch {
		case len(rows) == 0:
			ix.classDefect(b, DefectStale, ClassTodo, ident(ClassTodo, def, id), "the claim names a todo absent from the backlog")
		case len(rows) > 1:
			ix.classDefect(b, DefectDuplicate, ClassTodo, ident(ClassTodo, def, id), "the backlog carries this todo id more than once")
		case rows[0].Retired:
			ix.classDefect(b, DefectStale, ClassTodo, ident(ClassTodo, def, id), "the bound todo is RETIRED; a retired todo cannot deliver a definition")
		case rows[0].Done:
			done = append(done, rows[0])
		default:
			open = append(open, rows[0])
		}
	}

	var coverage *CoverageRow
	if !ix.unsourced[ClassTodo] {
		rows := ix.coverage[bare]
		switch len(rows) {
		case 0:
			b.defect(DefectReverseAbsent, ClassTodo, ident(ClassTodo, def, "coverage_registry"), "the checked-in coverage registry has no intent row for this definition")
		case 1:
		default:
			b.defect(DefectDuplicate, ClassTodo, ident(ClassTodo, def, "coverage_registry"), "the checked-in coverage registry has more than one intent row for this definition")
		}
		if len(rows) > 0 {
			coverage = &rows[0]
			b.w.Phase.ClaimedMaturity = coverage.State
			registered := toSet(coverage.DirectTodos)
			fresh := toSet(direct)
			for _, id := range direct {
				if registered[id] {
					b.edge(ClassTodo, Reverse, "coverage:todo:"+id, def, RegistryCoverage)
				} else {
					b.defect(DefectStale, ClassTodo, ident(ClassTodo, def, id, "coverage_registry"), "todos.md claims this definition but the checked-in coverage registry does not list the claim")
				}
			}
			for _, id := range uniqueSorted(coverage.DirectTodos) {
				if !fresh[id] {
					b.defect(DefectStale, ClassTodo, ident(ClassTodo, def, id, "coverage_registry"), "the checked-in coverage registry lists a DIRECT claim todos.md no longer makes")
				}
			}
		}
	}

	evidenceTests := map[string]bool{}
	for _, todo := range done {
		ref := EvidenceRef{Todo: todo.ID, Digest: todo.EvidenceDigest, Dates: uniqueSorted(todo.EvidenceDates), Expiry: NoWaiver}
		if len(todo.EvidenceTests) == 0 {
			ix.classDefect(b, DefectAbsent, ClassEvidence, ident(ClassEvidence, def, todo.ID), "the ticked todo names no evidence test; prose completion cannot close a definition")
		}
		for _, name := range uniqueSorted(todo.EvidenceTests) {
			if ix.snap.TestExists[name] {
				ref.Tests = append(ref.Tests, name)
				evidenceTests[name] = true
				b.tests[name] = true
				ix.classEdge(b, ClassTest, Forward, "todo:"+todo.ID, "test:"+name, RegistryTestSources)
			} else {
				ix.classDefect(b, DefectStale, ClassTest, ident(ClassTest, def, todo.ID, name), "the evidence names a test that exists in no scanned test source")
			}
		}
		if todo.PrimaryTest != "" {
			if ix.snap.TestExists[todo.PrimaryTest] {
				b.tests[todo.PrimaryTest] = true
				ix.classEdge(b, ClassTest, Forward, "todo:"+todo.ID, "test:"+todo.PrimaryTest, RegistryTestSources)
			} else {
				ix.classDefect(b, DefectStale, ClassTest, ident(ClassTest, def, todo.ID, todo.PrimaryTest), "the ticked todo's PRIMARY test exists in no scanned test source")
			}
		}
		if strings.TrimSpace(todo.EvidenceDigest) == "" {
			ix.classDefect(b, DefectAbsent, ClassEvidence, ident(ClassEvidence, def, todo.ID, "digest"), "the ticked todo carries no evidence text to digest")
		} else {
			ix.classEdge(b, ClassEvidence, Forward, "todo:"+todo.ID, "evidence:"+todo.EvidenceDigest, RegistryTodos)
		}
		for _, waiver := range ix.waivers[todo.ID] {
			if !validDate(waiver.Expiry) {
				ix.classDefect(b, DefectStale, ClassEvidence, ident(ClassEvidence, def, todo.ID, "waiver", waiver.Issue), "the evidence-freshness waiver carries no valid expiry date")
				continue
			}
			if ref.Expiry == NoWaiver || waiver.Expiry < ref.Expiry {
				ref.Expiry = waiver.Expiry
			}
			if waiver.Expiry < ix.snap.AsOf {
				ix.classDefect(b, DefectStale, ClassEvidence, ident(ClassEvidence, def, todo.ID, "waiver", waiver.Issue),
					fmt.Sprintf("the evidence-freshness waiver expired on %s (as of %s)", waiver.Expiry, ix.snap.AsOf))
			}
		}
		if ref.Tests == nil {
			ref.Tests = []string{}
		}
		if ref.Dates == nil {
			ref.Dates = []string{}
		}
		b.w.Evidence = append(b.w.Evidence, ref)
	}
	for _, todo := range open {
		ix.classDefect(b, DefectAbsent, ClassEvidence, ident(ClassEvidence, def, todo.ID), "the bound todo is still open and has produced no evidence")
	}
	if len(direct) == 0 {
		ix.classDefect(b, DefectAbsent, ClassEvidence, ident(ClassEvidence, def), "no todo is bound to this definition, so no evidence can exist")
	}
	if len(b.tests) == 0 {
		ix.classDefect(b, DefectAbsent, ClassTest, ident(ClassTest, def), "no existing test is bound to this definition through ticked todos")
	}
	if coverage != nil && !ix.unsourced[ClassTest] {
		registered := toSet(coverage.Tests)
		for _, name := range sortedKeys(evidenceTests) {
			if registered[name] {
				b.edge(ClassTest, Reverse, "coverage:test:"+name, def, RegistryCoverage)
			} else {
				b.defect(DefectStale, ClassTest, ident(ClassTest, def, name, "coverage_registry"), "current evidence cites this test but the checked-in coverage registry does not list it")
			}
		}
		for _, name := range uniqueSorted(coverage.Tests) {
			if !evidenceTests[name] {
				b.defect(DefectStale, ClassTest, ident(ClassTest, def, name, "coverage_registry"), "the checked-in coverage registry lists a test no current evidence cites")
			}
		}
	}
}

func (ix *index) classEdge(b *builder, class EdgeClass, dir Direction, from, to, registry string) {
	if ix.unsourced[class] {
		return
	}
	b.edge(class, dir, from, to, registry)
}

// applySharedTests marks a witness whose every bound test also serves
// another definition: its test edges exist only in aggregate.
func applySharedTests(builders []*builder) {
	owners := map[string]map[string]bool{}
	for _, b := range builders {
		for name := range b.tests {
			if owners[name] == nil {
				owners[name] = map[string]bool{}
			}
			owners[name][b.w.Definition] = true
		}
	}
	for _, b := range builders {
		if len(b.tests) == 0 {
			continue
		}
		shared := true
		for name := range b.tests {
			if len(owners[name]) < 2 {
				shared = false
				break
			}
		}
		if shared {
			b.defect(DefectAggregateOnly, ClassTest, ident(ClassTest, b.w.Definition, "shared"),
				"every test bound to this definition also serves another definition; none proves this definition alone")
		}
	}
}

// ---------------------------------------------------------------------------
// orphans

func (ix *index) orphans() []Defect {
	var out []Defect
	add := func(class EdgeClass, identity, detail string) {
		if ix.unsourced[class] {
			return
		}
		out = append(out, Defect{Result: ResultIncomplete, Code: DefectOrphan, Class: class, Identity: identity, Detail: detail})
	}
	for _, c := range ix.snap.Catalog {
		if !ix.accepted[c.Definition] {
			add(ClassSource, ident(ClassSource, c.Definition, "catalog"), "a compiled definition has no source descriptor")
		}
	}
	for _, c := range ix.snap.Ceiling {
		if !ix.accepted[c.Definition] {
			add(ClassPhaseGate, ident(ClassPhaseGate, c.Definition, "ceiling"), "a scope-ceiling row names no source-bound definition")
		}
	}
	for _, s := range ix.snap.Slices {
		for _, def := range uniqueSorted(s.Intents) {
			if !ix.accepted[def] {
				add(ClassSlice, ident(ClassSlice, s.SliceID, def), "a product slice names a definition that is not source-bound")
			}
		}
	}
	for _, m := range ix.snap.ModelBindings {
		if !ix.accepted[m.Definition] {
			add(ClassModel, ident(ClassModel, m.Definition), "a model binding names no source-bound definition")
		}
	}
	for _, g := range ix.snap.ModelGaps {
		if !ix.accepted[g.Definition] {
			add(ClassModel, ident(ClassModel, g.Definition, g.Element), "a model-binding gap names no source-bound definition")
		}
	}
	for _, e := range ix.snap.Engines {
		if !ix.acceptedBare[e.Intent] {
			add(ClassEngine, ident(ClassEngine, e.Intent, e.Package, e.Computation), "an engine responsibility names no source-bound definition")
		}
	}
	claimedByAccepted := map[string]bool{}
	for _, c := range ix.snap.Claims {
		switch {
		case c.DefinitionRef == "":
			add(ClassCapability, ident(ClassCapability, c.CapabilityID, "definition"), "a reviewed capability claim names no source-bound definition")
		case !ix.accepted[c.DefinitionRef]:
			add(ClassCapability, ident(ClassCapability, c.CapabilityID, c.DefinitionRef), "a reviewed capability claim names a definition that is not source-bound")
		default:
			claimedByAccepted[c.CapabilityID] = true
		}
	}
	for _, p := range ix.snap.Capabilities {
		if len(ix.claimsByCap[p.CapabilityID]) == 0 {
			add(ClassCapability, ident(ClassCapability, p.CapabilityID, "claim"), "a published capability has no reviewed claim")
		}
	}
	for _, g := range ix.snap.BindingGaps {
		if g.CapabilityID != "" && claimedByAccepted[g.CapabilityID] {
			continue
		}
		class := ClassCapability
		switch g.Kind {
		case "WIRE_METHOD_UNBOUND", "NO_WIRE_METHOD", "AMBIGUOUS_WIRE_METHOD", "UNKNOWN_WIRE_METHOD", "STREAMING_WIRE_METHOD":
			class = ClassEndpoint
		case "HANDLER_BOUND_TWICE", "NO_HANDLER", "AMBIGUOUS_HANDLER", "MISSING_HANDLER_SYMBOL":
			class = ClassHandler
		case "NO_MODEL_BINDING":
			class = ClassModel
		}
		detail := "binding gap " + g.Kind + " belongs to no source-bound definition"
		if g.OwnerTodo != "" {
			detail += " (allowlisted; closing owner " + g.OwnerTodo + ")"
		}
		add(class, ident(class, g.Kind, g.CapabilityID, g.Subject), detail)
	}
	for _, d := range ix.snap.IntentDispositions {
		if !ix.accepted[d.Definition] {
			add(ClassEndpoint, ident(ClassEndpoint, d.Definition, "disposition"), "an endpoint disposition names no source-bound definition")
		}
	}
	for _, e := range ix.snap.Endpoints {
		accepted := uniqueSorted(e.AcceptedDefinitions)
		for _, def := range accepted {
			if !ix.accepted[def] {
				add(ClassEndpoint, ident(ClassEndpoint, e.EndpointID, def), "an endpoint accepts a definition that is not source-bound")
			}
		}
		if e.Disposition == "SERVED" && len(accepted) == 0 {
			add(ClassEndpoint, ident(ClassEndpoint, e.EndpointID), "a served endpoint accepts no source-bound definition")
		}
	}
	for _, s := range ix.snap.Scenarios {
		if !ix.accepted[s.Definition] {
			add(ClassScenario, ident(ClassScenario, s.Definition), "a scenario matrix names no source-bound definition")
		}
	}
	for _, t := range ix.snap.ContextTokens {
		switch t.Field {
		case "DIRECT":
			if !ix.displayNames[t.Token] {
				add(ClassTodo, ident(ClassTodo, t.TodoID, "DIRECT", t.Token), "an INTENT CONTEXT DIRECT token names no source-bound definition")
			}
		default:
			if !ix.acceptedBare[bareID(t.Token)] {
				add(ClassTodo, ident(ClassTodo, t.TodoID, t.Field, t.Token), "an INTENT CONTEXT "+t.Field+" token names no source-bound definition")
			}
		}
	}
	for _, c := range ix.snap.TodoClaims {
		if !ix.acceptedBare[c.Intent] {
			add(ClassTodo, ident(ClassTodo, c.TodoID, c.Intent), "a todo claim names no source-bound definition")
		}
	}
	for _, c := range ix.snap.Coverage {
		if !ix.acceptedBare[c.Intent] {
			add(ClassTodo, ident(ClassTodo, "coverage_registry", c.Intent), "a checked-in coverage registry intent row names no source-bound definition")
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// totals, digests and helpers

func summarize(witnesses []Witness, orphans []Defect) Totals {
	totals := Totals{
		Definitions:  len(witnesses),
		Orphans:      len(orphans),
		ByDefectCode: map[string]int{},
		ByClassState: map[EdgeClass]map[EdgeState]int{},
		Incompletes:  []string{},
	}
	for _, class := range Classes() {
		totals.ByClassState[class] = map[EdgeState]int{}
	}
	for _, w := range witnesses {
		if w.Result == ResultComplete {
			totals.Complete++
		} else {
			totals.Incomplete++
			totals.Incompletes = append(totals.Incompletes, w.Definition)
		}
		for _, cs := range w.Classes {
			totals.ByClassState[cs.Class][cs.State]++
		}
		for _, d := range w.Defects {
			totals.ByDefectCode[d.Code]++
		}
	}
	for _, d := range orphans {
		totals.ByDefectCode[d.Code]++
	}
	return totals
}

func witnessDigest(w Witness) string {
	w.Digest = ""
	return sha256JSON(w)
}

func digestOf(report Report) string {
	report.Digest = ""
	return sha256JSON(report)
}

func sha256JSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		// Every field is a string, int, bool, slice or string-keyed map;
		// json.Marshal cannot fail on these types.
		panic("closurewitness: canonical marshal failed: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, r := range s {
		if i == 4 || i == 7 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return s[5:7] >= "01" && s[5:7] <= "12" && s[8:10] >= "01" && s[8:10] <= "31"
}

// bareID strips a definition ref's "/vN" suffix.
func bareID(def string) string {
	if i := strings.LastIndex(def, "/v"); i >= 0 {
		return def[:i]
	}
	return def
}

func ident(class EdgeClass, parts ...string) string {
	return string(class) + "|" + strings.Join(parts, "|")
}

func sortEdges(edges []Edge) []Edge {
	seen := map[Edge]bool{}
	out := make([]Edge, 0, len(edges))
	for _, e := range edges {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Class != b.Class {
			return classRank(a.Class) < classRank(b.Class)
		}
		if a.Direction != b.Direction {
			return a.Direction < b.Direction
		}
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Registry < b.Registry
	})
	return out
}

func sortDefects(defects []Defect) []Defect {
	seen := map[Defect]bool{}
	out := make([]Defect, 0, len(defects))
	for _, d := range defects {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Class != b.Class {
			return classRank(a.Class) < classRank(b.Class)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Identity != b.Identity {
			return a.Identity < b.Identity
		}
		return a.Detail < b.Detail
	})
	return out
}

func sortGaps(gaps []BindingGapRow) []BindingGapRow {
	out := append([]BindingGapRow(nil), gaps...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, v := range values {
		if v != "" {
			set[v] = true
		}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
