package main

// The chat demo seeder. It lays down one tenant's collaboration history the way
// the product would have produced it over two weeks: real rooms, real people
// from the demo workforce, threads, reactions, pins, read state and media that
// lives in the media service and is only referenced from posts.
//
// It writes through the chat store adapter rather than the authorized service on
// purpose. The adapter is the one seam that accepts an explicit timestamp, and a
// seeder that had to satisfy live authority for every one of several hundred
// historical posts would either need a fake authority or take minutes.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// chatSeedAdminPrefix identifies the persona the workspace signs in as, and
// therefore the one whose read state is worth seeding: an unread badge on every
// room is a worse first impression than a mostly-read inbox. It is matched by the
// stable numeric prefix rather than the full worker key, because the name half of
// the key comes from the generated corpus and is not this seeder's to assume.
const chatSeedAdminPrefix = "hc-050-"

// defaultArtifactRootPath is the repository's ignored artifact tree, which is
// where generated media bytes belong by convention.
const defaultArtifactRootPath = ".artifacts"

// chatSeedDays is the span of history the seeder lays down, ending at the run's
// own clock.
const chatSeedDays = 14

// chatSeedNamespace scopes every generated conversation identifier, so a seed is
// reproducible and a reset can find exactly what it created and nothing else.
var chatSeedNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext.chat.demo-seed"))

// chatSeedRouteShard and chatSeedRoutePlacementPolicy mirror the constants
// composeChatRouting (internal/application/chat_routing.go) hands
// chatroutingadapter.Options for every conversation the live server creates.
// A seeded room has to land on the same placement a real CreateConversation
// call would choose, or the routed service refuses every later write against
// it (CHAT-04's AddReaction/UpdateReadState failures): this seeder writes
// straight to the chat store adapter to accept explicit historical
// timestamps, which never runs chatroutingadapter.CreateConversation, so
// nothing ever registers these rooms in the core route directory otherwise.
const (
	chatSeedRouteShard           = "chat-default"
	chatSeedRoutePlacementPolicy = "tenant-default"
	chatSeedRoutePolicyVersion   = 1
	// chatSeedRouteEpoch is the epoch chatroutestore.Reserve always assigns a
	// brand-new route (see the literal 1 in its INSERT), and the seeder never
	// moves a room to a different shard, so every seeded conversation keeps
	// this epoch for its whole life.
	chatSeedRouteEpoch = 1
)

type chatSeedOptions struct {
	Tenant     string
	Scale      string
	Reset      bool
	MediaRoot  string
	AssetDir   string
	Now        time.Time
	RandomSeed int64
}

type chatSeedReceipt struct {
	Conversations, Memberships, Posts, Threads, ThreadReplies int
	Reactions, Pins, Images, GIFs, MediaPosts                 int
	EarliestPost, LatestPost                                  time.Time
}

type roomKind string

const (
	roomPublic  roomKind = "public"
	roomPrivate roomKind = "private"
	roomDirect  roomKind = "direct"
	roomGroup   roomKind = "group"
)

// roomSpec is one seeded room: what it is called, who is in it and what the
// people in it talk about.
type roomSpec struct {
	Key, Name string
	Kind      roomKind
	// Members are indexes into the seeded workforce; the first is the owner.
	Members []int
	Topics  []string
}

