package main

import (
	"errors"
	"math"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type voiceRecorder interface {
	Start() error
	Pause() error
	Resume() error
	Stop() error
	Cancel()
}
type voiceRecordingState string

const (
	voiceIdle         voiceRecordingState = "idle"
	voiceExplained    voiceRecordingState = "explained"
	voiceRequesting   voiceRecordingState = "requesting"
	voiceRecording    voiceRecordingState = "recording"
	voicePaused       voiceRecordingState = "paused"
	voicePreview      voiceRecordingState = "preview"
	voiceDenied       voiceRecordingState = "denied"
	voiceNoMicrophone voiceRecordingState = "no_microphone"
	voiceFailed       voiceRecordingState = "failed"
)

var errVoiceRecorderState = errors.New("voice recorder: invalid transition")

type voiceRecordingSession struct {
	Recorder  voiceRecorder
	State     voiceRecordingState
	ElapsedMS int64
	Level     float64
	Waveform  []float64
}

func (s *voiceRecordingSession) Explain() {
	if s.State == "" || s.State == voiceIdle {
		s.State = voiceExplained
	}
}
func (s *voiceRecordingSession) Start() error {
	if s.State != voiceExplained && s.State != voicePreview && s.State != voiceDenied && s.State != voiceNoMicrophone && s.State != voiceFailed {
		return errVoiceRecorderState
	}
	if s.Recorder == nil {
		s.State = voiceNoMicrophone
		return chat.ErrVoiceUnavailable
	}
	if err := s.Recorder.Start(); err != nil {
		s.State = voiceFailed
		return err
	}
	s.State = voiceRequesting
	s.ElapsedMS = 0
	s.Level = 0
	s.Waveform = nil
	return nil
}
func (s *voiceRecordingSession) Ready() error {
	if s.State != voiceRequesting {
		return errVoiceRecorderState
	}
	s.State = voiceRecording
	return nil
}
func (s *voiceRecordingSession) Pause() error {
	if s.State == voiceRecording {
		if err := s.Recorder.Pause(); err != nil {
			return err
		}
		s.State = voicePaused
		return nil
	}
	if s.State == voicePaused {
		if err := s.Recorder.Resume(); err != nil {
			return err
		}
		s.State = voiceRecording
		return nil
	}
	return errVoiceRecorderState
}
func (s *voiceRecordingSession) Tick(deltaMS int64, level float64) error {
	if s.State != voiceRecording {
		return nil
	}
	if deltaMS < 0 || math.IsNaN(level) || math.IsInf(level, 0) || level < 0 || level > 1 {
		return errVoiceRecorderState
	}
	if deltaMS >= chat.VoiceMaxDurationMS-s.ElapsedMS {
		s.ElapsedMS = chat.VoiceMaxDurationMS
	} else {
		s.ElapsedMS += deltaMS
	}
	s.Level = level
	if len(s.Waveform) < 128 {
		s.Waveform = append(s.Waveform, level)
	} else {
		copy(s.Waveform, s.Waveform[1:])
		s.Waveform[len(s.Waveform)-1] = level
	}
	if s.ElapsedMS >= chat.VoiceMaxDurationMS {
		s.ElapsedMS = chat.VoiceMaxDurationMS
		return s.Stop()
	}
	return nil
}
func (s *voiceRecordingSession) Stop() error {
	if s.State != voiceRecording && s.State != voicePaused {
		return errVoiceRecorderState
	}
	if err := s.Recorder.Stop(); err != nil {
		s.State = voiceFailed
		return err
	}
	s.State = voicePreview
	return nil
}
func (s *voiceRecordingSession) Cancel() {
	if s.Recorder != nil {
		s.Recorder.Cancel()
	}
	s.State = voiceIdle
	s.ElapsedMS = 0
	s.Level = 0
	s.Waveform = nil
}
func (s *voiceRecordingSession) RemainingMS() int64 { return chat.VoiceMaxDurationMS - s.ElapsedMS }

// Playback state belongs to the view owner, so virtualized rows can unmount
// without resetting the person's position or allowing a second active player.
type voicePlayback struct {
	Positions map[string]float64
	Active    string
	Speed     float64
	Collapsed bool
}

func newVoicePlayback() *voicePlayback {
	return &voicePlayback{Positions: map[string]float64{}, Speed: 1, Collapsed: true}
}
func (p *voicePlayback) Play(id string) string { previous := p.Active; p.Active = id; return previous }
func (p *voicePlayback) Pause(id string) {
	if p.Active == id {
		p.Active = ""
	}
}
func (p *voicePlayback) Seek(id string, position, duration float64) error {
	if math.IsNaN(position) || math.IsInf(position, 0) || math.IsNaN(duration) || math.IsInf(duration, 0) || position < 0 || duration < 0 || position > duration {
		return errVoiceRecorderState
	}
	p.Positions[id] = position
	return nil
}
func (p *voicePlayback) SetSpeed(speed float64) error {
	if speed != 1 && speed != 1.5 && speed != 2 {
		return errVoiceRecorderState
	}
	p.Speed = speed
	return nil
}

func voiceSegmentActive(positionMS float64, startMS, endMS float64, playing bool) bool {
	return playing && !math.IsNaN(positionMS) && positionMS >= startMS && positionMS < endMS
}

func voiceGrantTTL(now, expires time.Time) (time.Duration, bool) {
	return expires.Sub(now), !expires.IsZero() && now.Before(expires)
}
