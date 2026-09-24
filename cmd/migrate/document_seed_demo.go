package main

// Showcase documents: a small, realistic People Operations set that uses
// every kind of reference the document page and chat can render, so a demo
// shows cross-links, #channel links, @mentions, chat message permalinks,
// inline images, attached PDFs, tables and Mermaid diagrams working
// together. Chat messages in the matching rooms link back to the documents
// with doc: references, so chat shows document previews too.
//
// The seed runs in three passes: the four reference documents, then the
// chat messages that link to them, then a highlights document that
// permalinks those messages (and one last chat message that links the
// highlights). Everything is idempotent: documents are found by owner and
// first title and rewritten only when their planned markdown changed;
// chat messages carry a client key, so the chat store returns the existing
// post instead of writing a second one; attachments are added to a version
// only when that version lacks them.
//
// Reference syntax follows what the document page resolves: doc:<id>
// links, [#name](channel:<id>) and [@Name](person:<subject>) chat
// references (productui/docs_chat_chips.go, application/
// document_chat_refs.go), /workspace/app/chat#share=<token> permalinks
// issued with chat's own locator codec, and attachment:<id> media
// (productui/docs_media.go). Each kind is one demoRef* helper, so a format
// change is a one-line edit and a re-run converges every document on it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// demoRefDoc links another document. doc:<id> is what the renderer and
// the related-links sections already use.
func demoRefDoc(title, id string) string { return fmt.Sprintf("[%s](doc:%s)", title, id) }

// demoRefChannel links a chat room by conversation id, or names it
// plainly (the page resolves #name for readers who can see the room) when
// the room was not seeded.
func demoRefChannel(name, conversationID string) string {
	if conversationID == "" {
		return "#" + name
	}
	return fmt.Sprintf("[#%s](channel:%s)", name, conversationID)
}

// demoRefMention names a person by subject id.
func demoRefMention(p seedPerson) string {
	return fmt.Sprintf("[@%s](person:%s)", personName(p), p.Key)
}

// demoRefMessage permalinks one chat post through chat's share locator,
// the same token CreateShareLink issues with the default codec.
func demoRefMessage(label, tenant, conversationID, postID string) string {
	token, err := chatcore.OpaqueLocator{}.Encode(tenant, conversationID, postID)
	if err != nil {
		return label
	}
	return fmt.Sprintf("[%s](/workspace/app/chat#share=%s)", label, token)
}

// demoRefImage shows an attached image (a documenthubstore.Media id)
// inline. Before the upload has an id it falls back to the alt text.
func demoRefImage(alt, mediaID string) string {
	if mediaID == "" {
		return "*" + alt + "*"
	}
	return fmt.Sprintf("![%s](attachment:%s)", alt, mediaID)
}

// demoRefFile links an attached file (a documenthubstore.Media id).
func demoRefFile(label, mediaID string) string {
	if mediaID == "" {
		return label
	}
	return fmt.Sprintf("[%s](attachment:%s)", label, mediaID)
}

// demoChatDocRef is how a chat message references a document; chat turns
// it into a link and a preview card.
func demoChatDocRef(id string) string { return "doc:" + id }

// demoAudience is who every showcase document is shared with besides its
// owner, as worker-key prefixes. One shared audience keeps every
// cross-link readable by everyone who can read the linking document.
var demoAudience = []string{chatSeedAdminPrefix, "hc-051-", "hc-052-", "hc-053-", "hc-054-"}

// demoRooms are the chat rooms the documents and messages reference.
var demoRooms = []string{"people-ops", "benefits", "onboarding", "announcements", "general"}

// demoEnv is what a document body can reference.
type demoEnv struct {
	tenant string
	person func(prefix string) seedPerson
	docs   map[string]string // doc key -> id
	titles map[string]string // doc key -> title
	rooms  map[string]string // room key -> conversation id, "" when absent
	posts  map[string]string // message key -> post id
	assets map[string]demoAsset
	// media maps asset key to the current document's attachment id.
	media    map[string]string
	fence    string
	messages []demoMessage
}

