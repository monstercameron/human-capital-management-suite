package inboundmsg_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inboundmsg"
	"github.com/monstercameron/human-capital-management-suite/internal/data/messagingmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// chain is the migration-00031 lineage one recipient's reply resolves
// against: a message_intent/delivery_endpoint/recipient_message triple plus
// the conversation_thread and thread_participant the reply is claimed to
// arrive in.
type chain struct {
	threadID           uuid.UUID
	recipientMessageID uuid.UUID
	correlationKey     string
}

// seedChain writes one full lineage for recipientRef, using correlationKey
// as the recipient_message's own correlation key (the token a legitimate
// reply must echo back). Passing the same correlationKey across two tenants
// is how the cross-tenant security test proves a token collision never
// crosses the tenant boundary.
func seedChain(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, recipientRef, correlationKey string) chain {
	t.Helper()
	ctx := context.Background()
	c := chain{correlationKey: correlationKey}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		intent := messagingmeta.MessageIntent{
			TenantID:            tenant,
			MessageIntentID:     uuid.New(),
			Purpose:             "TASK",
			AudienceExpression:  json.RawMessage(`{}`),
			TemplateKey:         "tpl:" + uuid.NewString(),
			TemplateVersion:     1,
			Classification:      "INTERNAL",
			Urgency:             "NORMAL",
			DeliveryRequirement: "BEST_EFFORT",
			ResponseRequirement: "REPLY",
			WorkflowRef:         "workflow:inboundmsg-seed",
			CorrelationKey:      "corr-intent:" + uuid.NewString(),
			CreatedAt:           fixedInstant,
		}
		if err := messagingmeta.InsertMessageIntent(ctx, tx, intent); err != nil {
			return err
		}
		endpoint := messagingmeta.DeliveryEndpoint{
			TenantID:          tenant,
			EndpointID:        uuid.New(),
			PrincipalRef:      recipientRef,
			Channel:           "EMAIL",
			AddressDigest:     digestOf("address-" + uuid.NewString()),
			Ownership:         "BUSINESS",
			VerificationState: "VERIFIED",
			PurposeScope:      json.RawMessage(`{}`),
			Locale:            "en-US",
			EffectiveFrom:     fixedInstant,
			Status:            "ACTIVE",
		}
		if err := messagingmeta.InsertDeliveryEndpoint(ctx, tx, endpoint); err != nil {
			return err
		}
		thread := messagingmeta.ConversationThread{
			TenantID:       tenant,
			ThreadID:       uuid.New(),
			SubjectRef:     recipientRef,
			Purpose:        "TASK",
			Classification: "INTERNAL",
			OpenedAt:       fixedInstant,
			Status:         "OPEN",
		}
		if err := messagingmeta.InsertConversationThread(ctx, tx, thread); err != nil {
			return err
		}
		c.threadID = thread.ThreadID

		message := messagingmeta.RecipientMessage{
			TenantID:             tenant,
			RecipientMessageID:   uuid.New(),
			MessageIntentID:      intent.MessageIntentID,
			ConversationThreadID: &c.threadID,
			RecipientRef:         recipientRef,
			EndpointID:           endpoint.EndpointID,
			RenderedDigest:       digestOf("rendered-" + uuid.NewString()),
			Classification:       "INTERNAL",
			CorrelationKey:       correlationKey,
			RecipientState:       "UNSEEN",
			SatisfactionState:    "PENDING",
			CreatedAt:            fixedInstant,
			UpdatedAt:            fixedInstant,
		}
		if err := messagingmeta.InsertRecipientMessage(ctx, tx, message); err != nil {
			return err
		}
		c.recipientMessageID = message.RecipientMessageID

		participant := messagingmeta.ThreadParticipant{
			TenantID:             tenant,
			ParticipantID:        uuid.New(),
			ThreadID:             thread.ThreadID,
			PrincipalRef:         recipientRef,
			ParticipantRole:      "MEMBER",
			MembershipFrom:       fixedInstant,
			AuthorizationDigest:  digestOf("auth-" + uuid.NewString()),
			HistoricalVisibility: "FULL_HISTORY",
			Status:               "ACTIVE",
		}
		return messagingmeta.InsertThreadParticipant(ctx, tx, participant)
	})
	return c
}

