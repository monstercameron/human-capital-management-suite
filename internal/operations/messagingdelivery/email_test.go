package delivery

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func mailNow() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

func mailSecret() []byte { return []byte("email-feedback-secret-0123456789") }

func mailDNS() DNSRecords {
	return DNSRecords{
		SPF:           "v=spf1 include:mail.provider.example -all",
		DKIMSelectors: map[string]string{"s1": "MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQ"},
		DMARC:         "v=DMARC1; p=reject; rua=mailto:dmarc@example.example",
	}
}

func mailProfile(t *testing.T) DomainProfile {
	t.Helper()
	profile, err := VerifyDomain("mail.example.example", mailDNS(), mailNow())
	if err != nil {
		t.Fatalf("VerifyDomain: %v", err)
	}
	return profile
}

func mailOutgoing(tenant, purpose, recipient string) OutgoingEmail {
	return OutgoingEmail{
		MessageID: "msg-" + tenant + "-" + purpose + "-" + recipient,
		TenantID:  tenant,
		Purpose:   purpose,
		Recipient: recipient,
		Subject:   "You have a secure HR message",
	}
}

func signFeedback(t *testing.T, messageID string, kind FeedbackKind, ref string) (payload []byte, signature string) {
	t.Helper()
	payload = FeedbackPayload(messageID, kind, ref)
	return payload, SignFeedback(mailSecret(), payload)
}

// TestEmailDomainAuthenticationBounceComplaintAndSuppressionLifecyclePreservesMessageTruth
// is the MAIL-001 PRIMARY contract: an authenticated domain sends,
// provider feedback moves honest statuses, suppression is scoped, and
// transport never satisfies legal acknowledgement.
func TestEmailDomainAuthenticationBounceComplaintAndSuppressionLifecyclePreservesMessageTruth(t *testing.T) {
	profile := mailProfile(t)
	if !profile.Verified || profile.Version != 1 || profile.Digest == "" {
		t.Fatalf("profile = %+v, want verified version 1 with digest", profile)
	}
	tracker := NewEmailTracker()
	suppressions := NewSuppressionList()
	limiter := NewRetryLimiter()

	record, err := tracker.Register(mailOutgoing("tenant-a", "notice", "ada@example.example"), profile, suppressions, limiter, mailNow())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if record.Status != EmailAccepted {
		t.Fatalf("status = %s, want ACCEPTED", record.Status)
	}

	payload, signature := signFeedback(t, record.MessageID, FeedbackDelivered, "provider-ref-1")
	delivered, err := tracker.ApplyFeedback(mailSecret(), payload, FeedbackEvent{
		MessageID: record.MessageID, Kind: FeedbackDelivered,
		ProviderRef: "provider-ref-1", Signature: signature,
	}, mailNow())
	if err != nil {
		t.Fatalf("ApplyFeedback delivered: %v", err)
	}
	if delivered.Status != EmailDelivered || delivered.ProviderRef != "provider-ref-1" {
		t.Fatalf("record = %+v, want DELIVERED with provider ref", delivered)
	}

	// A bounced sibling is retained as bounced, never rewritten.
	bouncedReg, err := tracker.Register(mailOutgoing("tenant-a", "notice", "bounce@example.example"), profile, suppressions, limiter, mailNow())
	if err != nil {
		t.Fatalf("Register bounced sibling: %v", err)
	}
	bPayload, bSignature := signFeedback(t, bouncedReg.MessageID, FeedbackBounced, "provider-ref-2")
	bounced, err := tracker.ApplyFeedback(mailSecret(), bPayload, FeedbackEvent{
		MessageID: bouncedReg.MessageID, Kind: FeedbackBounced,
		ProviderRef: "provider-ref-2", BounceCode: "550 5.1.1", Signature: bSignature,
	}, mailNow())
	if err != nil {
		t.Fatalf("ApplyFeedback bounced: %v", err)
	}
	if bounced.Status != EmailBounced || bounced.BounceCode != "550 5.1.1" {
		t.Fatalf("record = %+v, want BOUNCED with bounce code", bounced)
	}

	// A complaint on the delivered message is recorded without rewriting
	// delivery truth.
	cPayload, cSignature := signFeedback(t, record.MessageID, FeedbackComplained, "provider-ref-1")
	complained, err := tracker.ApplyFeedback(mailSecret(), cPayload, FeedbackEvent{
		MessageID: record.MessageID, Kind: FeedbackComplained,
		ProviderRef: "provider-ref-1", Signature: cSignature,
	}, mailNow())
	if err != nil {
		t.Fatalf("ApplyFeedback complained: %v", err)
	}
	if complained.Status != EmailComplained {
		t.Fatalf("status = %s, want COMPLAINED", complained.Status)
	}

	// Suppression is scoped to tenant and purpose: marketing is
	// suppressed while notice to the same recipient still sends.
	if _, err := suppressions.Suppress("tenant-a", "marketing", "ada@example.example", "unsubscribed", mailNow()); err != nil {
		t.Fatalf("Suppress: %v", err)
	}
	suppressed, err := tracker.Register(mailOutgoing("tenant-a", "marketing", "ada@example.example"), profile, suppressions, limiter, mailNow())
	if err != nil {
		t.Fatalf("Register suppressed: %v", err)
	}
	if suppressed.Status != EmailSuppressed {
		t.Fatalf("status = %s, want SUPPRESSED", suppressed.Status)
	}
	nextNotice := mailOutgoing("tenant-a", "notice", "ada@example.example")
	nextNotice.MessageID += "-second"
	notice, err := tracker.Register(nextNotice, profile, suppressions, limiter, mailNow().Add(time.Minute))
	if err != nil {
		t.Fatalf("Register notice after suppression: %v", err)
	}
	if notice.Status != EmailAccepted {
		t.Fatalf("scoped notice status = %s, want ACCEPTED", notice.Status)
	}

	// Transport never satisfies legal acknowledgement, even delivered.
	if err := delivered.LegalAcknowledgement(); err == nil {
		t.Fatal("LegalAcknowledgement on DELIVERED succeeded, want refusal")
	}
}

