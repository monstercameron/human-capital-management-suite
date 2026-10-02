package chat

import (
	"context"
	"fmt"
	"html"
	"math"
	"strconv"
)

type MapSize struct{ Width, Height int }
type MapTheme struct {
	Dark                  bool
	Locale                string
	PanX, PanY            int
	Surface, Text, Accent string
}
type MapPicture struct {
	Image       []byte
	Attribution string
	ContentType string
}
type MapPicturePort interface {
	Render(LocationPlace, int, MapSize, MapTheme) (MapPicture, error)
}
type SchematicMap struct{}

func (SchematicMap) Render(place LocationPlace, zoom int, size MapSize, theme MapTheme) (MapPicture, error) {
	if place.Position == nil || zoom < 1 || zoom > 20 || size.Width < 160 || size.Width > 1200 || size.Height < 100 || size.Height > 800 || theme.PanX < -1200 || theme.PanX > 1200 || theme.PanY < -800 || theme.PanY > 800 {
		return MapPicture{}, ErrInvalidArgument
	}
	message := "Map detail unavailable"
	attribution := "Schematic · no map data"
	if theme.Locale == "de-DE" {
		message = "Kartendetails nicht verfügbar"
		attribution = "Schema · keine Kartendaten"
	}
	if theme.Locale == "ar" {
		message = "تفاصيل الخريطة غير متاحة"
		attribution = "رسم تخطيطي · لا توجد بيانات خريطة"
	}
	scale := math.Pow(2, float64(20-zoom)) * 10
	radius := math.Max(1, LocationAccuracy(place)/scale*80)
	cx, cy := float64(size.Width)/2+float64(theme.PanX), float64(size.Height)/2+float64(theme.PanY)
	bg, fg := "var(--hcm-color-surface,Canvas)", "var(--hcm-color-text,CanvasText)"
	accent := "var(--hcm-color-brand-primary,currentColor)"
	var err error
	if bg, err = chatmapColour(theme.Surface, bg); err != nil {
		return MapPicture{}, err
	}
	if fg, err = chatmapColour(theme.Text, fg); err != nil {
		return MapPicture{}, err
	}
	if accent, err = chatmapColour(theme.Accent, accent); err != nil {
		return MapPicture{}, err
	}
	scheme := "light"
	if theme.Dark {
		scheme = "dark"
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s"><style>:root{color-scheme:%s;color:%s}text{fill:currentColor}.pin{fill:%s} .grid{stroke:currentColor;opacity:.12}</style><defs><pattern id="grid" width="32" height="32" patternUnits="userSpaceOnUse"><path class="grid" d="M 32 0 L 0 0 0 32" fill="none"/></pattern></defs><rect width="100%%" height="100%%" fill="%s"/><rect width="100%%" height="100%%" fill="url(#grid)"/><circle cx="%.0f" cy="%.0f" r="%.1f" fill="currentColor" fill-opacity=".12" stroke="currentColor"/><path class="pin" d="M %.0f %.0f l -8 -16 a 9 9 0 1 1 16 0 Z" fill="currentColor"/><text x="50%%" y="28" text-anchor="middle">%s</text><path d="M 20 %d h 80 m -80 -4 v 8 m 80 -8 v 8" stroke="currentColor" fill="none"/><text x="20" y="%d">%.0f m</text><text x="50%%" y="%d" text-anchor="middle" font-size="10">%s</text></svg>`, size.Width, size.Height, size.Width, size.Height, html.EscapeString(message), scheme, fg, accent, bg, cx, cy, radius, cx, cy, message, size.Height-35, size.Height-45, scale, size.Height-5, html.EscapeString(attribution))
	return MapPicture{Image: []byte(svg), Attribution: attribution, ContentType: "image/svg+xml"}, nil
}
func chatmapColour(raw, fallback string) (string, error) {
	if raw == "" {
		return fallback, nil
	}
	if raw == "Canvas" || raw == "CanvasText" {
		return raw, nil
	}
	if len(raw) != 4 && len(raw) != 7 && len(raw) != 9 {
		return "", ErrInvalidArgument
	}
	if raw[0] != '#' {
		return "", ErrInvalidArgument
	}
	if _, err := strconv.ParseUint(raw[1:], 16, 32); err != nil {
		return "", ErrInvalidArgument
	}
	return raw, nil
}
func LocationMapZoom(place LocationPlace, size MapSize) int {
	for zoom := 16; zoom > 1; zoom-- {
		if LocationAccuracy(place)/(math.Pow(2, float64(20-zoom))*10)*80 <= float64(min(size.Width, size.Height))*.3 {
			return zoom
		}
	}
	return 1
}

type LocationPictureService struct {
	Locations *LocationService
	Renderer  MapPicturePort
}

func (s LocationPictureService) Picture(ctx context.Context, p Principal, k LocationKey, zoom int, size MapSize, theme MapTheme) (MapPicture, error) {
	if s.Locations == nil || s.Renderer == nil {
		return MapPicture{}, ErrUnavailable
	}
	v, err := s.Locations.Read(ctx, p, k)
	if err != nil {
		return MapPicture{}, err
	}
	if v.Ended {
		return MapPicture{}, ErrNotFound
	}
	return s.Renderer.Render(v.Place, zoom, size, theme)
}