func addParticipant(t *testing.T, conn *pgxadapter.Conn, tenant, threadID uuid.UUID, principalRef string) {
	t.Helper()
	ctx := context.Background()
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return messagingmeta.InsertThreadParticipant(ctx, tx, messagingmeta.ThreadParticipant{
			TenantID:             tenant,
			ParticipantID:        uuid.New(),
			ThreadID:             threadID,
			PrincipalRef:         principalRef,
			ParticipantRole:      "MEMBER",
			MembershipFrom:       fixedInstant,
			AuthorizationDigest:  digestOf("auth-" + uuid.NewString()),
			HistoricalVisibility: "FULL_HISTORY",
			Status:               "ACTIVE",
		})
	})
}

func newInboundMessage(tenant, thread uuid.UUID) inboundmsg.InboundMessage {
	return inboundmsg.InboundMessage{
		TenantID:             tenant,
		ThreadID:             thread,
		Channel:              "EMAIL",
		SenderEndpointDigest: digestOf("sender-" + uuid.NewString()),
		ReceivedAt:           fixedInstant,
		ProviderMessageID:    "provider:" + uuid.NewString(),
		ContentDigest:        digestOf("content-" + uuid.NewString()),
		ContentRef:           "artifact:" + uuid.NewString(),
		Classification:       "INTERNAL",
	}
}

