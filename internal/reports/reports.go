// Package reports fetches situational reports from an external source and
// serves them through a Redis cache-aside layer.
package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/example/disaster-response/internal/cache"
	"github.com/example/disaster-response/internal/models"
)

// CacheTTL is how long a disaster's reports stay cached.
const CacheTTL = 300 * time.Second

// Report is the normalised report shape exposed by the API.
type Report struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Author     string    `json:"author"`
	Message    string    `json:"message"`
	Severity   string    `json:"severity"`
	ReportedAt time.Time `json:"reported_at"`
}

// RawPost is the external provider's schema (different field names/types).
type RawPost struct {
	PostID   string `json:"post_id"`
	User     string `json:"user"`
	Text     string `json:"text"`
	PostedAt string `json:"posted_at"` // RFC3339
	Urgency  int    `json:"urgency"`   // 1 (low) .. 5 (critical)
}

// Feed is an external report source.
type Feed interface {
	Fetch(ctx context.Context, d models.Disaster) ([]RawPost, error)
}

// MockFeed generates deterministic social-media-style posts per disaster.
type MockFeed struct{}

var templates = []string{
	"Roads near %s are impassable, emergency crews needed.",
	"Shelter open at the community center in %s, room for 200 people.",
	"Power outage reported across parts of %s.",
	"Volunteers needed for supply distribution in %s.",
	"Medical assistance requested in %s, multiple injuries reported.",
	"Situation stabilising in %s; authorities urge caution.",
}

// Fetch implements Feed.
func (MockFeed) Fetch(ctx context.Context, d models.Disaster) ([]RawPost, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(d.ID))
	seed := int64(h.Sum64())
	rng := rand.New(rand.NewSource(seed))
	now := time.Now().UTC()
	posts := make([]RawPost, 0, 5)
	for i := 0; i < 5; i++ {
		posts = append(posts, RawPost{
			PostID:   fmt.Sprintf("post-%x-%d", uint32(seed), i),
			User:     fmt.Sprintf("@witness_%d", rng.Intn(900)+100),
			Text:     fmt.Sprintf(templates[rng.Intn(len(templates))], d.LocationName),
			PostedAt: now.Add(-time.Duration(i*7) * time.Minute).Format(time.RFC3339),
			Urgency:  rng.Intn(5) + 1,
		})
	}
	return posts, nil
}

// Normalize converts raw provider posts into Reports (newest first),
// dropping malformed entries.
func Normalize(raws []RawPost) []Report {
	out := make([]Report, 0, len(raws))
	for _, p := range raws {
		msg := strings.TrimSpace(p.Text)
		ts, err := time.Parse(time.RFC3339, p.PostedAt)
		if msg == "" || err != nil {
			continue
		}
		out = append(out, Report{
			ID:         p.PostID,
			Source:     "mock-social",
			Author:     strings.TrimSpace(p.User),
			Message:    msg,
			Severity:   severity(p.Urgency),
			ReportedAt: ts.UTC(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ReportedAt.After(out[j].ReportedAt) })
	return out
}

func severity(urgency int) string {
	switch {
	case urgency >= 4:
		return "critical"
	case urgency == 3:
		return "high"
	case urgency == 2:
		return "medium"
	default:
		return "low"
	}
}

// Service implements cache-aside over a Feed.
type Service struct {
	feed  Feed
	cache cache.Cache
	ttl   time.Duration
	log   *slog.Logger
}

// NewService builds a Service.
func NewService(feed Feed, c cache.Cache, ttl time.Duration, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{feed: feed, cache: c, ttl: ttl, log: log}
}

// CacheKey returns the Redis key for a disaster's reports.
func CacheKey(disasterID string) string { return "disaster:" + disasterID + ":reports" }

// Get returns reports and whether they were served from cache. Cache failures
// degrade to a direct fetch; only feed failures surface as errors.
func (s *Service) Get(ctx context.Context, d models.Disaster) ([]Report, bool, error) {
	key := CacheKey(d.ID)
	raw, hit, err := s.cache.Get(ctx, key)
	switch {
	case err != nil:
		s.log.Warn("redis get failed; bypassing cache", "key", key, "err", err)
	case hit:
		var cached []Report
		if err := json.Unmarshal(raw, &cached); err == nil {
			return cached, true, nil
		}
		s.log.Warn("corrupt cache entry; refetching", "key", key)
	}

	posts, err := s.feed.Fetch(ctx, d)
	if err != nil {
		return nil, false, fmt.Errorf("fetch external reports: %w", err)
	}
	normalized := Normalize(posts)
	if payload, err := json.Marshal(normalized); err == nil {
		if err := s.cache.Set(ctx, key, payload, s.ttl); err != nil {
			s.log.Warn("redis set failed", "key", key, "err", err)
		}
	}
	return normalized, false, nil
}

// Invalidate drops cached reports for a disaster (best effort).
func (s *Service) Invalidate(ctx context.Context, disasterID string) {
	if err := s.cache.Delete(ctx, CacheKey(disasterID)); err != nil {
		s.log.Warn("redis delete failed", "disaster_id", disasterID, "err", err)
	}
}