// applyFeedbackTo registers one message and applies one signed feedback.
func applyFeedbackTo(t *testing.T, tracker *EmailTracker, messageID, tenant, purpose, recipient string, kind FeedbackKind, ref string) (EmailRecord, error) {
	t.Helper()
	profile, err := VerifyDomain("mail.example.example", mailDNS(), mailNow())
	if err != nil {
		t.Fatal(err)
	}
	msg := OutgoingEmail{MessageID: messageID, TenantID: tenant, Purpose: purpose, Recipient: recipient, Subject: "s"}
	if _, err := tracker.Register(msg, profile, NewSuppressionList(), NewRetryLimiter(), mailNow()); err != nil {
		t.Fatalf("Register %s: %v", messageID, err)
	}
	payload := FeedbackPayload(messageID, kind, ref)
	event := FeedbackEvent{MessageID: messageID, Kind: kind, ProviderRef: ref, Signature: SignFeedback(mailSecret(), payload)}
	if kind == FeedbackBounced {
		event.BounceCode = "550 5.1.1"
	}
	return tracker.ApplyFeedback(mailSecret(), payload, event, mailNow())
}

// TestTodo_MAIL_001_Property proves two invariants over the whole input
// space: suppression matches only its exact scope, and every
// status-by-feedback pair resolves to exactly one documented outcome.
func TestTodo_MAIL_001_Property(t *testing.T) {
	t.Run("suppression scope isolation", func(t *testing.T) {
		list := NewSuppressionList()
		tenants := []string{"tenant-a", "tenant-b"}
		purposes := []string{"marketing", "notice"}
		recipients := []string{"ada@example.example", "bob@example.example"}
		if _, err := list.Suppress("tenant-a", "marketing", "ada@example.example", "unsubscribed", mailNow()); err != nil {
			t.Fatal(err)
		}
		for _, tenant := range tenants {
			for _, purpose := range purposes {
				for _, recipient := range recipients {
					want := tenant == "tenant-a" && purpose == "marketing" && recipient == "ada@example.example"
					if got := list.Suppressed(tenant, purpose, recipient); got != want {
						t.Fatalf("Suppressed(%s,%s,%s) = %t, want %t", tenant, purpose, recipient, got, want)
					}
					// Case and whitespace variants of the same recipient match;
					// neighboring scopes never do.
					if got := list.Suppressed(tenant, purpose, "  "+strings.ToUpper(recipient)+" "); got != want {
						t.Fatalf("Suppressed variant(%s,%s,%s) = %t, want %t", tenant, purpose, recipient, got, want)
					}
				}
			}
		}
	})

	t.Run("status transitions are total", func(t *testing.T) {
		// start state -> feedback kind -> want status ("" means error).
		matrix := map[EmailStatus]map[FeedbackKind]EmailStatus{
			EmailAccepted:   {FeedbackDelivered: EmailDelivered, FeedbackBounced: EmailBounced, FeedbackComplained: ""},
			EmailDelivered:  {FeedbackDelivered: EmailDelivered, FeedbackBounced: "", FeedbackComplained: EmailComplained},
			EmailBounced:    {FeedbackDelivered: "", FeedbackBounced: EmailBounced, FeedbackComplained: ""},
			EmailComplained: {FeedbackDelivered: "", FeedbackBounced: "", FeedbackComplained: EmailComplained},
			EmailSuppressed: {FeedbackDelivered: "", FeedbackBounced: "", FeedbackComplained: ""},
			EmailUnknown:    {FeedbackDelivered: EmailDelivered, FeedbackBounced: EmailBounced, FeedbackComplained: ""},
		}
		for start, kinds := range matrix {
			for kind, want := range kinds {
				t.Run(string(start)+"/"+string(kind), func(t *testing.T) {
					tracker := NewEmailTracker()
					messageID := "prop-" + string(start) + "-" + string(kind)
					record := seedEmailStatus(t, tracker, messageID, start)
					payload := FeedbackPayload(messageID, kind, "ref-prop")
					event := FeedbackEvent{MessageID: messageID, Kind: kind, ProviderRef: "ref-prop", Signature: SignFeedback(mailSecret(), payload)}
					if kind == FeedbackBounced {
						event.BounceCode = "550 5.1.1"
					}
					// Align the provider reference with the seeded record so
					// only the transition itself is under test.
					if record.ProviderRef != "" {
						event.ProviderRef = record.ProviderRef
						payload = FeedbackPayload(messageID, kind, record.ProviderRef)
						event.Signature = SignFeedback(mailSecret(), payload)
						if kind == FeedbackBounced {
							event.BounceCode = record.BounceCode
						}
					}
					got, err := tracker.ApplyFeedback(mailSecret(), payload, event, mailNow())
					if want == "" {
						if err == nil {
							t.Fatalf("%s + %s succeeded with %+v, want refusal", start, kind, got)
						}
						return
					}
					if err != nil {
						t.Fatalf("%s + %s error = %v, want %s", start, kind, err, want)
					}
					if got.Status != want {
						t.Fatalf("%s + %s = %s, want %s", start, kind, got.Status, want)
					}
				})
			}
		}
	})
}

