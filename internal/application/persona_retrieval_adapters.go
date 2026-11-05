package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentcontext/retrieval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// personaRetrievalFreshTTL bounds how long one owner observation counts as
// fresh. Retrieval consumes the observation within the same call, so the bound
// only needs to cover Retrieve-to-Finalize, never the run.
const personaRetrievalFreshTTL = 5 * time.Minute

// personaRetrievalChatScope binds one admitted run to the AGENT-018 retrieval
// request shape. Every value comes from the durable admission record; the
// retrieval service itself validates presence before any owner is reached.
func personaRetrievalChatScope(admission agentrun.Record) (retrieval.Access, retrieval.Query, agentinvoke.ThreadReadRequest) {
	request := admission.Request
	access := retrieval.Access{
		TenantID: request.Source.TenantID, LegalEntityID: request.LegalEntity,
		PrincipalID: request.Principal.InvokerID, InvocationID: admission.ID,
		ConversationID: request.Audience.ID, Audience: request.Audience.ID, Purpose: request.Purpose,
		AuthorizationVersion: admission.Authority.PolicyDigest,
		GrantIDs:             nonBlankStrings(request.Principal.DelegatedCredentialRef, admission.Authority.GrantRef),
		RevocationTokens:     nonBlankStrings(request.Audience.Digest, request.Context.Digest),
	}
	query := retrieval.Query{
		Text: request.Source.Ref, ConversationID: request.Audience.ID,
		ThreadID: request.Context.ID, Limit: agentinvoke.MaxThreadPosts, Sources: []retrieval.Kind{retrieval.Chat},
	}
	base := agentinvoke.ThreadReadRequest{
		TenantID: request.Source.TenantID, ConversationID: request.Audience.ID,
		ThreadID: request.Context.ID, InvokerID: request.Principal.InvokerID,
		InvokingPostID: request.Source.Ref, Limit: agentinvoke.MaxThreadPosts,
	}
	return access, query, base
}

func nonBlankStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

// personaRetrievalChatOwner implements retrieval.ChatOwner over the run path's
// existing thread port. It performs no store access of its own: Chat remains
// the sole owner of visibility, and the admitted thread scope is fixed at
// construction from the durable admission record.
type personaRetrievalChatOwner struct {
	threads agentinvoke.ThreadReader
	request agentinvoke.ThreadReadRequest
	class   string
	now     func() time.Time
	// observed carries the exact posts mapped by the last RetrieveChat call so
	// disclosure can reattach owner-observed authorship, which is immutable in
	// chat, to the retrieval-authorized content bytes.
	observed map[string]agentinvoke.ThreadPost
}

var _ retrieval.ChatOwner = (*personaRetrievalChatOwner)(nil)

// RetrieveChat reads the admitted thread through the composed thread port and
// attaches one typed envelope per content-bearing post. A request scoped
// outside the admitted thread is refused before any read.
func (o *personaRetrievalChatOwner) RetrieveChat(ctx context.Context, req retrieval.OwnerRequest) ([]retrieval.Record, error) {
	if o == nil || o.threads == nil || o.now == nil {
		return nil, errPersonaRunModelWork
	}
	if req.Access.TenantID != o.request.TenantID || req.Access.PrincipalID != o.request.InvokerID ||
		req.Access.ConversationID != o.request.ConversationID ||
		req.Query.ConversationID != o.request.ConversationID || req.Query.ThreadID != o.request.ThreadID {
		return nil, fmt.Errorf("%w: retrieval scope does not match admitted thread", errPersonaRunModelWork)
	}
	limit := req.Query.Limit
	if limit <= 0 || limit > o.request.Limit {
		limit = o.request.Limit
	}
	scoped := o.request
	scoped.Limit = limit
	posts, err := o.threads.ReadThread(ctx, scoped)
	if err != nil {
		return nil, err
	}
	now := o.now().UTC()
	records := make([]retrieval.Record, 0, len(posts))
	observed := make(map[string]agentinvoke.ThreadPost, len(posts))
	for _, post := range posts {
		if strings.TrimSpace(post.Body) == "" {
			// The envelope contract carries content-bearing records only; a
			// post without content cannot be cited and stays undisclosed.
			continue
		}
		token := digestPersonaThreadPost(post)
		records = append(records, retrieval.Record{
			Envelope: retrieval.Envelope{
				Kind: retrieval.Chat, Owner: "chat",
				TenantID: post.TenantID, LegalEntityID: req.Access.LegalEntityID,
				SourceID: post.ID, Version: "thread", Classification: o.class,
				Audience: []string{req.Access.Audience}, Purpose: req.Access.Purpose,
				ConversationID: post.ConversationID, ThreadID: o.request.ThreadID,
				ObservedAt: now, RetrievedAt: now, FreshUntil: now.Add(personaRetrievalFreshTTL),
				Citation: retrieval.Citation{SourceID: post.ID, Version: "thread",
					Target: "chat://" + post.TenantID + "/" + post.ConversationID + "#" + post.ID},
				RevocationToken: token,
			},
			Content: post.Body,
		})
		observed[post.ID] = post
	}
	o.observed = observed
	return records, nil
}