// chatSeedRooms is the room plan. Public channels come first so a small profile
// is a prefix of the full one rather than a different shape.
func chatSeedRooms(scale string, people int) []roomSpec {
	pick := func(n, from int) []int {
		out := make([]int, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, (from+i*3)%people)
		}
		return out
	}
	rooms := []roomSpec{
		{Key: "general", Name: "general", Kind: roomPublic, Members: pick(18, 0), Topics: generalTopics},
		{Key: "announcements", Name: "announcements", Kind: roomPublic, Members: pick(20, 1), Topics: announcementTopics},
		{Key: "people-ops", Name: "people-ops", Kind: roomPublic, Members: pick(12, 2), Topics: peopleOpsTopics},
		{Key: "comp-review", Name: "comp-review", Kind: roomPublic, Members: pick(9, 3), Topics: compTopics},
		{Key: "engineering", Name: "engineering", Kind: roomPublic, Members: pick(14, 4), Topics: engineeringTopics},
		{Key: "design", Name: "design", Kind: roomPublic, Members: pick(8, 5), Topics: designTopics},
		{Key: "sales", Name: "sales", Kind: roomPublic, Members: pick(11, 6), Topics: salesTopics},
		{Key: "benefits", Name: "benefits", Kind: roomPublic, Members: pick(13, 7), Topics: benefitsTopics},
		{Key: "onboarding", Name: "onboarding", Kind: roomPublic, Members: pick(10, 8), Topics: onboardingTopics},
		{Key: "random", Name: "random", Kind: roomPublic, Members: pick(16, 9), Topics: randomTopics},
		{Key: "leadership-private", Name: "leadership-private", Kind: roomPrivate, Members: pick(6, 0), Topics: leadershipTopics},
		{Key: "payroll-close", Name: "payroll-close", Kind: roomPrivate, Members: pick(5, 11), Topics: payrollTopics},
		{Key: "incident-review", Name: "incident-review", Kind: roomPrivate, Members: pick(7, 13), Topics: incidentTopics},
	}
	for i := 0; i < 8; i++ {
		a, b := (i*5)%people, (i*5+2)%people
		rooms = append(rooms, roomSpec{Key: fmt.Sprintf("dm-%d", i), Kind: roomDirect, Members: []int{a, b}, Topics: dmTopics})
	}
	rooms = append(rooms,
		roomSpec{Key: "group-comp-cycle", Name: "comp cycle working group", Kind: roomGroup, Members: pick(5, 4), Topics: compTopics},
		roomSpec{Key: "group-q4-hiring", Name: "Q4 hiring huddle", Kind: roomGroup, Members: pick(4, 12), Topics: onboardingTopics},
	)
	if scale == "small" {
		// One of every kind: three public channels, one private, one direct and
		// one group, so the small profile exercises the same shapes as the full
		// one instead of only the cheapest ones.
		return []roomSpec{rooms[0], rooms[2], rooms[4], rooms[10], rooms[13], rooms[len(rooms)-1]}
	}
	return rooms
}

func kindOf(k roomKind) chatcore.ConversationKind {
	switch k {
	case roomPrivate:
		return chatcore.PrivateChannel
	case roomDirect:
		return chatcore.Direct
	case roomGroup:
		return chatcore.Group
	}
	return chatcore.PublicChannel
}

// seedScanner admits the seeder's own generated bytes. It is a seeder-local
// decision and never reachable from a request path: the serve role composes the
// real scanner, and an absent one still fails closed there.
type seedScanner struct{}

func (seedScanner) Scan(context.Context, string, io.Reader) (quarantine.Verdict, error) {
	return quarantine.Verdict{Safe: true, Reason: "demo seed content"}, nil
}