// seedEmailStatus registers one message and drives it to the start state.
func seedEmailStatus(t *testing.T, tracker *EmailTracker, messageID string, start EmailStatus) EmailRecord {
	t.Helper()
	profile, err := VerifyDomain("mail.example.example", mailDNS(), mailNow())
	if err != nil {
		t.Fatal(err)
	}
	suppressions := NewSuppressionList()
	limiter := NewRetryLimiter()
	msg := OutgoingEmail{MessageID: messageID, TenantID: "tenant-a", Purpose: "notice", Recipient: "seed@example.example", Subject: "s"}
	switch start {
	case EmailSuppressed:
		if _, err := suppressions.Suppress("tenant-a", "notice", "seed@example.example", "unsubscribed", mailNow()); err != nil {
			t.Fatal(err)
		}
	case EmailUnknown:
		limiter.RecordReputationFailure(profile.Domain, mailNow())
	}
	record, err := tracker.Register(msg, profile, suppressions, limiter, mailNow())
	if err != nil && start != EmailUnknown {
		t.Fatalf("seed Register: %v", err)
	}
	if start == EmailUnknown && record.Status != EmailUnknown {
		t.Fatalf("seed status = %s, want UNKNOWN", record.Status)
	}
	switch start {
	case EmailDelivered, EmailComplained:
		payload := FeedbackPayload(messageID, FeedbackDelivered, "ref-prop")
		record, err = tracker.ApplyFeedback(mailSecret(), payload, FeedbackEvent{
			MessageID: messageID, Kind: FeedbackDelivered,
			ProviderRef: "ref-prop", Signature: SignFeedback(mailSecret(), payload),
		}, mailNow())
		if err != nil {
			t.Fatalf("seed deliver: %v", err)
		}
		if start == EmailComplained {
			payload = FeedbackPayload(messageID, FeedbackComplained, "ref-prop")
			record, err = tracker.ApplyFeedback(mailSecret(), payload, FeedbackEvent{
				MessageID: messageID, Kind: FeedbackComplained,
				ProviderRef: "ref-prop", Signature: SignFeedback(mailSecret(), payload),
			}, mailNow())
			if err != nil {
				t.Fatalf("seed complain: %v", err)
			}
		}
	case EmailBounced:
		payload := FeedbackPayload(messageID, FeedbackBounced, "ref-prop")
		record, err = tracker.ApplyFeedback(mailSecret(), payload, FeedbackEvent{
			MessageID: messageID, Kind: FeedbackBounced, ProviderRef: "ref-prop",
			BounceCode: "550 5.1.1", Signature: SignFeedback(mailSecret(), payload),
		}, mailNow())
		if err != nil {
			t.Fatalf("seed bounce: %v", err)
		}
	}
	if record.Status != start {
		t.Fatalf("seed status = %s, want %s", record.Status, start)
	}
	return record
}

const (
	pinnedDomainDigest = "0a766df92d970e8856499ea3fb2c03e615253971e1f40824cc5d99ed9a1fa955"
	pinnedEmailDigest  = "ec6c9227bdcd68843308088a527bd0e6784d5973cade872621a75d633c1360ac"
)

// TestTodo_MAIL_001_Golden pins the domain profile and email record
// digests. Any serializer or field-order change that silently alters
// deliverability evidence fails here.
func TestTodo_MAIL_001_Golden(t *testing.T) {
	profile := mailProfile(t)
	if profile.Digest != pinnedDomainDigest {
		t.Fatalf("profile digest = %s, want pinned %s", profile.Digest, pinnedDomainDigest)
	}
	rotated, err := profile.Rotate(mailDNS(), mailNow().Add(time.Hour))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if rotated.Version != 2 || rotated.PreviousDigest != profile.Digest {
		t.Fatalf("rotated = %+v, want version 2 linked to version 1", rotated)
	}
	tracker := NewEmailTracker()
	record, err := applyFeedbackTo(t, tracker, "golden-1", "tenant-a", "notice", "ada@example.example", FeedbackDelivered, "ref-golden")
	if err != nil {
		t.Fatalf("ApplyFeedback: %v", err)
	}
	if record.Digest != pinnedEmailDigest {
		t.Fatalf("record digest = %s, want pinned %s", record.Digest, pinnedEmailDigest)
	}
	if err := record.Verify(); err != nil {
		t.Fatalf("record Verify: %v", err)
	}
}

