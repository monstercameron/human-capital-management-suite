package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentUXDemoLocalSupportObjects keeps the stored customer emails and plans as
// immutable, content-addressed files under the local cell's artifact root, one
// directory per tenant. It exists for the local development cell only: the
// agent database holds the receipt (who, which digest), and these files are the
// governed object the receipt names. A stored file is never replaced; a write of
// the same bytes is a no-op and a write of different bytes under the same name
// is refused.
type AgentUXDemoLocalSupportObjects struct{ Root string }

var agentUXDemoObjectTenant = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

const agentUXDemoObjectPrefix = "local-object:"

func (o AgentUXDemoLocalSupportObjects) path(tenant, ref string) (string, error) {
	digest := strings.TrimPrefix(ref, agentUXDemoObjectPrefix)
	if strings.TrimSpace(o.Root) == "" || tenant != localAgentDemoTenant || !agentUXDemoObjectTenant.MatchString(tenant) || !strings.HasPrefix(ref, agentUXDemoObjectPrefix) || len(digest) != 64 {
		return "", ErrAgentUXDemoSupportDenied
	}
	if _, err := hex.DecodeString(digest); err != nil || digest != strings.ToLower(digest) {
		return "", ErrAgentUXDemoSupportDenied
	}
	return filepath.Join(o.Root, "support-objects", tenant, digest+".json"), nil
}

func (o AgentUXDemoLocalSupportObjects) put(tenant string, raw []byte) (string, error) {
	sum := sha256.Sum256(raw)
	ref := agentUXDemoObjectPrefix + hex.EncodeToString(sum[:])
	path, err := o.path(tenant, ref)
	if err != nil {
		return "", err
	}
	if current, readErr := os.ReadFile(path); readErr == nil {
		if !bytes.Equal(current, raw) {
			return "", ErrAgentUXDemoSupportDenied
		}
		return ref, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".object-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(raw); err != nil || file.Sync() != nil {
		_ = file.Close()
		return "", errors.New("persist support object")
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		// A concurrent writer of the same bytes won the rename; that is success.
		if current, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(current, raw) {
			return ref, nil
		}
		return "", err
	}
	return ref, nil
}

func (o AgentUXDemoLocalSupportObjects) PutSupportEmail(_ context.Context, tenant, _ string, raw []byte) (string, error) {
	return o.put(tenant, raw)
}

func (o AgentUXDemoLocalSupportObjects) PutSupportPlan(_ context.Context, tenant, _ string, raw []byte) (string, error) {
	return o.put(tenant, raw)
}

func (o AgentUXDemoLocalSupportObjects) ReadSupportObject(_ context.Context, tenant, ref string) ([]byte, error) {
	path, err := o.path(tenant, ref)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrAgentUXDemoSupportDenied
	}
	return raw, nil
}

// AgentUXDemoSupportOwner lets an administrator of the local demo workspace
// simulate a customer email. It is the simulation's only authority: a person
// who is not an administrator, a service or an agent is refused, and so is any
// other workspace.
type AgentUXDemoSupportOwner struct{}

func (AgentUXDemoSupportOwner) AuthorizeSupportInboxOwner(_ context.Context, p *trust.Principal) error {
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant().String() != localAgentDemoTenant || !p.HasRole("hcm_admin") {
		return chatcore.ErrPermissionDenied
	}
	return nil
}

// AgentUXDemoLocalSupportPlanner classifies an email by rules, with no model:
// the local cell's stand-in for the governed model step, so a sample email can
// become a ticket without a paid call. It reads the same quarantined envelope a
// model would and writes the same plan; the desk decodes it as strictly as a
// model's, so a rule can never do more than a model could.
type AgentUXDemoLocalSupportPlanner struct{}

var agentUXDemoSentence = regexp.MustCompile(`[^.!?]+[.!?]?`)