func (e demoEnv) doc(key string) string   { return demoRefDoc(e.titles[key], e.docs[key]) }
func (e demoEnv) room(key string) string  { return demoRefChannel(key, e.rooms[key]) }
func (e demoEnv) at(prefix string) string { return demoRefMention(e.person(prefix)) }
func (e demoEnv) image(key string) string { return demoRefImage(e.assets[key].Alt, e.media[key]) }
func (e demoEnv) file(label, key string) string {
	return demoRefFile(label, e.media[key])
}
func (e demoEnv) mermaid(src string) string { return e.fence + "mermaid\n" + src + e.fence + "\n" }

type demoDoc struct {
	Key, Title, OwnerPrefix, Folder string
	Star                            bool
	// AgeHours places the first version in the past.
	AgeHours int
	// Assets are attached to the version readers see.
	Assets []string
	Body   func(e demoEnv) string
}

type demoMessage struct {
	Key, Room, AuthorPrefix, Doc string
	// MentionPrefix, when set, @mentions that person with a durable
	// reference, the way the chat composer does.
	MentionPrefix string
	// AgeMinutes places the post in the past.
	AgeMinutes int
	// Label is how the highlights document names the message.
	Label string
	Text  func(e demoEnv, docRef string) string
}

// demoDocsBefore are written before the chat messages; demoDocsAfter
// permalink them.
var demoDocsBefore = []demoDoc{
	{Key: "charter", Title: "People Operations team charter", OwnerPrefix: chatSeedAdminPrefix, Star: true, AgeHours: 21 * 24, Assets: []string{"orgchart"},
		Body: func(e demoEnv) string {
			return `# People Operations team charter

**Owner:** ` + e.at(chatSeedAdminPrefix) + ` · **Team channel:** ` + e.room("people-ops") + `

People Operations looks after everyone who works at HarborCare, from the offer letter to the last paycheck. This charter says who owns what and where to ask, so a question reaches the right person the first time.

## Team

` + e.image("orgchart") + `

| Area | Lead | Where to ask |
| --- | --- | --- |
| HR business partners | ` + e.at("hc-051-") + ` | ` + e.room("people-ops") + ` |
| Talent acquisition and onboarding | ` + e.at("hc-052-") + ` | ` + e.room("onboarding") + ` |
| Benefits and leave | ` + e.at("hc-053-") + ` | ` + e.room("benefits") + ` |
| Payroll liaison | ` + e.at("hc-054-") + ` | ` + e.room("people-ops") + ` |

## How a request flows

` + e.mermaid(`flowchart LR
    Q[Question in chat or Workspace] --> T{Who owns it?}
    T -- Benefits or leave --> B[Benefits and leave]
    T -- Hiring or onboarding --> H[Talent acquisition]
    T -- Pay --> P[Payroll liaison]
    T -- Anything else --> R[HR business partner]
    B --> A[Answer within 2 business days]
    H --> A
    P --> A
    R --> A
`) + `
## Service levels

| Request | First response | Resolution |
| --- | --- | --- |
| General question | 1 business day | 3 business days |
| Leave request | 1 business day | 5 business days |
| Pay correction | Same day | Next pay run |
| Employee relations concern | Same day | Case by case |

Announcements go to ` + e.room("announcements") + `; everything else starts in ` + e.room("people-ops") + `.
`
		}},
	{Key: "leave", Title: "Parental leave handout 2027", OwnerPrefix: "hc-053-", Folder: "Policies", AgeHours: 9 * 24, Assets: []string{"leave-pdf"},
		Body: func(e demoEnv) string {
			return `# Parental leave handout 2027

**Owner:** ` + e.at("hc-053-") + ` · **Questions:** ` + e.room("benefits") + `

The printable handout is attached: ` + e.file("Parental leave handout 2027 (PDF)", "leave-pdf") + `. Hand it to anyone expecting a child, adopting or fostering. It replaces the 2026 handout.

## What you receive

| Situation | Paid weeks | Blocks allowed | Within |
| --- | --- | --- | --- |
| Birthing parent | 16 | 2 | 12 months |
| Non-birthing parent | 12 | 2 | 12 months |
| Adoption or foster placement | 12 | 2 | 12 months of placement |

State paid family leave runs at the same time; HarborCare tops it up to full base pay, and benefits continue unchanged.

## Requesting leave

` + e.mermaid(`sequenceDiagram
    participant E as Employee
    participant W as Workspace
    participant B as Benefits and leave
    participant M as Manager
    E->>W: Request leave, 30 days ahead where possible
    W->>B: Open a leave case
    B-->>E: Confirm dates and pay
    B->>M: Share the return date
`) + `
Managers with a question about coverage should talk to ` + e.at("hc-051-") + `. How the team handles requests is in the ` + e.doc("charter") + `.
`
		}},
	{Key: "onboarding", Title: "Manager onboarding checklist: first 90 days", OwnerPrefix: "hc-052-", Folder: "Onboarding", AgeHours: 6 * 24, Assets: []string{"badge"},
		Body: func(e demoEnv) string {
			return `# Manager onboarding checklist: first 90 days

**Owner:** ` + e.at("hc-052-") + ` · **Questions:** ` + e.room("onboarding") + `

Every new hire in the October cohort gets this badge on day one:

` + e.image("badge") + `

## Timeline

` + e.mermaid(`gantt
    title First 90 days
    dateFormat YYYY-MM-DD
    section Before day one
    Accounts and equipment :done, a1, 2026-09-28, 5d
    section First month
    Orientation            :active, o1, 2026-10-05, 2d
    Unit shadowing         :s1, after o1, 10d
    Day 30 check-in        :milestone, m1, 2026-11-04, 0d
    section Months two and three
    Goals agreed           :g1, 2026-11-05, 10d
    Day 90 review          :milestone, m2, 2027-01-05, 0d
`) + `
## Checklist

| When | Manager does | Done when |
| --- | --- | --- |
| Before day one | Confirms the start date and equipment | Accounts work on day one |
| Week one | Introduces the buddy and the team | Buddy named in Workspace |
| Day 30 | Holds the first check-in | Notes filed in Workspace |
| Day 90 | Reviews goals and confirms the role | Review signed |

New parents joining the cohort should also get the ` + e.doc("leave") + `. The whole team's responsibilities are in the ` + e.doc("charter") + `.
`
		}},
	{Key: "enrollment", Title: "Open enrollment 2027: questions and answers", OwnerPrefix: "hc-051-", Folder: "Policies", AgeHours: 4 * 24, Assets: []string{"enroll-pdf"},
		Body: func(e demoEnv) string {
			return `# Open enrollment 2027: questions and answers

**Owners:** ` + e.at("hc-051-") + ` and ` + e.at("hc-053-") + ` · **Announcements:** ` + e.room("announcements") + ` · **Questions:** ` + e.room("benefits") + `

Open enrollment runs from November 2 to November 20. The one-page checklist is attached: ` + e.file("Open enrollment checklist 2027 (PDF)", "enroll-pdf") + `.

` + e.mermaid(`timeline
    title Open enrollment 2027
    October 19 : Plan comparison published
    November 2 : Enrollment opens
    November 12 : Benefits fair
    November 20 : Enrollment closes
    January 1 : New elections take effect
`) + `
## Common questions

| Question | Answer |
| --- | --- |
| What happens if I do nothing? | Your medical plan continues; flexible spending elections end. |
| Can I add a newborn later? | Yes, within 30 days of the birth. See the ` + e.doc("leave") + `. |
| Does the HSA contribution change? | No, HarborCare still contributes $750 a year. |
| Who approves exceptions? | ` + e.at("hc-053-") + ` for benefits, ` + e.at("hc-054-") + ` for payroll deductions. |

Ask in ` + e.room("benefits") + ` and the team answers within a business day, as the ` + e.doc("charter") + ` promises.
`
		}},
}