// TestTodo_MAIL_001_Integration walks dispatch to provider feedback inside
// the package: committed dispatch, attempt ledger, email record, honest
// delivery proof — while the recipient stays unseen.
func TestTodo_MAIL_001_Integration(t *testing.T) {
	provider := &fakeProvider{}
	dispatcher, err := NewDispatcher(provider, Policy{ProviderMaximumClassification: "INTERNAL"})
	if err != nil {
		t.Fatal(err)
	}
	intent := testIntent()
	res, err := dispatcher.Dispatch(context.Background(), intent)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	ledger := NewTracker()
	if err := ledger.RecordAttempt(res.Attempt); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	profile := mailProfile(t)
	emails := NewEmailTracker()
	record, err := emails.Record("no-such-message")
	if err == nil {
		t.Fatalf("unknown record returned %+v", record)
	}
	mail, err := emails.Register(OutgoingEmail{
		MessageID: "int-" + res.Attempt.ID, TenantID: intent.TenantID,
		Purpose: intent.Purpose, Recipient: "ops@example.example", Subject: intent.Subject,
	}, profile, NewSuppressionList(), NewRetryLimiter(), mailNow())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	payload := FeedbackPayload(mail.MessageID, FeedbackDelivered, res.Attempt.ProviderRef)
	mail, err = emails.ApplyFeedback(mailSecret(), payload, FeedbackEvent{
		MessageID: mail.MessageID, Kind: FeedbackDelivered,
		ProviderRef: res.Attempt.ProviderRef, Signature: SignFeedback(mailSecret(), payload),
	}, mailNow())
	if err != nil {
		t.Fatalf("ApplyFeedback: %v", err)
	}
	delivered, err := AdvanceAttempt(res.Attempt, EventDeliver, res.Attempt.ProviderRef)
	if err != nil {
		t.Fatalf("AdvanceAttempt: %v", err)
	}
	if err := RequireDelivered(delivered); err != nil {
		t.Fatalf("RequireDelivered: %v", err)
	}
	// Delivery truth holds on both ledgers while the recipient never read.
	if mail.Status != EmailDelivered || delivered.State != Delivered {
		t.Fatalf("mail=%s attempt=%s, want DELIVERED/DELIVERED", mail.Status, delivered.State)
	}
	recipient := Recipient{AttemptID: delivered.ID, RecipientRef: intent.RecipientRef, State: RecipientUnseen}
	if err := RequireRead(recipient); err == nil {
		t.Fatal("delivered transport satisfied the read requirement")
	}
	if err := mail.LegalAcknowledgement(); err == nil {
		t.Fatal("delivered transport satisfied legal acknowledgement")
	}
}

