package main

// CHATSEED-002: a moderation history for the demo tenant.
//
// The Moderation page is empty on a freshly seeded demo, so nobody can judge
// it. This step lays down what a busy fortnight would have left there: reports
// waiting for a decision, messages held or noted by filters, and the decisions
// already taken, one of them reversed after an appeal.
//
// Everything goes through the product's own services, not around them: reports
// through the records service after the same "may this person report this
// message" check the page makes, decisions through the moderation service as
// the workspace administrator, removals through its preview and apply pair,
// the appeal through the author's own command, and filters through the filter
// service, which records the hits and delivers the notices exactly as a sent
// message does. The messages themselves are written the way the chat seed
// writes its history (the chat store adapter accepts an explicit time), because
// a live send could not be dated in the past.
//
// The step is additive and safe to repeat. Messages carry a fixed idempotency
// key, reports a deterministic identifier, and every decision is first checked
// against what is already recorded, so a second run changes nothing. It never
// touches a room's membership or history.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatrecordstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatSeedModerationNamespace scopes the identifiers this step derives.
var chatSeedModerationNamespace = uuid.NewSHA1(chatSeedNamespace, []byte("moderation"))

// The filters the step adds. The word list that holds a message is the
// product's own English profanity list, switched on in one channel to flag for
// review; the two custom filters are ordinary versioned definitions.
const (
	chatSeedModerationBuiltinList = "builtin-en-profanity"
	chatSeedModerationNotifyRule  = "seed-client-codenames"
	chatSeedModerationChannelRule = "seed-offer-details"
	chatSeedModerationFilterEdit  = "1.0.0"
)

// chatSeedModerationReceipt says what one run did.
type chatSeedModerationReceipt struct{ Created, Present int }

// moderationScenario is one message the history is about: where it was said,
// by which member of the room, what it said and when. DaysAgo and Hour place
// it in office hours; the step moves it later when the room already has newer
// messages, so a room never shows time running backwards.
type moderationScenario struct {
	Key, Room, Body string
	Slot            int
	DaysAgo         int
	Hour, Minute    int
}

var (
	modHarassment = moderationScenario{Key: "open-harassment", Room: "people-ops", Slot: 1, DaysAgo: 2, Hour: 10, Minute: 15,
		Body: "Honestly, if you had read the policy document before posting you would not need to ask this every week. Please stop filling this channel with questions that are already answered."}
	modSpam = moderationScenario{Key: "open-spam", Room: "general", Slot: 2, DaysAgo: 3, Hour: 14, Minute: 40,
		Body: "FINAL NOTICE: you have been selected for a free gift card. Claim yours today at http://gift-rewards.example/claim before the offer disappears!"}
	modHeld = moderationScenario{Key: "open-filter-held", Room: "engineering", Slot: 1, DaysAgo: 1, Hour: 15, Minute: 5,
		Body: "The damn staging deploy failed again overnight. Who touched the config file last night?"}
	modNotified = moderationScenario{Key: "open-filter-notify", Room: "engineering", Slot: 2, DaysAgo: 2, Hour: 9, Minute: 20,
		Body: "Sending the Project Falcon load numbers to the client this afternoon, can someone double-check the totals before I do?"}
	modPrivate = moderationScenario{Key: "open-private-report", Room: "leadership-private", Slot: 1, DaysAgo: 1, Hour: 11, Minute: 30,
		Body: "The draft bonus figures for each team are in the shared planning sheet if anyone wants to compare them before Friday."}
	modDismissed = moderationScenario{Key: "resolved-dismissed", Room: "people-ops", Slot: 3, DaysAgo: 6, Hour: 13, Minute: 10,
		Body: "The new onboarding checklist is a mess. Half the steps are duplicated and nobody has fixed it in weeks."}
	modRemoved = moderationScenario{Key: "resolved-removed", Room: "general", Slot: 5, DaysAgo: 8, Hour: 11, Minute: 45,
		Body: "If the benefits portal keeps timing out, call the carrier's rep directly on her mobile, 303-555-0142, she answers everything."}
	modRestored = moderationScenario{Key: "resolved-restored", Room: "engineering", Slot: 3, DaysAgo: 10, Hour: 16, Minute: 0,
		Body: "That vendor demo wasted a whole afternoon. Whoever booked it owes the team four hours."}
	modReleased = moderationScenario{Key: "resolved-released", Room: "engineering", Slot: 4, DaysAgo: 5, Hour: 10, Minute: 30,
		Body: "This crap build cache is finally fixed, thanks for staying late to sort it out."}
)