var demoDocsAfter = []demoDoc{
	{Key: "highlights", Title: "People Ops chat highlights: this month", OwnerPrefix: chatSeedAdminPrefix, Star: true, AgeHours: 1,
		Body: func(e demoEnv) string {
			var b strings.Builder
			b.WriteString(`# People Ops chat highlights: this month

**Compiled by:** ` + e.at(chatSeedAdminPrefix) + `

The conversations worth keeping from ` + e.room("people-ops") + `, ` + e.room("benefits") + ` and ` + e.room("onboarding") + `, with a link back to each message so the thread is one click away.

## Messages

| Where | What | Message |
| --- | --- | --- |
`)
			for _, m := range e.messages {
				id, ok := e.posts[m.Key]
				if !ok || m.Label == "" {
					continue
				}
				fmt.Fprintf(&b, "| %s | %s | %s |\n", e.room(m.Room), m.Label, demoRefMessage("Open message", e.tenant, e.rooms[m.Room], id))
			}
			b.WriteString(`
## Documents discussed

- ` + e.doc("charter") + `
- ` + e.doc("leave") + `
- ` + e.doc("onboarding") + `
- ` + e.doc("enrollment") + `

` + e.mermaid(`pie showData
    title Questions by channel this month
    "benefits" : 41
    "people-ops" : 27
    "onboarding" : 18
    "announcements" : 6
`) + `
Follow-ups are owned by ` + e.at("hc-051-") + ` and ` + e.at("hc-053-") + `.
`)
			return b.String()
		}},
}

