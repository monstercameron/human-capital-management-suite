// TestTodo_WF_DATA_022 proves inherited attributes resolve along declared
// relationship paths under a declared policy with a full explanation.
package customobject_test

import (
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/customobject"
)

func wfData022Links() []customobject.HierarchyLink {
	return []customobject.HierarchyLink{
		{Hierarchy: "org", Parent: "ENT", ParentLevel: "enterprise", Child: "C-US", ChildLevel: "company"},
		{Hierarchy: "org", Parent: "C-US", ParentLevel: "company", Child: "D-ENG", ChildLevel: "department"},
		{Hierarchy: "org", Parent: "D-ENG", ParentLevel: "department", Child: "T-PLAT", ChildLevel: "team"},
		{Hierarchy: "loc", Parent: "R-NA", ParentLevel: "region", Child: "S-AUS", ChildLevel: "site"},
		{Hierarchy: "loc", Parent: "S-AUS", ParentLevel: "site", Child: "T-PLAT", ChildLevel: "team"},
	}
}

func wfData022Hierarchies(t *testing.T) *customobject.StaticHierarchies {
	t.Helper()
	src, err := customobject.NewStaticHierarchies(wfData022Links())
	if err != nil {
		t.Fatalf("build fixture hierarchies: %v", err)
	}
	return src
}

func wfData022Projection(t *testing.T, attrs []customobject.InheritedAttribute, values []customobject.StoredValue) *customobject.Projection {
	t.Helper()
	p := customobject.NewProjection()
	if _, err := p.Rebuild(attrs, wfData022Hierarchies(t), values); err != nil {
		t.Fatalf("rebuild projection: %v", err)
	}
	return p
}

func wfData022Resolve(t *testing.T, p *customobject.Projection, attr, record string) customobject.AttributeResolution {
	t.Helper()
	res, err := p.Resolve(attr, record)
	if err != nil {
		t.Fatalf("resolve %s/%s: %v", attr, record, err)
	}
	return res
}