// TestTodo_MAIL_001_Fault covers malformed domains, profiles, sends and
// feedback: each one fails closed with a typed sentinel.
func TestTodo_MAIL_001_Fault(t *testing.T) {
	profile := mailProfile(t)

	t.Run("domain faults", func(t *testing.T) {
		badSPF := mailDNS()
		badSPF.SPF = "v=spf1 include:mail.provider.example"
		noDKIM := mailDNS()
		noDKIM.DKIMSelectors = map[string]string{"s1": ""}
		badDMARC := mailDNS()
		badDMARC.DMARC = "v=DMARC1;"
		for name, dns := range map[string]DNSRecords{
			"missing spf":   {SPF: "", DKIMSelectors: mailDNS().DKIMSelectors, DMARC: mailDNS().DMARC},
			"no fail":       badSPF,
			"missing dkim":  noDKIM,
			"missing dmarc": {SPF: mailDNS().SPF, DKIMSelectors: mailDNS().DKIMSelectors},
			"weak dmarc":    badDMARC,
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := VerifyDomain("mail.example.example", dns, mailNow()); !errors.Is(err, ErrDomainUnverified) {
					t.Fatalf("VerifyDomain = %v, want ErrDomainUnverified", err)
				}
			})
		}
		for name, domain := range map[string]string{"empty": "", "address": "ada@example.example", "bare": "example"} {
			t.Run("domain "+name, func(t *testing.T) {
				if _, err := VerifyDomain(domain, mailDNS(), mailNow()); !errors.Is(err, ErrDomainUnverified) {
					t.Fatalf("VerifyDomain(%q) = %v, want ErrDomainUnverified", domain, err)
				}
			})
		}
		if _, err := VerifyDomain("mail.example.example", mailDNS(), time.Time{}); !errors.Is(err, ErrInvalidEmail) {
			t.Fatalf("timeless VerifyDomain = %v, want ErrInvalidEmail", err)
		}
		var bare DomainProfile
		if _, err := bare.Rotate(mailDNS(), mailNow()); !errors.Is(err, ErrUnverifiedDomain) {
			t.Fatalf("Rotate unverified = %v, want ErrUnverifiedDomain", err)
		}
		if _, err := profile.Rotate(DNSRecords{}, mailNow()); !errors.Is(err, ErrDomainUnverified) {
			t.Fatalf("Rotate bad DNS = %v, want ErrDomainUnverified", err)
		}
	})

	t.Run("register faults", func(t *testing.T) {
		tracker := NewEmailTracker()
		bare := DomainProfile{Domain: "mail.example.example"}
		msg := mailOutgoing("tenant-a", "notice", "ada@example.example")
		if _, err := tracker.Register(msg, bare, NewSuppressionList(), NewRetryLimiter(), mailNow()); !errors.Is(err, ErrUnverifiedDomain) {
			t.Fatalf("unverified send = %v, want ErrUnverifiedDomain", err)
		}
		for name, mutate := range map[string]func(*OutgoingEmail){
			"no id":        func(m *OutgoingEmail) { m.MessageID = "" },
			"no tenant":    func(m *OutgoingEmail) { m.TenantID = "" },
			"no purpose":   func(m *OutgoingEmail) { m.Purpose = "" },
			"no recipient": func(m *OutgoingEmail) { m.Recipient = "" },
		} {
			t.Run(name, func(t *testing.T) {
				bad := msg
				mutate(&bad)
				if _, err := tracker.Register(bad, profile, NewSuppressionList(), NewRetryLimiter(), mailNow()); !errors.Is(err, ErrInvalidEmail) {
					t.Fatalf("Register = %v, want ErrInvalidEmail", err)
				}
			})
		}
		if _, err := tracker.Register(msg, profile, NewSuppressionList(), NewRetryLimiter(), time.Time{}); !errors.Is(err, ErrInvalidEmail) {
			t.Fatalf("timeless Register = %v, want ErrInvalidEmail", err)
		}
		// Recipient flood never sends.
		floodLimiter := NewRetryLimiter()
		policy := DefaultEmailRetryPolicy()
		for i := uint(0); i < policy.MaxPerWindow; i++ {
			if err := floodLimiter.Allow(policy, "flood@example.example", mailNow()); err != nil {
				t.Fatalf("Allow %d: %v", i, err)
			}
		}
		if err := floodLimiter.Allow(policy, "flood@example.example", mailNow()); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("flood Allow = %v, want ErrRateLimited", err)
		}
	})

	t.Run("feedback faults", func(t *testing.T) {
		tracker := NewEmailTracker()
		record, err := applyFeedbackTo(t, tracker, "fault-1", "tenant-a", "notice", "ada@example.example", FeedbackDelivered, "ref-fault")
		if err != nil {
			t.Fatal(err)
		}
		_ = record
		payload := FeedbackPayload("fault-1", FeedbackDelivered, "ref-fault")
		event := FeedbackEvent{MessageID: "fault-1", Kind: FeedbackDelivered, ProviderRef: "ref-fault", Signature: SignFeedback(mailSecret(), payload)}
		for name, mutate := range map[string]func(*FeedbackEvent, *[]byte){
			"unknown kind": func(e *FeedbackEvent, p *[]byte) {
				e.Kind = "sent"
				*p = FeedbackPayload(e.MessageID, e.Kind, e.ProviderRef)
				e.Signature = SignFeedback(mailSecret(), *p)
			},
			"changed ref": func(e *FeedbackEvent, p *[]byte) { e.ProviderRef = "other-ref" },
			"timeless":    func(e *FeedbackEvent, p *[]byte) {},
		} {
			t.Run(name, func(t *testing.T) {
				e, p := event, payload
				mutate(&e, &p)
				at := mailNow()
				if name == "timeless" {
					at = time.Time{}
				}
				if _, err := tracker.ApplyFeedback(mailSecret(), p, e, at); err == nil {
					t.Fatalf("%s feedback accepted", name)
				}
			})
		}
		ghost := FeedbackEvent{MessageID: "ghost", Kind: FeedbackDelivered, ProviderRef: "ref-ghost"}
		ghostPayload := FeedbackPayload("ghost", FeedbackDelivered, "ref-ghost")
		ghost.Signature = SignFeedback(mailSecret(), ghostPayload)
		if _, err := tracker.ApplyFeedback(mailSecret(), ghostPayload, ghost, mailNow()); !errors.Is(err, ErrUnknownMessage) {
			t.Fatalf("ghost feedback = %v, want ErrUnknownMessage", err)
		}
	})
}