// demoMessagesBefore link the reference documents; demoMessagesAfter link
// the highlights.
var demoMessagesBefore = []demoMessage{
	{Key: "charter-published", Room: "people-ops", AuthorPrefix: chatSeedAdminPrefix, Doc: "charter", MentionPrefix: "hc-053-", AgeMinutes: 20 * 60, Label: "Team charter published",
		Text: func(e demoEnv, ref string) string {
			return "The team charter is published: who owns what and where to ask. " + ref + " Benefits questions go to @" + personName(e.person("hc-053-")) + "."
		}},
	{Key: "charter-general", Room: "general", AuthorPrefix: "hc-052-", Doc: "charter", AgeMinutes: 18 * 60, Label: "Where to ask People Ops",
		Text: func(_ demoEnv, ref string) string {
			return "New here, or not sure who to ask in People Ops? Start with the team charter: " + ref
		}},
	{Key: "leave-handout", Room: "benefits", AuthorPrefix: "hc-053-", Doc: "leave", AgeMinutes: 16 * 60, Label: "2027 parental leave handout",
		Text: func(_ demoEnv, ref string) string {
			return "The 2027 parental leave handout is live, with the printable PDF inside: " + ref + " It replaces the 2026 version."
		}},
	{Key: "leave-crosspost", Room: "people-ops", AuthorPrefix: "hc-053-", Doc: "leave", AgeMinutes: 15 * 60, Label: "Leave handout cross-post",
		Text: func(_ demoEnv, ref string) string {
			return "Cross-posting from #benefits so partners have it for their managers: " + ref
		}},
	{Key: "onboarding-checklist", Room: "onboarding", AuthorPrefix: "hc-052-", Doc: "onboarding", AgeMinutes: 12 * 60, Label: "First-90-days checklist",
		Text: func(_ demoEnv, ref string) string {
			return "Managers of the October cohort: here is the first-90-days checklist with the timeline. " + ref
		}},
	{Key: "enrollment-faq", Room: "announcements", AuthorPrefix: "hc-051-", Doc: "enrollment", AgeMinutes: 8 * 60, Label: "Open enrollment answers",
		Text: func(_ demoEnv, ref string) string {
			return "Open enrollment runs November 2 to 20. Answers to the most common questions, plus a one-page checklist: " + ref
		}},
	{Key: "enrollment-benefits", Room: "benefits", AuthorPrefix: "hc-051-", Doc: "enrollment", AgeMinutes: 7 * 60, Label: "Enrollment questions thread",
		Text: func(_ demoEnv, ref string) string {
			return "Please ask enrollment questions here and we will add the answers to " + ref
		}},
}

