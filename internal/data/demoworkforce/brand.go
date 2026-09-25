package demoworkforce

import _ "embed"

// PackTheme is a demo company's organization appearance: the same fields
// internal/experience/preferences.Theme stores and productui.CustomerTheme
// admits, repeated here as plain data because this package is a seeder and
// does not import the experience layer. A pack with no theme keeps the
// product's default look.
type PackTheme struct {
	BrandName, BrandMark                       string
	ColorMode, Palette, Shape, Density, Glyphs string
	Typeface, Navigation, Motion               string
	TokenOverrides, DarkTokenOverrides         map[string]string
}

// PackLogo is a demo company's raster logo revision for the tenant's brand
// asset library: the PNG original and the bounded JPEG proxy the library
// requires beside it.
type PackLogo struct {
	Name          string
	Width, Height int
	PNG, Proxy    []byte
}

//go:embed brand/ironridge-logo.png
var ironridgeLogoPNG []byte

//go:embed brand/ironridge-logo-proxy.jpg
var ironridgeLogoProxy []byte

// ironridgeTheme is Ironridge's construction look: charcoal and steel ink
// with a rust safety-orange accent, the Modern typeface, precise corners,
// bold square glyphs, a tinted navigation and brisk motion. Every color pair
// passes the product's own contrast qualification in light and dark
// (TestIronridgeThemeIsAdmitted).
var ironridgeTheme = &PackTheme{
	BrandName: "Ironridge Builders", BrandMark: "IB",
	ColorMode: "system", Palette: "custom", Shape: "precise", Density: "comfortable",
	Glyphs: "square-bold", Typeface: "modern", Navigation: "tinted", Motion: "brisk",
	TokenOverrides: map[string]string{
		"color.brand.primary": "#c2410c", "color.brand.hover": "#9a3412", "color.brand.soft": "#fdeee5",
		"color.text.primary": "#1f2328", "color.text.muted": "#515861",
		"color.canvas": "#f4f4f2", "color.surface": "#ffffff", "color.border": "#d4d2cd",
	},
	DarkTokenOverrides: map[string]string{
		"color.brand.primary": "#fb923c", "color.brand.hover": "#fdba74", "color.brand.soft": "#2b1d14",
		"color.text.primary": "#f2f0ec", "color.text.muted": "#b5b1aa",
		"color.canvas": "#111315", "color.surface": "#1b1e22", "color.border": "#3d4249",
	},
}

var ironridgeLogo = &PackLogo{Name: "ironridge-logo.png", Width: 360, Height: 80, PNG: ironridgeLogoPNG, Proxy: ironridgeLogoProxy}