func TestTodo_WF_DATA_022(t *testing.T) {
	nearestAttr := customobject.InheritedAttribute{
		Name:              "pto.max_carryover_days",
		Paths:             []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}},
		Policy:            customobject.InheritanceNearestWins,
		ValueKind:         customobject.ScalarNumber,
		OverridableLevels: []string{"company", "department", "team"},
		SetRoles:          []string{"hr_admin"},
	}

	t.Run("value_set_on_one_record_applies_below_it", func(t *testing.T) {
		p := wfData022Projection(t, []customobject.InheritedAttribute{nearestAttr}, []customobject.StoredValue{
			{Attribute: nearestAttr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
		})
		res := wfData022Resolve(t, p, nearestAttr.Name, "T-PLAT")
		if !res.Found || res.Ambiguous || res.Value.Num != 10 {
			t.Fatalf("enterprise value did not inherit down the hierarchy: %+v", res)
		}
		if len(res.Sources) != 1 || res.Sources[0].Record != "ENT" || res.Sources[0].Depth != 3 {
			t.Fatalf("inherited value lost its source: %+v", res.Sources)
		}
		// Values never flow upward: the company above the team sees the same
		// enterprise value, and a team-only value is invisible to its parent.
		up := wfData022Resolve(t, p, nearestAttr.Name, "C-US")
		if !up.Found || up.Value.Num != 10 || up.Sources[0].Record != "ENT" {
			t.Fatalf("company resolution is wrong: %+v", up)
		}
	})

	t.Run("nearest_allowed_override_wins_and_reports_overrode", func(t *testing.T) {
		p := wfData022Projection(t, []customobject.InheritedAttribute{nearestAttr}, []customobject.StoredValue{
			{Attribute: nearestAttr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
			{Attribute: nearestAttr.Name, Hierarchy: "org", Record: "D-ENG", Level: "department", Value: customobject.NumberScalar(15), SetByRole: "hr_admin"},
		})
		res := wfData022Resolve(t, p, nearestAttr.Name, "T-PLAT")
		if !res.Found || res.Value.Num != 15 {
			t.Fatalf("nearest override did not win: %+v", res)
		}
		if len(res.Sources) != 1 || res.Sources[0].Record != "D-ENG" || res.Sources[0].Depth != 1 {
			t.Fatalf("winner source is wrong: %+v", res.Sources)
		}
		if len(res.Overrode) != 1 || res.Overrode[0].Record != "ENT" {
			t.Fatalf("shadowed value not reported: %+v", res.Overrode)
		}
		if len(res.Refused) != 0 {
			t.Fatalf("nothing should be refused here: %+v", res.Refused)
		}
	})

	t.Run("override_at_forbidden_level_is_refused", func(t *testing.T) {
		attr := nearestAttr
		attr.OverridableLevels = []string{"company"}
		p := wfData022Projection(t, []customobject.InheritedAttribute{attr}, []customobject.StoredValue{
			{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
			{Attribute: attr.Name, Hierarchy: "org", Record: "D-ENG", Level: "department", Value: customobject.NumberScalar(15), SetByRole: "hr_admin"},
		})
		res := wfData022Resolve(t, p, attr.Name, "T-PLAT")
		if !res.Found || res.Value.Num != 10 {
			t.Fatalf("forbidden override must not apply: %+v", res)
		}
		if len(res.Refused) != 1 || res.Refused[0].Source.Record != "D-ENG" ||
			res.Refused[0].Reason != customobject.RefuseLevelNotOverridable {
			t.Fatalf("refused override not explained: %+v", res.Refused)
		}
	})

	t.Run("only_declared_roles_may_set", func(t *testing.T) {
		bad := customobject.StoredValue{Attribute: nearestAttr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "manager"}
		if err := bad.Validate(nearestAttr); !errors.Is(err, customobject.ErrRoleNotPermitted) {
			t.Fatalf("unauthorized set must fail closed, got %v", err)
		}
		p := customobject.NewProjection()
		if _, err := p.Rebuild([]customobject.InheritedAttribute{nearestAttr}, wfData022Hierarchies(t), []customobject.StoredValue{bad}); !errors.Is(err, customobject.ErrRoleNotPermitted) {
			t.Fatalf("rebuild with unauthorized value must fail closed, got %v", err)
		}
	})

	t.Run("minimum_and_maximum_fold_allowed_candidates", func(t *testing.T) {
		for _, tc := range []struct {
			policy customobject.InheritancePolicy
			want   float64
			source string
		}{
			{customobject.InheritanceMinimum, 7, "C-US"},
			{customobject.InheritanceMaximum, 20, "T-PLAT"},
		} {
			attr := nearestAttr
			attr.Name = "leave." + strings.ToLower(string(tc.policy))
			attr.Policy = tc.policy
			p := wfData022Projection(t, []customobject.InheritedAttribute{attr}, []customobject.StoredValue{
				{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
				{Attribute: attr.Name, Hierarchy: "org", Record: "C-US", Level: "company", Value: customobject.NumberScalar(7), SetByRole: "hr_admin"},
				{Attribute: attr.Name, Hierarchy: "org", Record: "T-PLAT", Level: "team", Value: customobject.NumberScalar(20), SetByRole: "hr_admin"},
			})
			res := wfData022Resolve(t, p, attr.Name, "T-PLAT")
			if !res.Found || res.Value.Num != tc.want {
				t.Fatalf("%s folded to %v, want %v: %+v", tc.policy, res.Value.Num, tc.want, res)
			}
			if len(res.Sources) != 1 || res.Sources[0].Record != tc.source {
				t.Fatalf("%s winner source is wrong: %+v", tc.policy, res.Sources)
			}
			if len(res.Overrode) != 2 {
				t.Fatalf("%s must report both folded candidates: %+v", tc.policy, res.Overrode)
			}
		}
	})

	t.Run("accumulate_unions_sets_root_first", func(t *testing.T) {
		attr := customobject.InheritedAttribute{
			Name:              "training.required",
			Paths:             []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}},
			Policy:            customobject.InheritanceAccumulate,
			ValueKind:         customobject.ScalarSet,
			OverridableLevels: []string{"company", "department", "team"},
			SetRoles:          []string{"hr_admin"},
		}
		p := wfData022Projection(t, []customobject.InheritedAttribute{attr}, []customobject.StoredValue{
			{Attribute: attr.Name, Hierarchy: "org", Record: "C-US", Level: "company", Value: customobject.SetScalar("sox"), SetByRole: "hr_admin"},
			{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.SetScalar("hipaa"), SetByRole: "hr_admin"},
			{Attribute: attr.Name, Hierarchy: "org", Record: "D-ENG", Level: "department", Value: customobject.SetScalar("pci", "hipaa"), SetByRole: "hr_admin"},
		})
		res := wfData022Resolve(t, p, attr.Name, "T-PLAT")
		if !res.Found {
			t.Fatalf("accumulated value not found: %+v", res)
		}
		want := []string{"hipaa", "sox", "pci"}
		if len(res.Value.Set) != len(want) {
			t.Fatalf("accumulated set is %v, want %v", res.Value.Set, want)
		}
		for i := range want {
			if res.Value.Set[i] != want[i] {
				t.Fatalf("accumulated set is %v, want %v", res.Value.Set, want)
			}
		}
		if len(res.Sources) != 3 || res.Sources[0].Record != "ENT" || res.Sources[2].Record != "D-ENG" {
			t.Fatalf("accumulation sources must be root-first: %+v", res.Sources)
		}
		if len(res.Values) != 3 {
			t.Fatalf("accumulation must keep every contributor: %+v", res.Values)
		}
	})

	t.Run("locked_is_never_overridden", func(t *testing.T) {
		attr := nearestAttr
		attr.Name = "pay.band_floor"
		attr.Policy = customobject.InheritanceLocked
		p := wfData022Projection(t, []customobject.InheritedAttribute{attr}, []customobject.StoredValue{
			{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
			{Attribute: attr.Name, Hierarchy: "org", Record: "C-US", Level: "company", Value: customobject.NumberScalar(7), SetByRole: "hr_admin"},
			{Attribute: attr.Name, Hierarchy: "org", Record: "T-PLAT", Level: "team", Value: customobject.NumberScalar(20), SetByRole: "hr_admin"},
		})
		res := wfData022Resolve(t, p, attr.Name, "T-PLAT")
		if !res.Found || res.Value.Num != 10 {
			t.Fatalf("locked base value was overridden: %+v", res)
		}
		if len(res.Refused) != 2 {
			t.Fatalf("locked overrides must all be refused: %+v", res.Refused)
		}
		for _, r := range res.Refused {
			if r.Reason != customobject.RefuseLocked {
				t.Fatalf("locked refusal reason is wrong: %+v", res.Refused)
			}
		}
	})

	t.Run("hierarchy_precedence_is_declared", func(t *testing.T) {
		attr := nearestAttr
		attr.Name = "policy.overtime"
		attr.Paths = []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}, {Hierarchy: "loc", Precedence: 1}}
		values := []customobject.StoredValue{
			{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
			{Attribute: attr.Name, Hierarchy: "loc", Record: "S-AUS", Level: "site", Value: customobject.NumberScalar(99), SetByRole: "hr_admin"},
		}
		res := wfData022Resolve(t, wfData022Projection(t, []customobject.InheritedAttribute{attr}, values), attr.Name, "T-PLAT")
		if !res.Found || res.Value.Num != 10 || res.Sources[0].Hierarchy != "org" {
			t.Fatalf("higher-precedence hierarchy must win: %+v", res)
		}
		if len(res.Overrode) != 1 || res.Overrode[0].Hierarchy != "loc" {
			t.Fatalf("losing hierarchy must be reported as overridden: %+v", res.Overrode)
		}

		attr.Paths = []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}, {Hierarchy: "loc", Precedence: 0}}
		res = wfData022Resolve(t, wfData022Projection(t, []customobject.InheritedAttribute{attr}, values), attr.Name, "T-PLAT")
		if res.Found || !res.Ambiguous {
			t.Fatalf("undecided conflict must resolve to AMBIGUOUS, never last-write-wins: %+v", res)
		}
		if len(res.Sources) != 2 {
			t.Fatalf("ambiguous result must name both contenders: %+v", res.Sources)
		}

		values[1].Value = customobject.NumberScalar(10)
		res = wfData022Resolve(t, wfData022Projection(t, []customobject.InheritedAttribute{attr}, values), attr.Name, "T-PLAT")
		if !res.Found || res.Ambiguous || res.Value.Num != 10 || len(res.Sources) != 2 {
			t.Fatalf("agreeing hierarchies must merge, not conflict: %+v", res)
		}
	})

	t.Run("access_restrictions_accumulate_and_never_loosen", func(t *testing.T) {
		attr := customobject.InheritedAttribute{
			Name:              "access.denied_exports",
			Paths:             []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}},
			Policy:            customobject.InheritanceAccumulate,
			ValueKind:         customobject.ScalarSet,
			OverridableLevels: []string{"company", "department", "team"},
			SetRoles:          []string{"security_admin"},
			RestrictsAccess:   true,
		}
		p := wfData022Projection(t, []customobject.InheritedAttribute{attr}, []customobject.StoredValue{
			{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.SetScalar("pii_export"), SetByRole: "security_admin"},
			{Attribute: attr.Name, Hierarchy: "org", Record: "D-ENG", Level: "department", Value: customobject.SetScalar("contractor_access"), SetByRole: "security_admin"},
		})
		res := wfData022Resolve(t, p, attr.Name, "T-PLAT")
		if !res.Found || len(res.Value.Set) != 2 || res.Value.Set[0] != "pii_export" || res.Value.Set[1] != "contractor_access" {
			t.Fatalf("parent restriction was loosened below it: %+v", res)
		}
		nearest := attr
		nearest.Policy = customobject.InheritanceNearestWins
		if err := nearest.Validate(); err == nil {
			t.Fatal("an access-restricting attribute must follow security accumulation")
		}
	})

	t.Run("results_come_from_a_watermarked_rebuildable_projection", func(t *testing.T) {
		values := []customobject.StoredValue{
			{Attribute: nearestAttr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
		}
		p := customobject.NewProjection()
		w1, err := p.Rebuild([]customobject.InheritedAttribute{nearestAttr}, wfData022Hierarchies(t), values)
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		if w1 == 0 || p.Watermark() != w1 {
			t.Fatalf("rebuild must publish a nonzero watermark, got %d", w1)
		}
		res := wfData022Resolve(t, p, nearestAttr.Name, "T-PLAT")
		if res.Watermark != w1 {
			t.Fatalf("resolution must report its serving watermark: %+v", res)
		}
		if res.PolicyVersion == "" {
			t.Fatal("resolution must identify the policy version it applied")
		}
		values = append(values, customobject.StoredValue{Attribute: nearestAttr.Name, Hierarchy: "org", Record: "C-US", Level: "company", Value: customobject.NumberScalar(7), SetByRole: "hr_admin"})
		w2, err := p.Rebuild([]customobject.InheritedAttribute{nearestAttr}, wfData022Hierarchies(t), values)
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		if w2 != w1+1 {
			t.Fatalf("watermark must advance on rebuild, got %d after %d", w2, w1)
		}
		res = wfData022Resolve(t, p, nearestAttr.Name, "T-PLAT")
		if res.Watermark != w2 || res.Value.Num != 7 {
			t.Fatalf("rebuild did not refresh the projection: %+v", res)
		}
		ghost := wfData022Resolve(t, p, nearestAttr.Name, "NO-SUCH-RECORD")
		if ghost.Found || ghost.Watermark != w2 {
			t.Fatalf("unknown records resolve empty at the current watermark: %+v", ghost)
		}
		if _, err := p.Resolve("no.such.attr", "T-PLAT"); err == nil {
			t.Fatal("unknown attributes must error, not resolve empty")
		}
	})

	t.Run("declarations_reject_incoherent_policies", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*customobject.InheritedAttribute)
		}{
			{"empty_name", func(a *customobject.InheritedAttribute) { a.Name = "" }},
			{"no_paths", func(a *customobject.InheritedAttribute) { a.Paths = nil }},
			{"empty_hierarchy", func(a *customobject.InheritedAttribute) { a.Paths[0].Hierarchy = "" }},
			{"duplicate_hierarchy", func(a *customobject.InheritedAttribute) {
				a.Paths = append(a.Paths, customobject.AttributePath{Hierarchy: "org", Precedence: 1})
			}},
			{"unknown_policy", func(a *customobject.InheritedAttribute) { a.Policy = "NEAREST_LOSES" }},
			{"no_roles", func(a *customobject.InheritedAttribute) { a.SetRoles = nil }},
			{"minimum_needs_numbers", func(a *customobject.InheritedAttribute) {
				a.Policy, a.ValueKind = customobject.InheritanceMinimum, customobject.ScalarString
			}},
			{"maximum_needs_numbers", func(a *customobject.InheritedAttribute) {
				a.Policy, a.ValueKind = customobject.InheritanceMaximum, customobject.ScalarBool
			}},
			{"accumulate_needs_sets", func(a *customobject.InheritedAttribute) {
				a.Policy = customobject.InheritanceAccumulate
			}},
			{"unknown_kind", func(a *customobject.InheritedAttribute) { a.ValueKind = "DATETIME" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				attr := nearestAttr
				attr.Paths = append([]customobject.AttributePath(nil), nearestAttr.Paths...)
				tc.mutate(&attr)
				if err := attr.Validate(); err == nil {
					t.Fatalf("%s must be rejected", tc.name)
				}
			})
		}
		if err := nearestAttr.Validate(); err != nil {
			t.Fatalf("fixture declaration must be valid: %v", err)
		}
	})

	t.Run("cyclic_hierarchies_are_rejected", func(t *testing.T) {
		links := append(wfData022Links(), customobject.HierarchyLink{Hierarchy: "org", Parent: "T-PLAT", ParentLevel: "team", Child: "ENT", ChildLevel: "enterprise"})
		if _, err := customobject.NewStaticHierarchies(links); !errors.Is(err, customobject.ErrHierarchyCycle) {
			t.Fatalf("cyclic hierarchy must be rejected, got %v", err)
		}
	})

	t.Run("resolve_results_are_isolated_from_the_cache", func(t *testing.T) {
		attr := customobject.InheritedAttribute{
			Name:            "access.deny",
			Paths:           []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}},
			Policy:          customobject.InheritanceAccumulate,
			ValueKind:       customobject.ScalarSet,
			SetRoles:        []string{"hr_admin"},
			RestrictsAccess: true,
		}
		p := wfData022Projection(t, []customobject.InheritedAttribute{attr}, []customobject.StoredValue{
			{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.SetScalar("payroll", "pii"), SetByRole: "hr_admin"},
		})
		first := wfData022Resolve(t, p, attr.Name, "T-PLAT")
		if !first.Found || len(first.Value.Set) != 2 {
			t.Fatalf("fixture must resolve an accumulated set: %+v", first)
		}
		first.Value.Set[0] = "MUTATED"
		if len(first.Values) > 0 {
			first.Values[0].Set[0] = "MUTATED"
		}
		second := wfData022Resolve(t, p, attr.Name, "T-PLAT")
		for _, m := range second.Value.Set {
			if m == "MUTATED" {
				t.Fatalf("caller mutation of a Resolve result leaked into the cached projection: %+v", second)
			}
		}
		for _, v := range second.Values {
			for _, m := range v.Set {
				if m == "MUTATED" {
					t.Fatalf("caller mutation of a Resolve contributor leaked into the cached projection: %+v", second)
				}
			}
		}
		if !second.Value.Equal(customobject.SetScalar("payroll", "pii")) {
			t.Fatalf("second resolve is not pristine: %+v", second)
		}
	})
}

