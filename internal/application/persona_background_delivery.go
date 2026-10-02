package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

var errPersonaBackgroundDelivery = errors.New("application: background persona delivery unavailable")

// PersonaBackgroundReplyCommitter accepts only a gateway-sealed output. Current
// authority and recipient routing are resolved by its configured owner.
type PersonaBackgroundReplyCommitter interface {
	CommitSealedBackgroundPersonaReply(context.Context, agentsecurity.FinalOutputPersistence) (chat.EphemeralPost, error)
}

type PersonaBackgroundReplyDeliveryConfig struct {
	Authority       agentrun.Authority
	OutputAuthority PersonaRunChatReplyAuthoritySource
	Worker          PersonaPrivateChatWorkloadIdentitySource
	Threads         chat.BackgroundThreadSnapshotSource
	Committer       PersonaBackgroundReplyCommitter
	OutputPolicy    PersonaReplyOutputPolicy
	Documents       personaAgentDocumentGroundingSource
	Now             func() time.Time
}

type PersonaBackgroundReplyDelivery struct {
	cfg      PersonaBackgroundReplyDeliveryConfig
	mu       sync.Mutex
	rendered map[string]string
}

func NewPersonaBackgroundReplyDelivery(cfg PersonaBackgroundReplyDeliveryConfig) (*PersonaBackgroundReplyDelivery, error) {
	if isNilPersonaOutputPort(cfg.Authority) || isNilPersonaOutputPort(cfg.OutputAuthority) || isNilPersonaOutputPort(cfg.Worker) || isNilPersonaOutputPort(cfg.Threads) || isNilPersonaOutputPort(cfg.Committer) || cfg.Now == nil {
		return nil, errPersonaBackgroundDelivery
	}
	cfg.OutputPolicy.AdminAllowedOrigins = append([]string(nil), cfg.OutputPolicy.AdminAllowedOrigins...)
	return &PersonaBackgroundReplyDelivery{cfg: cfg, rendered: make(map[string]string)}, nil
}

// NewDatabasePersonaBackgroundReplyDelivery binds the native transaction owner
// and safe renderer to this exact current-authority delivery instance.
func NewDatabasePersonaBackgroundReplyDelivery(cfg PersonaBackgroundReplyDeliveryConfig, store *chatstore.Store) (*PersonaBackgroundReplyDelivery, error) {
	if store == nil || !isNilPersonaOutputPort(cfg.Committer) {
		return nil, errPersonaBackgroundDelivery
	}
	d := &PersonaBackgroundReplyDelivery{cfg: cfg, rendered: make(map[string]string)}
	native, err := chatstore.NewSealedBackgroundPersonaDelivery(store, d, d, cfg.Now)
	if err != nil {
		return nil, errPersonaBackgroundDelivery
	}
	cfg.Committer = native
	composed, err := NewPersonaBackgroundReplyDelivery(cfg)
	if err != nil {
		return nil, err
	}
	d.cfg = composed.cfg
	return d, nil
}

type personaBackgroundDeliveryBinding struct {
	record agentrun.Record
	run    runstate.Run
}
type personaBackgroundDeliveryKey struct{}

// DeliverBackgroundPersonaReply never installs the retained invoker into a
// trust context. The native chat owner resolves all effects from sealed identity.
func (d *PersonaBackgroundReplyDelivery) DeliverBackgroundPersonaReply(ctx context.Context, record agentrun.Record, run runstate.Run, output agentsecurity.FinalOutputPersistence) (PersonaReplyDeliveryReceipt, error) {
	if d == nil || ctx == nil || !validBackgroundPersonaDeliveryBinding(record, run) || output.Identity() != personaRunFinalOutputIdentity(record, run) {
		return PersonaReplyDeliveryReceipt{}, errPersonaBackgroundDelivery
	}
	ctx = context.WithValue(ctx, personaBackgroundDeliveryKey{}, personaBackgroundDeliveryBinding{record: record, run: run})
	if _, err := d.AuthorizeSealedBackgroundPersonaReply(ctx, output); err != nil {
		return PersonaReplyDeliveryReceipt{}, err
	}
	if !isNilPersonaOutputPort(d.cfg.Documents) {
		documents, omissions, err := d.cfg.Documents.ResolvePersonaAgentDocuments(ctx, record)
		if err != nil {
			return PersonaReplyDeliveryReceipt{}, err
		}
		result, err := personaChatReplyDeliveryResult(output)
		if err != nil || len(result.Items) != 1 {
			return PersonaReplyDeliveryReceipt{}, errPersonaBackgroundDelivery
		}
		body := renderPersonaReplyWithAgentDocuments(result.Items[0].Text, output.Identity().TenantID, output.Identity().ConversationID, d.cfg.OutputPolicy, personaCitedAgentDocuments(documents, output.Citations()), omissions)
		if body == "" {
			return PersonaReplyDeliveryReceipt{}, errPersonaBackgroundDelivery
		}
		d.mu.Lock()
		d.rendered[output.Digest()] = body
		d.mu.Unlock()
		defer func() {
			d.mu.Lock()
			delete(d.rendered, output.Digest())
			d.mu.Unlock()
		}()
	}
	p, err := d.cfg.Committer.CommitSealedBackgroundPersonaReply(ctx, output)
	i := output.Identity()
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, err
	}
	if p.ID == "" || p.TenantID != i.TenantID || p.ConversationID != i.ConversationID || p.ThreadID != i.ThreadID || p.RecipientHomeTenantID != i.TenantID || p.RecipientSubjectID != i.InvokerID || !p.OnlyVisibleToYou || p.DurableCopyPostID == "" || p.DurableCopyConversationID == "" {
		return PersonaReplyDeliveryReceipt{}, errPersonaBackgroundDelivery
	}
	return PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: p.ID, DurableCopyPostID: p.DurableCopyPostID, DurableCopyConversationID: p.DurableCopyConversationID}, nil
}