// routes is the core route directory (internal/data/chatroutestore in
// production; see composeChatRouting). It is optional -- nil keeps the
// seeder usable in tests and compositions that never wire chat routing -- but
// a live demo tenant must always pass one, or its rooms come up unreachable
// through every lease-guarded write the routed service makes.
func runChatSeedCommand(ctx context.Context, store *chatstore.Store, routes chatrouting.Directory, opts chatSeedOptions, out io.Writer) error {
	if strings.TrimSpace(opts.Tenant) == "" {
		return fmt.Errorf("-%s is required by chat seed", fieldTenant)
	}
	if opts.Scale == "" {
		opts.Scale = "full"
	}
	if opts.Scale != "small" && opts.Scale != "full" {
		return fmt.Errorf("unknown -%s %q; use small or full", fieldChatSeedScale, opts.Scale)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.MediaRoot == "" {
		opts.MediaRoot = filepath.Join(defaultArtifactRootPath, "chat-media")
	}
	employees, err := demoworkforce.Plan(pgstore.TenantID(opts.Tenant))
	if err != nil {
		return err
	}
	people := make([]string, 0, len(employees))
	names := make([]string, 0, len(employees))
	for _, e := range employees {
		people = append(people, e.Row.WorkerKey)
		names = append(names, e.Row.LegalName)
	}
	adapter := chatstore.NewAdapter(store)
	rooms := chatSeedRooms(opts.Scale, len(people))
	// The signed-in persona belongs in every room: a demo where the person
	// looking at it is a member of nothing shows an empty product.
	if admin := adminIndex(people); admin >= 0 {
		for i := range rooms {
			if rooms[i].Kind == roomDirect {
				continue
			}
			if !containsIndex(rooms[i].Members, admin) {
				rooms[i].Members = append(rooms[i].Members, admin)
			}
		}
		// One direct message with the persona in it, so the DM list is not all
		// other people's conversations.
		if len(rooms) > 0 {
			for i := range rooms {
				if rooms[i].Kind == roomDirect {
					rooms[i].Members = []int{admin, rooms[i].Members[1]}
					break
				}
			}
		}
	}

	present, err := chatSeedPresent(ctx, store, opts.Tenant, rooms)
	if err != nil {
		return err
	}
	if present && !opts.Reset {
		return fmt.Errorf("chat demo rooms already exist in %s; pass -%s to wipe and recreate them", opts.Tenant, fieldChatSeedReset)
	}
	if present {
		if err := chatSeedWipe(ctx, store, opts.Tenant, rooms); err != nil {
			return err
		}
	}

	media, err := newSeedMedia(opts.MediaRoot, opts.AssetDir)
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewSource(chatSeedSource(opts.RandomSeed)))
	receipt := chatSeedReceipt{}
	start := opts.Now.Add(-chatSeedDays * 24 * time.Hour)

	// Every write after a room's own CreateConversation -- its history,
	// reactions, pins and the admin's read state -- needs the same route
	// lease creation did, or chatstore's routeFence refuses it with
	// ErrNoRouteLease now that route_shard is no longer empty (store.go).
	// fenceContextWrite only ever reads the lease's Epoch and ShardID (see
	// leaseFence), never cross-checking it against the conversation being
	// written, so one lease naming the shard every seeded room reserves and
	// the epoch chatroutestore.Reserve always starts a fresh route at covers
	// all of them; leaseSeedRoute below still builds a conversation-exact
	// lease for CreateConversation itself, which does check identity. The
	// document seed's showcase chat posts (document_seed_demo.go,
	// postDemoMessages) write to these same rooms after the fact and need
	// exactly this lease too, so it is built by the shared
	// chatSeedWriteLeaseContext helper rather than duplicated there.
	writeCtx := chatSeedWriteLeaseContext(ctx, routes, opts.Tenant, opts.Now)

	for roomIndex, room := range rooms {
		conversation := chatSeedConversationID(opts.Tenant, room.Key)
		owner := people[room.Members[0]]
		name := room.Name
		if name == "" {
			name = strings.Join(memberNames(names, room.Members), ", ")
		}
		members := make([]chatcore.Membership, 0, len(room.Members))
		for _, index := range room.Members {
			role := chatcore.Member
			if people[index] == owner {
				role = chatcore.Manager
			}
			// FULL_HISTORY throughout: a seeded member joining "now" with
			// FROM_JOIN would see none of the history this seeder just wrote.
			members = append(members, chatcore.Membership{ConversationID: conversation, TenantID: opts.Tenant, HomeTenantID: opts.Tenant, SubjectID: people[index], Role: role, HistoryVisibility: chatcore.FullHistory, Revision: 1})
		}
		leasedCtx, reserved, err := leaseSeedRoute(ctx, routes, opts.Tenant, conversation, "seed:"+room.Key)
		if err != nil {
			return fmt.Errorf("reserve route %s: %w", room.Key, err)
		}
		if _, err = adapter.CreateConversation(leasedCtx, chatcore.Conversation{ID: conversation, TenantID: opts.Tenant, Kind: kindOf(room.Kind), Name: name, OwnerID: owner, Revision: 1}, members, "seed:"+room.Key); err != nil {
			return fmt.Errorf("create %s: %w", room.Key, err)
		}
		if err = activateSeedRoute(ctx, routes, opts.Tenant, conversation, reserved); err != nil {
			return fmt.Errorf("activate route %s: %w", room.Key, err)
		}
		receipt.Conversations++
		receipt.Memberships += len(members)

		if err = seedRoomHistory(writeCtx, adapter, media, seedRoomInput{
			opts: opts, room: room, roomIndex: roomIndex, conversation: conversation,
			people: people, names: names, start: start, rng: rng,
		}, &receipt); err != nil {
			return fmt.Errorf("seed %s: %w", room.Key, err)
		}
	}

	if err = seedAdminReadState(writeCtx, adapter, opts, rooms, people); err != nil {
		return err
	}
	fmt.Fprintf(out, "seeded chat demo for %s (%s): %d conversations, %d memberships, %d posts, %d threads with %d replies, %d reactions, %d pins, %d image posts, %d animated posts\n",
		opts.Tenant, opts.Scale, receipt.Conversations, receipt.Memberships, receipt.Posts, receipt.Threads, receipt.ThreadReplies, receipt.Reactions, receipt.Pins, receipt.Images, receipt.GIFs)
	fmt.Fprintf(out, "history spans %s to %s\n", receipt.EarliestPost.Format(time.RFC3339), receipt.LatestPost.Format(time.RFC3339))
	return nil
}