// TestTodo_MSG_011 is MSG-011's primary acceptance test: an inbound message
// is ingested idempotently, a correctly correlated reply resolves to BOUND,
// an uncorrelated one resolves to REJECTED, and a thread participant can
// list what arrived.
func TestTodo_MSG_011(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "msg011-primary")

	t.Run("the inbound-reply tables exist in the live schema", func(t *testing.T) {
		for _, table := range []string{"inbound_message", "reply_binding"} {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("Ingest is idempotent by provider message id and seeds an UNRESOLVED binding", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)

		var firstID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, created, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			if err != nil {
				return err
			}
			if !created {
				t.Fatalf("first Ingest reported created=false")
			}
			firstID = stored.InboundMessageID
			return nil
		})

		conflictingReplay := in
		conflictingReplay.ContentDigest = digestOf("different bytes for reused provider id")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, err := inboundmsg.Store{}.Ingest(ctx, tx, conflictingReplay, c.correlationKey)
			return err
		})
		if !errors.Is(err, inboundmsg.ErrReplayConflict) {
			t.Fatalf("provider id replay with different content returned %v, want ErrReplayConflict", err)
		}

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, created, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			if err != nil {
				return err
			}
			if created {
				t.Fatalf("redelivery of the same provider message reported created=true")
			}
			if stored.InboundMessageID != firstID {
				t.Fatalf("redelivery returned a different inbound_message_id: %s, want %s", stored.InboundMessageID, firstID)
			}
			return nil
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			binding, err := inboundmsg.Store{}.LoadBinding(ctx, tx, tenant, firstID)
			if err != nil {
				return err
			}
			if binding.State != inboundmsg.Unresolved || binding.Version != 1 || binding.RecipientMessageID != nil {
				t.Fatalf("seeded binding = %+v, want UNRESOLVED/v1/no recipient", binding)
			}
			return nil
		})
	})

	t.Run("Bind moves a correctly correlated reply to BOUND", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)

		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			binding, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 1, fixedInstant.Add(time.Minute))
			if err != nil {
				return err
			}
			if binding.State != inboundmsg.Bound {
				t.Fatalf("Bind resulted in state %s, want BOUND", binding.State)
			}
			if binding.RecipientMessageID == nil || *binding.RecipientMessageID != c.recipientMessageID {
				t.Fatalf("Bind resolved to %+v, want recipient_message %s", binding.RecipientMessageID, c.recipientMessageID)
			}
			return nil
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			binding, err := inboundmsg.Store{}.LoadBinding(ctx, tx, tenant, inboundID)
			if err != nil {
				return err
			}
			if binding.State != inboundmsg.Bound || binding.Version != 2 {
				t.Fatalf("stored binding = %+v, want BOUND/v2", binding)
			}
			return nil
		})
	})

	t.Run("Bind rejects a correlation token that resolves to nothing", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)

		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			// A token nobody's recipient_message actually carries.
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, "corr:"+uuid.NewString())
			inboundID = stored.InboundMessageID
			return err
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			binding, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 1, fixedInstant.Add(time.Minute))
			if err != nil {
				return err
			}
			if binding.State != inboundmsg.Rejected || binding.RejectionReason == "" || binding.RecipientMessageID != nil {
				t.Fatalf("Bind on an unresolved token = %+v, want REJECTED with a reason and no recipient", binding)
			}
			return nil
		})
	})

	t.Run("ListThread returns a participant's own thread, oldest first", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())

		first := newInboundMessage(tenant, c.threadID)
		first.ReceivedAt = fixedInstant
		second := newInboundMessage(tenant, c.threadID)
		second.ReceivedAt = fixedInstant.Add(time.Hour)

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			store := inboundmsg.Store{}
			secondStored, _, err := store.Ingest(ctx, tx, second, c.correlationKey)
			if err != nil {
				return err
			}
			if _, err := store.Bind(ctx, tx, tenant, secondStored.InboundMessageID, recipientRef, 1, fixedInstant.Add(time.Minute)); err != nil {
				return err
			}
			firstStored, _, err := store.Ingest(ctx, tx, first, c.correlationKey)
			if err != nil {
				return err
			}
			_, err = store.Bind(ctx, tx, tenant, firstStored.InboundMessageID, recipientRef, 1, fixedInstant.Add(2*time.Minute))
			return err
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			list, err := inboundmsg.Store{}.ListThread(ctx, tx, tenant, recipientRef, c.threadID)
			if err != nil {
				return err
			}
			if len(list) != 2 {
				t.Fatalf("ListThread returned %d messages, want 2", len(list))
			}
			if !list[0].ReceivedAt.Equal(first.ReceivedAt) || !list[1].ReceivedAt.Equal(second.ReceivedAt) {
				t.Fatalf("ListThread order = %v, %v, want oldest first", list[0].ReceivedAt, list[1].ReceivedAt)
			}
			return nil
		})
	})

	t.Run("append-only inbound_message rejects rewrites", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})
		if err := db.ExecErr(`UPDATE inbound_message SET classification='PUBLIC' WHERE tenant_id=$1 AND inbound_message_id=$2`, tenant, inboundID); err == nil {
			t.Fatal("an append-only inbound_message row was rewritten")
		}
		if err := db.ExecErr(`DELETE FROM inbound_message WHERE tenant_id=$1 AND inbound_message_id=$2`, tenant, inboundID); err == nil {
			t.Fatal("an append-only inbound_message row was deleted")
		}
	})
}

// TestTodo_MSG_011_Integration exercises the whole lineage a governed reply
// depends on end to end: a MessageIntent's recipient copy, the thread it is
// claimed to belong to, an inbound reply that names the right correlation
// token, and the resulting thread listing a fellow participant can read.
func TestTodo_MSG_011_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "msg011-integration")

	replier := "principal:" + uuid.NewString()
	observer := "principal:" + uuid.NewString()
	c := seedChain(t, conn, tenant, replier, "corr:"+uuid.NewString())
	addParticipant(t, conn, tenant, c.threadID, observer)

	in := newInboundMessage(tenant, c.threadID)
	var inboundID uuid.UUID
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		stored, created, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
		if err != nil {
			return err
		}
		if !created {
			t.Fatalf("Ingest of a fresh provider message reported created=false")
		}
		inboundID = stored.InboundMessageID
		return nil
	})

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		binding, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, replier, 1, fixedInstant.Add(time.Minute))
		if err != nil {
			return err
		}
		if binding.State != inboundmsg.Bound || binding.RecipientMessageID == nil || *binding.RecipientMessageID != c.recipientMessageID {
			t.Fatalf("integration Bind = %+v, want BOUND to %s", binding, c.recipientMessageID)
		}
		return nil
	})

	// A second participant on the same thread -- who did not send the reply
	// -- can still see it: thread visibility is per-thread, not per-sender.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		list, err := inboundmsg.Store{}.ListThread(ctx, tx, tenant, observer, c.threadID)
		if err != nil {
			return err
		}
		if len(list) != 1 || list[0].InboundMessageID != inboundID {
			t.Fatalf("observer's ListThread = %+v, want exactly the one inbound message", list)
		}
		return nil
	})

	// The recipient_message the reply resolved to is unchanged by MSG-011:
	// binding is additive evidence, not a rewrite of migration 00031's own
	// row.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		msg, err := messagingmeta.LoadRecipientMessage(ctx, tx, tenant, c.recipientMessageID)
		if err != nil {
			return err
		}
		if msg.RecipientRef != replier {
			t.Fatalf("recipient_message %+v was mutated by the reply path", msg)
		}
		return nil
	})
}

