package main

// The document demo seeder. It lays down a HarborCare document library the
// way people would have built it over ten months: documents written by
// about forty people, shared with colleagues as viewers or commenters,
// revised, commented on, and organized by the signed-in persona into
// folders and stars. Every write goes through documenthubstore's own APIs,
// so grants, deployments, search terms and links are exactly what the
// product would have produced; only the timestamps are moved into the past
// afterwards, because the store stamps everything with now().
//
// The seed is idempotent and resumable: a document whose owner and first
// title already exist is reused, never created twice, and folders, filing
// and stars converge on the plan.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// defaultDocumentSeedTenant is the demo tenant the dev cell serves.
const defaultDocumentSeedTenant = "harborcare-demo"

// Plan sizes for the signed-in persona.
const (
	seedViewerOwned        = 45
	seedViewerPrivate      = 15
	seedSharedWithViewer   = 110
	seedSharedPeopleDomain = 75
	seedViewerStars        = 12
	seedViewerFiled        = 70
	// seedRestrictedLinks is how many documents keep one deliberately
	// restricted link.
	seedRestrictedLinks = 2
	// seedAuthors is how many people other than the viewer own documents.
	seedAuthors = 39
)

// seedViewerFolders is the persona's folder set and how many documents each
// holds, in creation order. The archive's count is the remainder of
// seedViewerFiled.
var seedViewerFolders = []struct {
	Name, Kind string
	Count      int
}{
	{"Policies", kindPolicy, 15},
	{"Onboarding", kindOnboarding, 10},
	{"Runbooks", kindRunbook, 10},
	{"Comp cycle 2026", kindComp, 12},
	{"Meeting notes", kindNotes, 15},
	{"Archive", "", 0},
}

type seedPerson struct{ Key, Name, Title, Unit string }

type seedShare struct{ Recipient, Role string }

type seedComment struct {
	Author, Body string
	At           time.Time
}

type seedDocPlan struct {
	Spec     seedSpec
	Title    string
	Owner    seedPerson
	Created  time.Time
	Revised  time.Time
	Shares   []seedShare
	Links    []int
	Folder   string
	Star     bool
	Comments []seedComment
	// Restricted marks the documents that deliberately link to a target
	// part of their audience cannot read.
	Restricted bool
	seed       uint64
}

type documentSeedPlan struct {
	Viewer seedPerson
	People []seedPerson
	Docs   []seedDocPlan
}

type documentSeedReceipt struct {
	Created, Reused, Owners            int
	ViewerOwned, ViewerPrivate         int
	SharedWithViewer, ViewerAsViewer   int
	Shares, Revisions, Comments, Links int
	Folders, Filed, Stars              int
	Relinked, RelinkVersions, Widened  int
}

var seedCommentBodies = []string{
	"Can we add an example for part-time staff here?",
	"This matches what we agreed in the leadership meeting. Thanks for writing it up.",
	"Is the effective date still correct after the latest change?",
	"Legal reviewed this section; no changes needed.",
	"Could we link the related FAQ so managers can find it?",
	"Night shift will need a version of step 3 that works after hours.",
	"We used this last week and it worked. One step was missing the vendor phone number.",
	"Suggest we mention the state exceptions explicitly.",
	"Looks good to me. Approving from the People Ops side.",
	"Please add who to contact on weekends.",
}

// ownedInDomain marks kinds only their own function writes: nobody outside
// People Operations owns a leave policy or the comp committee's notes.
var ownedInDomain = map[string]bool{kindPolicy: true, kindComp: true, kindNotes: true, kindFAQ: true, kindClinical: true, kindShiftSwap: true}