var demoMessagesAfter = []demoMessage{
	{Key: "highlights", Room: "people-ops", AuthorPrefix: chatSeedAdminPrefix, Doc: "highlights", AgeMinutes: 45,
		Text: func(_ demoEnv, ref string) string {
			return "This month's highlights, with links back to each thread: " + ref
		}},
}

type demoSeedReceipt struct {
	Created, Updated, Current, Pending int
	Attachments, Posts, Filed, Stars   int
}

// seedDemoDocuments lays down the showcase set. chat may be nil, in which
// case channels are named without links and no messages are written.
func seedDemoDocuments(ctx context.Context, store *documenthubstore.Store, chat *chatstore.Store, plan documentSeedPlan, opts documentSeedOptions, existing map[string]string, out io.Writer) error {
	byPrefix := func(prefix string) seedPerson {
		for _, p := range plan.People {
			if strings.HasPrefix(p.Key, prefix) {
				return p
			}
		}
		return plan.Viewer
	}
	env := demoEnv{tenant: opts.Tenant, person: byPrefix, docs: map[string]string{}, titles: map[string]string{}, rooms: map[string]string{}, posts: map[string]string{}, assets: demoAssets(), fence: "```"}
	env.messages = append(append([]demoMessage(nil), demoMessagesBefore...), demoMessagesAfter...)
	for _, d := range append(append([]demoDoc(nil), demoDocsBefore...), demoDocsAfter...) {
		env.titles[d.Key] = d.Title
	}
	root := opts.MediaRoot
	if root == "" {
		root = defaultDocumentMediaRoot
	}
	blobs, err := documenthubstore.NewMediaFiles(root)
	if err != nil {
		return fmt.Errorf("document media root: %w", err)
	}
	for _, room := range demoRooms {
		env.rooms[room] = ""
		if chat == nil {
			continue
		}
		id := chatSeedConversationID(opts.Tenant, room)
		found, err := chat.ConversationExists(ctx, opts.Tenant, id)
		if err != nil {
			return fmt.Errorf("find chat room %s: %w", room, err)
		}
		if found {
			env.rooms[room] = id
		}
	}
	var receipt demoSeedReceipt
	ensureAll := func(docs []demoDoc) error {
		for _, d := range docs {
			id, err := ensureDemoDocument(ctx, store, blobs, opts, byPrefix, env, d, existing, &receipt)
			if err != nil {
				return err
			}
			env.docs[d.Key] = id
		}
		return nil
	}
	if err := ensureAll(demoDocsBefore); err != nil {
		return err
	}
	if err := postDemoMessages(ctx, chat, opts, plan, byPrefix, env, demoMessagesBefore, &receipt); err != nil {
		return err
	}
	if err := ensureAll(demoDocsAfter); err != nil {
		return err
	}
	if err := postDemoMessages(ctx, chat, opts, plan, byPrefix, env, demoMessagesAfter, &receipt); err != nil {
		return err
	}
	if err := organizeDemoDocuments(ctx, store, opts.Tenant, plan.Viewer.Key, env, &receipt); err != nil {
		return err
	}
	docs := len(demoDocsBefore) + len(demoDocsAfter)
	fmt.Fprintf(out, "document seed %s showcase documents: %d (%d created, %d updated, %d already current, %d left with a pending draft); %d attachments added, %d chat posts linked, %d filed, %d starred\n",
		opts.Tenant, docs, receipt.Created, receipt.Updated, receipt.Current, receipt.Pending, receipt.Attachments, receipt.Posts, receipt.Filed, receipt.Stars)
	return nil
}

