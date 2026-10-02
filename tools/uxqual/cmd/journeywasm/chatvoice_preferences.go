package main

import "encoding/json"

type voicePreferences struct {
	Speed     float64
	Collapsed bool
}

func voicePreferenceDecode(raw string) voicePreferences {
	p := voicePreferences{Speed: 1, Collapsed: true}
	if raw == "" {
		return p
	}
	var stored voicePreferences
	if json.Unmarshal([]byte(raw), &stored) != nil {
		return p
	}
	if stored.Speed != 1 && stored.Speed != 1.5 && stored.Speed != 2 {
		return p
	}
	return stored
}
func voicePreferenceEncode(p *voicePlayback) string {
	raw, _ := json.Marshal(voicePreferences{Speed: p.Speed, Collapsed: p.Collapsed})
	return string(raw)
}
