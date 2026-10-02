package chat

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// CrewPin is one person sharing live to a channel, or one job site.
type CrewPin struct {
	Number   int
	Label    string
	Position LocationPosition
	Paused   bool
	Site     bool
}

// RenderCrewMap draws every pin on one schematic picture from the product's own
// code: a grid, numbered pins (squares for job sites), a scale bar and the
// attribution. It fits the pins in view. Names are in the list beside the map
// and in the picture's text alternative, never only in the picture.
func RenderCrewMap(pins []CrewPin, size MapSize, theme MapTheme) (MapPicture, error) {
	if len(pins) == 0 || len(pins) > 200 || size.Width < 160 || size.Width > 1200 || size.Height < 100 || size.Height > 800 {
		return MapPicture{}, ErrInvalidArgument
	}
	minLat, maxLat, minLon, maxLon := 90.0, -90.0, 180.0, -180.0
	for _, p := range pins {
		minLat, maxLat = math.Min(minLat, p.Position.Latitude), math.Max(maxLat, p.Position.Latitude)
		minLon, maxLon = math.Min(minLon, p.Position.Longitude), math.Max(maxLon, p.Position.Longitude)
	}
	midLat, midLon := (minLat+maxLat)/2, (minLon+maxLon)/2
	const metresPerDegree = 111320.0
	spanY := math.Max(200, (maxLat-minLat)*metresPerDegree)
	spanX := math.Max(200, (maxLon-minLon)*metresPerDegree*math.Cos(midLat*math.Pi/180))
	usableW, usableH := float64(size.Width)-80, float64(size.Height)-90
	metresPerPixel := math.Max(spanX/usableW, spanY/usableH)
	message, attribution := "Crew map", "Schematic · no map data"
	switch theme.Locale {
	case "de-DE":
		message, attribution = "Teamkarte", "Schema · keine Kartendaten"
	case "ar":
		message, attribution = "خريطة الفريق", "رسم تخطيطي · لا توجد بيانات خريطة"
	}
	bg, err := chatmapColour(theme.Surface, "var(--hcm-color-surface,Canvas)")
	if err != nil {
		return MapPicture{}, err
	}
	fg, err := chatmapColour(theme.Text, "var(--hcm-color-text,CanvasText)")
	if err != nil {
		return MapPicture{}, err
	}
	accent, err := chatmapColour(theme.Accent, "var(--hcm-color-brand-primary,currentColor)")
	if err != nil {
		return MapPicture{}, err
	}
	scheme := "light"
	if theme.Dark {
		scheme = "dark"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s"><style>:root{color-scheme:%s;color:%s}text{fill:currentColor;font:11px sans-serif}.grid{stroke:currentColor;opacity:.12}.pin{fill:%s;stroke:currentColor}.paused{opacity:.45}.site{fill:none;stroke:currentColor;stroke-width:2}</style><defs><pattern id="grid" width="32" height="32" patternUnits="userSpaceOnUse"><path class="grid" d="M 32 0 L 0 0 0 32" fill="none"/></pattern></defs><rect width="100%%" height="100%%" fill="%s"/><rect width="100%%" height="100%%" fill="url(#grid)"/>`, size.Width, size.Height, size.Width, size.Height, html.EscapeString(message), scheme, fg, accent, bg)
	for _, p := range pins {
		x := float64(size.Width)/2 + (p.Position.Longitude-midLon)*metresPerDegree*math.Cos(midLat*math.Pi/180)/metresPerPixel
		y := float64(size.Height)/2 - 10 - (p.Position.Latitude-midLat)*metresPerDegree/metresPerPixel
		if p.Site {
			fmt.Fprintf(&b, `<rect class="site" x="%.0f" y="%.0f" width="14" height="14"/><text x="%.0f" y="%.0f" text-anchor="middle">%d</text>`, x-7, y-7, x, y+22, p.Number)
			continue
		}
		class := "pin"
		if p.Paused {
			class += " paused"
		}
		fmt.Fprintf(&b, `<circle class="%s" cx="%.0f" cy="%.0f" r="11"/><text x="%.0f" y="%.0f" text-anchor="middle">%d</text>`, class, x, y, x, y+4, p.Number)
	}
	scale := niceScale(metresPerPixel * 60)
	bar := scale / metresPerPixel
	fmt.Fprintf(&b, `<path d="M 20 %d h %.0f m -%.0f -4 v 8 m %.0f -8 v 8" stroke="currentColor" fill="none"/><text x="20" y="%d">%.0f m</text><text x="50%%" y="%d" text-anchor="middle" font-size="10">%s</text></svg>`, size.Height-35, bar, bar, bar, size.Height-45, scale, size.Height-5, html.EscapeString(attribution))
	return MapPicture{Image: []byte(b.String()), Attribution: attribution, ContentType: "image/svg+xml"}, nil
}

// niceScale rounds a distance to 1, 2 or 5 times a power of ten for the scale bar.
func niceScale(metres float64) float64 {
	if metres <= 0 {
		return 1
	}
	power := math.Pow(10, math.Floor(math.Log10(metres)))
	for _, step := range []float64{1, 2, 5, 10} {
		if metres <= step*power {
			return step * power
		}
	}
	return 10 * power
}