func chatSeedModerationScenario(key string) (moderationScenario, bool) {
	for _, scenario := range []moderationScenario{modHarassment, modSpam, modHeld, modNotified, modPrivate, modDismissed, modRemoved, modRestored, modReleased} {
		if scenario.Key == key {
			return scenario, true
		}
	}
	return moderationScenario{}, false
}

func chatSeedModerationMember(room roomSpec, people []string, admin string, slot int) string {
	var candidates []string
	seen := map[string]bool{admin: true}
	for i, index := range room.Members {
		if i == 0 || index < 0 || index >= len(people) {
			continue
		}
		subject := people[index]
		if seen[subject] {
			continue
		}
		seen[subject] = true
		candidates = append(candidates, subject)
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[slot%len(candidates)]
}

type chatSeedModerationPersonaPostSource struct {
	tenant   string
	people   []string
	admin    string
	scenario moderationScenario
	id       string
}

func (s chatSeedModerationPersonaPostSource) IsKnownSyntheticPersonaPost(_ context.Context, post chatcore.Post) bool {
	canonical, known := chatSeedModerationScenario(s.scenario.Key)
	if !known || canonical != s.scenario || s.tenant == "" || s.id == "" || adminIndexForTenant(s.tenant, s.people) < 0 {
		return false
	}
	adminIndex := adminIndexForTenant(s.tenant, s.people)
	if s.people[adminIndex] != s.admin {
		return false
	}
	var room roomSpec
	roomFound := false
	for _, candidate := range chatSeedRoomsForTenant("full", s.tenant, s.people) {
		if candidate.Key == s.scenario.Room {
			room, roomFound = candidate, true
			break
		}
	}
	expectedKind := roomPublic
	if s.scenario.Room == "leadership-private" {
		expectedKind = roomPrivate
	}
	if !roomFound || room.Kind != expectedKind {
		return false
	}
	expectedAuthor := chatSeedModerationMember(room, s.people, s.admin, s.scenario.Slot)
	return expectedAuthor != "" && post.ID == s.id && post.TenantID == s.tenant &&
		post.ConversationID == chatSeedConversationID(s.tenant, s.scenario.Room) &&
		post.AuthorID == expectedAuthor && post.AuthorHomeTenantID == s.tenant &&
		post.Body == s.scenario.Body && post.ParentID == "" && !post.Deleted
}

// chatSeedModerationRooms are the rooms the history needs. Each is part of both
// the small and the full chat seed.
var chatSeedModerationRooms = []string{"general", "people-ops", "engineering", "leadership-private"}

// runChatSeedModerationCommand is `migrate chat seed-moderation`: it adds the
// moderation history to a chat database that already holds the demo rooms.
func runChatSeedModerationCommand(ctx context.Context, store *chatstore.Store, routes chatrouting.Directory, opts chatSeedOptions, out io.Writer) error {
	if strings.TrimSpace(opts.Tenant) == "" {
		return fmt.Errorf("-%s is required by chat seed-moderation", fieldTenant)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	_, err := seedChatModeration(chatSeedWriteLeaseContext(ctx, routes, opts.Tenant, opts.Now), store, opts.Tenant, opts.Now, out)
	return err
}

// moderationReportID is the identifier of one seeded report: derived from the
// tenant and the scenario's key, so a fresh database always gets the same one.
func moderationReportID(tenant, key string, extra ...string) string {
	name := tenant + "\x00" + key
	for _, part := range extra {
		name += "\x00" + part
	}
	return uuid.NewSHA1(chatSeedModerationNamespace, []byte(name)).String()
}

// seedModerationReportAuthority admits the records service's report path. The
// question "may this person report this message" is asked of the chat store
// before the report is filed (CanReportMessage); the records service only wants
// the evidence reference. It is a seeder-local decision and never reachable from
// a request path.
type seedModerationReportAuthority struct{}

func (seedModerationReportAuthority) Authorize(_ context.Context, _, _, _, target string) (string, error) {
	return "seed:chat-moderation:" + target, nil
}

// seedModerationFilterAuthority lets the workspace administrator manage filters
// everywhere, which is what the administrator's role holds by default.
type seedModerationFilterAuthority struct{ admin string }

func (a seedModerationFilterAuthority) AuthorizeFilters(_ context.Context, actor chatfilter.Actor, _ string) error {
	if actor.Tenant == "" || actor.Subject != a.admin {
		return chatfilter.ErrDenied
	}
	return nil
}

func (seedModerationFilterAuthority) CanReadFilterConversation(context.Context, chatfilter.Actor, string) bool {
	return true
}

type moderationSeeder struct {
	ctx        context.Context
	store      *chatstore.Store
	adapter    *chatstore.Adapter
	classifier *application.LocalDevPersonaPostClassifier
	moderated  *chatstore.ModeratedAdapter
	filters    *chatstore.FilterStore
	records    *chatrecordstore.Store
	tenant     string
	now        time.Time
	office     *time.Location
	latest     time.Time
	admin      string
	people     []string
	rooms      map[string]roomSpec
	convs      map[string]string
	reports    map[string]chatrecords.Report
	receipt    chatSeedModerationReceipt
}

func (s *moderationSeeder) created() { s.receipt.Created++ }
func (s *moderationSeeder) present() { s.receipt.Present++ }

// seedChatModeration adds the moderation history to tenant. ctx must carry the
// write lease the chat seed's own writes carry (chatSeedWriteLeaseContext).
func seedChatModeration(ctx context.Context, store *chatstore.Store, tenant string, now time.Time, out io.Writer) (chatSeedModerationReceipt, error) {
	classifier, err := application.NewLocalDevPersonaPostClassifier(store, true)
	if err != nil {
		return chatSeedModerationReceipt{}, err
	}
	return seedChatModerationWithClassifier(ctx, store, tenant, now, classifier, out)
}

func seedChatModerationWithClassifier(ctx context.Context, store *chatstore.Store, tenant string, now time.Time, classifier *application.LocalDevPersonaPostClassifier, out io.Writer) (chatSeedModerationReceipt, error) {
	s, err := newModerationSeeder(ctx, store, tenant, now, classifier)
	if err != nil {
		return chatSeedModerationReceipt{}, err
	}
	if err = s.ensureFilters(); err != nil {
		return s.receipt, fmt.Errorf("filters: %w", err)
	}
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"harassment report", s.openHarassment},
		{"spam reports", s.openSpam},
		{"held by a channel filter", s.openHeld},
		{"noted by a workspace filter", s.openNotified},
		{"private channel report", s.openPrivate},
		{"dismissed report", s.resolvedDismissed},
		{"removal", s.resolvedRemoved},
		{"removal restored after an appeal", s.resolvedRestored},
		{"released filter hit", s.resolvedReleased},
	} {
		if err = step.run(); err != nil {
			return s.receipt, fmt.Errorf("%s: %w", step.name, err)
		}
	}
	fmt.Fprintf(out, "seeded the moderation history for %s: %d added, %d already there\n", tenant, s.receipt.Created, s.receipt.Present)
	return s.receipt, nil
}