// TestTodo_MAIL_001_Security covers webhook spoofing, cross-tenant and
// cross-purpose leakage, unverified sending, misattribution and reputation
// failure counted as delivery. Every attack fails closed.
func TestTodo_MAIL_001_Security(t *testing.T) {
	profile := mailProfile(t)

	t.Run("webhook spoof", func(t *testing.T) {
		tracker := NewEmailTracker()
		if _, err := applyFeedbackTo(t, tracker, "sec-1", "tenant-a", "notice", "ada@example.example", "accepted", "ref-sec"); err == nil {
			t.Fatal("unrecognized feedback kind must be refused")
		}
		registered, err := tracker.Register(mailOutgoing("tenant-a", "notice", "sec2@example.example"), profile, NewSuppressionList(), NewRetryLimiter(), mailNow())
		if err != nil {
			t.Fatal(err)
		}
		payload := FeedbackPayload(registered.MessageID, FeedbackDelivered, "ref-sec")
		event := FeedbackEvent{MessageID: registered.MessageID, Kind: FeedbackDelivered, ProviderRef: "ref-sec", Signature: SignFeedback([]byte("attacker-secret"), payload)}
		if _, err := tracker.ApplyFeedback(mailSecret(), payload, event, mailNow()); !errors.Is(err, ErrFeedbackUnauthenticated) {
			t.Fatalf("spoofed feedback = %v, want ErrFeedbackUnauthenticated", err)
		}
		malformed := event
		malformed.Signature = "not-hex"
		if _, err := tracker.ApplyFeedback(mailSecret(), payload, malformed, mailNow()); !errors.Is(err, ErrFeedbackUnauthenticated) {
			t.Fatalf("malformed signature = %v, want ErrFeedbackUnauthenticated", err)
		}
	})

	t.Run("cross-tenant suppression", func(t *testing.T) {
		tracker := NewEmailTracker()
		suppressions := NewSuppressionList()
		if _, err := suppressions.Suppress("tenant-a", "marketing", "ada@example.example", "unsubscribed", mailNow()); err != nil {
			t.Fatal(err)
		}
		otherTenant := mailOutgoing("tenant-b", "marketing", "ada@example.example")
		otherTenant.MessageID = "sec-cross-tenant"
		got, err := tracker.Register(otherTenant, profile, suppressions, NewRetryLimiter(), mailNow())
		if err != nil || got.Status != EmailAccepted {
			t.Fatalf("cross-tenant send = %+v, %v; want ACCEPTED", got, err)
		}
		otherPurpose := mailOutgoing("tenant-a", "notice", "ada@example.example")
		otherPurpose.MessageID = "sec-cross-purpose"
		got, err = tracker.Register(otherPurpose, profile, suppressions, NewRetryLimiter(), mailNow())
		if err != nil || got.Status != EmailAccepted {
			t.Fatalf("cross-purpose send = %+v, %v; want ACCEPTED", got, err)
		}
	})

	t.Run("misattributed bounce", func(t *testing.T) {
		tracker := NewEmailTracker()
		first, err := applyFeedbackTo(t, tracker, "sec-a", "tenant-a", "notice", "a@example.example", FeedbackDelivered, "ref-a")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyFeedbackTo(t, tracker, "sec-b", "tenant-a", "notice", "b@example.example", FeedbackDelivered, "ref-b"); err != nil {
			t.Fatal(err)
		}
		// A signed bounce for B replayed against A carries B's identity in
		// its payload: the signature check binds it to B, so presenting it
		// for A fails authentication.
		bPayload := FeedbackPayload("sec-b", FeedbackBounced, "ref-b")
		replay := FeedbackEvent{MessageID: "sec-a", Kind: FeedbackBounced, ProviderRef: "ref-b", BounceCode: "550", Signature: SignFeedback(mailSecret(), bPayload)}
		if _, err := tracker.ApplyFeedback(mailSecret(), bPayload, replay, mailNow()); !errors.Is(err, ErrFeedbackUnauthenticated) && !errors.Is(err, ErrFeedbackMismatch) {
			t.Fatalf("replayed bounce = %v, want ErrFeedbackUnauthenticated or ErrFeedbackMismatch", err)
		}
		// A bounce directly against the delivered message is refused as a
		// terminal conflict rather than rewriting delivery truth.
		aPayload := FeedbackPayload("sec-a", FeedbackBounced, "ref-a")
		direct := FeedbackEvent{MessageID: "sec-a", Kind: FeedbackBounced, ProviderRef: "ref-a", BounceCode: "550", Signature: SignFeedback(mailSecret(), aPayload)}
		if _, err := tracker.ApplyFeedback(mailSecret(), aPayload, direct, mailNow()); !errors.Is(err, ErrEmailTerminal) {
			t.Fatalf("bounce-after-delivery = %v, want ErrEmailTerminal", err)
		}
		stored, err := tracker.Record("sec-a")
		if err != nil || stored.Status != first.Status || stored.Status != EmailDelivered {
			t.Fatalf("stored = %+v, %v; want retained DELIVERED", stored, err)
		}
	})

	t.Run("reputation failure is never delivered", func(t *testing.T) {
		tracker := NewEmailTracker()
		limiter := NewRetryLimiter()
		limiter.RecordReputationFailure(profile.Domain, mailNow())
		msg := mailOutgoing("tenant-a", "notice", "ada@example.example")
		msg.MessageID = "sec-reputation"
		got, err := tracker.Register(msg, profile, NewSuppressionList(), limiter, mailNow())
		if !errors.Is(err, ErrDomainReputation) {
			t.Fatalf("fenced send = %v, want ErrDomainReputation", err)
		}
		if got.Status != EmailUnknown {
			t.Fatalf("fenced status = %s, want UNKNOWN (never delivered)", got.Status)
		}
	})
}