var relatedKinds = map[string][]string{
	kindPolicy:     {kindPolicy, kindFAQ, kindGuide},
	kindFAQ:        {kindPolicy, kindFAQ},
	kindOnboarding: {kindOnboarding, kindGuide, kindPolicy},
	kindRunbook:    {kindRunbook, kindIncident, kindEngineer},
	kindIncident:   {kindRunbook, kindIncident},
	kindNotes:      {kindNotes, kindComp, kindPolicy},
	kindComp:       {kindComp, kindGuide},
	kindShiftSwap:  {kindShiftSwap, kindClinical, kindPolicy},
	kindClinical:   {kindClinical, kindShiftSwap},
	kindGuide:      {kindGuide, kindPolicy, kindOnboarding},
	kindEngineer:   {kindEngineer, kindRunbook},
	kindOps:        {kindOps, kindPolicy},
}

// loadSeedPeople reads the tenant's active persona workers from the core
// database, one row per worker key.
func loadSeedPeople(ctx context.Context, db dbport.Beginner, tenant string) ([]seedPerson, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin persona read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := pgstore.TenantID(tenant)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (worker_key) worker_key, COALESCE(legal_name,''), COALESCE(job_title,''), COALESCE(org_unit,'')
		FROM journey_worker WHERE tenant_id=$1 AND lower(lifecycle_status)='active'
		ORDER BY worker_key, revision_sequence DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("read persona workers: %w", err)
	}
	defer rows.Close()
	var out []seedPerson
	for rows.Next() {
		var p seedPerson
		if err := rows.Scan(&p.Key, &p.Name, &p.Title, &p.Unit); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// planDocumentSeed decides every document, owner, audience, revision,
// comment, link, folder and star. It is deterministic for a given people
// list and clock.
func planDocumentSeed(people []seedPerson, now time.Time) (documentSeedPlan, error) {
	viewerIndex := -1
	for i, p := range people {
		if strings.HasPrefix(p.Key, chatSeedAdminPrefix) {
			viewerIndex = i
		}
	}
	if viewerIndex < 0 {
		return documentSeedPlan{}, fmt.Errorf("document seed: no %s* persona in journey_worker; run the workforce seed first", chatSeedAdminPrefix)
	}
	if len(people) < 12 {
		return documentSeedPlan{}, fmt.Errorf("document seed: need at least 12 persona workers, found %d", len(people))
	}
	viewer := people[viewerIndex]
	others := make([]seedPerson, 0, len(people)-1)
	for i, p := range people {
		if i != viewerIndex {
			others = append(others, p)
		}
	}
	r := rand.New(rand.NewPCG(20260923, 50))
	specs := seedCatalog()
	docs := make([]seedDocPlan, len(specs))

	// Ages: dated meeting series are spread evenly, everything else is
	// scattered over the last ten months.
	seriesSeen := map[string]int{}
	seriesCount := map[string]int{}
	for _, s := range specs {
		if s.Dated {
			seriesCount[s.Topic]++
		}
	}
	for i, s := range specs {
		var age time.Duration
		if s.Dated {
			n := seriesSeen[s.Topic]
			seriesSeen[s.Topic]++
			step := 290.0 / float64(seriesCount[s.Topic])
			days := (float64(n)+0.5)*step + float64(r.IntN(5)-2)
			age = time.Duration(days*24) * time.Hour
		} else {
			age = time.Duration(2*24+r.IntN(298*24)) * time.Hour
		}
		created := now.Add(-age).Add(-time.Duration(r.IntN(3600)) * time.Second).Truncate(time.Second)
		if s.Dated {
			created = time.Date(created.Year(), created.Month(), created.Day(), 15+r.IntN(3), r.IntN(60), 0, 0, time.UTC)
			// Meetings happen on weekdays.
			switch created.Weekday() {
			case time.Saturday:
				created = created.AddDate(0, 0, -1)
			case time.Sunday:
				created = created.AddDate(0, 0, 1)
			}
		}
		title := s.Title
		if s.Dated {
			title = s.Title + ": " + created.Format("Jan 2, 2006")
		}
		docs[i] = seedDocPlan{Spec: s, Title: title, Created: created, seed: r.Uint64()}
	}

	// Owners: about forty authors, always including the viewer's own team,
	// write everything; the rest of the company only reads.
	authors := make([]seedPerson, 0, seedAuthors)
	var rest []seedPerson
	for _, p := range others {
		if containsString(domainUnits[domainPeople], p.Unit) {
			authors = append(authors, p)
		} else {
			rest = append(rest, p)
		}
	}
	r.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	authors = append(authors, rest[:max(0, min(len(rest), seedAuthors-len(authors)))]...)
	byDomain := func(domain string) []seedPerson {
		var out []seedPerson
		for _, p := range authors {
			for _, unit := range domainUnits[domain] {
				if p.Unit == unit {
					out = append(out, p)
				}
			}
		}
		if len(out) == 0 {
			return authors
		}
		return out
	}
	var peopleDomain []int
	for i, d := range docs {
		if d.Spec.Domain == domainPeople {
			peopleDomain = append(peopleDomain, i)
		}
	}
	r.Shuffle(len(peopleDomain), func(i, j int) { peopleDomain[i], peopleDomain[j] = peopleDomain[j], peopleDomain[i] })
	viewerOwned := map[int]bool{}
	for _, i := range peopleDomain[:min(seedViewerOwned, len(peopleDomain))] {
		viewerOwned[i] = true
		docs[i].Owner = viewer
	}
	for i := range docs {
		if viewerOwned[i] {
			continue
		}
		pool := byDomain(docs[i].Spec.Domain)
		if !ownedInDomain[docs[i].Spec.Kind] && r.Float64() < 0.35 {
			pool = authors
		}
		docs[i].Owner = pick(r, pool)
	}

	// Audiences.
	var viewerDocs []int
	for i := range docs {
		if viewerOwned[i] {
			viewerDocs = append(viewerDocs, i)
		}
	}
	r.Shuffle(len(viewerDocs), func(i, j int) { viewerDocs[i], viewerDocs[j] = viewerDocs[j], viewerDocs[i] })
	for n, i := range viewerDocs {
		if n < seedViewerPrivate {
			continue
		}
		docs[i].Shares = seedAudience(r, others, "", 1+r.IntN(6), 0.7)
	}
	var otherPeople, otherRest []int
	for i := range docs {
		if viewerOwned[i] {
			continue
		}
		if docs[i].Spec.Domain == domainPeople {
			otherPeople = append(otherPeople, i)
		} else {
			otherRest = append(otherRest, i)
		}
	}
	r.Shuffle(len(otherPeople), func(i, j int) { otherPeople[i], otherPeople[j] = otherPeople[j], otherPeople[i] })
	r.Shuffle(len(otherRest), func(i, j int) { otherRest[i], otherRest[j] = otherRest[j], otherRest[i] })
	takePeople := min(seedSharedPeopleDomain, len(otherPeople))
	takeRest := min(seedSharedWithViewer-takePeople, len(otherRest))
	withViewer := map[int]bool{}
	for _, i := range append(append([]int(nil), otherPeople[:takePeople]...), otherRest[:takeRest]...) {
		withViewer[i] = true
		role := documenthubstore.RoleCommenter
		if r.IntN(2) == 0 {
			role = documenthubstore.RoleViewer
		}
		docs[i].Shares = append([]seedShare{{Recipient: viewer.Key, Role: role}}, seedAudience(r, others, docs[i].Owner.Key, r.IntN(4), 0.5)...)
	}
	for i := range docs {
		if viewerOwned[i] || withViewer[i] || r.Float64() < 0.4 {
			continue
		}
		docs[i].Shares = seedAudience(r, others, docs[i].Owner.Key, 1+r.IntN(5), 0.5)
	}

	// Revisions and comments.
	for i := range docs {
		d := &docs[i]
		if r.Float64() < 0.25 {
			revised := d.Created.Add(time.Duration(24+r.IntN(24*24)) * time.Hour)
			if revised.Before(now.Add(-36 * time.Hour)) {
				d.Revised = revised
			}
		}
		var commenters []string
		for _, s := range d.Shares {
			if s.Role == documenthubstore.RoleCommenter {
				commenters = append(commenters, s.Recipient)
			}
		}
		if len(commenters) == 0 || r.Float64() > 0.2 {
			continue
		}
		at := d.Created
		for c := 0; c < 1+r.IntN(3); c++ {
			author := d.Owner.Key
			if c%2 == 0 {
				author = pick(r, commenters)
			}
			at = at.Add(time.Duration(2+r.IntN(72)) * time.Hour)
			if at.After(now) {
				break
			}
			d.Comments = append(d.Comments, seedComment{Author: author, Body: pick(r, seedCommentBodies), At: at})
		}
	}

	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Created.Before(docs[j].Created) })

	// Links point at earlier related documents that everyone who can read
	// the linking document can also read: the target's owner and readers
	// must include the source's whole audience. The draws below mirror the
	// original link selection call for call, so the rest of the plan
	// (folders, stars) stays identical for an already-seeded database; the
	// choice itself uses its own source.
	lr := rand.New(rand.NewPCG(20260924, 7))
	for i := range docs {
		if r.Float64() > 0.6 {
			continue
		}
		var legacy, candidates []int
		for j := 0; j < i; j++ {
			if !containsString(relatedKinds[docs[i].Spec.Kind], docs[j].Spec.Kind) {
				continue
			}
			if docs[j].Owner.Key == docs[i].Owner.Key || len(docs[j].Shares) > 0 {
				legacy = append(legacy, j)
			}
			if audienceCovered(docs[i], docs[j]) {
				candidates = append(candidates, j)
			}
		}
		n := 1 + r.IntN(3)
		_ = pickN(r, legacy, n)
		docs[i].Links = pickN(lr, candidates, n)
		if len(docs[i].Links) < n {
			// Owners share a related document of theirs with the people they
			// link it for. Only already-shared targets widen, and never to
			// the viewer, so private drafts and the viewer's library stay put.
			var widen []int
			for j := 0; j < i; j++ {
				if docs[j].Owner.Key == docs[i].Owner.Key && len(docs[j].Shares) > 0 && !audienceCovered(docs[i], docs[j]) &&
					containsString(relatedKinds[docs[i].Spec.Kind], docs[j].Spec.Kind) && !containsString(missingReaders(docs[i], docs[j]), viewer.Key) {
					widen = append(widen, j)
				}
			}
			for _, j := range pickN(lr, widen, n-len(docs[i].Links)) {
				for _, key := range missingReaders(docs[i], docs[j]) {
					docs[j].Shares = append(docs[j].Shares, seedShare{Recipient: key, Role: documenthubstore.RoleViewer})
				}
				docs[i].Links = append(docs[i].Links, j)
			}
		}
		sort.Ints(docs[i].Links)
	}
	// Widening a document's audience can leave links it already made
	// pointing at targets its new readers cannot open: widen those targets
	// the same way, or drop the link, until nothing changes.
	for changed := true; changed; {
		changed = false
		for i := range docs {
			kept := docs[i].Links[:0]
			for _, j := range docs[i].Links {
				missing := missingReaders(docs[i], docs[j])
				switch {
				case len(missing) == 0:
					kept = append(kept, j)
				case docs[j].Owner.Key == docs[i].Owner.Key && len(docs[j].Shares) > 0 && !containsString(missing, viewer.Key):
					for _, key := range missing {
						docs[j].Shares = append(docs[j].Shares, seedShare{Recipient: key, Role: documenthubstore.RoleViewer})
					}
					kept = append(kept, j)
					changed = true
				default:
					changed = true
				}
			}
			docs[i].Links = kept
		}
	}
	// A couple of shared documents also link to their owner's private
	// draft, as real documents do; readers see those links as restricted.
	restricted := 0
	for i := range docs {
		if restricted == seedRestrictedLinks {
			break
		}
		if docs[i].Owner.Key == viewer.Key || !sharesWith(docs[i].Shares, viewer.Key) || len(docs[i].Links) == 0 {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if docs[j].Owner.Key == docs[i].Owner.Key && len(docs[j].Shares) == 0 {
				docs[i].Links = append(docs[i].Links, j)
				docs[i].Restricted = true
				restricted++
				break
			}
		}
	}

	// The viewer's folders and stars, over everything the viewer can read.
	var readable []int
	for i := range docs {
		if docs[i].Owner.Key == viewer.Key || sharesWith(docs[i].Shares, viewer.Key) {
			readable = append(readable, i)
		}
	}
	r.Shuffle(len(readable), func(i, j int) { readable[i], readable[j] = readable[j], readable[i] })
	total := 0
	for _, folder := range seedViewerFolders {
		filed := 0
		want := folder.Count
		if folder.Kind == "" {
			// The archive takes up whatever the kind folders could not fill.
			want = seedViewerFiled - total
		}
		for _, i := range readable {
			if filed == want {
				break
			}
			if docs[i].Folder != "" {
				continue
			}
			archived := folder.Kind == "" && now.Sub(docs[i].Created) > 200*24*time.Hour
			if archived || (folder.Kind != "" && docs[i].Spec.Kind == folder.Kind) {
				docs[i].Folder = folder.Name
				filed++
			}
		}
		total += filed
	}
	for _, i := range readable[:min(seedViewerStars, len(readable))] {
		docs[i].Star = true
	}
	return documentSeedPlan{Viewer: viewer, People: people, Docs: docs}, nil
}

