package chatfilter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestTodo_CHATMOD_002_Security: a filter result never reveals a list's
// contents beyond the matched term; one tenant's rules never reach another.
func TestTodo_CHATMOD_002_Security(t *testing.T) {
	ctx := t.Context()

	t.Run("a result names only the matched term", func(t *testing.T) {
		// A custom list with sentinel terms that the message does not contain.
		d := rule("block")
		d.Match = []string{"alphaterm", "sentinelbeta", "gammaterm", "deltaterm"}
		ev, err := NewRegistry().Compile([]Definition{d})
		if err != nil {
			t.Fatal(err)
		}
		body := "please say alphaterm now"
		res, err := ev.Evaluate(Input{Body: body})
		if err != nil || len(res.Hits) != 1 {
			t.Fatalf("%+v %v", res, err)
		}
		dumps := []string{fmt.Sprintf("%+v", res), fmt.Sprintf("%#v", res), fmt.Sprintf("%+v", res.Hits[0]), res.Refusal().Error()}
		for _, v := range []any{res, res.Hits, res.Hits[0], res.Refusal()} {
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			dumps = append(dumps, string(raw))
		}
		for _, dump := range dumps {
			for _, sentinel := range []string{"sentinelbeta", "gammaterm", "deltaterm"} {
				if strings.Contains(strings.ToLower(dump), sentinel) {
					t.Fatalf("result reveals %q: %s", sentinel, dump)
				}
			}
		}
		sum := sha256.Sum256([]byte("alphaterm"))
		if res.Hits[0].Digest != "sha256:"+hex.EncodeToString(sum[:]) || res.Hits[0].Masked != removed {
			t.Fatalf("hit carries %+v", res.Hits[0])
		}
	})

	t.Run("Try on a built-in list does not return its entries", func(t *testing.T) {
		svc := &Service{Store: newMemStore(), Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true}}
		admin := Actor{Tenant: "t", Subject: "admin"}
		for _, list := range Builtins() {
			if list.ID != "builtin-en-profanity" && list.ID != "builtin-ar-harassment" {
				continue
			}
			matched := list.Match[0]
			res, err := svc.Try(ctx, admin, list, Input{Body: "so " + matched + " now"})
			if err != nil || len(res.Hits) == 0 {
				t.Fatalf("%s: %+v %v", list.ID, res, err)
			}
			raw, _ := json.Marshal(res)
			dump := strings.ToLower(string(raw) + fmt.Sprintf("%+v", res) + fmt.Sprint(res.Refusal()))
			digest := strings.ToLower(res.Hits[0].Digest)
			h := res.Hits[0]
			shellJSON, _ := json.Marshal(Result{Action: res.Action, Hits: []Hit{{RuleID: h.RuleID, RuleName: h.RuleName, Version: h.Version, Action: h.Action, Span: h.Span}}})
			shell := strings.ToLower(string(shellJSON))
			for _, term := range list.Match {
				folded := strings.ToLower(term)
				if strings.Contains(strings.ToLower(matched), folded) || strings.Contains(strings.ToLower(res.Hits[0].RuleID+res.Hits[0].RuleName), folded) || strings.Contains(digest, folded) {
					continue // the matched term itself, or part of the rule's own name or hex digest
				}
				if strings.Contains(shell, folded) {
					continue
				}
				if strings.Contains(dump, folded) {
					t.Fatalf("%s: result reveals list entry %q: %s", list.ID, term, dump)
				}
			}
			if strings.Contains(string(raw), `"Match"`) {
				t.Fatalf("result carries the Match field: %s", raw)
			}
		}
	})

	t.Run("a service result for all lists carries only what matched", func(t *testing.T) {
		svc, _ := allBuiltinsOn(t)
		res, err := svc.Evaluate(ctx, Input{Tenant: "t", Channel: "c", Body: "ok damn it"}, false)
		if err != nil || res.Action != "block" {
			t.Fatalf("%+v %v", res, err)
		}
		raw, _ := json.Marshal(res)
		dump := strings.ToLower(string(raw) + fmt.Sprintf("%+v", res))
		for _, other := range []string{"motherfucker", "idiot", "kill yourself", "اللعنة", "verdammt", "scheiße", "shut up"} {
			if strings.Contains(dump, other) {
				t.Fatalf("result reveals %q: %s", other, dump)
			}
		}
	})

	t.Run("a tenant never sees another tenant's rules", func(t *testing.T) {
		store := newMemStore()
		mk := func(term string) Definition {
			d := rule("block")
			d.ID, d.Version, d.Match = "shared-id", "1.0.0", []string{term}
			return d
		}
		// the same id and version, different terms: only the tenant's own applies.
		store.defs["a"], store.defs["b"] = []Definition{mk("alphaterm")}, []Definition{mk("betaterm")}
		store.rawEnable("a", Enablement{RuleID: "shared-id", Enabled: true})
		store.rawEnable("b", Enablement{RuleID: "shared-id", Enabled: true})
		store.rawEnable("c", Enablement{RuleID: "shared-id", Enabled: true}) // no definition: nothing
		svc := &Service{Store: store, Registry: NewRegistry()}
		var wg sync.WaitGroup
		failures := make(chan string, 64)
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				for round := 0; round < 40; round++ {
					for _, c := range []struct {
						tenant, body string
						hit          bool
					}{{"a", "alphaterm", true}, {"a", "betaterm", false}, {"b", "betaterm", true}, {"b", "alphaterm", false}, {"c", "alphaterm", false}, {"d", "alphaterm", false}} {
						r, err := svc.Evaluate(ctx, Input{Tenant: c.tenant, Channel: "ch", Body: c.body}, false)
						if err != nil || (len(r.Hits) > 0) != c.hit {
							failures <- fmt.Sprintf("tenant %s body %s: hits %d err %v", c.tenant, c.body, len(r.Hits), err)
							return
						}
					}
				}
			}(i)
		}
		wg.Wait()
		close(failures)
		for f := range failures {
			t.Error(f)
		}
		if on, err := svc.HasActive(ctx, "c", "ch"); err != nil || on {
			t.Fatalf("tenant c has an enablement row but no definition of its own: %v %v", on, err)
		}
		if on, _ := svc.HasActive(ctx, "d", "ch"); on {
			t.Fatal("tenant d has nothing enabled")
		}
	})

	t.Run("caches stay bounded", func(t *testing.T) {
		store := newMemStore()
		svc := &Service{Store: store, Registry: NewRegistry()}
		for i := 0; i < 200; i++ {
			tenant := fmt.Sprintf("tenant-%d", i)
			d := rule("block")
			d.Match = []string{fmt.Sprintf("term%dx", i)}
			store.defs[tenant] = []Definition{d}
			store.rawEnable(tenant, Enablement{RuleID: d.ID, Enabled: true})
			if r, err := svc.Evaluate(ctx, Input{Tenant: tenant, Body: fmt.Sprintf("term%dx", i)}, false); err != nil || len(r.Hits) != 1 {
				t.Fatalf("tenant %d: %+v %v", i, r, err)
			}
		}
		svc.cacheMu.Lock()
		defer svc.cacheMu.Unlock()
		if len(svc.evaluators) > filterCacheEvaluators || len(svc.evalOrder) > filterCacheEvaluators || len(svc.tenants) > filterCacheTenants {
			t.Fatalf("unbounded: %d evaluators, %d tenants", len(svc.evaluators), len(svc.tenants))
		}
	})
}
