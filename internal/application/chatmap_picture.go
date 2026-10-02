package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type ChatmapUsagePort interface {
	RecordLocationUsage(context.Context, string, string, string, string) error
}
type ChatmapNoopUsage struct{}

func (ChatmapNoopUsage) RecordLocationUsage(context.Context, string, string, string, string) error {
	return nil
}

type chatmapCachedPicture struct {
	image chat.MapPicture
	until time.Time
}
type chatmapRate struct {
	start time.Time
	count int
}
type ChatmapPictureCache struct {
	Locations *chat.LocationService
	Renderer  chat.MapPicturePort
	Usage     ChatmapUsagePort
	mu        sync.Mutex
	pictures  map[string]chatmapCachedPicture
	rates     map[string]chatmapRate
}

func (c *ChatmapPictureCache) Picture(ctx context.Context, p chat.Principal, k chat.LocationKey, zoom int, size chat.MapSize, theme chat.MapTheme) (chat.MapPicture, error) {
	if c == nil || c.Locations == nil || c.Renderer == nil {
		return chat.MapPicture{}, chat.ErrUnavailable
	}
	v, err := c.Locations.Read(ctx, p, k)
	if err != nil {
		return chat.MapPicture{}, err
	}
	if v.Ended {
		return chat.MapPicture{}, chat.ErrNotFound
	}
	now := time.Now().UTC()
	if c.Locations.Now != nil {
		now = c.Locations.Now().UTC()
	}
	scope := p.TenantID + "\x00" + p.SubjectID + "\x00" + k.TenantID + "\x00" + k.ConversationID
	encoded, _ := json.Marshal(struct {
		Share chat.LocationShare
		Zoom  int
		Size  chat.MapSize
		Theme chat.MapTheme
	}{v, zoom, size, theme})
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	c.mu.Lock()
	if c.rates == nil {
		c.rates = map[string]chatmapRate{}
	}
	rate := c.rates[scope]
	if !now.Before(rate.start.Add(time.Minute)) {
		rate = chatmapRate{start: now}
	}
	if rate.count >= 120 {
		c.mu.Unlock()
		return chat.MapPicture{}, chat.ErrUnavailable
	}
	rate.count++
	if len(c.rates) >= 1000 {
		for key, r := range c.rates {
			if !now.Before(r.start.Add(time.Minute)) {
				delete(c.rates, key)
			}
		}
		if len(c.rates) >= 1000 {
			c.mu.Unlock()
			return chat.MapPicture{}, chat.ErrUnavailable
		}
	}
	c.rates[scope] = rate
	if cached, ok := c.pictures[digest]; ok && now.Before(cached.until) {
		c.mu.Unlock()
		cached.image.Image = append([]byte(nil), cached.image.Image...)
		return cached.image, nil
	}
	c.mu.Unlock()
	image, err := c.Renderer.Render(v.Place, zoom, size, theme)
	if err != nil {
		return chat.MapPicture{}, err
	}
	if c.Usage != nil {
		if err = c.Usage.RecordLocationUsage(ctx, k.TenantID, p.SubjectID, "map_picture", digest); err != nil {
			return chat.MapPicture{}, chat.ErrUnavailable
		}
	}
	c.mu.Lock()
	if len(c.pictures) >= 256 {
		c.pictures = nil
	}
	if c.pictures == nil {
		c.pictures = map[string]chatmapCachedPicture{}
	}
	retained := image
	retained.Image = append([]byte(nil), image.Image...)
	c.pictures[digest] = chatmapCachedPicture{image: retained, until: now.Add(time.Minute)}
	c.mu.Unlock()
	return image, nil
}
