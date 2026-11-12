package productui

import (
	"strconv"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// avatarPaletteSlot is one person-avatar colour: the background the initials sit
// on and the colour of the initials, in the light and in the dark theme.
type avatarPaletteSlot struct{ LightBG, LightInk, DarkBG, DarkInk string }

// avatarPalette is the person-avatar palette (#3770): eight hues, each pair at
// least 4.5:1 in its theme (TestChatDesign_3770_AvatarPaletteMeetsContrast reads
// the served tokens and computes it). Chat picks the slot from the person id
// (chatui.AvatarHue) and reads --hcm-avatar-N-bg and --hcm-avatar-N-ink.
func avatarPalette() [8]avatarPaletteSlot {
	return [8]avatarPaletteSlot{
		{"#d9f0ea", "#0b5f4e", "#123f37", "#8fe3cd"}, // teal
		{"#dde9fb", "#1a4f9c", "#173a66", "#a9ccff"}, // blue
		{"#e8e1fa", "#5a3aa6", "#33276a", "#c9b8ff"}, // violet
		{"#f8dff0", "#96266f", "#54213f", "#ffb3de"}, // magenta
		{"#fbe1de", "#a32a1e", "#5a221d", "#ffb4a8"}, // red
		{"#fbe9cc", "#7d4a00", "#4a3408", "#f6cf7e"}, // amber
		{"#e6efcf", "#47610f", "#2f3d10", "#c9e08a"}, // olive
		{"#e1e7ee", "#3b4f66", "#2b3a4b", "#c3d3e4"}, // slate
	}
}

// avatarPaletteRules declares the palette tokens of one theme.
func avatarPaletteRules(dark bool) []gwccss.Rule {
	rules := []gwccss.Rule{}
	for index, slot := range avatarPalette() {
		background, ink := slot.LightBG, slot.LightInk
		if dark {
			background, ink = slot.DarkBG, slot.DarkInk
		}
		name := "hcm-avatar-" + strconv.Itoa(index+1)
		rules = append(rules, gwccss.Custom(name+"-bg", background), gwccss.Custom(name+"-ink", ink))
	}
	return rules
}