// personaRetrievalGrantObserver implements retrieval.CurrentGrantChecker by
// re-reading the admitted thread through the same composed port and comparing
// exact content pins. One observation serves every record checked in a phase;
// nextObservation advances to a fresh read so Finalize rechecks against
// current owner state instead of the Retrieve-phase image.
type personaRetrievalGrantObserver struct {
	threads agentinvoke.ThreadReader
	request agentinvoke.ThreadReadRequest

	mu         sync.Mutex
	generation uint64
	observed   uint64
	pins       map[string]string
	valid      bool
}

var _ retrieval.CurrentGrantChecker = (*personaRetrievalGrantObserver)(nil)

// CheckCurrent reports whether one exact post version is still visible with
// unchanged content. A vanished or altered post is a revocation; a failed
// re-read stays a distinct authorization fault without owner detail.
func (g *personaRetrievalGrantObserver) CheckCurrent(ctx context.Context, access retrieval.Access, envelope retrieval.Envelope) error {
	if g == nil || g.threads == nil {
		return errPersonaRunModelWork
	}
	if envelope.Kind != retrieval.Chat || envelope.TenantID != access.TenantID ||
		envelope.ConversationID != access.ConversationID || envelope.ThreadID != g.request.ThreadID {
		return retrieval.ErrGrantRevoked
	}
	pins, err := g.currentPins(ctx)
	if err != nil {
		return err
	}
	if digest, ok := pins[envelope.SourceID]; !ok || digest != envelope.RevocationToken {
		return retrieval.ErrGrantRevoked
	}
	return nil
}

func (g *personaRetrievalGrantObserver) currentPins(ctx context.Context) (map[string]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.valid && g.observed == g.generation {
		return g.pins, nil
	}
	posts, err := g.threads.ReadThread(ctx, g.request)
	if err != nil {
		return nil, err
	}
	pins := make(map[string]string, len(posts))
	for _, post := range posts {
		pins[post.ID] = digestPersonaThreadPost(post)
	}
	g.pins, g.observed, g.valid = pins, g.generation, true
	return pins, nil
}

func (g *personaRetrievalGrantObserver) nextObservation() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generation++
	g.valid = false
}

// disclosePersonaThreadPosts runs the admitted thread fetch through AGENT-018
// retrieval: the owner read, a current-grant observation for Retrieve, and a
// fresh observation for Finalize. Only currently authorized posts are
// returned, in owner order, with retrieval-authorized content bytes.
func disclosePersonaThreadPosts(ctx context.Context, threads agentinvoke.ThreadReader, now func() time.Time, admission agentrun.Record, class string) ([]agentinvoke.ThreadPost, error) {
	access, query, base := personaRetrievalChatScope(admission)
	owner := &personaRetrievalChatOwner{threads: threads, request: base, class: class, now: now}
	grants := &personaRetrievalGrantObserver{threads: threads, request: base}
	service, err := retrieval.New(retrieval.Config{Chat: owner, Grants: grants, Now: now})
	if err != nil {
		return nil, err
	}
	batch, err := service.Retrieve(ctx, access, query)
	if err != nil {
		return nil, err
	}
	grants.nextObservation()
	disclosed, err := service.Finalize(ctx, access, batch)
	if err != nil {
		return nil, err
	}
	items := disclosed.Items()
	if len(items) == 0 {
		return nil, errPersonaRunModelWork
	}
	posts := make([]agentinvoke.ThreadPost, 0, len(items))
	for _, item := range items {
		envelope := item.Envelope
		meta, ok := owner.observed[envelope.SourceID]
		if !ok {
			return nil, errPersonaRunModelWork
		}
		meta.TenantID, meta.ConversationID, meta.ThreadID = envelope.TenantID, envelope.ConversationID, envelope.ThreadID
		meta.ID, meta.Body = envelope.SourceID, item.Content
		posts = append(posts, meta)
	}
	return posts, nil
}