// leaseSeedRoute places a seeded conversation in the core route directory
// exactly as chatroutingadapter.Service.CreateConversation would for a live
// caller: reserve the default shard under a stable idempotency key, both
// idempotent so re-running the seeder (including a reset that recreates the
// same deterministic conversation ID) neither errors nor drifts the route.
//
// It returns a context carrying the reservation as a write lease, which the
// caller must pass to chatstore.Adapter.CreateConversation. That is not
// optional decoration: chatstore's own CreateConversation (see the "shard"
// and "epoch" locals in internal/data/chatstore/contracts_adapter.go) reads
// exactly this lease to stamp chat_conversation.route_shard/route_epoch, its
// own mirror of the placement chatroutestore just reserved. Registering the
// route here without also threading the lease through creation leaves that
// mirror at its unrouted default ("" / epoch 1) forever: chatstore's own
// routeFence then refuses every later lease-guarded write against the room
// with chatrouting.ErrStaleEpoch, because the two placement records disagree
// about whether the room is routed at all. Call activateSeedRoute once
// CreateConversation has committed.
//
// A nil directory is a no-op returning ctx unchanged: some compositions
// (tests, environments with chat routing not wired) never pass one, and such
// rooms simply stay unrouted the way they always have.
func leaseSeedRoute(ctx context.Context, routes chatrouting.Directory, tenant, conversationID, idempotencyKey string) (context.Context, chatrouting.Route, error) {
	if routes == nil {
		return ctx, chatrouting.Route{}, nil
	}
	reserved, err := routes.Reserve(ctx, chatrouting.ReserveRequest{
		ConversationID:         conversationID,
		HostTenantID:           tenant,
		ShardID:                chatSeedRouteShard,
		PlacementPolicy:        chatSeedRoutePlacementPolicy,
		PlacementPolicyVersion: chatSeedRoutePolicyVersion,
		IdempotencyKey:         idempotencyKey,
	})
	if err != nil {
		return ctx, chatrouting.Route{}, err
	}
	// chatstore's CreateConversation reads only the Route fields (see
	// leaseFence/fenceContextWrite in internal/data/chatstore/store.go); it
	// never verifies a lease signature or expiry the way the routed
	// service's own RouteCache does, so a locally-built lease is sufficient
	// here and never touches the routed service's signing key.
	lease := chatrouting.WriteLease{Route: reserved, ExpiresAt: time.Now().Add(time.Hour)}
	return chatrouting.WithWriteLease(ctx, lease), reserved, nil
}

// activateSeedRoute finishes the reservation leaseSeedRoute began, the same
// second step chatroutingadapter.Service.CreateConversation takes after its
// own inner create succeeds. reserved.State is only StatePending on a fresh
// reservation; a retried seed run whose idempotency key already resolved to
// an ACTIVE route has nothing left to activate.
func activateSeedRoute(ctx context.Context, routes chatrouting.Directory, tenant, conversationID string, reserved chatrouting.Route) error {
	if routes == nil || reserved.State != chatrouting.StatePending {
		return nil
	}
	_, err := routes.Activate(ctx, conversationID, tenant, reserved.Epoch)
	return err
}

// chatSeedWriteLeaseContext builds the write-lease context every chat write
// against an already-routed seeded room needs: one lease naming the shard
// every seeded room reserves (chatSeedRouteShard) and the epoch
// chatroutestore.Reserve always starts a fresh route at (chatSeedRouteEpoch).
// fenceContextWrite (internal/data/chatstore/store.go) only ever compares the
// lease's Epoch and ShardID against the conversation's own route_epoch/
// route_shard, never the conversation identity, so this single lease covers
// every seeded room on the default shard -- both the rooms runChatSeedCommand
// itself writes history into and the ones the document seed's showcase posts
// (document_seed_demo.go, postDemoMessages) write into afterwards, in a
// separate process invocation.
//
// A nil directory is a no-op returning ctx unchanged: some compositions
// (tests, environments with chat routing not wired) never pass one, and such
// rooms simply stay unrouted the way they always have.
func chatSeedWriteLeaseContext(ctx context.Context, routes chatrouting.Directory, tenant string, now time.Time) context.Context {
	if routes == nil {
		return ctx
	}
	return chatrouting.WithWriteLease(ctx, chatrouting.WriteLease{
		Route:     chatrouting.Route{HostTenantID: tenant, ShardID: chatSeedRouteShard, Epoch: chatSeedRouteEpoch, State: chatrouting.StateActive},
		ExpiresAt: now.Add(24 * time.Hour),
	})
}