// newModerationSeeder resolves the tenant's people and rooms and what the
// history has already recorded. It fails, before writing anything, when the
// demo rooms are not there.
func newModerationSeeder(ctx context.Context, store *chatstore.Store, tenant string, now time.Time, classifier *application.LocalDevPersonaPostClassifier) (*moderationSeeder, error) {
	if classifier == nil {
		return nil, fmt.Errorf("chat seed-moderation: local-demo classifier unavailable")
	}
	employees, err := chatSeedWorkforce(tenant)
	if err != nil {
		return nil, err
	}
	people := make([]string, 0, len(employees))
	for _, e := range employees {
		people = append(people, e.Row.WorkerKey)
	}
	admin := adminIndexForTenant(tenant, people)
	if admin < 0 {
		return nil, fmt.Errorf("chat seed-moderation: no administrator persona in %s", tenant)
	}
	adapter := chatstore.NewAdapter(store)
	s := &moderationSeeder{
		ctx: ctx, store: store, adapter: adapter, classifier: classifier, moderated: chatstore.NewModeratedAdapter(adapter),
		filters: chatstore.NewFilterStore(store), records: chatrecordstore.New(store),
		tenant: tenant, now: now.UTC(), office: chatSeedOfficeZone(tenant), admin: people[admin], people: people,
		rooms: map[string]roomSpec{}, convs: map[string]string{}, reports: map[string]chatrecords.Report{},
	}
	s.latest = latestOfficeMoment(s.now, s.office)
	// The role projection the live server makes from the person's current
	// roles: the demo administrator holds the workspace administrator's
	// moderation role, everybody else only what their membership gives.
	s.moderated.CurrentRoles = func(_ context.Context, p chatcore.Principal) ([]string, error) {
		if p.SubjectID == s.admin {
			return []string{chatcore.WorkspaceAdministratorModerationRole}, nil
		}
		return nil, nil
	}
	for _, room := range chatSeedRoomsForTenant("full", tenant, people) {
		s.rooms[room.Key] = room
	}
	for _, key := range chatSeedModerationRooms {
		id := chatSeedConversationID(tenant, key)
		found, existsErr := store.ConversationExists(ctx, tenant, id)
		if existsErr != nil {
			return nil, existsErr
		}
		if !found {
			return nil, fmt.Errorf("chat seed-moderation: room %q is missing in %s; run the chat seed first", key, tenant)
		}
		s.convs[key] = id
	}
	existing, err := s.records.Reports(ctx, tenant)
	if err != nil {
		return nil, err
	}
	for _, r := range existing {
		s.reports[r.ReportID] = r
	}
	return s, nil
}

