package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The browser's side of the agent access, connection console, cost and
// extend-budget routes (internal/application/agent_access_http.go). Each call
// is one JSON request with the signed-in person's bearer; the answer is a typed
// error the page turns into a sentence, never a raw status.

const (
	agentAccessAPI = "/api/agent-access/v1"
	agentCostAPI   = "/api/agent-cost/v1"
	agentTaskAPI   = "/api/agent-tasks/v1/extend-budget"
)

var (
	errAgentAccessDenied      = errors.New("agent access: not permitted")
	errAgentAccessConflict    = errors.New("agent access: the state changed")
	errAgentAccessInvalid     = errors.New("agent access: not accepted")
	errAgentAccessRejected    = errors.New("agent access: the link request is not valid")
	errAgentAccessUnavailable = errors.New("agent access: unavailable")
)

type agentAccessLinkRequest struct{ ConnectionID string }
type agentAccessCompleteRequest struct{ State, Code string }
type agentAccessRevokeRequest struct{ TaskID string }
type agentAdminRevisionRequest struct{ RevisionID string }
type agentAdminImportRequest struct{ ConnectionID, SnapshotID string }
type agentAdminTierRequest struct{ RevisionID, SkillID, Tier string }
type agentAdminPreviewRequest struct{ RevisionID, UserID, Label string }
type agentAdminCreateRequest struct {
	Revision productui.AgentConnectionRevision
}
type agentAdminGrantRequest struct {
	ID                 string
	Roles              []string
	Population         string
	OrganizationScopes []string
	Skills             []string
}
type agentAdminGrantsRequest struct {
	RevisionID string
	Grants     []agentAdminGrantRequest
}
type agentCostLimitRequest struct {
	AgentID              string
	MaxRunsPerDay        int64
	MaxSpendMicrosPerDay int64
}
type agentCostLimitsRequest struct{ AgentIDs []string }
type agentTaskBudgetRequest struct{ TaskID, RequestID string }

// agentSpendLimitReply mirrors application.AgentSpendLimitView.
type agentSpendLimitReply struct {
	AgentID              string
	MaxRunsPerDay        int64
	MaxSpendMicrosPerDay int64
	Reached              string
	Denied               bool
}

// agentCostReply mirrors application.AgentCostReportView.
type agentCostReply struct {
	Report productui.AgentCostReport
	Agents []struct{ ID, Name string }
	Limits []agentSpendLimitReply
}

func agentAccessEndpoint(cfg journeyclient.Config, path string) (string, error) {
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" {
		return "", errAgentAccessUnavailable
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return "", errAgentAccessUnavailable
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = path, "", ""
	return endpoint.String(), nil
}

// agentAccessCall sends one request and decodes the answer into out when it is
// not nil. A refusal comes back as one of the typed errors above.
func agentAccessCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, path string, input, out any) error {
	target, err := agentAccessEndpoint(cfg, path)
	if err != nil {
		return err
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return errAgentAccessUnavailable
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &refusal)
		switch {
		case refusal.Code == "state_rejected":
			return errAgentAccessRejected
		case response.StatusCode == http.StatusForbidden:
			return errAgentAccessDenied
		case response.StatusCode == http.StatusConflict:
			return errAgentAccessConflict
		case response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusNotFound:
			return errAgentAccessInvalid
		}
		return errAgentAccessUnavailable
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return errAgentAccessUnavailable
		}
	}
	return nil
}

// agentAccessMessageKey names the sentence a refusal becomes on the person's
// own access page.
func agentAccessMessageKey(err error) string {
	switch {
	case errors.Is(err, errAgentAccessRejected):
		return "state_rejected"
	case errors.Is(err, errAgentAccessUnavailable):
		return "unavailable"
	}
	return "failed"
}

// agentAdminMessageKey names the sentence a refusal becomes on the console.
func agentAdminMessageKey(err error) string {
	switch {
	case errors.Is(err, errAgentAccessDenied):
		return "denied"
	case errors.Is(err, errAgentAccessConflict):
		return "conflict"
	case errors.Is(err, errAgentAccessInvalid):
		return "invalid"
	}
	return "failed"
}

// parseAgentSkillLines reads the new-revision form's skills box: one skill per
// line, the name and then its tier. A line with no tier is T4, the most
// restrictive, so a typo never lowers what a skill can do.
func parseAgentSkillLines(text string) []productui.AgentAdminSkillGrant {
	var out []productui.AgentAdminSkillGrant
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		tier := "T4"
		if len(fields) > 1 {
			tier = strings.ToUpper(fields[1])
		}
		out = append(out, productui.AgentAdminSkillGrant{SkillID: fields[0], SkillName: fields[0], Tier: tier})
	}
	return out
}

// splitAgentList reads a comma-separated field.
func splitAgentList(text string) []string {
	var out []string
	for _, part := range strings.Split(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// agentGrantRowsAfterRemoving returns the grants a revision keeps once one is
// removed.
func agentGrantRowsAfterRemoving(rows []productui.AgentGrantRow, grantID string) []agentAdminGrantRequest {
	out := make([]agentAdminGrantRequest, 0, len(rows))
	for _, row := range rows {
		if row.ID != grantID {
			out = append(out, agentAdminGrantRequest{ID: row.ID, Roles: row.Roles, Population: row.Population, OrganizationScopes: row.OrganizationScopes, Skills: row.Skills})
		}
	}
	return out
}

// agentGrantRowsWith returns a revision's grants plus one new grant, numbered
// after the highest existing one.
func agentGrantRowsWith(rows []productui.AgentGrantRow, roles, population, scopes string, skills []string) []agentAdminGrantRequest {
	out := make([]agentAdminGrantRequest, 0, len(rows)+1)
	highest := 0
	for _, row := range rows {
		out = append(out, agentAdminGrantRequest{ID: row.ID, Roles: row.Roles, Population: row.Population, OrganizationScopes: row.OrganizationScopes, Skills: row.Skills})
		if number := trailingNumber(row.ID); number > highest {
			highest = number
		}
	}
	return append(out, agentAdminGrantRequest{ID: "grant-" + itoa(highest+1), Roles: splitAgentList(roles), Population: strings.TrimSpace(population), OrganizationScopes: splitAgentList(scopes), Skills: skills})
}

func trailingNumber(id string) int {
	digits := ""
	for i := len(id) - 1; i >= 0 && id[i] >= '0' && id[i] <= '9'; i-- {
		digits = string(id[i]) + digits
	}
	number := 0
	for _, r := range digits {
		number = number*10 + int(r-'0')
	}
	return number
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