func (AgentUXDemoLocalSupportPlanner) PlanSupportEmail(_ context.Context, _, _, _ string, messages []agentmodel.ModelMessage) ([]byte, error) {
	if len(messages) < 2 {
		return nil, ErrAgentUXDemoSupportDenied
	}
	lines := strings.Split(messages[1].Content, "\n")
	var email struct{ Subject, Body string }
	if len(lines) < 3 || json.Unmarshal([]byte(lines[1]), &email) != nil {
		return nil, ErrAgentUXDemoSupportDenied
	}
	title := agentUXDemoSupportPlainText(email.Subject, 80)
	if title == "" {
		title = "Customer email"
	}
	var sentences []string
	for _, sentence := range agentUXDemoSentence.FindAllString(email.Body, -1) {
		if text := strings.TrimSpace(sentence); text != "" {
			sentences = append(sentences, text)
		}
		if len(sentences) == 2 {
			break
		}
	}
	summary := agentUXDemoSupportPlainText(strings.Join(sentences, " "), 600)
	if summary == "" {
		summary = "The customer wrote in; the message has no readable text."
	}
	text := strings.ToLower(email.Subject + " " + email.Body)
	severity := "Normal"
	switch {
	case strings.Contains(text, "outage") || strings.Contains(text, "is down") || strings.Contains(text, "cannot sign in") || strings.Contains(text, "cannot log in"):
		severity = "High"
	case strings.Contains(text, "thank") && !strings.Contains(text, "?"):
		severity = "Low"
	}
	return json.Marshal(AgentUXDemoSupportPlan{Title: title, Summary: summary, Severity: severity})
}

// agentUXDemoSupportPlainText is the text a plan field may carry: one line, no
// markup, no control characters, at most max characters.
func agentUXDemoSupportPlainText(text string, max int) string {
	var out strings.Builder
	for _, r := range strings.Join(strings.Fields(text), " ") {
		switch {
		case r == '<' || r == '>' || r < 0x20 || r == 0x7f:
			continue
		}
		out.WriteRune(r)
	}
	value := strings.TrimSpace(out.String())
	for utf8.RuneCountInString(value) > max {
		runes := []rune(value)
		value = strings.TrimSpace(string(runes[:max]))
	}
	return value
}

// AgentUXDemoChatSupportAlerts posts the alert as the support agent. In a public
// channel the agent is installed in, it takes the chat store's public-agent
// write; in a private channel (the incident channel is one) it writes as the
// member the demo preparation added it as. A conversation where the agent is
// neither is refused, and the alert says only what the ticket is: its title,
// severity, claimed customer and link, never the email's text.
type AgentUXDemoChatSupportAlerts struct {
	Chat  *chatstore.Store
	Agent string
}

func (a AgentUXDemoChatSupportAlerts) PostSupportAlert(ctx context.Context, tenant, conversation string, alert AgentUXDemoSupportAlert, key string) (string, error) {
	if a.Chat == nil || a.Agent == "" || key == "" || !strings.HasPrefix(alert.TicketHref, "/workspace/app/project?") {
		return "", ErrAgentUXDemoSupportUnavailable
	}
	body := fmt.Sprintf("Support ticket: %s\nSeverity: %s. Customer (claimed, unverified): %s.", alert.Title, alert.Severity, alert.ClaimedCustomer)
	if alert.NeedsHumanReview {
		body += " Needs human review."
	}
	body += fmt.Sprintf("\n\n[Open the ticket](%s)", alert.TicketHref)
	if post, err := a.Chat.PostAmbientAgentMessage(ctx, tenant, conversation, a.Agent, body, key); err == nil {
		return post.ID, nil
	} else if !errors.Is(err, chatstore.ErrNotMember) {
		return "", err
	}
	post, err := chatstore.NewAdapter(a.Chat).SendPost(ctx, chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: tenant, SubjectID: a.Agent}, TenantID: tenant, ConversationID: conversation, IdempotencyKey: key}, chatcore.Post{AuthorID: a.Agent, AuthorHomeTenantID: tenant, Body: body})
	if err != nil {
		return "", err
	}
	return post.ID, nil
}
