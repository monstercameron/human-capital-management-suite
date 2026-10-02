//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// ---- the cost tab of Agent operations ----

type agentCostSurface struct {
	mount  js.Value
	locale string
	loaded bool
	state  productui.AgentAccessLoadState
	report productui.AgentCostReport
	agents map[string]string
	order  []string
}

func agentCostTabActive() bool {
	location := js.Global().Get("location")
	return agentOperationsRoute(location.Get("pathname").String()) && agentOperationsSelectedTab(strings.TrimPrefix(location.Get("search").String(), "?")) == "cost"
}

func findAgentCostMount() {
	if !agentCostTabActive() {
		agentAccessBrowser.Lock()
		agentAccessBrowser.cost = agentCostSurface{}
		agentAccessBrowser.Unlock()
		return
	}
	mount := js.Global().Get("document").Call("getElementById", "agent-cost")
	if !mount.Truthy() {
		return
	}
	locale := domAttribute(mount, "data-locale")
	agentAccessBrowser.Lock()
	surface := &agentAccessBrowser.cost
	if surface.mount.Truthy() && surface.mount.Equal(mount) && surface.locale == locale {
		agentAccessBrowser.Unlock()
		return
	}
	*surface = agentCostSurface{mount: mount, locale: locale, state: productui.AgentAccessStateLoading}
	agentAccessBrowser.Unlock()
	go loadAgentCost(mount)
}

func renderAgentCost(mount js.Value) {
	agentAccessBrowser.Lock()
	surface := agentAccessBrowser.cost
	agentAccessBrowser.Unlock()
	locale := agentMountLocale(mount)
	nodes := []ui.Node{productui.AgentCostPanel(productui.AgentCostPanelProps{
		I18nProps: productui.I18nProps{Locale: locale}, State: surface.state, Report: surface.report,
		OnRetry: func() {},
	})}
	// Each owned agent's limits card is a placeholder the spend surface fills,
	// the same one the Agent setup page uses.
	if surface.state == productui.AgentAccessStateReady {
		for _, id := range surface.order {
			nodes = append(nodes, productui.AgentSpendLimitsMount(locale, id, surface.agents[id]))
		}
	}
	renderAgentMarkup(mount, html.Div(html.Props{}, nodes...))
	renderAgentSpendCards()
}

func loadAgentCost(mount js.Value) {
	cfg := agentAccessConfig()
	ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
	defer cancel()
	var reply agentCostReply
	err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodGet, agentCostAPI+"/report", nil, &reply)
	ui.PostAsync(func() {
		agentAccessBrowser.Lock()
		surface := &agentAccessBrowser.cost
		if !surface.mount.Truthy() || !surface.mount.Equal(mount) {
			agentAccessBrowser.Unlock()
			return
		}
		switch {
		case err == nil:
			surface.state, surface.report, surface.loaded = productui.AgentAccessStateReady, reply.Report, true
			surface.agents, surface.order = map[string]string{}, nil
			for _, agent := range reply.Agents {
				surface.agents[agent.ID] = agent.Name
				surface.order = append(surface.order, agent.ID)
			}
			for _, limit := range reply.Limits {
				agentAccessBrowser.spend.set(limit)
			}
		case !surface.loaded:
			surface.state = productui.AgentAccessStateUnavailable
		}
		agentAccessBrowser.Unlock()
		renderAgentCost(mount)
	})
}

func handleAgentCostClick(event js.Value) {
	button := agentClosest(event, "[data-agent-cost-action]")
	if !button.Truthy() {
		return
	}
	agentAccessBrowser.Lock()
	mount := agentAccessBrowser.cost.mount
	agentAccessBrowser.Unlock()
	if !mount.Truthy() || !mount.Call("contains", button).Bool() || domAttribute(button, "data-agent-cost-action") != "retry" {
		return
	}
	button.Set("disabled", true)
	go loadAgentCost(mount)
}

// ---- spend limit cards: the cost tab's and the Agent setup page's ----

type agentSpendSurface struct {
	limits   map[string]agentSpendLimitReply
	statuses map[string]string
	failed   bool
	loading  map[string]bool
}

func (s *agentSpendSurface) set(limit agentSpendLimitReply) {
	if s.limits == nil {
		s.limits = map[string]agentSpendLimitReply{}
	}
	s.limits[limit.AgentID] = limit
}

func findAgentSpendMounts() {
	mounts := js.Global().Get("document").Call("querySelectorAll", "[data-agent-spend-mount]")
	if mounts.Length() == 0 {
		return
	}
	agentAccessBrowser.Lock()
	surface := &agentAccessBrowser.spend
	if surface.loading == nil {
		surface.loading = map[string]bool{}
	}
	var missing []string
	for i := 0; i < mounts.Length(); i++ {
		id := domAttribute(mounts.Index(i), "data-agent-spend-mount")
		if _, known := surface.limits[id]; id != "" && !known && !surface.loading[id] && !surface.failed {
			surface.loading[id] = true
			missing = append(missing, id)
		}
	}
	agentAccessBrowser.Unlock()
	if len(missing) > 0 {
		go loadAgentSpend(missing)
		return
	}
	renderAgentSpendCards()
}