// TestTodo_MSG_011_Fault proves MSG-011's RED clause: invalid input, a
// nonexistent binding and an already-resolved binding are all refused
// before anything is written, and an append-only row cannot be overwritten
// even by a superuser-level statement outside the store's own API.
func TestTodo_MSG_011_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "msg011-fault")

	t.Run("Ingest refuses a row missing required fields", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		bad := newInboundMessage(tenant, c.threadID)
		bad.ContentRef = ""
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, err := inboundmsg.Store{}.Ingest(ctx, tx, bad, "corr:"+uuid.NewString())
			return err
		})
		if !errors.Is(err, inboundmsg.ErrInvalid) {
			t.Fatalf("Ingest with no content_ref returned %v, want ErrInvalid", err)
		}

		bad2 := newInboundMessage(tenant, c.threadID)
		bad2.Channel = "CARRIER_PIGEON"
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, err := inboundmsg.Store{}.Ingest(ctx, tx, bad2, "corr:"+uuid.NewString())
			return err
		})
		if !errors.Is(err, inboundmsg.ErrInvalid) {
			t.Fatalf("Ingest with an undeclared channel returned %v, want ErrInvalid", err)
		}
	})

	t.Run("Ingest refuses a missing correlation token", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, "")
			return err
		})
		if !errors.Is(err, inboundmsg.ErrInvalid) {
			t.Fatalf("Ingest with no correlation token returned %v, want ErrInvalid", err)
		}
	})

	t.Run("Bind refuses a nonexistent inbound message", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, uuid.New(), "principal:"+uuid.NewString(), 1, fixedInstant)
			return err
		})
		if !errors.Is(err, inboundmsg.ErrNotFound) {
			t.Fatalf("Bind on a nonexistent binding returned %v, want ErrNotFound", err)
		}
	})

	t.Run("Bind refuses invalid arguments before touching the database", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, "", 1, fixedInstant)
			return err
		})
		if !errors.Is(err, inboundmsg.ErrInvalid) {
			t.Fatalf("Bind with no claimed sender returned %v, want ErrInvalid", err)
		}

		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 0, fixedInstant)
			return err
		})
		if !errors.Is(err, inboundmsg.ErrInvalid) {
			t.Fatalf("Bind with expected version 0 returned %v, want ErrInvalid", err)
		}
	})

	t.Run("a binding resolves exactly once", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 1, fixedInstant.Add(time.Minute))
			return err
		})

		// Same expected version the row started at: now stale, since the
		// bind above already advanced it. The already-resolved check fires
		// first because state moved off UNRESOLVED.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 1, fixedInstant.Add(2*time.Minute))
			return err
		})
		if !errors.Is(err, inboundmsg.ErrAlreadyResolved) {
			t.Fatalf("rebinding a resolved binding returned %v, want ErrAlreadyResolved", err)
		}

		// The correct current version is refused too: BOUND is terminal in
		// this phase.
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 2, fixedInstant.Add(3*time.Minute))
			return err
		})
		if !errors.Is(err, inboundmsg.ErrAlreadyResolved) {
			t.Fatalf("rebinding at the live version returned %v, want ErrAlreadyResolved", err)
		}
	})

	t.Run("append-only inbound_message rejects rewrites even outside the store API", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})
		if err := db.ExecErr(`UPDATE inbound_message SET content_ref='tampered' WHERE tenant_id=$1 AND inbound_message_id=$2`, tenant, inboundID); err == nil {
			t.Fatal("forbid_mutation did not stop an inbound_message rewrite")
		}
	})
}