// as is the context of one person acting: a signed-in human of the tenant, the
// way the transport layer presents one, so the store derives their moderation
// roles the way it does for a request.
func (s *moderationSeeder) as(subject string) (context.Context, chatcore.Principal, error) {
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(s.tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "chat-seed-moderation", IssuedAt: s.now.Add(-time.Minute), ExpiresAt: s.now.Add(time.Hour), CredentialDigest: "in-process",
	})
	if err != nil {
		return nil, chatcore.Principal{}, err
	}
	return trust.WithPrincipal(s.ctx, principal), chatcore.Principal{TenantID: s.tenant, SubjectID: subject}, nil
}

// member names one ordinary member of a room: neither its owner nor the
// administrator, so no seeded author can already moderate the room.
func (s *moderationSeeder) member(room string, slot int) string {
	return chatSeedModerationMember(s.rooms[room], s.people, s.admin, slot)
}

// latestOfficeMoment is the most recent moment at or before now that is inside
// office hours: now itself during the day, closing time after it, and the
// previous evening's closing time before it. Nothing in the history is dated
// later, so a run at night never leaves a report in the middle of one.
func latestOfficeMoment(now time.Time, office *time.Location) time.Time {
	local := now.In(office)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, office)
	since := local.Sub(midnight)
	moment := now.UTC()
	switch {
	case since >= chatSeedWorkdayEnd:
		moment = midnight.Add(chatSeedWorkdayEnd).UTC()
	case since < chatSeedWorkdayStart:
		moment = time.Date(local.Year(), local.Month(), local.Day()-1, 0, 0, 0, 0, office).Add(chatSeedWorkdayEnd).UTC()
	}
	// A weekend has no office hours: the latest moment is Friday's closing time.
	for chatSeedIsWeekend(moment, office) {
		day := moment.In(office)
		moment = time.Date(day.Year(), day.Month(), day.Day()-1, 0, 0, 0, 0, office).Add(chatSeedWorkdayEnd).UTC()
	}
	return moment
}

