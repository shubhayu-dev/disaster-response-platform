// Package geo resolves a disaster's location from free text.
//
// Strategy: (1) ask a pluggable LLM Extractor for the location name and
// geocode it via the built-in gazetteer; (2) if the extractor is absent,
// fails, times out or returns something unknown, scan the description for
// any gazetteer entry. The fallback is fully deterministic and offline.
package geo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrLocationNotFound means no known location could be resolved.
var ErrLocationNotFound = errors.New("no known location found")

// Location is a named point.
type Location struct {
	Name      string
	Latitude  float64
	Longitude float64
}

// Extractor extracts a location name from a description (typically via an LLM).
// It is a plain function type so tests can substitute a mock.
type Extractor func(ctx context.Context, description string) (string, error)

type entry struct {
	loc     Location
	aliases []string
}

func e(name string, lat, lng float64, aliases ...string) entry {
	return entry{loc: Location{Name: name, Latitude: lat, Longitude: lng}, aliases: aliases}
}

// gazetteer is the deterministic fallback dictionary.
var gazetteer = []entry{
	e("Manhattan, NYC", 40.7831, -73.9712, "manhattan", "midtown manhattan", "lower manhattan"),
	e("Brooklyn, NYC", 40.6782, -73.9442, "brooklyn"),
	e("Queens, NYC", 40.7282, -73.7949, "queens"),
	e("New York, NY", 40.7128, -74.0060, "new york city", "new york", "nyc"),
	e("Los Angeles, CA", 34.0522, -118.2437, "los angeles"),
	e("San Francisco, CA", 37.7749, -122.4194, "san francisco"),
	e("Seattle, WA", 47.6062, -122.3321, "seattle"),
	e("Chicago, IL", 41.8781, -87.6298, "chicago"),
	e("Houston, TX", 29.7604, -95.3698, "houston"),
	e("Miami, FL", 25.7617, -80.1918, "miami"),
	e("New Orleans, LA", 29.9511, -90.0715, "new orleans"),
	e("London, UK", 51.5074, -0.1278, "london"),
	e("Tokyo, Japan", 35.6762, 139.6503, "tokyo"),
	e("Mumbai, India", 19.0760, 72.8777, "mumbai", "bombay"),
	e("Kolkata, India", 22.5726, 88.3639, "kolkata", "calcutta"),
	e("Jakarta, Indonesia", -6.2088, 106.8456, "jakarta"),
	e("Manila, Philippines", 14.5995, 120.9842, "manila"),
	e("Sydney, Australia", -33.8688, 151.2093, "sydney"),
}

type aliasPattern struct {
	key string
	re  *regexp.Regexp
	loc Location
}

// Resolver resolves locations using an optional Extractor plus the gazetteer.
type Resolver struct {
	extract  Extractor
	byAlias  map[string]Location
	patterns []aliasPattern // longest alias first so "manhattan, nyc" beats "nyc"
	timeout  time.Duration
	log      *slog.Logger
}

// NewResolver builds a Resolver. extract may be nil (fallback-only mode).
func NewResolver(extract Extractor, log *slog.Logger) *Resolver {
	if log == nil {
		log = slog.Default()
	}
	r := &Resolver{extract: extract, byAlias: map[string]Location{}, timeout: 5 * time.Second, log: log}
	for _, g := range gazetteer {
		for _, a := range append([]string{g.loc.Name}, g.aliases...) {
			key := normalize(a)
			r.byAlias[key] = g.loc
			r.patterns = append(r.patterns, aliasPattern{
				key: key,
				re:  regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `\b`),
				loc: g.loc,
			})
		}
	}
	sort.SliceStable(r.patterns, func(i, j int) bool { return len(r.patterns[i].key) > len(r.patterns[j].key) })
	return r
}

// Resolve determines the location described by description.
func (r *Resolver) Resolve(ctx context.Context, description string) (Location, error) {
	if r.extract != nil {
		ectx, cancel := context.WithTimeout(ctx, r.timeout)
		name, err := r.extract(ectx, description)
		cancel()
		switch {
		case err != nil:
			r.log.Warn("llm location extraction failed; using fallback", "err", err)
		default:
			if loc, ok := r.Lookup(name); ok {
				return loc, nil
			}
			r.log.Warn("llm returned an unknown location; using fallback", "location", name)
		}
	}
	return r.scan(description)
}

// Lookup geocodes an explicit location name via the gazetteer.
func (r *Resolver) Lookup(name string) (Location, bool) {
	loc, ok := r.byAlias[normalize(name)]
	return loc, ok
}

func (r *Resolver) scan(description string) (Location, error) {
	text := strings.ToLower(description)
	for _, p := range r.patterns {
		if p.re.MatchString(text) {
			return p.loc, nil
		}
	}
	return Location{}, ErrLocationNotFound
}

func normalize(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	return strings.Trim(s, " .,;:!?\"'")
}

// NewHTTPExtractor returns an Extractor backed by an HTTP endpoint.
func NewHTTPExtractor(apiKey, baseURL, model string) Extractor {
	client := &http.Client{Timeout: 8 * time.Second}
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", strings.TrimRight(baseURL, "/"), model)
	return func(ctx context.Context, description string) (string, error) {
		payload, err := json.Marshal(map[string]any{
			"systemInstruction": map[string]any{"parts": []map[string]string{{
				"text": "Extract the single primary geographic location from the text. " +
					"Reply with only the location name (e.g. \"Manhattan, NYC\"), or NONE if there is none.",
			}}},
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]string{{"text": description}},
			}},
			"generationConfig": map[string]any{"temperature": 0},
		})
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", apiKey)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("gemini status %d", resp.StatusCode)
		}
		var out struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return "", fmt.Errorf("decode gemini response: %w", err)
		}
		if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
			return "", errors.New("gemini returned no candidates")
		}
		name := strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text)
		if name == "" || strings.EqualFold(name, "none") {
			return "", ErrLocationNotFound
		}
		return name, nil
	}
}