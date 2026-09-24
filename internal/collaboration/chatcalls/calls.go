// Package chatcalls contains the deferred calling contracts only.
//
// Calling admission deliberately has no dependency on chat membership. A
// message conversation can provide context to a caller, but media authority
// comes from the call's participant authority and explicit consent records.
package chatcalls

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalid         = errors.New("chatcalls: invalid contract")
	ErrAuthority       = errors.New("chatcalls: participant authority is not current")
	ErrConsent         = errors.New("chatcalls: explicit participant consent is required")
	ErrRecordingPolicy = errors.New("chatcalls: recording policy is invalid")
	ErrCaptionPolicy   = errors.New("chatcalls: caption policy is invalid")
	ErrNetworkPolicy   = errors.New("chatcalls: network policy is invalid")
	ErrCapacity        = errors.New("chatcalls: server capacity is unavailable")
	ErrRouting         = errors.New("chatcalls: server routing is invalid")
	ErrCostBudget      = errors.New("chatcalls: cost budget exceeded")
)

type MediaKind string

const (
	MediaAudio MediaKind = "AUDIO"
	MediaVideo MediaKind = "VIDEO"
)

type Authority struct {
	PrincipalID string
	TenantID    string
	Revision    uint64
	Active      bool
	ExpiresAt   time.Time
}

func (a Authority) current(at time.Time) bool {
	return canonicalID(a.PrincipalID) && canonicalID(a.TenantID) && a.Revision > 0 && a.Active && (a.ExpiresAt.IsZero() || at.Before(a.ExpiresAt))
}

func canonicalID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

type Consent struct {
	CallID           string
	ParticipantID    string
	Revision         uint64
	Granted          bool
	Media            map[MediaKind]bool
	RecordingGranted bool
	GrantedAt        time.Time
	ExpiresAt        time.Time
}

func (c Consent) grants(callID, participantID string, media MediaKind, at time.Time) bool {
	return c.CallID == callID && c.ParticipantID == participantID && c.Revision > 0 && c.Granted && c.Media[media] && !c.GrantedAt.IsZero() && !c.GrantedAt.After(at) && (c.ExpiresAt.IsZero() || at.Before(c.ExpiresAt))
}

// Participant is the only source of media admission authority. In
// particular, it has no membership field: message membership is not media
// permission.
type Participant struct {
	Authority Authority
	Consent   Consent
}

type RecordingPolicy struct {
	Enabled         bool
	ConsentRequired bool
	Notice          string
}

func (p RecordingPolicy) validate() error {
	if !p.Enabled {
		return nil
	}
	if !p.ConsentRequired || strings.TrimSpace(p.Notice) == "" {
		return ErrRecordingPolicy
	}
	return nil
}

type CaptionPolicy struct {
	Enabled  bool
	Language string
	Provider string
}

func (p CaptionPolicy) validate() error {
	if !p.Enabled {
		return nil
	}
	if strings.TrimSpace(p.Language) == "" || strings.TrimSpace(p.Provider) == "" {
		return ErrCaptionPolicy
	}
	return nil
}

type MediaPolicy struct {
	Audio     bool
	Video     bool
	Recording RecordingPolicy
	Captions  CaptionPolicy
}

func (p MediaPolicy) validate() error {
	if !p.Audio && !p.Video {
		return ErrInvalid
	}
	if err := p.Recording.validate(); err != nil {
		return err
	}
	return p.Captions.validate()
}

func (p MediaPolicy) kinds() []MediaKind {
	var out []MediaKind
	if p.Audio {
		out = append(out, MediaAudio)
	}
	if p.Video {
		out = append(out, MediaVideo)
	}
	return out
}

type P2PNetworkPolicy struct {
	ICEIdentity       string
	DTLSPeerIdentity  string
	AllowedEgressZone string
}

func (p P2PNetworkPolicy) validate() error {
	if strings.TrimSpace(p.ICEIdentity) == "" || strings.TrimSpace(p.DTLSPeerIdentity) == "" || strings.TrimSpace(p.AllowedEgressZone) == "" {
		return ErrNetworkPolicy
	}
	return nil
}

type P2PCallRequest struct {
	CallID         string
	ConversationID string
	At             time.Time
	Participants   [2]Participant
	Media          MediaPolicy
	Network        P2PNetworkPolicy
}

// P2PCallAdmission is the later one-to-one contract. It carries only facts
// independently admitted for media and network use.
type P2PCallAdmission struct {
	CallID             string
	ParticipantIDs     [2]string
	Media              MediaPolicy
	Network            P2PNetworkPolicy
	AuthorityRevisions [2]uint64
	ConsentRevisions   [2]uint64
}