// ensureDemoDocument creates one showcase document, or converges an
// existing one on its planned markdown, and returns its id. Attachments are
// per document and their ids are only known after upload, so a new
// document with attachments is written, given its files, and then revised
// to reference them before anyone else can see it.
func ensureDemoDocument(ctx context.Context, store *documenthubstore.Store, blobs documenthubstore.MediaBlobs, opts documentSeedOptions, byPrefix func(string) seedPerson, env demoEnv, d demoDoc, existing map[string]string, receipt *demoSeedReceipt) (string, error) {
	owner := byPrefix(d.OwnerPrefix)
	sp := seedDocPlan{Title: d.Title, Owner: owner}
	for _, prefix := range demoAudience {
		if p := byPrefix(prefix); p.Key != owner.Key && !sharesWith(sp.Shares, p.Key) {
			sp.Shares = append(sp.Shares, seedShare{Recipient: p.Key, Role: documenthubstore.RoleCommenter})
		}
	}
	env.media = map[string]string{}
	id, ok := existing[owner.Key+"\x00"+d.Title]
	if !ok {
		newID, first, err := store.CreatePersonalDocument(ctx, opts.Tenant, owner.Key, d.Title, documenthubstore.NormalizeMarkdown(d.Body(env)))
		if err != nil {
			return "", fmt.Errorf("create %q: %w", d.Title, err)
		}
		if err := addDemoMedia(ctx, store, blobs, opts.Tenant, newID, owner.Key, env, d.Assets, receipt); err != nil {
			return "", err
		}
		sp.Created = opts.Now.Add(-time.Duration(d.AgeHours) * time.Hour).Truncate(time.Second)
		revisionID := ""
		if len(d.Assets) > 0 {
			v, err := store.CreatePersonalDocumentVersion(ctx, opts.Tenant, newID, owner.Key, first.ID, d.Title, documenthubstore.NormalizeMarkdown(d.Body(env)))
			if err != nil {
				return "", fmt.Errorf("reference attachments in %q: %w", d.Title, err)
			}
			revisionID, sp.Revised = v.ID, sp.Created.Add(5*time.Minute)
		}
		for _, sh := range sp.Shares {
			if err := store.SharePersonalDocumentRole(ctx, opts.Tenant, newID, owner.Key, sh.Recipient, sh.Role); err != nil {
				return "", fmt.Errorf("share %q with %s: %w", d.Title, sh.Recipient, err)
			}
		}
		if err := backdateSeedDocument(ctx, store, opts.Tenant, newID, first.ID, revisionID, sp, nil); err != nil {
			return "", fmt.Errorf("backdate %q: %w", d.Title, err)
		}
		existing[owner.Key+"\x00"+d.Title] = newID
		receipt.Created++
		return newID, nil
	}
	if _, err := convergeSeedShares(ctx, store, opts.Tenant, id, sp); err != nil {
		return "", err
	}
	if err := addDemoMedia(ctx, store, blobs, opts.Tenant, id, owner.Key, env, d.Assets, receipt); err != nil {
		return "", err
	}
	markdown := documenthubstore.NormalizeMarkdown(d.Body(env))
	_, latest, err := store.ReadPersonalDocument(ctx, opts.Tenant, owner.Key, id)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", d.Title, err)
	}
	if sameMarkdown(markdown, latest.Markdown) {
		receipt.Current++
		return id, nil
	}
	live, err := store.ResolveDeployment(ctx, opts.Tenant, id, "default", "")
	deployed := err == nil
	if err != nil && !errors.Is(err, documenthubstore.ErrNoDeployment) {
		return "", fmt.Errorf("resolve %q: %w", d.Title, err)
	}
	if deployed && live.VersionID != latest.ID {
		// Someone has an unpublished draft on top of the live version;
		// leave their work alone.
		receipt.Pending++
		return id, nil
	}
	v, err := store.CreatePersonalDocumentVersion(ctx, opts.Tenant, id, owner.Key, latest.ID, latest.Title, markdown)
	if err != nil {
		return "", fmt.Errorf("update %q: %w", d.Title, err)
	}
	if deployed {
		if err := publishSeedVersion(ctx, store, opts.Tenant, sp, id, v.ID, live.VersionID); err != nil {
			return "", fmt.Errorf("publish %q: %w", d.Title, err)
		}
	}
	receipt.Updated++
	return id, nil
}