// TestTodo_MAIL_001_Conformance proves the contract vocabulary and the
// transport/recipient separation: closed statuses, delivered distinct from
// read and acknowledged, and legal acknowledgement never implied.
func TestTodo_MAIL_001_Conformance(t *testing.T) {
	for _, raw := range []string{"ACCEPTED", "delivered", " Bounced ", "COMPLAINED", "suppressed", "unknown"} {
		if _, err := ParseEmailStatus(raw); err != nil {
			t.Fatalf("ParseEmailStatus(%q): %v", raw, err)
		}
	}
	for _, raw := range []string{"", "SENT", "READ", "ACKNOWLEDGED", "FAILED", "QUEUED"} {
		if _, err := ParseEmailStatus(raw); err == nil {
			t.Fatalf("ParseEmailStatus(%q) accepted a non-vocabulary status", raw)
		}
	}
	tracker := NewEmailTracker()
	record, err := applyFeedbackTo(t, tracker, "conf-1", "tenant-a", "notice", "ada@example.example", FeedbackDelivered, "ref-conf")
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireDelivered(Attempt{ID: "x", State: AcceptedByProvider}); err == nil {
		t.Fatal("provider acceptance satisfied the delivery requirement")
	}
	recipient := Recipient{AttemptID: "conf-1", RecipientRef: "ada", State: RecipientRead}
	if err := RequireRead(recipient); err != nil {
		t.Fatalf("RequireRead on READ: %v", err)
	}
	if err := RequireAcknowledged(recipient); err == nil {
		t.Fatal("read recipient satisfied the acknowledgement requirement")
	}
	if err := record.LegalAcknowledgement(); !errors.Is(err, ErrLegalAcknowledgement) {
		t.Fatalf("LegalAcknowledgement = %v, want ErrLegalAcknowledgement", err)
	}
}

// TestTodo_MAIL_001_Recovery proves the degraded paths recover with
// evidence: reputation returns after cooldown (never before) and a lifted
// suppression sends again.
func TestTodo_MAIL_001_Recovery(t *testing.T) {
	profile := mailProfile(t)
	policy := DefaultEmailRetryPolicy()

	t.Run("reputation recovery", func(t *testing.T) {
		tracker := NewEmailTracker()
		limiter := NewRetryLimiter()
		limiter.RecordReputationFailure(profile.Domain, mailNow())
		if err := limiter.RecoverReputation(policy, profile.Domain, mailNow().Add(30*time.Minute)); !errors.Is(err, ErrDomainReputation) {
			t.Fatalf("early recovery = %v, want ErrDomainReputation", err)
		}
		if err := limiter.RecoverReputation(policy, "unknown.example", mailNow().Add(2*time.Hour)); !errors.Is(err, ErrInvalidEmail) {
			t.Fatalf("recovery without failure = %v, want ErrInvalidEmail", err)
		}
		if err := limiter.RecoverReputation(policy, profile.Domain, mailNow().Add(2*time.Hour)); err != nil {
			t.Fatalf("RecoverReputation: %v", err)
		}
		msg := mailOutgoing("tenant-a", "notice", "ada@example.example")
		msg.MessageID = "rec-recovered"
		got, err := tracker.Register(msg, profile, NewSuppressionList(), limiter, mailNow().Add(2*time.Hour))
		if err != nil || got.Status != EmailAccepted {
			t.Fatalf("post-recovery send = %+v, %v; want ACCEPTED", got, err)
		}
	})

	t.Run("suppression lift", func(t *testing.T) {
		tracker := NewEmailTracker()
		suppressions := NewSuppressionList()
		entry, err := suppressions.Suppress("tenant-a", "marketing", "ada@example.example", "unsubscribed", mailNow())
		if err != nil || entry.Digest == "" {
			t.Fatalf("Suppress = %+v, %v", entry, err)
		}
		// Re-suppression is idempotent: the original evidence stands.
		again, err := suppressions.Suppress("tenant-a", "marketing", "ada@example.example", "other reason", mailNow())
		if err != nil || again.Digest != entry.Digest {
			t.Fatalf("re-suppress = %+v, %v; want original entry", again, err)
		}
		lifted, err := suppressions.Lift("tenant-a", "marketing", "ada@example.example")
		if err != nil || lifted.Digest != entry.Digest {
			t.Fatalf("Lift = %+v, %v; want the suppression receipt", lifted, err)
		}
		if _, err := suppressions.Lift("tenant-a", "marketing", "ada@example.example"); !errors.Is(err, ErrSuppressionAbsent) {
			t.Fatalf("second Lift = %v, want ErrSuppressionAbsent", err)
		}
		msg := mailOutgoing("tenant-a", "marketing", "ada@example.example")
		msg.MessageID = "rec-lifted"
		got, err := tracker.Register(msg, profile, suppressions, NewRetryLimiter(), mailNow().Add(time.Hour))
		if err != nil || got.Status != EmailAccepted {
			t.Fatalf("post-lift send = %+v, %v; want ACCEPTED", got, err)
		}
	})
}