// TestTodo_WF_DATA_022_Property proves a locked value is never overridden,
// no matter where below the base a value is set.
func TestTodo_WF_DATA_022_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(22))
	levels := []string{"enterprise", "company", "department", "team"}
	for i := 0; i < 200; i++ {
		depth := 1 + rng.Intn(6)
		links := make([]customobject.HierarchyLink, 0, depth)
		records := make([]string, 0, depth+1)
		records = append(records, "R0")
		for d := 0; d < depth; d++ {
			parent := records[d]
			child := strings.ToUpper(string(rune('a'+d))) + string(rune('0'+i%10))
			links = append(links, customobject.HierarchyLink{
				Hierarchy: "org", Parent: parent, ParentLevel: levels[d%len(levels)],
				Child: child, ChildLevel: levels[(d+1)%len(levels)],
			})
			records = append(records, child)
		}
		src, err := customobject.NewStaticHierarchies(links)
		if err != nil {
			t.Fatalf("case %d: build hierarchy: %v", i, err)
		}
		attr := customobject.InheritedAttribute{
			Name:              "locked.case",
			Paths:             []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}},
			Policy:            customobject.InheritanceLocked,
			ValueKind:         customobject.ScalarNumber,
			OverridableLevels: levels,
			SetRoles:          []string{"hr_admin"},
		}
		setAt := map[string]float64{}
		var values []customobject.StoredValue
		for _, rec := range records {
			if rng.Intn(2) == 0 {
				continue
			}
			v := float64(rng.Intn(1000))
			setAt[rec] = v
			values = append(values, customobject.StoredValue{
				Attribute: attr.Name, Hierarchy: "org", Record: rec, Value: customobject.NumberScalar(v), SetByRole: "hr_admin",
			})
		}
		p := customobject.NewProjection()
		if _, err := p.Rebuild([]customobject.InheritedAttribute{attr}, src, values); err != nil {
			t.Fatalf("case %d: rebuild: %v", i, err)
		}
		leaf := records[len(records)-1]
		res, err := p.Resolve(attr.Name, leaf)
		if err != nil {
			t.Fatalf("case %d: resolve: %v", i, err)
		}
		// The base is the root-most candidate: records[0] is the root and
		// records are ordered root-first.
		baseRecord := ""
		for _, rec := range records {
			if _, ok := setAt[rec]; ok {
				baseRecord = rec
				break
			}
		}
		if baseRecord == "" {
			if res.Found {
				t.Fatalf("case %d: resolution without any value must be empty: %+v", i, res)
			}
			continue
		}
		if !res.Found || res.Value.Num != setAt[baseRecord] || len(res.Sources) != 1 || res.Sources[0].Record != baseRecord {
			t.Fatalf("case %d: locked base %v at %s was overridden: %+v", i, setAt[baseRecord], baseRecord, res)
		}
		for _, r := range res.Refused {
			if r.Reason != customobject.RefuseLocked {
				t.Fatalf("case %d: refusal reason is wrong: %+v", i, res.Refused)
			}
		}
		if len(res.Refused) != len(values)-1 {
			t.Fatalf("case %d: refused %d overrides, want %d: %+v", i, len(res.Refused), len(values)-1, res)
		}
	}
}