func chatSeedSource(seed int64) int64 {
	if seed != 0 {
		return seed
	}
	return 20260921
}

func containsIndex(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func memberNames(names []string, members []int) []string {
	out := make([]string, 0, len(members))
	for _, index := range members {
		out = append(out, names[index])
	}
	return out
}

// chatSeedConversationID is the deterministic identifier for one seeded room. It
// is what makes the seed idempotent: a reset knows exactly which rows it owns.
func chatSeedConversationID(tenant, key string) string {
	return uuid.NewSHA1(chatSeedNamespace, []byte(tenant+"\x00"+key)).String()
}

func chatSeedPresent(ctx context.Context, store *chatstore.Store, tenant string, rooms []roomSpec) (bool, error) {
	for _, room := range rooms {
		found, err := store.ConversationExists(ctx, tenant, chatSeedConversationID(tenant, room.Key))
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func chatSeedWipe(ctx context.Context, store *chatstore.Store, tenant string, rooms []roomSpec) error {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		ids = append(ids, chatSeedConversationID(tenant, room.Key))
	}
	return store.PurgeConversations(ctx, tenant, ids)
}

type seedRoomInput struct {
	opts         chatSeedOptions
	room         roomSpec
	roomIndex    int
	conversation string
	people       []string
	names        []string
	start        time.Time
	rng          *rand.Rand
}

// seedRoomHistory writes one room's two weeks. The clock steps forward for every
// post, so sequence order and wall-clock order agree and a client's newest page
// is the newest conversation.
func seedRoomHistory(ctx context.Context, adapter *chatstore.Adapter, media *seedMedia, in seedRoomInput, receipt *chatSeedReceipt) error {
	posts := 34
	if in.opts.Scale == "small" {
		posts = 9
	}
	if in.room.Kind == roomDirect {
		posts = posts / 2
	}
	// Each post gets a scheduled slot across the whole window with a little
	// jitter inside it. Stepping by a random amount instead would make the span a
	// random walk, and a room whose history covers nine of fourteen days looks
	// like a room that went quiet rather than a room with two weeks in it.
	window := time.Duration(chatSeedDays) * 24 * time.Hour
	slot := window / time.Duration(posts+1)
	roots := make([]chatcore.Post, 0, posts)
	for i := 0; i < posts; i++ {
		jitter := time.Duration(in.rng.Int63n(int64(slot/2+time.Minute))) - slot/4
		clock := in.start.Add(time.Duration(i+1)*slot + jitter)
		if !clock.After(in.start) {
			clock = in.start.Add(time.Minute)
		}
		if clock.After(in.opts.Now) {
			clock = in.opts.Now.Add(-time.Duration(posts-i) * time.Minute)
		}
		author := in.people[in.room.Members[i%len(in.room.Members)]]
		body, refs := seedBody(in, i, author)
		mediaRefs, mediaErr := seedMediaFor(ctx, media, in, i, author, receipt)
		if mediaErr != nil {
			return mediaErr
		}
		refs = append(refs, mediaRefs...)
		post, err := adapter.SendPost(ctx,
			chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: in.opts.Tenant, SubjectID: author}, TenantID: in.opts.Tenant, ConversationID: in.conversation, IdempotencyKey: fmt.Sprintf("seed:%s:%d", in.room.Key, i)},
			chatcore.Post{AuthorID: author, AuthorHomeTenantID: in.opts.Tenant, Body: body, References: refs, CreatedAt: clock})
		if err != nil {
			return err
		}
		receipt.Posts++
		if len(mediaRefs) > 0 {
			receipt.MediaPosts++
		}
		trackSpan(receipt, clock)
		roots = append(roots, post)

		// Threads: a reply chain hanging off a root post, which is what makes a
		// room look discussed rather than broadcast.
		if i%4 == 1 {
			replies := 2 + in.rng.Intn(7)
			receipt.Threads++
			for r := 0; r < replies; r++ {
				clock = clock.Add(time.Duration(3+in.rng.Intn(40)) * time.Minute)
				if clock.After(in.opts.Now) {
					clock = in.opts.Now
				}
				replyAuthor := in.people[in.room.Members[(i+r+1)%len(in.room.Members)]]
				if _, err = adapter.SendPost(ctx,
					chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: in.opts.Tenant, SubjectID: replyAuthor}, TenantID: in.opts.Tenant, ConversationID: in.conversation, IdempotencyKey: fmt.Sprintf("seed:%s:%d:%d", in.room.Key, i, r), ParentID: post.ID},
					chatcore.Post{AuthorID: replyAuthor, AuthorHomeTenantID: in.opts.Tenant, Body: threadReplies[(i+r)%len(threadReplies)], ParentID: post.ID, CreatedAt: clock}); err != nil {
					return err
				}
				receipt.Posts++
				receipt.ThreadReplies++
				trackSpan(receipt, clock)
			}
		}

		// Reactions cluster on a few posts rather than spreading evenly: that is
		// what a popular message looks like.
		if i%3 == 0 {
			reactors := 1 + in.rng.Intn(5)
			for r := 0; r < reactors && r < len(in.room.Members); r++ {
				subject := in.people[in.room.Members[(i+r+2)%len(in.room.Members)]]
				emoji := seedEmoji[(i+r)%len(seedEmoji)]
				if _, err = adapter.PutReaction(ctx, chatcore.Reaction{ConversationID: in.conversation, PostID: post.ID, TenantID: in.opts.Tenant, HomeTenantID: in.opts.Tenant, SubjectID: subject, Emoji: emoji}); err != nil {
					return err
				}
				receipt.Reactions++
			}
		}
	}
	// One pin per room, by its owner: five rooms is already past the five the
	// product wants to show, and a pinned post is only meaningful if somebody
	// with standing pinned it.
	if len(roots) > 2 {
		pinned := roots[len(roots)/2]
		owner := in.people[in.room.Members[0]]
		if _, err := adapter.PutPin(ctx, chatcore.Pin{ConversationID: in.conversation, PostID: pinned.ID, TenantID: in.opts.Tenant, PinnedBy: owner, PinnedByHomeTenantID: in.opts.Tenant, Revision: 1}); err != nil {
			return err
		}
		receipt.Pins++
	}
	return nil
}