// TestTodo_MSG_011_Security proves the RED clause naming isolation: a wrong
// tenant cannot reach another tenant's inbound message or binding, a
// correlation token collision never crosses the tenant boundary, a token
// naming another participant's message is refused rather than bound, and a
// non-participant sees no messages in a thread they do not belong to.
func TestTodo_MSG_011_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "msg011-security")
	otherTenant := insertTenant(t, db, "msg011-security-other")

	t.Run("a wrong tenant cannot reach another tenant's inbound message or binding", func(t *testing.T) {
		recipientRef := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, recipientRef, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})

		err := inTenantTxErr(conn, otherTenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.LoadMessage(ctx, tx, tenant, inboundID)
			return err
		})
		if !errors.Is(err, inboundmsg.ErrNotFound) {
			t.Fatalf("another tenant's LoadMessage (naming this tenant's id) returned %v, want ErrNotFound", err)
		}

		err = inTenantTxErr(conn, otherTenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.LoadBinding(ctx, tx, tenant, inboundID)
			return err
		})
		if !errors.Is(err, inboundmsg.ErrNotFound) {
			t.Fatalf("another tenant's LoadBinding (naming this tenant's id) returned %v, want ErrNotFound", err)
		}

		err = inTenantTxErr(conn, otherTenant, func(tx dbport.Tx) error {
			_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 1, fixedInstant.Add(time.Minute))
			return err
		})
		if err == nil {
			t.Fatal("another tenant's Bind against this tenant's binding id succeeded")
		}
	})

	t.Run("a correlation token collision never crosses the tenant boundary", func(t *testing.T) {
		sharedToken := "corr:" + uuid.NewString()
		recipientRef := "principal:" + uuid.NewString()

		mine := seedChain(t, conn, tenant, recipientRef, sharedToken)
		// The other tenant has a recipient_message with the exact same
		// correlation_key string and the exact same recipient_ref string --
		// the only way this test can prove the lookup is tenant-scoped, not
		// merely value-scoped.
		theirs := seedChain(t, conn, otherTenant, recipientRef, sharedToken)

		in := newInboundMessage(tenant, mine.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, sharedToken)
			inboundID = stored.InboundMessageID
			return err
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			binding, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 1, fixedInstant.Add(time.Minute))
			if err != nil {
				return err
			}
			if binding.State != inboundmsg.Bound {
				t.Fatalf("Bind against a same-tenant match reported %s, want BOUND", binding.State)
			}
			if binding.RecipientMessageID == nil || *binding.RecipientMessageID != mine.recipientMessageID {
				t.Fatalf("Bind resolved to %+v, want this tenant's own recipient_message %s, not %s", binding.RecipientMessageID, mine.recipientMessageID, theirs.recipientMessageID)
			}
			if binding.RecipientMessageID != nil && *binding.RecipientMessageID == theirs.recipientMessageID {
				t.Fatalf("Bind resolved to another tenant's recipient_message %s", theirs.recipientMessageID)
			}
			return nil
		})
	})

	t.Run("a token naming another participant's message is rejected, never bound", func(t *testing.T) {
		owner := "principal:" + uuid.NewString()
		impersonator := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, owner, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			binding, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, impersonator, 1, fixedInstant.Add(time.Minute))
			if err != nil {
				return err
			}
			if binding.State != inboundmsg.Rejected {
				t.Fatalf("Bind claimed by %q against %q's message reported %s, want REJECTED", impersonator, owner, binding.State)
			}
			if binding.RecipientMessageID != nil {
				t.Fatalf("a rejected binding named a recipient_message: %+v", binding)
			}
			return nil
		})
	})

	t.Run("a non-participant sees no messages in a thread they do not belong to", func(t *testing.T) {
		member := "principal:" + uuid.NewString()
		stranger := "principal:" + uuid.NewString()
		c := seedChain(t, conn, tenant, member, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, c.threadID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
			if err != nil {
				return err
			}
			_, err = inboundmsg.Store{}.Bind(ctx, tx, tenant, stored.InboundMessageID, member, 1, fixedInstant.Add(time.Minute))
			return err
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			list, err := inboundmsg.Store{}.ListThread(ctx, tx, tenant, stranger, c.threadID)
			if err != nil {
				return err
			}
			if len(list) != 0 {
				t.Fatalf("a non-participant's ListThread returned %d messages, want 0", len(list))
			}
			return nil
		})

		// The thread genuinely has a message -- the member sees it, proving
		// the stranger's empty result above was authorization, not an empty
		// thread.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			list, err := inboundmsg.Store{}.ListThread(ctx, tx, tenant, member, c.threadID)
			if err != nil {
				return err
			}
			if len(list) != 1 {
				t.Fatalf("the thread's own member ListThread returned %d messages, want 1", len(list))
			}
			return nil
		})
	})

	t.Run("a claimed thread cannot redirect a correlated reply", func(t *testing.T) {
		member := "principal:" + uuid.NewString()
		canonical := seedChain(t, conn, tenant, member, "corr:"+uuid.NewString())
		other := seedChain(t, conn, tenant, member, "corr:"+uuid.NewString())
		in := newInboundMessage(tenant, other.threadID)
		var inboundID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, canonical.correlationKey)
			inboundID = stored.InboundMessageID
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			binding, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, member, 1, fixedInstant.Add(time.Minute))
			if err != nil {
				return err
			}
			if binding.State != inboundmsg.Rejected || binding.RecipientMessageID != nil {
				t.Fatalf("thread-mismatched reply binding = %+v, want REJECTED without recipient", binding)
			}
			list, err := inboundmsg.Store{}.ListThread(ctx, tx, tenant, member, other.threadID)
			if err != nil {
				return err
			}
			if len(list) != 0 {
				t.Fatalf("claimed thread exposed %d unresolved reply messages, want 0", len(list))
			}
			return nil
		})
	})
}