// workday is a moment of the office day, some days before the run, never later
// than the latest office moment of the run.
func (s *moderationSeeder) workday(daysAgo, hour, minute int) time.Time {
	local := s.now.In(s.office)
	at := time.Date(local.Year(), local.Month(), local.Day()-daysAgo, hour, minute, 0, 0, s.office)
	for at.After(s.latest) || chatSeedIsWeekend(at, s.office) {
		at = at.AddDate(0, 0, -1)
	}
	return at.UTC()
}

// after is a later moment of the same story: the given delay after at, carried
// to the next morning when it falls after closing, and never later than the
// latest office moment of the run.
func (s *moderationSeeder) after(at time.Time, delay time.Duration) time.Time {
	later := chatSeedReplyTime(at.Add(delay), s.office)
	if later.After(s.latest) {
		later = s.latest
	}
	if !later.After(at) {
		later = at.Add(time.Minute)
	}
	if later.After(s.latest) {
		later = s.latest
	}
	return later
}

// send writes one scenario's message into its room. A room that already has a
// newer message gets this one just after it.
func (s *moderationSeeder) send(sc moderationScenario) (chatcore.Post, error) {
	conversation := s.convs[sc.Room]
	author := s.member(sc.Room, sc.Slot)
	ctx, admin, err := s.as(s.admin)
	if err != nil {
		return chatcore.Post{}, err
	}
	at := s.workday(sc.DaysAgo, sc.Hour, sc.Minute)
	newest, err := s.adapter.ListPosts(ctx, admin, s.tenant, conversation, 0, chatcore.Page{PageSize: 1}, chatcore.PostWindow{Descending: true})
	if err != nil {
		return chatcore.Post{}, err
	}
	if len(newest.Posts) > 0 && !at.After(newest.Posts[0].CreatedAt) {
		at = newest.Posts[0].CreatedAt.Add(time.Minute)
	}
	if at.After(s.latest) {
		at = s.latest
	}
	post, err := s.adapter.SendPost(s.ctx,
		chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: s.tenant, SubjectID: author}, TenantID: s.tenant, ConversationID: conversation, IdempotencyKey: "seed-moderation:" + sc.Key},
		chatcore.Post{AuthorID: author, AuthorHomeTenantID: s.tenant, Body: sc.Body, CreatedAt: at})
	if err != nil {
		return chatcore.Post{}, fmt.Errorf("send %s: %w", sc.Key, err)
	}
	if s.classifier == nil {
		return chatcore.Post{}, fmt.Errorf("classify %s: local-demo classifier unavailable", sc.Key)
	}
	source := chatSeedModerationPersonaPostSource{tenant: s.tenant, people: s.people, admin: s.admin, scenario: sc, id: post.ID}
	if err = s.classifier.ClassifyKnownSyntheticPost(s.ctx, post, source); err != nil {
		return chatcore.Post{}, fmt.Errorf("classify %s: %w", sc.Key, err)
	}
	return post, nil
}

// report files one member's report of a message, as the report command does:
// the member must be allowed to report it, and the reason is the code from the
// short list with the member's note after it.
func (s *moderationSeeder) report(key string, post chatcore.Post, reporter, code, note string, at time.Time) (string, error) {
	id := moderationReportID(s.tenant, key)
	if existing, ok := s.reports[id]; ok && existing.TargetID != post.ID {
		// A report of a message a reset wiped: this scenario's message is a new one.
		id = moderationReportID(s.tenant, key, post.ID)
	}
	if _, ok := s.reports[id]; ok {
		s.present()
		return id, nil
	}
	ctx, principal, err := s.as(reporter)
	if err != nil {
		return "", err
	}
	if err = s.moderated.CanReportMessage(ctx, principal, s.tenant, post.ConversationID, post.ID); err != nil {
		return "", fmt.Errorf("report %s: %w", key, err)
	}
	reason := code
	if note != "" {
		reason += " — " + note
	}
	service := &chatrecords.Service{Repo: s.records, Auth: seedModerationReportAuthority{}, Clock: func() time.Time { return at }}
	report := chatrecords.Report{TenantID: s.tenant, ReportID: id, ConversationID: post.ConversationID, TargetID: post.ID, ReporterID: reporter, Reason: reason, CreatedAt: at}
	if err = service.Report(ctx, reporter, report); err != nil {
		return "", fmt.Errorf("report %s: %w", key, err)
	}
	report.State = "OPEN"
	s.reports[id] = report
	s.created()
	return id, nil
}