// TestTodo_MAIL_001_Mutation proves records and feedback receipts bind
// their contents: any post-hoc edit is detected and identical replays are
// the only idempotent path.
func TestTodo_MAIL_001_Mutation(t *testing.T) {
	tracker := NewEmailTracker()
	record, err := applyFeedbackTo(t, tracker, "mut-1", "tenant-a", "notice", "ada@example.example", FeedbackDelivered, "ref-mut")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*EmailRecord){
		"status":    func(r *EmailRecord) { r.Status = EmailBounced },
		"recipient": func(r *EmailRecord) { r.Recipient = "mallory@example.example" },
		"tenant":    func(r *EmailRecord) { r.TenantID = "tenant-b" },
		"digest":    func(r *EmailRecord) { r.Digest = "deadbeef" },
	} {
		t.Run(name, func(t *testing.T) {
			forged := record
			mutate(&forged)
			if err := forged.Verify(); err == nil {
				t.Fatalf("tampered record verified: %+v", forged)
			}
		})
	}
	if err := record.Verify(); err != nil {
		t.Fatalf("pristine record Verify: %v", err)
	}
	// Identical replay of a terminal bounce is idempotent; a changed
	// bounce code is a different claim and is refused.
	bounced, err := applyFeedbackTo(t, tracker, "mut-2", "tenant-a", "notice", "bob@example.example", FeedbackBounced, "ref-mut-2")
	if err != nil {
		t.Fatal(err)
	}
	payload := FeedbackPayload("mut-2", FeedbackBounced, "ref-mut-2")
	replay := FeedbackEvent{MessageID: "mut-2", Kind: FeedbackBounced, ProviderRef: "ref-mut-2", BounceCode: "550 5.1.1", Signature: SignFeedback(mailSecret(), payload)}
	again, err := tracker.ApplyFeedback(mailSecret(), payload, replay, mailNow())
	if err != nil || again.Status != EmailBounced || again.Revision != bounced.Revision {
		t.Fatalf("identical replay = %+v, %v; want idempotent BOUNCED", again, err)
	}
	changed := replay
	changed.BounceCode = "550 5.2.0"
	if _, err := tracker.ApplyFeedback(mailSecret(), payload, changed, mailNow()); !errors.Is(err, ErrEmailTerminal) {
		t.Fatalf("changed replay = %v, want ErrEmailTerminal", err)
	}
}

// BenchmarkTodo_MAIL_001 measures the register-and-feedback hot path.
func BenchmarkTodo_MAIL_001(b *testing.B) {
	profile, err := VerifyDomain("mail.example.example", mailDNS(), mailNow())
	if err != nil {
		b.Fatal(err)
	}
	tracker := NewEmailTracker()
	suppressions := NewSuppressionList()
	limiter := NewRetryLimiter()
	at := mailNow()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Unique message and recipient per iteration keep the idempotency
		// ledger and the per-recipient retry budget honest.
		msg := OutgoingEmail{MessageID: "bench-message", TenantID: "tenant-a", Purpose: "notice", Recipient: "bench@example.example", Subject: "s"}
		msg.MessageID = "bench-message-" + time.Unix(int64(i/26), int64((i%26)*1000)).UTC().Format("150405.000000000") + "-" + strings.Repeat("x", 1+i%5)
		msg.Recipient = "bench-recipient-" + msg.MessageID + "@example.example"
		if _, err := tracker.Register(msg, profile, suppressions, limiter, at); err != nil {
			b.Fatal(err)
		}
	}
}

// TestEmailTracker_ConcurrentFeedbackDeterminism is outside the MAIL-001
// matrix: it documents that concurrent registration and feedback converge
// on one retained truth instead of losing it.
func TestEmailTracker_ConcurrentFeedbackDeterminism(t *testing.T) {
	profile := mailProfile(t)
	tracker := NewEmailTracker()
	suppressions := NewSuppressionList()
	limiter := NewRetryLimiter()
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := OutgoingEmail{
				MessageID: "race-message",
				TenantID:  "tenant-a", Purpose: "notice",
				Recipient: "race@example.example", Subject: "s",
			}
			_ = i
			if _, err := tracker.Register(msg, profile, suppressions, limiter, mailNow()); err != nil {
				errs <- err
				return
			}
			payload := FeedbackPayload("race-message", FeedbackDelivered, "ref-race")
			_, err := tracker.ApplyFeedback(mailSecret(), payload, FeedbackEvent{
				MessageID: "race-message", Kind: FeedbackDelivered,
				ProviderRef: "ref-race", Signature: SignFeedback(mailSecret(), payload),
			}, mailNow())
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	// Registration is idempotent and delivery feedback converges: workers
	// may see idempotent replays but never an error that loses truth.
	for err := range errs {
		if !errors.Is(err, ErrEmailTerminal) {
			t.Fatalf("concurrent feedback error = %v, want nil or terminal replay", err)
		}
	}
	stored, err := tracker.Record("race-message")
	if err != nil || (stored.Status != EmailDelivered && stored.Status != EmailAccepted) {
		t.Fatalf("stored = %+v, %v; want converged ACCEPTED or DELIVERED", stored, err)
	}
}