func loadAgentSpend(ids []string) {
	cfg := agentAccessConfig()
	ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
	defer cancel()
	var limits []agentSpendLimitReply
	err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodPost, agentCostAPI+"/limits", agentCostLimitsRequest{AgentIDs: ids}, &limits)
	ui.PostAsync(func() {
		agentAccessBrowser.Lock()
		surface := &agentAccessBrowser.spend
		for _, id := range ids {
			delete(surface.loading, id)
		}
		if err == nil {
			surface.failed = false
			for _, limit := range limits {
				surface.set(limit)
			}
		} else {
			surface.failed = true
		}
		agentAccessBrowser.Unlock()
		renderAgentSpendCards()
	})
}

// renderAgentSpendCards draws the card (or the failure) into every spend
// placeholder on the page. The values the person typed into a card stay: a card
// is only redrawn when its data or its status changed.
func renderAgentSpendCards() {
	mounts := js.Global().Get("document").Call("querySelectorAll", "[data-agent-spend-mount]")
	agentAccessBrowser.Lock()
	surface := agentAccessBrowser.spend
	agentAccessBrowser.Unlock()
	for i := 0; i < mounts.Length(); i++ {
		mount := mounts.Index(i)
		id := domAttribute(mount, "data-agent-spend-mount")
		locale := agentMountLocale(mount)
		limit, known := surface.limits[id]
		switch {
		case known:
			status := surface.statuses[id]
			if domAttribute(mount, "data-spend-drawn") == drawnKey(limit, status) {
				continue
			}
			// What the person typed survives a redraw that only adds a message.
			typedRuns, typedSpend := agentFormValueByField(mount, "runs_per_day"), agentFormValueByField(mount, "spend_per_day")
			renderAgentMarkup(mount, productui.AgentSpendLimitsCard(productui.AgentSpendLimitsProps{
				I18nProps: productui.I18nProps{Locale: locale}, AgentID: id, AgentName: domAttribute(mount, "data-agent-name"),
				MaxRunsPerDay: limit.MaxRunsPerDay, MaxSpendMicrosPerDay: limit.MaxSpendMicrosPerDay, Reached: limit.Reached,
				Client: agentAccessNoop{}, Status: status, Denied: limit.Denied,
			}))
			mount.Call("setAttribute", "data-spend-drawn", drawnKey(limit, status))
			if status == "invalid" || status == "failed" {
				for field, value := range map[string]string{"runs_per_day": typedRuns, "spend_per_day": typedSpend} {
					if input := mount.Call("querySelector", "[data-spend-field='"+field+"']"); input.Truthy() {
						input.Set("value", value)
					}
				}
			}
		case surface.failed:
			if domAttribute(mount, "data-spend-drawn") == "failed" {
				continue
			}
			renderAgentMarkup(mount, productui.AgentSpendLimitsFailed(locale))
			mount.Call("setAttribute", "data-spend-drawn", "failed")
		}
	}
}

func drawnKey(limit agentSpendLimitReply, status string) string {
	return strings.Join([]string{itoa(int(limit.MaxRunsPerDay)), itoa(int(limit.MaxSpendMicrosPerDay)), limit.Reached, status, boolKey(limit.Denied)}, "|")
}

func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func handleAgentSpendClick(event js.Value) {
	button := agentClosest(event, "[data-agent-spend-action]")
	if !button.Truthy() || domAttribute(button, "data-agent-spend-action") != "retry" {
		return
	}
	mount := agentClosest(event, "[data-agent-spend-mount]")
	if !mount.Truthy() {
		return
	}
	agentAccessBrowser.Lock()
	agentAccessBrowser.spend.failed = false
	agentAccessBrowser.Unlock()
	button.Set("disabled", true)
	findAgentSpendMounts()
}

func handleAgentSpendSubmit(event js.Value) {
	form := agentClosest(event, "form[data-agent-spend-form]")
	if !form.Truthy() {
		return
	}
	event.Call("preventDefault")
	agentID := domAttribute(form, "data-agent-spend-form")
	runs, spend, ok := productui.ParseAgentSpendLimitFields(agentFormValueByField(form, "runs_per_day"), agentFormValueByField(form, "spend_per_day"))
	if !ok {
		setAgentSpendStatus(agentID, "invalid")
		return
	}
	go func() {
		cfg := agentAccessConfig()
		ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
		defer cancel()
		var limits []agentSpendLimitReply
		err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodPost, agentCostAPI+"/limit", agentCostLimitRequest{AgentID: agentID, MaxRunsPerDay: runs, MaxSpendMicrosPerDay: spend}, &limits)
		ui.PostAsync(func() {
			agentAccessBrowser.Lock()
			surface := &agentAccessBrowser.spend
			if surface.statuses == nil {
				surface.statuses = map[string]string{}
			}
			if err == nil {
				for _, limit := range limits {
					surface.set(limit)
				}
				surface.statuses[agentID] = "saved"
			} else {
				surface.statuses[agentID] = "failed"
			}
			agentAccessBrowser.Unlock()
			renderAgentSpendCards()
		})
	}()
}

func setAgentSpendStatus(agentID, status string) {
	agentAccessBrowser.Lock()
	if agentAccessBrowser.spend.statuses == nil {
		agentAccessBrowser.spend.statuses = map[string]string{}
	}
	agentAccessBrowser.spend.statuses[agentID] = status
	agentAccessBrowser.Unlock()
	renderAgentSpendCards()
}

func agentFormValueByField(form js.Value, field string) string {
	input := form.Call("querySelector", "[data-spend-field='"+field+"']")
	if !input.Truthy() {
		return ""
	}
	return input.Get("value").String()
}