// audienceCovered reports whether everyone who can read source (its owner
// and recipients) can also read target.
func audienceCovered(source, target seedDocPlan) bool {
	readers := map[string]bool{target.Owner.Key: true}
	for _, sh := range target.Shares {
		readers[sh.Recipient] = true
	}
	if !readers[source.Owner.Key] {
		return false
	}
	for _, sh := range source.Shares {
		if !readers[sh.Recipient] {
			return false
		}
	}
	return true
}

// missingReaders lists the people who read source but cannot read target.
func missingReaders(source, target seedDocPlan) []string {
	readers := map[string]bool{target.Owner.Key: true}
	for _, sh := range target.Shares {
		readers[sh.Recipient] = true
	}
	var out []string
	for _, key := range append([]string{source.Owner.Key}, recipientKeys(source.Shares)...) {
		if !readers[key] {
			out = append(out, key)
		}
	}
	return out
}

func recipientKeys(shares []seedShare) []string {
	out := make([]string, 0, len(shares))
	for _, sh := range shares {
		out = append(out, sh.Recipient)
	}
	return out
}

func seedAudience(r *rand.Rand, people []seedPerson, exclude string, n int, commenterShare float64) []seedShare {
	var out []seedShare
	for _, p := range pickN(r, people, n+1) {
		if p.Key == exclude || len(out) == n {
			continue
		}
		role := documenthubstore.RoleViewer
		if r.Float64() < commenterShare {
			role = documenthubstore.RoleCommenter
		}
		out = append(out, seedShare{Recipient: p.Key, Role: role})
	}
	return out
}