func AdmitP2P(req P2PCallRequest) (P2PCallAdmission, error) {
	if !canonicalID(req.CallID) || !canonicalID(req.ConversationID) || req.At.IsZero() || req.Participants[0].Authority.PrincipalID == req.Participants[1].Authority.PrincipalID {
		return P2PCallAdmission{}, ErrInvalid
	}
	if err := req.Media.validate(); err != nil {
		return P2PCallAdmission{}, err
	}
	if err := req.Network.validate(); err != nil {
		return P2PCallAdmission{}, err
	}
	for _, participant := range req.Participants {
		if !participant.Authority.current(req.At) {
			return P2PCallAdmission{}, ErrAuthority
		}
		for _, kind := range req.Media.kinds() {
			if !participant.Consent.grants(req.CallID, participant.Authority.PrincipalID, kind, req.At) {
				return P2PCallAdmission{}, ErrConsent
			}
		}
		if req.Media.Recording.Enabled && !participant.Consent.RecordingGranted {
			return P2PCallAdmission{}, ErrConsent
		}
	}
	return P2PCallAdmission{CallID: req.CallID, ParticipantIDs: [2]string{req.Participants[0].Authority.PrincipalID, req.Participants[1].Authority.PrincipalID}, Media: req.Media, Network: req.Network, AuthorityRevisions: [2]uint64{req.Participants[0].Authority.Revision, req.Participants[1].Authority.Revision}, ConsentRevisions: [2]uint64{req.Participants[0].Consent.Revision, req.Participants[1].Consent.Revision}}, nil
}

type ServerRouting struct {
	Region     string
	RelayID    string
	RouteEpoch uint64
}

func (r ServerRouting) validate() error {
	if strings.TrimSpace(r.Region) == "" || strings.TrimSpace(r.RelayID) == "" || r.RouteEpoch == 0 {
		return ErrRouting
	}
	return nil
}

type ServerCapacity struct {
	MaxParticipants      int
	ReservedParticipants int
	AudioSlots           int
	ReservedAudioSlots   int
	VideoSlots           int
	ReservedVideoSlots   int
}

func (c ServerCapacity) admits(participants int, audio, video bool) bool {
	if participants <= 0 || c.MaxParticipants < 0 || c.ReservedParticipants < 0 || c.ReservedParticipants > c.MaxParticipants ||
		c.AudioSlots < 0 || c.ReservedAudioSlots < 0 || c.ReservedAudioSlots > c.AudioSlots ||
		c.VideoSlots < 0 || c.ReservedVideoSlots < 0 || c.ReservedVideoSlots > c.VideoSlots ||
		c.MaxParticipants-c.ReservedParticipants < participants {
		return false
	}
	if audio && c.AudioSlots-c.ReservedAudioSlots < participants {
		return false
	}
	if video && c.VideoSlots-c.ReservedVideoSlots < participants {
		return false
	}
	return true
}

type CostBudget struct {
	Currency       string
	MaximumCents   int64
	EstimatedCents int64
}

func (b CostBudget) validate() error {
	if strings.TrimSpace(b.Currency) == "" || b.MaximumCents < 0 || b.EstimatedCents < 0 || b.EstimatedCents > b.MaximumCents {
		return ErrCostBudget
	}
	return nil
}

type ServerGroupCallRequest struct {
	CallID         string
	ConversationID string
	At             time.Time
	Participants   []Participant
	Media          MediaPolicy
	Routing        ServerRouting
	Capacity       ServerCapacity
	Cost           CostBudget
}

// ServerGroupCallAdmission is intentionally a separate contract from P2P.
// Server routing, capacity and cost facts are mandatory for group admission.
type ServerGroupCallAdmission struct {
	CallID             string
	ParticipantIDs     []string
	Media              MediaPolicy
	Routing            ServerRouting
	ReservedCapacity   ServerCapacity
	Cost               CostBudget
	AuthorityRevisions map[string]uint64
	ConsentRevisions   map[string]uint64
}

func AdmitServerGroup(req ServerGroupCallRequest) (ServerGroupCallAdmission, error) {
	if strings.TrimSpace(req.CallID) == "" || strings.TrimSpace(req.ConversationID) == "" || req.At.IsZero() || len(req.Participants) < 3 {
		return ServerGroupCallAdmission{}, ErrInvalid
	}
	if err := req.Media.validate(); err != nil {
		return ServerGroupCallAdmission{}, err
	}
	if err := req.Routing.validate(); err != nil {
		return ServerGroupCallAdmission{}, err
	}
	if !req.Capacity.admits(len(req.Participants), req.Media.Audio, req.Media.Video) {
		return ServerGroupCallAdmission{}, ErrCapacity
	}
	if err := req.Cost.validate(); err != nil {
		return ServerGroupCallAdmission{}, err
	}
	ids := make([]string, 0, len(req.Participants))
	authorities := make(map[string]uint64, len(req.Participants))
	consents := make(map[string]uint64, len(req.Participants))
	seen := make(map[string]bool, len(req.Participants))
	for _, participant := range req.Participants {
		id := participant.Authority.PrincipalID
		if seen[id] {
			return ServerGroupCallAdmission{}, ErrInvalid
		}
		seen[id] = true
		if !participant.Authority.current(req.At) {
			return ServerGroupCallAdmission{}, ErrAuthority
		}
		for _, kind := range req.Media.kinds() {
			if !participant.Consent.grants(req.CallID, id, kind, req.At) {
				return ServerGroupCallAdmission{}, ErrConsent
			}
		}
		if req.Media.Recording.Enabled && !participant.Consent.RecordingGranted {
			return ServerGroupCallAdmission{}, ErrConsent
		}
		ids = append(ids, id)
		authorities[id] = participant.Authority.Revision
		consents[id] = participant.Consent.Revision
	}
	return ServerGroupCallAdmission{CallID: req.CallID, ParticipantIDs: ids, Media: req.Media, Routing: req.Routing, ReservedCapacity: req.Capacity, Cost: req.Cost, AuthorityRevisions: authorities, ConsentRevisions: consents}, nil
}