// TestTodo_WF_DATA_022_Golden pins the canonical explanation bytes served
// for one inherited resolution.
func TestTodo_WF_DATA_022_Golden(t *testing.T) {
	attr := customobject.InheritedAttribute{
		Name:              "pto.max_carryover_days",
		Paths:             []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}},
		Policy:            customobject.InheritanceNearestWins,
		ValueKind:         customobject.ScalarNumber,
		OverridableLevels: []string{"company", "department", "team"},
		SetRoles:          []string{"hr_admin"},
	}
	p := wfData022Projection(t, []customobject.InheritedAttribute{attr}, []customobject.StoredValue{
		{Attribute: attr.Name, Hierarchy: "org", Record: "ENT", Level: "enterprise", Value: customobject.NumberScalar(10), SetByRole: "hr_admin"},
		{Attribute: attr.Name, Hierarchy: "org", Record: "D-ENG", Level: "department", Value: customobject.NumberScalar(15), SetByRole: "hr_admin"},
	})
	res := wfData022Resolve(t, p, attr.Name, "T-PLAT")
	got, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal resolution: %v", err)
	}
	want := `{"attribute":"pto.max_carryover_days","record":"T-PLAT","found":true,"ambiguous":false,"value":{"kind":"NUMBER","num":15},"sources":[{"hierarchy":"org","record":"D-ENG","level":"department","depth":1}],"overrode":[{"hierarchy":"org","record":"ENT","level":"enterprise","depth":3}],"watermark":1,"policy_version":"` + attr.Digest() + `"}`
	if string(got) != want {
		t.Fatalf("resolution bytes changed\n got: %s\nwant: %s", got, want)
	}
}

