package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

const docsSuggestRows = 8

// docsSuggestRank orders a label for query: 0 starts with it, 1 has a word
// that starts with it, 2 contains it, -1 does not match.
func docsSuggestRank(label, query string) int {
	label, query = strings.ToLower(label), strings.ToLower(strings.TrimSpace(query))
	switch {
	case query == "" || strings.HasPrefix(label, query):
		return 0
	case strings.Contains(" "+strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(label), " "+query):
		return 1
	case strings.Contains(label, query):
		return 2
	}
	return -1
}

// docsSuggestPeople lists people for "@query". The inserted handle falls
// back to the subject ID when two people in the directory share it, the
// same rule the server resolves by.
func docsSuggestPeople(people []chatui.SearchPerson, query string) []productui.DocsReferenceSuggestion {
	handles := map[string]int{}
	for _, p := range people {
		handles[productui.DocsPersonHandle(p.Name)]++
	}
	type scored struct {
		row  productui.DocsReferenceSuggestion
		rank int
	}
	var out []scored
	seen := map[string]bool{}
	for _, p := range people {
		if p.ID == "" || strings.TrimSpace(p.Name) == "" || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		handle := productui.DocsPersonHandle(p.Name)
		rank := docsSuggestRank(p.Name, query)
		if r := docsSuggestRank(handle, query); r >= 0 && (rank < 0 || r < rank) {
			rank = r
		}
		if rank < 0 {
			continue
		}
		insert := productui.DocsSuggestPersonInsert(p.ID, p.Name, handles[handle] > 1)
		out = append(out, scored{productui.DocsReferenceSuggestion{ID: p.ID, Label: p.Name, Detail: insert, Insert: insert}, rank})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return strings.ToLower(out[i].row.Label) < strings.ToLower(out[j].row.Label)
	})
	rows := make([]productui.DocsReferenceSuggestion, 0, docsSuggestRows)
	for _, s := range out {
		if len(rows) == docsSuggestRows {
			break
		}
		rows = append(rows, s.row)
	}
	return rows
}

// docsSuggestChannels lists channels the viewer can see for "#query":
// joined rooms first, then public channels they could join.
func docsSuggestChannels(rooms []chatui.Conversation, query, members string) []productui.DocsReferenceSuggestion {
	type scored struct {
		row          productui.DocsReferenceSuggestion
		rank, joined int
	}
	var out []scored
	seen := map[string]bool{}
	for _, room := range rooms {
		if room.ID == "" || strings.TrimSpace(room.Name) == "" || seen[room.ID] || (room.Kind != chatui.PublicChannel && room.Kind != chatui.PrivateChannel) {
			continue
		}
		seen[room.ID] = true
		rank := docsSuggestRank(room.Name, query)
		if rank < 0 {
			continue
		}
		joined := 1
		if room.Joined {
			joined = 0
		}
		detail := strings.ReplaceAll(members, "{count}", strconv.Itoa(room.MemberCount))
		out = append(out, scored{productui.DocsReferenceSuggestion{ID: room.ID, Label: "#" + room.Name, Detail: detail, Insert: productui.DocsSuggestChannelInsert(room.ID, room.Name)}, rank, joined})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].joined != out[j].joined {
			return out[i].joined < out[j].joined
		}
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return strings.ToLower(out[i].row.Label) < strings.ToLower(out[j].row.Label)
	})
	rows := make([]productui.DocsReferenceSuggestion, 0, docsSuggestRows)
	for _, s := range out {
		if len(rows) == docsSuggestRows {
			break
		}
		rows = append(rows, s.row)
	}
	return rows
}