func trackSpan(receipt *chatSeedReceipt, at time.Time) {
	if receipt.EarliestPost.IsZero() || at.Before(receipt.EarliestPost) {
		receipt.EarliestPost = at
	}
	if at.After(receipt.LatestPost) {
		receipt.LatestPost = at
	}
}

// seedBody composes one message and the mentions inside it. A mention is a
// durable reference, not a substring: the body carries the display text and the
// reference carries the subject.
func seedBody(in seedRoomInput, index int, author string) (string, []chatcore.Reference) {
	body := seedLine(in.room.Topics, in.roomIndex, index)
	var refs []chatcore.Reference
	if index%5 == 2 && len(in.room.Members) > 1 {
		target := in.room.Members[(index+1)%len(in.room.Members)]
		subject, display := in.people[target], in.names[target]
		if subject != author {
			body = "@" + display + " " + body
			refs = append(refs, chatcore.Reference{Kind: chatcore.PersonMention, TenantID: in.opts.Tenant, ID: subject, Display: display})
		}
	}
	if index%7 == 3 {
		body += "\n\n" + multilineTail[(in.roomIndex*5+index)%len(multilineTail)]
	}
	return body, refs
}

// seedLine says each of a room's topic lines once, in order, and then draws
// ordinary replies from chatterLines. The stride is coprime with the pool size
// so consecutive posts in one room never repeat a line.
func seedLine(topics []string, roomIndex, index int) string {
	if index < len(topics) {
		return topics[index]
	}
	return chatterLines[(roomIndex*11+(index-len(topics))*7)%len(chatterLines)]
}

// adminIndex locates the signed-in persona in the seeded workforce, or -1.
func adminIndex(people []string) int {
	for i, p := range people {
		if strings.HasPrefix(p, chatSeedAdminPrefix) {
			return i
		}
	}
	return -1
}