func BenchmarkTodo_WF_DATA_022(b *testing.B) {
	const depth = 64
	links := make([]customobject.HierarchyLink, 0, depth)
	for d := 0; d < depth; d++ {
		links = append(links, customobject.HierarchyLink{
			Hierarchy: "org", Parent: "N", ParentLevel: "node",
			Child: "N", ChildLevel: "node",
		})
		links[d].Parent += string(rune('0'+d/10)) + string(rune('0'+d%10))
		links[d].Child += string(rune('0'+(d+1)/10)) + string(rune('0'+(d+1)%10))
	}
	src, err := customobject.NewStaticHierarchies(links)
	if err != nil {
		b.Fatalf("build deep hierarchy: %v", err)
	}
	attr := customobject.InheritedAttribute{
		Name: "bench.attr", Paths: []customobject.AttributePath{{Hierarchy: "org", Precedence: 0}},
		Policy: customobject.InheritanceNearestWins, ValueKind: customobject.ScalarNumber,
		OverridableLevels: []string{"node"}, SetRoles: []string{"hr_admin"},
	}
	var values []customobject.StoredValue
	for d := 0; d <= depth; d += 8 {
		values = append(values, customobject.StoredValue{
			Attribute: attr.Name, Hierarchy: "org",
			Record: "N" + string(rune('0'+d/10)) + string(rune('0'+d%10)),
			Value:  customobject.NumberScalar(float64(d)), SetByRole: "hr_admin",
		})
	}
	leaf := "N" + string(rune('0'+depth/10)) + string(rune('0'+depth%10))
	b.Run("resolve", func(b *testing.B) {
		p := customobject.NewProjection()
		if _, err := p.Rebuild([]customobject.InheritedAttribute{attr}, src, values); err != nil {
			b.Fatalf("rebuild: %v", err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := p.Resolve(attr.Name, leaf); err != nil {
				b.Fatalf("resolve: %v", err)
			}
		}
	})
	b.Run("rebuild", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			p := customobject.NewProjection()
			if _, err := p.Rebuild([]customobject.InheritedAttribute{attr}, src, values); err != nil {
				b.Fatalf("rebuild: %v", err)
			}
		}
	})
}