// AuthorizeSealedBackgroundPersonaReply is the native commit's current owner
// callback. It rechecks exact durable authority and reads invoker-visible source
// state; the chat transaction compares that state again before writing.
func (d *PersonaBackgroundReplyDelivery) AuthorizeSealedBackgroundPersonaReply(ctx context.Context, output agentsecurity.FinalOutputPersistence) (chatstore.SealedBackgroundPersonaAuthority, error) {
	if d == nil || ctx == nil {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	binding, ok := ctx.Value(personaBackgroundDeliveryKey{}).(personaBackgroundDeliveryBinding)
	if !ok || !validBackgroundPersonaDeliveryBinding(binding.record, binding.run) || output.Identity() != personaRunFinalOutputIdentity(binding.record, binding.run) {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	if _, _, err := output.Payload(); err != nil {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	current, err := d.cfg.Authority.VerifyAdmission(WithPersonaBackgroundAdmission(ctx, binding.record), binding.record.Request)
	if err != nil || !reflect.DeepEqual(current, binding.record.Authority) {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	// Rebuild semantic grounding from current source owners. This does not
	// persist a replacement output, and any changed source citation refuses the
	// old sealed result before it can reach either recipient destination.
	currentOutput, err := d.cfg.OutputAuthority.ResolvePersonaRunChatReplyAuthority(WithPersonaBackgroundAdmission(ctx, binding.record), binding.record, binding.run)
	result, resultErr := personaChatReplyDeliveryResult(output)
	if err != nil || resultErr != nil || len(result.Items) != 1 || !validPersonaRunChatReplyAuthority(binding.record, binding.run, currentOutput) {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	resealed, err := sealPersonaRunChatReply(ctx, binding.record, binding.run, currentOutput, result.Items[0].Text)
	if err != nil || resealed.SemanticDigest() != output.SemanticDigest() {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	worker, err := d.cfg.Worker.ResolvePersonaChatWorker(ctx)
	if err != nil || !privatePersonaReplyWorkerMatches(worker, d.cfg.Now().UTC()) {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	i := output.Identity()
	snapshot, err := d.cfg.Threads.CaptureBackgroundThreadSnapshot(ctx, chat.BackgroundThreadSnapshotRequest{TenantID: i.TenantID, ReaderID: i.InvokerID, ConversationID: i.ConversationID, ThreadID: i.ThreadID, InvokingPostID: i.PostID, Limit: chat.MaxThreadSnapshotPosts})
	if err != nil {
		return chatstore.SealedBackgroundPersonaAuthority{}, errPersonaBackgroundDelivery
	}
	return chatstore.SealedBackgroundPersonaAuthority{Worker: worker, Source: snapshot}, nil
}

func validBackgroundPersonaDeliveryBinding(record agentrun.Record, run runstate.Run) bool {
	id, idErr := agentrun.AdmissionRequestID(record.Request.Source)
	digest, digestErr := agentrun.AdmissionRequestDigest(record.Request)
	return validPersonaRunIdentityBinding(record, run) && idErr == nil && digestErr == nil && record.ID == id && record.RequestDigest == digest && record.Request.Source.Kind == agentrun.SourcePersonaMention && record.Request.Principal.Mode == agentrun.ModeOnBehalfOf
}

// RenderSealedBackgroundPersonaReply uses the same safe renderer as foreground
// delivery; callers cannot supply a replacement body to the native commit.
func (d *PersonaBackgroundReplyDelivery) RenderSealedBackgroundPersonaReply(output agentsecurity.FinalOutputPersistence) (string, error) {
	if d == nil {
		return "", errPersonaBackgroundDelivery
	}
	d.mu.Lock()
	prepared := d.rendered[output.Digest()]
	d.mu.Unlock()
	if prepared != "" {
		return prepared, nil
	}
	result, err := personaChatReplyDeliveryResult(output)
	if err != nil || len(result.Items) != 1 {
		return "", errPersonaBackgroundDelivery
	}
	i := output.Identity()
	body := renderPersonaReplyText(result.Items[0].Text, i.TenantID, i.ConversationID, d.cfg.OutputPolicy)
	if body == "" {
		return "", errPersonaBackgroundDelivery
	}
	return body, nil
}
