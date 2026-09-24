package main

import (
	"net/url"
	"sort"
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatPersonOrgChartHref uses the existing organization route's tree view and
// selected-person query contract, so the chart opens rooted on this coworker.
func chatPersonOrgChartHref(workerRef string) string {
	workerRef = strings.TrimSpace(workerRef)
	if workerRef == "" {
		return ""
	}
	query := url.Values{}
	query.Set("org_view", "tree")
	query.Set("person", workerRef)
	return productui.Path(productui.PageOrganization) + "?" + query.Encode()
}

// chatSearchVisibleWorkers searches only the session's already-governed worker
// projection. Names and identifiers come from ListWorkers; callers must not
// substitute chat membership ids when that projection is unavailable.
func chatSearchVisibleWorkers(directory []chatui.SearchPerson, query string, limit int) []chatui.SearchPerson {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || limit <= 0 {
		return nil
	}
	identityQuery := strings.TrimPrefix(query, "@")
	seen := make(map[string]bool)
	out := make([]chatui.SearchPerson, 0, min(limit, len(directory)))
	for _, person := range directory {
		id, name := strings.TrimSpace(person.ID), strings.TrimSpace(person.Name)
		if id == "" || name == "" || seen[id] {
			continue
		}
		matched := strings.Contains(strings.ToLower(name), query) || (identityQuery != "" && strings.Contains(strings.ToLower(id), identityQuery))
		for _, alias := range person.Aliases {
			alias = strings.TrimSpace(alias)
			matched = matched || (identityQuery != "" && strings.Contains(strings.ToLower(alias), identityQuery))
		}
		if !matched {
			continue
		}
		seen[id] = true
		out = append(out, chatui.SearchPerson{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool {
		if strings.EqualFold(out[i].Name, out[j].Name) {
			return out[i].ID < out[j].ID
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func chatSearchDirectoryFromWorkers(workers []*journeyv1.Worker) []chatui.SearchPerson {
	names := chatDirectoryFromWorkers(workers)
	seen := make(map[string]bool)
	out := make([]chatui.SearchPerson, 0, len(workers))
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		id := chatWorkerSubject(worker)
		name := strings.TrimSpace(names[id])
		if id == "" || name == "" || seen[id] {
			continue
		}
		seen[id] = true
		var aliases []string
		for _, alias := range []string{worker.GetWorkerRef(), worker.GetWorkerId()} {
			if alias = strings.TrimSpace(alias); alias != "" && alias != id {
				aliases = append(aliases, alias)
			}
		}
		out = append(out, chatui.SearchPerson{ID: id, Name: name, Aliases: aliases})
	}
	return out
}

// chatWorkerSubject is the identity chat names a directory worker by: the
// principal subject they sign in and post as. worker_ref is a display slug
// no other service shares, so it is never used in its place.
func chatWorkerSubject(worker *journeyv1.Worker) string {
	return strings.TrimSpace(worker.GetSubjectId())
}

// chatPersonDetailsFromWorkers uses only the already governed ListWorkers
// projection. In particular, it never copies compensation, personal contact
// data, or the raw manager reference into the chat profile.
func chatPersonDetailsFromWorkers(workers []*journeyv1.Worker, subjectID string) (chatui.PersonDetails, bool) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return chatui.PersonDetails{}, false
	}
	directory := chatDirectoryFromWorkers(workers)
	var person *journeyv1.Worker
	counts := make(map[string]int, len(workers))
	byRef := make(map[string]*journeyv1.Worker, len(workers))
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		ref := strings.TrimSpace(worker.GetWorkerRef())
		if ref == "" {
			continue
		}
		counts[ref]++
		byRef[ref] = worker
		if chatWorkerSubject(worker) == subjectID {
			if person != nil {
				return chatui.PersonDetails{}, false
			}
			person = worker
		}
	}
	if person == nil {
		return chatui.PersonDetails{}, false
	}
	personRef := strings.TrimSpace(person.GetWorkerRef())
	detail := chatui.PersonDetails{
		ID: subjectID, Name: directory[subjectID],
		OrgChartHref: chatPersonOrgChartHref(personRef),
		Ready:        true,
		JobTitle:     strings.TrimSpace(person.GetJobTitle()),
		Department:   strings.TrimSpace(person.GetOrgUnit()),
		Location:     strings.TrimSpace(person.GetLocation()),
		Company:      strings.TrimSpace(person.GetCompany()),
		BusinessUnit: strings.TrimSpace(person.GetBusinessUnit()),
		PhotoURL:     strings.TrimSpace(person.GetProfilePhotoUrl()),
	}
	rel := person.GetManagerRelationship()
	if rel.GetDisposition() == journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE {
		ref := strings.TrimSpace(rel.GetManagerWorkerRef())
		if manager := byRef[ref]; ref != "" && ref != personRef && counts[ref] == 1 && directory[chatWorkerSubject(manager)] != "" {
			detail.ManagerID = chatWorkerSubject(manager)
			detail.Manager = directory[detail.ManagerID]
			detail.ManagerPhotoURL = strings.TrimSpace(byRef[ref].GetProfilePhotoUrl())
		}
	}
	for _, worker := range workers {
		if worker == nil || worker == person {
			continue
		}
		ref := strings.TrimSpace(worker.GetWorkerRef())
		subject := chatWorkerSubject(worker)
		if ref == "" || ref == personRef || counts[ref] != 1 || directory[subject] == "" {
			continue
		}
		relationship := worker.GetManagerRelationship()
		if relationship.GetDisposition() != journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE || strings.TrimSpace(relationship.GetManagerWorkerRef()) != personRef {
			continue
		}
		detail.DirectReports = append(detail.DirectReports, chatui.PersonLink{ID: subject, Name: directory[subject], PhotoURL: strings.TrimSpace(worker.GetProfilePhotoUrl())})
	}
	sort.Slice(detail.DirectReports, func(i, j int) bool {
		if strings.EqualFold(detail.DirectReports[i].Name, detail.DirectReports[j].Name) {
			return detail.DirectReports[i].ID < detail.DirectReports[j].ID
		}
		return strings.ToLower(detail.DirectReports[i].Name) < strings.ToLower(detail.DirectReports[j].Name)
	})
	return detail, true
}

// adoptChatPersonDM adds an admitted room to both the model and its visible
// direct section. Its local label comes from the governed directory; the
// server's stable DM identity is the opaque room ID, never a display name.
func adoptChatPersonDM(model *chatui.Model, room chatui.Conversation, peerID, peerName string) {
	if room.ID == "" || room.Kind != chatui.DirectMessage || peerID == "" {
		return
	}
	if peerName != "" {
		room.Name = peerName
	}
	if model.PeerIDs == nil {
		model.PeerIDs = make(map[string]string)
	}
	model.PeerIDs[room.ID] = peerID
	found := false
	for i := range model.Conversations {
		if model.Conversations[i].ID == room.ID {
			model.Conversations[i] = room
			found = true
			break
		}
	}
	if !found {
		model.Conversations = append(model.Conversations, room)
	}
	if len(model.Sections) == 0 {
		return
	}
	inSection := false
	direct := -1
	for i := range model.Sections {
		if model.Sections[i].ID == "direct" {
			direct = i
		}
		for j := range model.Sections[i].Chats {
			if model.Sections[i].Chats[j].ID == room.ID {
				model.Sections[i].Chats[j] = room
				inSection = true
			}
		}
	}
	if inSection {
		return
	}
	if direct < 0 {
		model.Sections = append(model.Sections, chatui.SidebarSection{ID: "direct", Name: "Direct messages"})
		direct = len(model.Sections) - 1
	}
	model.Sections[direct].Chats = append(model.Sections[direct].Chats, room)
}

func chatDirectoryNamedDirect(room chatui.Conversation, peerIDs, directory map[string]string) chatui.Conversation {
	if room.Kind == chatui.DirectMessage {
		if peer := peerIDs[room.ID]; peer != "" && directory[peer] != "" {
			room.Name = directory[peer]
		}
	}
	return room
}