func seedAdminReadState(ctx context.Context, adapter *chatstore.Adapter, opts chatSeedOptions, rooms []roomSpec, people []string) error {
	index := adminIndex(people)
	if index < 0 {
		return nil
	}
	admin := people[index]
	for i, room := range rooms {
		conversation := chatSeedConversationID(opts.Tenant, room.Key)
		member, err := adapter.GetMembership(ctx, opts.Tenant, conversation, opts.Tenant, admin)
		if err != nil || member.SubjectID != admin {
			continue
		}
		// An inbox with texture: most rooms are read, a few carry one new
		// message and a couple have a short backlog. Every room showing the
		// same "1" reads as a fixture, not a workday.
		unread := 0
		switch i % 5 {
		case 1:
			unread = 1
		case 3:
			unread = 2 + i%3
		}
		page, err := adapter.ListPosts(ctx, chatcore.Principal{TenantID: opts.Tenant, SubjectID: admin}, opts.Tenant, conversation, 0, chatcore.Page{PageSize: 200}, chatcore.PostWindow{Descending: true})
		if err != nil || len(page.Posts) <= unread {
			continue
		}
		last := page.Posts[len(page.Posts)-1-unread].Sequence
		// A reseed keeps the persona's read state rows (they hang off the
		// member, not the room), so the write must carry the revision that is
		// there; a fixed "1" conflicted the moment anyone had read a room.
		expected := uint64(1)
		if current, readErr := adapter.GetReadState(ctx, opts.Tenant, conversation, opts.Tenant, admin); readErr == nil && current.Revision > 0 {
			expected = current.Revision
		}
		if _, err = adapter.PutReadState(ctx, chatcore.ReadState{ConversationID: conversation, TenantID: opts.Tenant, HomeTenantID: opts.Tenant, SubjectID: admin, LastReadSequence: last, Revision: expected}, expected); err != nil {
			return err
		}
	}
	return nil
}

// seedMedia owns the generated artifacts and the media service they go through.
// Nothing here writes bytes into a post: every attachment is an upload followed
// by a reference to the artifact it produced.
type seedMedia struct {
	svc      *chatmedia.Service
	photos   [][]byte
	drawings [][]byte
	loops    [][]byte
	names    []string
}

func newSeedMedia(root, assetDir string) (*seedMedia, error) {
	store, err := chatmedia.NewFilesystemStore(root)
	if err != nil {
		return nil, err
	}
	svc := chatmedia.New(chatmedia.Config{Store: store, Scanner: seedScanner{}, Authorize: func(context.Context, chatmedia.AccessRequest) error { return nil }})
	m := &seedMedia{svc: svc}
	m.drawings = [][]byte{orgChartSketch(), compBandChart(), headcountTable()}
	for i := 0; i < 6; i++ {
		m.loops = append(m.loops, celebrationLoop(i))
	}
	m.loadPhotos(assetDir)
	return m, nil
}

// loadPhotos converts the demo profile photos to PNG. The media service admits
// PNG, GIF, BMP, MP3, WAV and MP4 and not JPEG, so the seeder re-encodes rather
// than declaring a type the service would refuse.
func (m *seedMedia) loadPhotos(assetDir string) {
	if assetDir == "" {
		return
	}
	for i := 1; i <= 60 && len(m.photos) < 25; i++ {
		name := fmt.Sprintf("person-hc-%03d-small.jpg", i)
		raw, err := os.ReadFile(filepath.Join(assetDir, name))
		if err != nil {
			continue
		}
		img, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		if png.Encode(&buf, img) != nil {
			continue
		}
		m.photos = append(m.photos, buf.Bytes())
		m.names = append(m.names, strings.TrimSuffix(name, ".jpg")+".png")
	}
}

// seedMediaFor attaches media to roughly every third post, alternating between a
// converted photo, a drawn chart and an animated loop.
func seedMediaFor(ctx context.Context, media *seedMedia, in seedRoomInput, index int, author string, receipt *chatSeedReceipt) ([]chatcore.Reference, error) {
	if index%3 != 2 {
		return nil, nil
	}
	choice := (in.roomIndex + index) % 3
	var content []byte
	var declared chatmedia.MediaType
	var filename string
	switch {
	case choice == 2 && len(media.loops) > 0:
		content, declared = media.loops[(in.roomIndex+index)%len(media.loops)], chatmedia.MediaGIF
		filename = "celebration.gif"
	case choice == 1 && len(media.drawings) > 0:
		content, declared = media.drawings[(in.roomIndex+index)%len(media.drawings)], chatmedia.MediaPNG
		filename = []string{"org-chart-sketch.png", "comp-bands.png", "headcount-table.png"}[(in.roomIndex+index)%3]
	case len(media.photos) > 0:
		pick := (in.roomIndex*7 + index) % len(media.photos)
		content, declared = media.photos[pick], chatmedia.MediaPNG
		filename = media.names[pick]
	default:
		return nil, nil
	}
	ref, err := media.svc.Upload(ctx, chatmedia.UploadRequest{
		TenantID: in.opts.Tenant, ConversationID: in.conversation, PrincipalID: author,
		Filename: filename, DeclaredType: string(declared), Content: content,
		AltText: "demo attachment " + filename,
	})
	if err != nil {
		return nil, fmt.Errorf("upload %s: %w", filename, err)
	}
	if declared == chatmedia.MediaGIF {
		receipt.GIFs++
	} else {
		receipt.Images++
	}
	width, height := imageBounds(content)
	return []chatcore.Reference{{
		Kind: chatcore.MediaAttachment, TenantID: in.opts.Tenant, ID: ref.ArtifactID,
		ConversationID: in.conversation, Display: filename,
		ContentType: string(ref.MediaType), ByteSize: uint64(ref.Size),
		Width: width, Height: height,
	}}, nil
}