// seedTeam is the people a document of one domain names as attendees,
// action owners and contacts: that function's people, or everyone when the
// function is too small to fill a meeting.
func seedTeam(people []seedPerson, domain string) []seedPerson {
	var team []seedPerson
	for _, p := range people {
		if containsString(domainUnits[domain], p.Unit) {
			team = append(team, p)
		}
	}
	if len(team) < 4 {
		return people
	}
	return team
}

func sharesWith(shares []seedShare, key string) bool {
	for _, s := range shares {
		if s.Recipient == key {
			return true
		}
	}
	return false
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// documentSeedOptions configures one seed run. MaxDocuments trims the plan
// for tests; zero means the whole plan.
type documentSeedOptions struct {
	Tenant       string
	Now          time.Time
	MaxDocuments int
	// Rich adds the diagram and table documents even to a trimmed plan;
	// the full plan always includes them.
	Rich bool
	// Demo adds the showcase documents (document_seed_demo.go) even to a
	// trimmed plan; the full plan always includes them.
	Demo bool
	// Chat, when set, lets the showcase link seeded chat rooms and post
	// messages that link back to the documents.
	Chat *chatstore.Store
	// Routes is the core route directory the chat seeder registers each
	// seeded room in (chatroutestore.New in main.go). The showcase chat
	// posts write into those same rooms and need the same route write
	// lease the chat seeder's own writes use (chatSeedWriteLeaseContext in
	// chat_seed.go), or chatstore's routeFence refuses them with
	// ErrNoRouteLease now that a live tenant's rooms carry a non-empty
	// route_shard. Nil when Chat is nil or chat routing is not wired; such
	// rooms stay unrouted and the showcase posts write unleased, as before.
	Routes chatrouting.Directory
	// MediaRoot is where showcase asset bytes are written.
	MediaRoot string
}

// runDocumentSeedCommand executes the plan through the document store.
func runDocumentSeedCommand(ctx context.Context, store *documenthubstore.Store, people []seedPerson, opts documentSeedOptions, out io.Writer) error {
	if store == nil {
		return errors.New("document seed: document store is required")
	}
	if strings.TrimSpace(opts.Tenant) == "" {
		opts.Tenant = defaultDocumentSeedTenant
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	plan, err := planDocumentSeed(people, opts.Now)
	if err != nil {
		return err
	}
	if opts.MaxDocuments > 0 && opts.MaxDocuments < len(plan.Docs) {
		// Links only point at earlier documents, so a prefix stays whole.
		plan.Docs = plan.Docs[:opts.MaxDocuments]
	}
	existing, err := existingSeedDocuments(ctx, store, opts.Tenant)
	if err != nil {
		return err
	}
	byKey := map[string]seedPerson{}
	for _, p := range people {
		byKey[p.Key] = p
	}
	ids := make([]string, len(plan.Docs))
	reused := make([]bool, len(plan.Docs))
	var receipt documentSeedReceipt
	owners := map[string]bool{}
	for i, d := range plan.Docs {
		owners[d.Owner.Key] = true
		if id, ok := existing[d.Owner.Key+"\x00"+d.Title]; ok {
			ids[i] = id
			reused[i] = true
			receipt.Reused++
			continue
		}
		var links []seedLink
		for _, j := range d.Links {
			links = append(links, seedLink{Title: plan.Docs[j].Title, ID: ids[j]})
		}
		input := seedBodyInput{Spec: d.Spec, Title: d.Title, Owner: d.Owner, People: seedTeam(plan.People, d.Spec.Domain), Links: links, Written: d.Created, Rand: rand.New(rand.NewPCG(d.seed, 1))}
		id, first, err := store.CreatePersonalDocument(ctx, opts.Tenant, d.Owner.Key, d.Title, documentSeedBody(input))
		if err != nil {
			return fmt.Errorf("create %q: %w", d.Title, err)
		}
		ids[i] = id
		for _, share := range d.Shares {
			if _, ok := byKey[share.Recipient]; !ok {
				continue
			}
			if err := store.SharePersonalDocumentRole(ctx, opts.Tenant, id, d.Owner.Key, share.Recipient, share.Role); err != nil {
				return fmt.Errorf("share %q with %s: %w", d.Title, share.Recipient, err)
			}
			receipt.Shares++
		}
		var comments []string
		for _, c := range d.Comments {
			comment, err := store.AddComment(ctx, opts.Tenant, documenthubstore.CommentInput{DocumentID: id, VersionID: first.ID, AuthorID: c.Author, Body: c.Body})
			if err != nil {
				return fmt.Errorf("comment on %q: %w", d.Title, err)
			}
			comments = append(comments, comment.ID)
			receipt.Comments++
		}
		revisionID := ""
		if !d.Revised.IsZero() {
			input.IsRevision, input.Written = true, d.Revised
			input.RevisionNote = pick(input.Rand, revisionNotes)
			revision, err := store.CreatePersonalDocumentVersion(ctx, opts.Tenant, id, d.Owner.Key, first.ID, d.Title, documentSeedBody(input))
			if err != nil {
				return fmt.Errorf("revise %q: %w", d.Title, err)
			}
			revisionID = revision.ID
			receipt.Revisions++
		}
		if err := backdateSeedDocument(ctx, store, opts.Tenant, id, first.ID, revisionID, d, comments); err != nil {
			return fmt.Errorf("backdate %q: %w", d.Title, err)
		}
		receipt.Links += len(links)
		receipt.Created++
	}
	for _, d := range plan.Docs {
		switch {
		case d.Owner.Key == plan.Viewer.Key:
			receipt.ViewerOwned++
			if len(d.Shares) == 0 {
				receipt.ViewerPrivate++
			}
		case sharesWith(d.Shares, plan.Viewer.Key):
			receipt.SharedWithViewer++
			for _, s := range d.Shares {
				if s.Recipient == plan.Viewer.Key && s.Role == documenthubstore.RoleViewer {
					receipt.ViewerAsViewer++
				}
			}
		}
	}
	receipt.Owners = len(owners)
	if receipt.Relinked, receipt.RelinkVersions, receipt.Widened, err = relinkSeedDocuments(ctx, store, opts.Tenant, plan, ids, reused); err != nil {
		return err
	}
	linksTotal, linksCovered := seedLinkCoverage(plan)
	if err := organizeSeedLibrary(ctx, store, opts.Tenant, plan, ids, &receipt); err != nil {
		return err
	}
	if opts.MaxDocuments == 0 || opts.Rich {
		if err := seedRichDocuments(ctx, store, plan, opts.Now, opts.Tenant, existing, out); err != nil {
			return err
		}
	}
	if opts.MaxDocuments == 0 || opts.Demo {
		if err := seedDemoDocuments(ctx, store, opts.Chat, plan, opts, existing, out); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "document seed %s: %d documents (%d created, %d already present) by %d owners; %s owns %d (%d private), %d shared with %s (%d as viewer); %d shares, %d revisions, %d comments, %d links; %d folders, %d filed, %d starred; links readable by the whole audience %d of %d; %d documents relinked (%d versions), %d shares added\n",
		opts.Tenant, len(plan.Docs), receipt.Created, receipt.Reused, receipt.Owners, plan.Viewer.Key, receipt.ViewerOwned, receipt.ViewerPrivate,
		receipt.SharedWithViewer, plan.Viewer.Key, receipt.ViewerAsViewer, receipt.Shares, receipt.Revisions, receipt.Comments, receipt.Links,
		receipt.Folders, receipt.Filed, receipt.Stars, linksCovered, linksTotal, receipt.Relinked, receipt.RelinkVersions, receipt.Widened)
	return nil
}

// existingSeedDocuments maps owner and first-version title to document ID,
// so a rerun reuses what an earlier run created.
func existingSeedDocuments(ctx context.Context, store *documenthubstore.Store, tenant string) (map[string]string, error) {
	out := map[string]string{}
	err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id, d.owner_id, v.title FROM document d
			JOIN LATERAL (SELECT x.title FROM document_version x WHERE x.tenant_id=d.tenant_id AND x.document_id=d.id ORDER BY x.created_at, x.id LIMIT 1) v ON true
			WHERE d.tenant_id=$1`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, owner, title string
			if err := rows.Scan(&id, &owner, &title); err != nil {
				return err
			}
			out[owner+"\x00"+title] = id
		}
		return rows.Err()
	})
	return out, err
}

// backdateSeedDocument moves one freshly created document's timestamps into
// the past. The store stamps every row with now() and version, deployment
// and comment rows are append-only, so this is the one place the seed
// writes SQL directly: inside a transaction that suspends triggers
// (session_replication_role, which needs a superuser dev connection) and
// changes nothing but timestamps.
func backdateSeedDocument(ctx context.Context, store *documenthubstore.Store, tenant, docID, firstID, revisionID string, d seedDocPlan, comments []string) error {
	return store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return fmt.Errorf("suspend triggers (the seed needs a superuser document connection): %w", err)
		}
		statements := []struct {
			sql  string
			args []any
		}{
			{`UPDATE document SET created_at=$3 WHERE tenant_id=$1 AND id=$2`, []any{tenant, docID, d.Created}},
			{`UPDATE document_version SET created_at=$3 WHERE tenant_id=$1 AND id=$2`, []any{tenant, firstID, d.Created}},
			{`UPDATE document_deployment SET created_at=$3, effective_at=$3 WHERE tenant_id=$1 AND document_id=$2`, []any{tenant, docID, d.Created.Add(10 * time.Minute)}},
			{`UPDATE document_active_pointer SET updated_at=$3 WHERE tenant_id=$1 AND document_id=$2`, []any{tenant, docID, d.Created.Add(10 * time.Minute)}},
			{`UPDATE document_grant SET created_at=$3 WHERE tenant_id=$1 AND document_id=$2`, []any{tenant, docID, d.Created.Add(10 * time.Minute)}},
		}
		if revisionID != "" {
			statements = append(statements, struct {
				sql  string
				args []any
			}{`UPDATE document_version SET created_at=$3 WHERE tenant_id=$1 AND id=$2`, []any{tenant, revisionID, d.Revised}})
		}
		for i, id := range comments {
			statements = append(statements, struct {
				sql  string
				args []any
			}{`UPDATE document_comment SET created_at=$3 WHERE tenant_id=$1 AND id=$2`, []any{tenant, id, d.Comments[i].At}})
		}
		for _, st := range statements {
			if _, err := tx.Exec(ctx, st.sql, st.args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// organizeSeedLibrary converges the viewer's folders, filing and stars.
func organizeSeedLibrary(ctx context.Context, store *documenthubstore.Store, tenant string, plan documentSeedPlan, ids []string, receipt *documentSeedReceipt) error {
	lib, err := store.GetLibrary(ctx, tenant, plan.Viewer.Key)
	if err != nil {
		return fmt.Errorf("read %s library: %w", plan.Viewer.Key, err)
	}
	folderIDs := map[string]string{}
	for _, f := range lib.Folders {
		folderIDs[strings.ToLower(f.Name)] = f.ID
	}
	for _, folder := range seedViewerFolders {
		var docIDs []string
		for i, d := range plan.Docs {
			if d.Folder == folder.Name {
				docIDs = append(docIDs, ids[i])
			}
		}
		id, ok := folderIDs[strings.ToLower(folder.Name)]
		if !ok {
			created, err := store.CreateFolder(ctx, tenant, plan.Viewer.Key, folder.Name)
			if err != nil {
				return fmt.Errorf("create folder %q: %w", folder.Name, err)
			}
			id = created.ID
		}
		receipt.Folders++
		if len(docIDs) == 0 {
			continue
		}
		if err := store.MoveDocuments(ctx, tenant, plan.Viewer.Key, docIDs, id); err != nil {
			return fmt.Errorf("file into %q: %w", folder.Name, err)
		}
		receipt.Filed += len(docIDs)
	}
	for i, d := range plan.Docs {
		if !d.Star {
			continue
		}
		if err := store.SetStarred(ctx, tenant, plan.Viewer.Key, ids[i], true); err != nil {
			return fmt.Errorf("star %q: %w", d.Title, err)
		}
		receipt.Stars++
	}
	return nil
}
