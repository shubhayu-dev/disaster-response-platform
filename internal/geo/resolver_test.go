package geo

import (
	"context"
	"errors"
	"testing"
)

func TestResolveFallbackDictionary(t *testing.T) {
	r := NewResolver(nil, nil)
	loc, err := r.Resolve(context.Background(), "Severe flooding reported across Manhattan, NYC after a burst water main")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Latitude != 40.7831 || loc.Longitude != -73.9712 {
		t.Fatalf("unexpected coords %+v", loc)
	}
}

func TestResolveLLMFailureFallsBack(t *testing.T) {
	r := NewResolver(func(context.Context, string) (string, error) { return "", errors.New("rate limited") }, nil)
	loc, err := r.Resolve(context.Background(), "Wildfire near San Francisco")
	if err != nil || loc.Name != "San Francisco, CA" {
		t.Fatalf("got %+v, %v", loc, err)
	}
}

func TestResolveLLMResultPreferred(t *testing.T) {
	r := NewResolver(func(context.Context, string) (string, error) { return "Tokyo", nil }, nil)
	loc, err := r.Resolve(context.Background(), "Aid flown from Manhattan to the quake zone in Tokyo")
	if err != nil || loc.Name != "Tokyo, Japan" {
		t.Fatalf("got %+v, %v", loc, err)
	}
}

func TestResolveNotFound(t *testing.T) {
	r := NewResolver(nil, nil)
	if _, err := r.Resolve(context.Background(), "Something happened somewhere"); !errors.Is(err, ErrLocationNotFound) {
		t.Fatalf("expected ErrLocationNotFound, got %v", err)
	}
}