func imageBounds(content []byte) (uint32, uint32) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return 0, 0
	}
	return uint32(cfg.Width), uint32(cfg.Height)
}

// orgChartSketch draws boxes and connectors: a recognizable org chart without
// any real name on it.
func orgChartSketch() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 480, 280))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{250, 250, 252, 255}}, image.Point{}, draw.Src)
	box := func(x, y, w, h int, c color.RGBA) {
		draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	line := func(x, y, w, h int) { box(x, y, w, h, color.RGBA{120, 130, 145, 255}) }
	box(190, 20, 100, 40, color.RGBA{63, 98, 168, 255})
	line(239, 60, 2, 30)
	line(70, 90, 342, 2)
	for i, x := range []int{40, 190, 340} {
		line(x+50, 90, 2, 20)
		box(x, 110, 100, 36, color.RGBA{92, 140, 196, 255})
		for j := 0; j < 2; j++ {
			line(x+50, 146, 2, 18)
			box(x+10+j*45-10, 164+j*40, 80, 28, color.RGBA{150, 180, 214, 255})
		}
		_ = i
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// compBandChart draws a bar chart of pay bands.
func compBandChart() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 460, 260))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(40, 220, 440, 222), &image.Uniform{color.RGBA{90, 90, 100, 255}}, image.Point{}, draw.Src)
	heights := []int{60, 92, 118, 140, 172, 196}
	for i, h := range heights {
		x := 56 + i*64
		draw.Draw(img, image.Rect(x, 220-h, x+40, 220), &image.Uniform{color.RGBA{uint8(60 + i*24), 120, 190, 255}}, image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// headcountTable draws a striped grid that reads like a screenshot of a table.
func headcountTable() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 520, 240))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 520, 28), &image.Uniform{color.RGBA{63, 98, 168, 255}}, image.Point{}, draw.Src)
	for row := 0; row < 7; row++ {
		y := 28 + row*30
		shade := uint8(244)
		if row%2 == 1 {
			shade = 232
		}
		draw.Draw(img, image.Rect(0, y, 520, y+28), &image.Uniform{color.RGBA{shade, shade, shade, 255}}, image.Point{}, draw.Src)
		for col := 1; col < 4; col++ {
			draw.Draw(img, image.Rect(col*130, y, col*130+2, y+28), &image.Uniform{color.RGBA{200, 205, 212, 255}}, image.Point{}, draw.Src)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// celebrationLoop builds a short animated GIF: confetti dots falling on a loop.
// Multi-frame and genuinely animated, because a one-frame GIF proves nothing
// about how the client renders one.
func celebrationLoop(variant int) []byte {
	const w, h, frames = 120, 90, 8
	palette := color.Palette{
		color.RGBA{255, 255, 255, 255},
		color.RGBA{232, 76, 92, 255},
		color.RGBA{63, 98, 168, 255},
		color.RGBA{246, 190, 75, 255},
		color.RGBA{86, 176, 128, 255},
	}
	anim := gif.GIF{LoopCount: 0}
	for f := 0; f < frames; f++ {
		frame := image.NewPaletted(image.Rect(0, 0, w, h), palette)
		draw.Draw(frame, frame.Bounds(), &image.Uniform{palette[0]}, image.Point{}, draw.Src)
		for dot := 0; dot < 12; dot++ {
			x := (dot*11 + variant*3) % (w - 6)
			y := (dot*17 + f*11) % (h - 6)
			shade := uint8(1 + (dot+variant)%4)
			draw.Draw(frame, image.Rect(x, y, x+5, y+5), &image.Uniform{palette[shade]}, image.Point{}, draw.Src)
		}
		anim.Image = append(anim.Image, frame)
		anim.Delay = append(anim.Delay, 12)
	}
	var buf bytes.Buffer
	_ = gif.EncodeAll(&buf, &anim)
	return buf.Bytes()
}
