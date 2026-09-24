package delivery

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

var ErrSMTPConfig = errors.New("delivery: invalid SMTP configuration")

// SMTPConfig configures a single authenticated SMTP submission endpoint.
// TLS is required: the adapter refuses servers that do not offer STARTTLS.
type SMTPConfig struct {
	Host     string
	Port     int
	From     string
	Username string
	Password string
	TLS      *tls.Config
	Timeout  time.Duration
}

// SMTPTransport is the protocol boundary used by SMTPProvider. It is exported
// so deployments and tests can supply a transport with the same envelope and
// message semantics.
type SMTPTransport interface {
	Send(context.Context, SMTPConfig, string, []string, []byte) error
}

// SendingDomainGate reads the current persisted verification status before
// each SMTP transaction. A cached SMTP configuration cannot authorize a send.
type SendingDomainGate interface {
	AuthorizeSendingDomain(context.Context, string, string) error
}

// SMTPProvider sends the Provider delivery contract as transactional email.
// Its Message-ID is stable for a tenant and idempotency key, allowing receivers
// and operational reconciliation to identify retries of the same logical send.
type SMTPProvider struct {
	config     SMTPConfig
	transport  SMTPTransport
	domainGate SendingDomainGate
}

// NewSMTPProvider validates configuration and returns an SMTP-backed provider.
// A nil transport selects the production STARTTLS SMTP transport.
func NewSMTPProvider(config SMTPConfig, transport SMTPTransport, domainGate SendingDomainGate) (*SMTPProvider, error) {
	config.Host = strings.TrimSpace(config.Host)
	if config.Port == 0 {
		config.Port = 587
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.Host == "" || strings.ContainsAny(config.Host, "\r\n \t/\\") || config.Port < 1 || config.Port > 65535 || config.Timeout <= 0 || (strings.TrimSpace(config.Username) == "") != (config.Password == "") {
		return nil, ErrSMTPConfig
	}
	if _, err := parseSingleAddress(config.From); err != nil {
		return nil, fmt.Errorf("%w: from address", ErrSMTPConfig)
	}
	if config.TLS != nil {
		config.TLS = config.TLS.Clone()
	}
	if transport == nil {
		transport = smtpTransport{}
	}
	return &SMTPProvider{config: config, transport: transport, domainGate: domainGate}, nil
}

// Send adapts a committed delivery into one SMTP transaction. It never
// retries within the call: SMTP cannot provide exactly-once semantics after an
// ambiguous disconnect, so retries retain the same deterministic Message-ID.
func (p *SMTPProvider) Send(ctx context.Context, d Delivery) (ProviderResult, error) {
	if p == nil || p.transport == nil || ctx == nil || strings.TrimSpace(d.TenantID) == "" || strings.TrimSpace(d.IdempotencyKey) == "" || strings.TrimSpace(d.RecipientRef) == "" || strings.TrimSpace(d.Purpose) == "" {
		return ProviderResult{}, ErrInvalidEmail
	}
	if d.AttentionOnly && strings.TrimSpace(d.Body) != "" {
		return ProviderResult{}, ErrDLPBlocked
	}
	if !validHeaderValue(d.Subject) || !validHeaderValue(d.Purpose) {
		return ProviderResult{}, ErrInvalidEmail
	}
	to, err := parseSingleAddress(d.RecipientRef)
	if err != nil {
		return ProviderResult{}, ErrInvalidEmail
	}
	from, err := parseSingleAddress(p.config.From)
	if err != nil {
		return ProviderResult{}, ErrSMTPConfig
	}
	if p.domainGate == nil {
		return ProviderResult{}, ErrUnverifiedDomain
	}
	fromDomain := from.Address[strings.LastIndex(from.Address, "@")+1:]
	if err := p.domainGate.AuthorizeSendingDomain(ctx, d.TenantID, strings.ToLower(fromDomain)); err != nil {
		return ProviderResult{}, fmt.Errorf("%w: %s", ErrUnverifiedDomain, strings.ToLower(fromDomain))
	}
	messageID := smtpMessageID(d.TenantID, d.IdempotencyKey, p.config.Host)
	message := buildSMTPMessage(from.String(), to.String(), d.Subject, d.Body, d.Purpose, messageID)
	if err := p.transport.Send(ctx, p.config, from.Address, []string{to.Address}, message); err != nil {
		return ProviderResult{}, fmt.Errorf("%w: SMTP submission failed", ErrProviderFailed)
	}
	return ProviderResult{Reference: messageID, Accepted: true}, nil
}

type smtpTransport struct{}

func (smtpTransport) Send(ctx context.Context, config SMTPConfig, from string, recipients []string, message []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	dialer := &net.Dialer{Timeout: config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	deadline := time.Now().Add(config.Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, config.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("SMTP server does not support STARTTLS")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host}
	if config.TLS != nil {
		tlsConfig = config.TLS.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = config.Host
		}
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		return err
	}
	if config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	// A successful DATA close is the SMTP acceptance boundary. QUIT is only
	// session cleanup; its failure must not turn an accepted message into a
	// retryable failure.
	_ = client.Quit()
	return nil
}

func parseSingleAddress(raw string) (mail.Address, error) {
	if strings.TrimSpace(raw) == "" || hasHeaderBreak(raw) {
		return mail.Address{}, ErrInvalidEmail
	}
	address, err := mail.ParseAddress(raw)
	if err != nil || address.Address == "" || strings.ContainsAny(address.Address, "\r\n") {
		return mail.Address{}, ErrInvalidEmail
	}
	return *address, nil
}

func buildSMTPMessage(from, to, subject, body, purpose, messageID string) []byte {
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\nTo: ")
	b.WriteString(to)
	b.WriteString("\r\nSubject: ")
	b.WriteString(mime.QEncoding.Encode("UTF-8", subject))
	b.WriteString("\r\nMessage-ID: <")
	b.WriteString(messageID)
	b.WriteString(">\r\nX-HCM-Purpose: ")
	b.WriteString(purpose)
	b.WriteString("\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(normalizeCRLF(body))
	if !strings.HasSuffix(b.String(), "\r\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func smtpMessageID(tenantID, key, host string) string {
	sum := sha256.Sum256([]byte(tenantID + "\x00" + key))
	return hex.EncodeToString(sum[:]) + "@" + host
}

func hasHeaderBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func validHeaderValue(value string) bool {
	if hasHeaderBreak(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

var _ Provider = (*SMTPProvider)(nil)
var _ SMTPTransport = smtpTransport{}
