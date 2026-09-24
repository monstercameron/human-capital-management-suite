package journeycss

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// This file holds the journey page's @keyframes frame bodies as typed data.
// Each animation site calls gwccss.Keyframes with the shared frames for its
// name (so identical bodies hash to one @keyframes block) and links it with
// the animation longhands. @keyframes names are content-hashed by GWC, so
// the animation declarations below reference the hashed names pinned in
// jnKeyframesHash; TestStylesheetAnimationNamesHaveKeyframes guards every
// animation-name against a missing block, which also fails loudly if an
// upstream hash change ever requires re-pinning.

var jnSlideinFrames = []gwccss.Frame{
	gwccss.At("from", gwccss.Raw("opacity", "0"), gwccss.Raw("transform", "translateY(6px)")),
	gwccss.At("to", gwccss.Raw("opacity", "1"), gwccss.Raw("transform", "none")),
}

var jnPopFrames = []gwccss.Frame{
	gwccss.At("from", gwccss.Raw("opacity", "0"), gwccss.Raw("transform", "scale(.4)")),
	gwccss.At("60%", gwccss.Raw("opacity", "1"), gwccss.Raw("transform", "scale(1.15)")),
	gwccss.At("to", gwccss.Raw("transform", "scale(1)")),
}

var jnGrowYFrames = []gwccss.Frame{
	gwccss.At("from", gwccss.Raw("height", "0")),
	gwccss.At("to", gwccss.Raw("height", "calc(100% - 2rem)")),
}

var jnGrowXFrames = []gwccss.Frame{
	gwccss.At("from", gwccss.Raw("width", "0")),
	gwccss.At("to", gwccss.Raw("width", "calc(100% - 2.75rem)")),
}

var jnGrowXSVGFrames = []gwccss.Frame{
	gwccss.At("from", gwccss.Raw("transform", "scaleX(0)")),
	gwccss.At("to", gwccss.Raw("transform", "scaleX(1)")),
}

var jnDropFrames = []gwccss.Frame{
	gwccss.At("from", gwccss.Raw("opacity", "0"), gwccss.Raw("transform", "translateY(-6px)")),
	gwccss.At("to", gwccss.Raw("opacity", "1"), gwccss.Raw("transform", "none")),
}

var jnSweepFrames = []gwccss.Frame{
	gwccss.At("from", gwccss.Raw("transform", "translateX(-100%)")),
	gwccss.At("to", gwccss.Raw("transform", "translateX(140%)")),
}

var jnHaloFrames = []gwccss.Frame{
	gwccss.At("0%,100%", gwccss.Raw("box-shadow", "0 0 0 0 rgba(43,58,143,.30)")),
	gwccss.At("50%", gwccss.Raw("box-shadow", "0 0 0 6px rgba(43,58,143,0)")),
}

var jnAmberFrames = []gwccss.Frame{
	gwccss.At("0%,100%", gwccss.Raw("box-shadow", "inset 0 0 0 1px rgba(122,74,0,.20)")),
	gwccss.At("50%", gwccss.Raw("box-shadow", "inset 0 0 0 1px rgba(122,74,0,.20),0 0 0 4px rgba(122,74,0,.10)")),
}

var jnAlertFrames = []gwccss.Frame{
	gwccss.At("0%,100%", gwccss.Raw("box-shadow", "inset 0 0 0 1px rgba(155,17,48,.25)")),
	gwccss.At("50%", gwccss.Raw("box-shadow", "inset 0 0 0 1px rgba(155,17,48,.25),0 0 0 4px rgba(155,17,48,.12)")),
}

var jnBreatheFrames = []gwccss.Frame{
	gwccss.At("0%,100%", gwccss.Raw("opacity", "1")),
	gwccss.At("50%", gwccss.Raw("opacity", ".82")),
}

// jnKeyframesHash pins the content-hashed @keyframes names GWC derives from
// the frame bodies above, so animation shorthands can reference them. The
// values below are filled in once the frames are final; the pinning test
// recomputes them from a live Harvest and fails on any drift.
const (
	jnSlideinHash  = "2zqd52uojwort"
	jnPopHash      = "3ridygbxw2ry0"
	jnGrowYHash    = "31etq5wrprejp"
	jnGrowXHash    = "2ybesrv1y7m9d"
	jnGrowXSVGHash = "12bsajj0jnz3p"
	jnDropHash     = "3rw36llel9w5q"
	jnSweepHash    = "1lm4ikpk3kwah"
	jnHaloHash     = "d2evgl2isjd5"
	jnAmberHash    = "u5d68e6kyj7y"
	jnAlertHash    = "20wtq4njut0zx"
	jnBreatheHash  = "75v4lrd889a5"
)