// TestTodo_MSG_011_Race proves the compare-and-swap fence in [inboundmsg.Store.Bind]:
// under concurrent attempts against the same UNRESOLVED binding at the same
// expected version, exactly one succeeds and the rest are refused. No two
// resolutions of the same inbound message are ever accepted.
func TestTodo_MSG_011_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	setup := appConn(t, db)
	tenant := insertTenant(t, db, "msg011-race")

	recipientRef := "principal:" + uuid.NewString()
	c := seedChain(t, setup, tenant, recipientRef, "corr:"+uuid.NewString())
	in := newInboundMessage(tenant, c.threadID)
	var inboundID uuid.UUID
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		stored, _, err := inboundmsg.Store{}.Ingest(ctx, tx, in, c.correlationKey)
		inboundID = stored.InboundMessageID
		return err
	})

	const attempts = 6
	var wins, losses atomic.Int64
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := 0; i < attempts; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			conn := appConn(t, db)
			start.Wait()
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				_, err := inboundmsg.Store{}.Bind(ctx, tx, tenant, inboundID, recipientRef, 1, fixedInstant.Add(time.Minute))
				return err
			})
			if err == nil {
				wins.Add(1)
			} else {
				losses.Add(1)
			}
		}()
	}
	start.Done()
	done.Wait()

	if wins.Load() != 1 {
		t.Fatalf("%d of %d concurrent Bind attempts against the same binding succeeded, want exactly 1", wins.Load(), attempts)
	}
	if losses.Load() != attempts-1 {
		t.Fatalf("%d attempts were refused, want %d", losses.Load(), attempts-1)
	}

	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		binding, err := inboundmsg.Store{}.LoadBinding(ctx, tx, tenant, inboundID)
		if err != nil {
			return err
		}
		if binding.Version != 2 {
			t.Fatalf("binding settled at version %d, want exactly 2 (one accepted transition)", binding.Version)
		}
		return nil
	})
}