// moderation is the moderation service the page uses, reading the clock this
// decision is dated by.
func (s *moderationSeeder) moderation(at time.Time) *chatcore.ModerationService {
	return &chatcore.ModerationService{Store: s.moderated, Filters: chatstore.NewFilterModeration(s.moderated), Clock: func() time.Time { return at }}
}

// resolve records the administrator's decision on one queue item. An item that
// is already decided answers with a conflict; that is the idempotent case.
func (s *moderationSeeder) resolve(id, action, reason string, at time.Time) error {
	ctx, admin, err := s.as(s.admin)
	if err != nil {
		return err
	}
	err = s.moderation(at).Resolve(ctx, admin, s.tenant, id, action, reason)
	if errors.Is(err, chatcore.ErrConflict) {
		s.present()
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, id, err)
	}
	s.created()
	return nil
}

// remove takes a message down as the administrator, with a reason from the list
// and a note, through the same preview and apply pair the removal dialog uses.
func (s *moderationSeeder) remove(post chatcore.Post, code, note string, at time.Time) error {
	ctx, admin, err := s.as(s.admin)
	if err != nil {
		return err
	}
	service := s.moderation(at)
	request := chatcore.RemovalRequest{Principal: admin, TenantID: s.tenant, Selection: chatcore.RemovalSelection{ConversationID: post.ConversationID, PostIDs: []string{post.ID}}, ReasonCode: code, Note: note, Action: "remove"}
	preview, err := service.Preview(ctx, request)
	if err != nil {
		return fmt.Errorf("preview removal: %w", err)
	}
	request.Confirmation, request.ConfirmedCount = preview.Confirmation, preview.Count
	if _, err = service.Apply(ctx, request); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	s.created()
	return nil
}

