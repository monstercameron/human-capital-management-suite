package industrypack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PACK-006: compose platform, country, industry and customer packs.
//
// Each pack contributes settings at its layer (PLATFORM < COUNTRY < INDUSTRY <
// CUSTOMER). Every setting family composes by an explicit strategy:
//
//   - ADDITIVE: the union of every layer's values; a removal of a mandatory
//     value is refused.
//   - PRECEDENCE: the highest layer wins, unless a lower layer's value is
//     mandatory and the higher layer differs.
//   - RESTRICTIVE: the strictest numeric value wins; a higher layer declaring
//     a laxer value than a mandatory lower one is refused rather than
//     silently tightened.
//   - CUSTOM: a named resolver decides; its result must still carry every
//     mandatory value.
//
// A family with conflicting contributions and no strategy, two packs at one
// layer, two different values at one layer, a missing custom resolver or a
// non-numeric restrictive value is a blocking ambiguity. Mandatory security
// and legal constraints are never weakened. Every refusal is PACK_006_REJECTED
// naming the setting, state and offending pack version.

// LayerRejectionCode is the stable PACK-006 refusal code.
const LayerRejectionCode = "PACK_006_REJECTED"

// Layer composition refusal states.
const (
	LayerWeakenedMandatory = "WEAKENED_MANDATORY"
	LayerBlockingAmbiguity = "BLOCKING_AMBIGUITY"
	LayerInvalid           = "INVALID_LAYER"
)

// ErrLayerComposition is the sentinel every PACK-006 refusal unwraps to.
var ErrLayerComposition = errors.New("industrypack: layered pack composition refused")

// LayerRefusal names what was refused.
type LayerRefusal struct {
	Code    string
	Field   string
	State   string
	Version string
	Detail  string
}

func (e *LayerRefusal) Error() string {
	return fmt.Sprintf("%s: %s %s@%s: %s", e.Code, e.Field, e.State, e.Version, e.Detail)
}

// Unwrap exposes the sentinel.
func (e *LayerRefusal) Unwrap() error { return ErrLayerComposition }

// Layer orders pack precedence.
type Layer int

// Layers, lowest precedence first.
const (
	LayerPlatform Layer = iota + 1
	LayerCountry
	LayerIndustry
	LayerCustomer
)

func (l Layer) String() string {
	switch l {
	case LayerPlatform:
		return "PLATFORM"
	case LayerCountry:
		return "COUNTRY"
	case LayerIndustry:
		return "INDUSTRY"
	case LayerCustomer:
		return "CUSTOMER"
	}
	return "UNKNOWN"
}

// Strategy is a family composition strategy.
type Strategy string

// Strategies.
const (
	StrategyAdditive    Strategy = "ADDITIVE"
	StrategyPrecedence  Strategy = "PRECEDENCE"
	StrategyRestrictive Strategy = "RESTRICTIVE"
	StrategyCustom      Strategy = "CUSTOM"
)

// Constraint classes a mandatory setting protects.
const (
	ClassSecurity = "SECURITY"
	ClassLegal    = "LEGAL"
)

// FamilyPolicy declares how one family composes.
type FamilyPolicy struct {
	Family   string
	Strategy Strategy
	// LowerIsStricter orders RESTRICTIVE values (true: smaller is stricter).
	LowerIsStricter bool
	// Resolver names the CUSTOM resolver.
	Resolver string
}

// LayerSetting is one contribution.
type LayerSetting struct {
	Family    string   `json:"family"`
	Key       string   `json:"key"`
	Values    []string `json:"values"`
	Mandatory bool     `json:"mandatory,omitempty"`
	Class     string   `json:"class,omitempty"`
	// Remove withdraws Values from an ADDITIVE family.
	Remove bool `json:"remove,omitempty"`
}

// LayerPack is one pack at its layer.
type LayerPack struct {
	Layer    Layer
	PackID   string
	Version  int
	Settings []LayerSetting
}

func (p LayerPack) ref() string { return p.PackID + "@" + strconv.Itoa(p.Version) }

// Contribution is a setting with the pack and layer it came from.
type Contribution struct {
	LayerSetting
	Layer Layer
	Pack  string
}

// CustomResolver decides a CUSTOM family key.
type CustomResolver func(key string, contributions []Contribution) ([]string, error)

// LayerSpec is the input to [ComposeLayers].
type LayerSpec struct {
	Packs     []LayerPack
	Policies  []FamilyPolicy
	Resolvers map[string]CustomResolver
}