// addDemoMedia uploads each asset to the document through the store's
// attachment path and records its id in env.media. The store hands back
// the existing attachment for bytes the document already has, so only new
// rows are counted.
func addDemoMedia(ctx context.Context, store *documenthubstore.Store, blobs documenthubstore.MediaBlobs, tenant, docID, owner string, env demoEnv, keys []string, receipt *demoSeedReceipt) error {
	for _, key := range keys {
		a := env.assets[key]
		had := false
		if list, err := store.ListMedia(ctx, tenant, owner, docID); err == nil {
			for _, m := range list {
				if m.Filename == a.Filename && m.SizeBytes == int64(len(a.Content)) {
					had = true
				}
			}
		}
		m, err := store.AddMedia(ctx, blobs, tenant, owner, docID, a.Filename, a.Content)
		if err != nil {
			return fmt.Errorf("attach %s: %w", a.Filename, err)
		}
		if !had {
			receipt.Attachments++
		}
		env.media[key] = m.ID
	}
	return nil
}

// postDemoMessages writes each message into its room when the room exists.
// The client key names the document id, so the same document never gets a
// second post, and a re-created document gets a fresh one.
func postDemoMessages(ctx context.Context, chat *chatstore.Store, opts documentSeedOptions, plan documentSeedPlan, byPrefix func(string) seedPerson, env demoEnv, messages []demoMessage, receipt *demoSeedReceipt) error {
	if chat == nil {
		return nil
	}
	adapter := chatstore.NewAdapter(chat)
	for _, m := range messages {
		conversation := env.rooms[m.Room]
		docID := env.docs[m.Doc]
		if conversation == "" || docID == "" {
			continue
		}
		body := m.Text(env, demoChatDocRef(docID))
		var refs []chatcore.Reference
		if m.MentionPrefix != "" {
			p := byPrefix(m.MentionPrefix)
			refs = append(refs, chatcore.Reference{Kind: chatcore.PersonMention, TenantID: opts.Tenant, ID: p.Key, Display: personName(p)})
		}
		send := func(author string) (chatcore.Post, error) {
			return adapter.SendPost(ctx,
				chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: opts.Tenant, SubjectID: author}, TenantID: opts.Tenant, ConversationID: conversation, IdempotencyKey: "docseed:" + m.Key + ":" + docID},
				chatcore.Post{AuthorID: author, AuthorHomeTenantID: opts.Tenant, Body: body, References: refs, CreatedAt: opts.Now.Add(-time.Duration(m.AgeMinutes) * time.Minute).Truncate(time.Second)})
		}
		post, err := send(byPrefix(m.AuthorPrefix).Key)
		if errors.Is(err, chatstore.ErrNotMember) {
			// The persona is in every seeded room; post as them when the
			// planned author is not a member of this one.
			post, err = send(plan.Viewer.Key)
		}
		if err != nil {
			return fmt.Errorf("post %s in %s: %w", m.Key, m.Room, err)
		}
		env.posts[m.Key] = post.ID
		receipt.Posts++
	}
	return nil
}

// organizeDemoDocuments files the showcase documents into the viewer's
// existing folders and stars the ones the plan stars.
func organizeDemoDocuments(ctx context.Context, store *documenthubstore.Store, tenant, viewer string, env demoEnv, receipt *demoSeedReceipt) error {
	lib, err := store.GetLibrary(ctx, tenant, viewer)
	if err != nil {
		return err
	}
	folders := map[string]string{}
	for _, f := range lib.Folders {
		folders[f.Name] = f.ID
	}
	for _, d := range append(append([]demoDoc(nil), demoDocsBefore...), demoDocsAfter...) {
		id := env.docs[d.Key]
		if id == "" {
			continue
		}
		if folder, ok := folders[d.Folder]; ok && d.Folder != "" {
			if err := store.MoveDocuments(ctx, tenant, viewer, []string{id}, folder); err != nil {
				return fmt.Errorf("file %q: %w", d.Title, err)
			}
			receipt.Filed++
		}
		if d.Star {
			if err := store.SetStarred(ctx, tenant, viewer, id, true); err != nil {
				return fmt.Errorf("star %q: %w", d.Title, err)
			}
			receipt.Stars++
		}
	}
	return nil
}