// notices reads the notices the author of a message has received about it.
func (s *moderationSeeder) notices(author string, post chatcore.Post) ([]chatcore.ModerationNotice, error) {
	ctx, principal, err := s.as(author)
	if err != nil {
		return nil, err
	}
	all, err := s.moderated.ModerationNotices(ctx, principal, s.tenant)
	if err != nil {
		return nil, err
	}
	var out []chatcore.ModerationNotice
	for _, n := range all {
		if n.PostID == post.ID {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *moderationSeeder) openHarassment() error {
	post, err := s.send(modHarassment)
	if err != nil {
		return err
	}
	_, err = s.report(modHarassment.Key, post, s.member("people-ops", 2), "harassment", "This was aimed at one colleague in front of the whole channel, and it is not the first time.", s.after(post.CreatedAt, 95*time.Minute))
	return err
}

func (s *moderationSeeder) openSpam() error {
	post, err := s.send(modSpam)
	if err != nil {
		return err
	}
	first := s.after(post.CreatedAt, 30*time.Minute)
	if _, err = s.report(modSpam.Key+":1", post, s.member("general", 3), "spam", "", first); err != nil {
		return err
	}
	_, err = s.report(modSpam.Key+":2", post, s.member("general", 4), "spam", "Looks like a scam link, please take it down.", s.after(first, 90*time.Minute))
	return err
}

func (s *moderationSeeder) openPrivate() error {
	post, err := s.send(modPrivate)
	if err != nil {
		return err
	}
	_, err = s.report(modPrivate.Key, post, s.member("leadership-private", 2), "sensitive_information", "Individual pay figures should not be shared outside HR.", s.after(post.CreatedAt, 2*time.Hour))
	return err
}

func (s *moderationSeeder) resolvedDismissed() error {
	post, err := s.send(modDismissed)
	if err != nil {
		return err
	}
	filed := s.after(post.CreatedAt, time.Hour)
	id, err := s.report(modDismissed.Key, post, s.member("people-ops", 4), "harassment", "It felt like a dig at the team that wrote it.", filed)
	if err != nil {
		return err
	}
	return s.resolve("report:"+id, "dismiss", "Blunt feedback about a document, not directed at a person. No action needed.", s.after(filed, 20*time.Hour))
}

func (s *moderationSeeder) resolvedRemoved() error {
	post, err := s.send(modRemoved)
	if err != nil {
		return err
	}
	told, err := s.notices(post.AuthorID, post)
	if err != nil {
		return err
	}
	for _, n := range told {
		if n.Outcome == "remove" {
			s.present()
			return nil
		}
	}
	return s.remove(post, "sensitive_information", "A personal phone number was shared in a public channel.", s.after(post.CreatedAt, 3*time.Hour))
}

// resolvedRestored takes a message down, has its author ask for a review, and
// restores it: the removal and the restoring are both recorded with their
// reasons. Each stage is skipped when the record already has it.
func (s *moderationSeeder) resolvedRestored() error {
	post, err := s.send(modRestored)
	if err != nil {
		return err
	}
	removedAt := s.after(post.CreatedAt, 4*time.Hour)
	told, err := s.notices(post.AuthorID, post)
	if err != nil {
		return err
	}
	removed := false
	for _, n := range told {
		removed = removed || n.Outcome == "remove"
	}
	if !removed {
		if err = s.remove(post, "policy_violation", "Reported as hostile toward a colleague.", removedAt); err != nil {
			return err
		}
		if told, err = s.notices(post.AuthorID, post); err != nil {
			return err
		}
	} else {
		s.present()
	}
	appealedAt := s.after(removedAt, 3*time.Hour)
	for _, n := range told {
		if n.Outcome != "remove" || !n.CanAppeal {
			continue
		}
		ctx, author, authorErr := s.as(post.AuthorID)
		if authorErr != nil {
			return authorErr
		}
		if err = s.moderated.AppealRemoval(ctx, author, s.tenant, post.ConversationID, post.ID, appealedAt); err != nil {
			return fmt.Errorf("appeal: %w", err)
		}
		s.created()
		break
	}
	// The appeal is the open item about this message; once it is decided the
	// queue has none, which is how a repeated run knows it is done.
	ctx, admin, err := s.as(s.admin)
	if err != nil {
		return err
	}
	open, err := s.moderation(appealedAt).Queue(ctx, admin, s.tenant, "")
	if err != nil {
		return err
	}
	for _, item := range open {
		if item.Kind == "appeal" && item.PostID == post.ID {
			return s.resolve(item.ID, "restore", "On review this was frustration about the schedule and was not aimed at a person. Restored.", s.after(appealedAt, 20*time.Hour))
		}
	}
	return nil
}

// ensureFilters adds the two custom filters and switches on the word list that
// holds a message, each only when it is not already there.
func (s *moderationSeeder) ensureFilters() error {
	service := func(at time.Time) *chatfilter.Service {
		return &chatfilter.Service{Store: s.filters, Registry: chatfilter.NewRegistry(), Authority: seedModerationFilterAuthority{admin: s.admin}, Delivery: s.filters, Now: func() time.Time { return at }}
	}
	actor := chatfilter.Actor{Tenant: s.tenant, Subject: s.admin}
	defined, err := s.filters.Definitions(s.ctx, s.tenant)
	if err != nil {
		return err
	}
	has := map[string]bool{}
	for _, d := range defined {
		has[d.ID] = true
	}
	enabled, err := s.filters.Enablements(s.ctx, s.tenant)
	if err != nil {
		return err
	}
	on := func(rule, channel string) bool {
		for _, e := range enabled {
			if e.RuleID == rule && e.Channel == channel && e.Enabled {
				return true
			}
		}
		return false
	}
	at := s.workday(12, 9, 0)
	svc := service(at)
	// The workspace filter tells the managers of #general when a client's code
	// name is mentioned; the channel filter flags offer details in #people-ops.
	filters := []chatfilter.Definition{
		{ID: chatSeedModerationNotifyRule, Name: "Client code names", Version: chatSeedModerationFilterEdit, Language: "en", Kind: "words", Action: "notify", Target: "#general", Match: []string{"project falcon", "project osprey"}},
		{ID: chatSeedModerationChannelRule, Name: "Offer details", Version: chatSeedModerationFilterEdit, Language: "en", Kind: "words", Action: "flag", Channels: []string{s.convs["people-ops"]}, Match: []string{"offer letter", "sign-on bonus"}},
	}
	for _, d := range filters {
		if has[d.ID] {
			s.present()
			continue
		}
		if err = svc.CreateVersion(s.ctx, actor, d); err != nil {
			return fmt.Errorf("create %s: %w", d.ID, err)
		}
		s.created()
	}
	for _, e := range []chatfilter.Enablement{
		{RuleID: chatSeedModerationNotifyRule, Channel: "", Enabled: true},
		{RuleID: chatSeedModerationChannelRule, Channel: s.convs["people-ops"], Enabled: true},
		{RuleID: chatSeedModerationBuiltinList, Channel: s.convs["engineering"], Enabled: true, Action: "flag"},
	} {
		if on(e.RuleID, e.Channel) {
			s.present()
			continue
		}
		if err = svc.Enable(s.ctx, actor, e, false); err != nil {
			return fmt.Errorf("enable %s: %w", e.RuleID, err)
		}
		s.created()
	}
	return nil
}

// hit records what the filters make of a message the way a send does: the rules
// switched on in its channel judge it, a hit is stored (with only a digest and
// the mask, never the text), and a "notify" hit is delivered. It returns the
// stored hit of the rule, so a decision can name it.
func (s *moderationSeeder) hit(post chatcore.Post, ruleID string) (chatfilter.Record, error) {
	at := post.CreatedAt.Add(3 * time.Second)
	find := func() (chatfilter.Record, bool, error) {
		hits, err := s.filters.Hits(s.ctx, s.tenant)
		if err != nil {
			return chatfilter.Record{}, false, err
		}
		for _, r := range hits {
			if r.Channel == post.ConversationID && r.Subject == post.AuthorID && r.Hit.RuleID == ruleID && r.At.Equal(at) {
				return r, true, nil
			}
		}
		return chatfilter.Record{}, false, nil
	}
	if record, ok, err := find(); err != nil || ok {
		if ok {
			s.present()
		}
		return record, err
	}
	service := &chatfilter.Service{Store: s.filters, Registry: chatfilter.NewRegistry(), Authority: seedModerationFilterAuthority{admin: s.admin}, Delivery: s.filters, Now: func() time.Time { return at }}
	if _, err := service.Evaluate(s.ctx, chatfilter.Input{Tenant: s.tenant, Channel: post.ConversationID, Subject: post.AuthorID, Language: "en", Body: post.Body}, true); err != nil {
		return chatfilter.Record{}, fmt.Errorf("judge %s: %w", ruleID, err)
	}
	record, ok, err := find()
	if err != nil {
		return record, err
	}
	if !ok {
		return record, fmt.Errorf("the filters did not record a hit of %s for the message", ruleID)
	}
	s.created()
	return record, nil
}

func (s *moderationSeeder) openHeld() error {
	post, err := s.send(modHeld)
	if err != nil {
		return err
	}
	_, err = s.hit(post, chatSeedModerationBuiltinList)
	return err
}

func (s *moderationSeeder) openNotified() error {
	post, err := s.send(modNotified)
	if err != nil {
		return err
	}
	_, err = s.hit(post, chatSeedModerationNotifyRule)
	return err
}

func (s *moderationSeeder) resolvedReleased() error {
	post, err := s.send(modReleased)
	if err != nil {
		return err
	}
	record, err := s.hit(post, chatSeedModerationBuiltinList)
	if err != nil {
		return err
	}
	return s.resolve(fmt.Sprintf("filter:%d", record.ID), "dismiss", "A casual remark that was not aimed at anyone. Released.", s.after(record.At, 20*time.Hour))
}