// ResolvedSetting is one composed setting with provenance.
type ResolvedSetting struct {
	Family     string   `json:"family"`
	Key        string   `json:"key"`
	Strategy   Strategy `json:"strategy,omitempty"`
	Values     []string `json:"values"`
	Mandatory  bool     `json:"mandatory,omitempty"`
	Class      string   `json:"class,omitempty"`
	Provenance []string `json:"provenance"`
}

// LayeredConfig is the composed configuration.
type LayeredConfig struct {
	Layers   []string          `json:"layers"`
	Settings []ResolvedSetting `json:"settings"`
	Digest   string            `json:"digest"`
}

// ComposeLayers composes the packs by their family strategies.
func ComposeLayers(spec LayerSpec) (LayeredConfig, error) {
	refuse := func(field, state, version, format string, args ...any) (LayeredConfig, error) {
		return LayeredConfig{}, &LayerRefusal{Code: LayerRejectionCode, Field: field, State: state, Version: version, Detail: fmt.Sprintf(format, args...)}
	}
	packs := append([]LayerPack(nil), spec.Packs...)
	sort.SliceStable(packs, func(i, j int) bool { return packs[i].Layer < packs[j].Layer })
	seen := map[Layer]string{}
	hasPlatform := false
	var layers []string
	for _, p := range packs {
		if p.Layer < LayerPlatform || p.Layer > LayerCustomer || strings.TrimSpace(p.PackID) == "" || p.Version < 1 {
			return refuse("packs", LayerInvalid, p.ref(), "pack %s has an unknown layer or no identity", p.ref())
		}
		if prior, dup := seen[p.Layer]; dup {
			return refuse("packs", LayerBlockingAmbiguity, p.ref(), "%s and %s both claim the %s layer", prior, p.ref(), p.Layer)
		}
		seen[p.Layer] = p.ref()
		hasPlatform = hasPlatform || p.Layer == LayerPlatform
		layers = append(layers, p.Layer.String()+":"+p.ref())
	}
	if !hasPlatform {
		return refuse("packs", LayerInvalid, "", "a composition needs a platform pack")
	}
	policies := map[string]FamilyPolicy{}
	for _, pol := range spec.Policies {
		policies[pol.Family] = pol
	}

	byKey := map[string][]Contribution{}
	var keys []string
	for _, p := range packs {
		for _, s := range p.Settings {
			k := s.Family + "." + s.Key
			if _, ok := byKey[k]; !ok {
				keys = append(keys, k)
			}
			byKey[k] = append(byKey[k], Contribution{LayerSetting: s, Layer: p.Layer, Pack: p.ref()})
		}
	}
	sort.Strings(keys)

	out := LayeredConfig{Layers: layers, Settings: []ResolvedSetting{}}
	for _, k := range keys {
		cs := byKey[k]
		first := cs[0]
		field := "settings[" + k + "]"
		res := ResolvedSetting{Family: first.Family, Key: first.Key}
		for _, c := range cs {
			res.Provenance = append(res.Provenance, c.Layer.String()+":"+c.Pack)
			if c.Mandatory {
				res.Mandatory = true
				if c.Class != "" {
					res.Class = c.Class
				}
			}
		}
		pol, hasPolicy := policies[first.Family]
		if !hasPolicy {
			if distinctValues(cs) > 1 || len(cs) > 1 && anyRemove(cs) {
				last := cs[len(cs)-1]
				return refuse(field, LayerBlockingAmbiguity, last.Pack, "family %s has conflicting contributions and no composition strategy", first.Family)
			}
			res.Values = sortedSet(first.Values)
			out.Settings = append(out.Settings, res)
			continue
		}
		res.Strategy = pol.Strategy
		switch pol.Strategy {
		case StrategyAdditive:
			set := map[string]bool{}
			mandatory := map[string]bool{}
			for _, c := range cs {
				for _, v := range c.Values {
					if c.Remove {
						if mandatory[v] {
							return refuse(field, LayerWeakenedMandatory, c.Pack, "%s cannot remove mandatory %s value %q", c.Pack, res.Class, v)
						}
						delete(set, v)
						continue
					}
					set[v] = true
					mandatory[v] = mandatory[v] || c.Mandatory
				}
			}
			for v := range set {
				res.Values = append(res.Values, v)
			}
			sort.Strings(res.Values)
		case StrategyPrecedence:
			if c, ok := sameLayerConflict(cs); ok {
				return refuse(field, LayerBlockingAmbiguity, c.Pack, "two different values at the %s layer", c.Layer)
			}
			winner := cs[len(cs)-1]
			for _, c := range cs[:len(cs)-1] {
				if c.Mandatory && !equalSets(c.Values, winner.Values) {
					return refuse(field, LayerWeakenedMandatory, winner.Pack, "%s overrides mandatory %s value set by %s", winner.Pack, res.Class, c.Pack)
				}
			}
			res.Values = sortedSet(winner.Values)
		case StrategyRestrictive:
			var best float64
			var bestSet bool
			var bestRaw string
			var strictestMandatory *Contribution
			for i, c := range cs {
				if len(c.Values) != 1 {
					return refuse(field, LayerBlockingAmbiguity, c.Pack, "a restrictive value must be a single number")
				}
				n, err := strconv.ParseFloat(c.Values[0], 64)
				if err != nil {
					return refuse(field, LayerBlockingAmbiguity, c.Pack, "restrictive value %q is not numeric", c.Values[0])
				}
				if strictestMandatory != nil {
					m, _ := strconv.ParseFloat(strictestMandatory.Values[0], 64)
					if !stricterOrEqual(n, m, pol.LowerIsStricter) {
						return refuse(field, LayerWeakenedMandatory, c.Pack, "%s declares %s, laxer than mandatory %s %s from %s",
							c.Pack, c.Values[0], res.Class, strictestMandatory.Values[0], strictestMandatory.Pack)
					}
				}
				if c.Mandatory {
					strictestMandatory = &cs[i]
				}
				if !bestSet || !stricterOrEqual(best, n, pol.LowerIsStricter) {
					best, bestRaw, bestSet = n, c.Values[0], true
				}
			}
			res.Values = []string{bestRaw}
		case StrategyCustom:
			resolver, ok := spec.Resolvers[pol.Resolver]
			if !ok || resolver == nil {
				return refuse(field, LayerBlockingAmbiguity, cs[len(cs)-1].Pack, "custom resolver %q is not registered", pol.Resolver)
			}
			values, err := resolver(first.Key, append([]Contribution(nil), cs...))
			if err != nil {
				return refuse(field, LayerBlockingAmbiguity, cs[len(cs)-1].Pack, "custom resolver %q: %v", pol.Resolver, err)
			}
			for _, c := range cs {
				if c.Mandatory && !containsAll(values, c.Values) {
					return refuse(field, LayerWeakenedMandatory, cs[len(cs)-1].Pack, "custom resolution drops mandatory %s values from %s", res.Class, c.Pack)
				}
			}
			res.Values = sortedSet(values)
		default:
			return refuse(field, LayerBlockingAmbiguity, cs[len(cs)-1].Pack, "family %s declares unknown strategy %q", first.Family, pol.Strategy)
		}
		out.Settings = append(out.Settings, res)
	}
	body, _ := json.Marshal(struct {
		Layers   []string          `json:"layers"`
		Settings []ResolvedSetting `json:"settings"`
	}{out.Layers, out.Settings})
	sum := sha256.Sum256(body)
	out.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return out, nil
}

func stricterOrEqual(a, b float64, lowerIsStricter bool) bool {
	if lowerIsStricter {
		return a <= b
	}
	return a >= b
}

func sortedSet(values []string) []string {
	set := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if !set[v] {
			set[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func equalSets(a, b []string) bool {
	return strings.Join(sortedSet(a), "\x00") == strings.Join(sortedSet(b), "\x00")
}

func containsAll(have, want []string) bool {
	set := map[string]bool{}
	for _, v := range have {
		set[v] = true
	}
	for _, v := range want {
		if !set[v] {
			return false
		}
	}
	return true
}

func distinctValues(cs []Contribution) int {
	seen := map[string]bool{}
	for _, c := range cs {
		seen[strings.Join(sortedSet(c.Values), "\x00")] = true
	}
	return len(seen)
}

func anyRemove(cs []Contribution) bool {
	for _, c := range cs {
		if c.Remove {
			return true
		}
	}
	return false
}

func sameLayerConflict(cs []Contribution) (Contribution, bool) {
	for i := 1; i < len(cs); i++ {
		if cs[i].Layer == cs[i-1].Layer && !equalSets(cs[i].Values, cs[i-1].Values) {
			return cs[i], true
		}
	}
	return Contribution{}, false
}
