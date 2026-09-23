package main

type chatTimelinePageFence struct {
	generation uint64
	edge       uint64
	newer      bool
}

func (s *chatState) claimLatestPage() (string, chatTimelinePageFence, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pageLoading || !s.model.HasNewer || s.model.SelectedID == "" {
		return "", chatTimelinePageFence{}, false
	}
	fence := chatTimelinePageFence{generation: s.generation, newer: true}
	for _, message := range s.model.Messages {
		if message.Sequence > fence.edge {
			fence.edge = message.Sequence
		}
	}
	if s.newerScanSequence > fence.edge {
		fence.edge = s.newerScanSequence
	}
	s.pageLoading = true
	return s.model.SelectedID, fence, true
}

func (s *chatState) timelineFenceCurrent(fences []chatTimelinePageFence) bool {
	if len(fences) == 0 {
		return true
	}
	fence := fences[0]
	if !s.pageLoading || s.generation != fence.generation {
		return false
	}
	var edge uint64
	for _, message := range s.model.Messages {
		if fence.newer {
			if message.Sequence > edge {
				edge = message.Sequence
			}
		} else if message.Sequence > 0 && (edge == 0 || message.Sequence < edge) {
			edge = message.Sequence
		}
	}
	if fence.newer && s.newerScanSequence > edge {
		edge = s.newerScanSequence
	}
	return edge == fence.edge
}

func (s *chatState) claimTimelinePage(newer bool) (room, cursor string, fence chatTimelinePageFence, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pageLoading || s.model.SelectedID == "" || len(s.model.Messages) == 0 {
		return "", "", fence, false
	}
	if newer && !s.model.HasNewer || !newer && !s.model.HasOlder {
		return "", "", fence, false
	}
	room = s.model.SelectedID
	fence = chatTimelinePageFence{generation: s.generation, newer: newer}
	for _, message := range s.model.Messages {
		if newer {
			if message.Sequence > fence.edge {
				fence.edge = message.Sequence
			}
		} else if message.Sequence > 0 && (fence.edge == 0 || message.Sequence < fence.edge) {
			fence.edge = message.Sequence
		}
	}
	if newer && s.newerScanSequence > fence.edge {
		fence.edge = s.newerScanSequence
	}
	if fence.edge == 0 {
		return "", "", fence, false
	}
	if !newer {
		cursor = s.olderCursor
	}
	s.pageLoading = true
	return room, cursor, fence, true
}

func (s *chatState) finishTimelinePage(fence chatTimelinePageFence) {
	s.mu.Lock()
	if s.generation == fence.generation {
		s.pageLoading = false
	}
	s.mu.Unlock()
}
