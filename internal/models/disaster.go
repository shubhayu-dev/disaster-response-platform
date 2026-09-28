// Package models contains the domain types and request validation.
package models

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Disaster statuses.
const (
	StatusActive     = "active"
	StatusMonitoring = "monitoring"
	StatusResolved   = "resolved"
)

const (
	maxTitleLen       = 200
	maxDescriptionLen = 5000
	maxLocationLen    = 200
	maxTags           = 20
	maxTagLen         = 50
)

var validStatuses = map[string]bool{StatusActive: true, StatusMonitoring: true, StatusResolved: true}

// Disaster is the core aggregate.
type Disaster struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	LocationName string    `json:"location_name"`
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	Tags         []string  `json:"tags"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateDisasterRequest is the POST /disasters body. Location fields are
// optional: when omitted the location is resolved from the description.
type CreateDisasterRequest struct {
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	LocationName string   `json:"location_name,omitempty"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Status       string   `json:"status,omitempty"`
}

// Validate trims/normalises fields in place and returns field errors.
func (r *CreateDisasterRequest) Validate() map[string]string {
	errs := map[string]string{}
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
	r.LocationName = strings.TrimSpace(r.LocationName)
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))

	checkText(errs, "title", r.Title, maxTitleLen, true)
	checkText(errs, "description", r.Description, maxDescriptionLen, true)
	checkText(errs, "location_name", r.LocationName, maxLocationLen, false)
	checkCoords(errs, r.Latitude, r.Longitude)
	if r.Status == "" {
		r.Status = StatusActive
	} else if !validStatuses[r.Status] {
		errs["status"] = "must be one of: active, monitoring, resolved"
	}
	r.Tags = NormalizeTags(r.Tags)
	checkTags(errs, r.Tags)
	return errs
}

// UpdateDisasterRequest is the PATCH body; nil fields are left unchanged.
type UpdateDisasterRequest struct {
	Title        *string   `json:"title,omitempty"`
	Description  *string   `json:"description,omitempty"`
	LocationName *string   `json:"location_name,omitempty"`
	Latitude     *float64  `json:"latitude,omitempty"`
	Longitude    *float64  `json:"longitude,omitempty"`
	Tags         *[]string `json:"tags,omitempty"`
	Status       *string   `json:"status,omitempty"`
}

// Validate trims/normalises fields in place and returns field errors.
func (r *UpdateDisasterRequest) Validate() map[string]string {
	errs := map[string]string{}
	if r.Title == nil && r.Description == nil && r.LocationName == nil && r.Latitude == nil &&
		r.Longitude == nil && r.Tags == nil && r.Status == nil {
		errs["body"] = "at least one field must be provided"
		return errs
	}
	if r.Title != nil {
		v := strings.TrimSpace(*r.Title)
		r.Title = &v
		checkText(errs, "title", v, maxTitleLen, true)
	}
	if r.Description != nil {
		v := strings.TrimSpace(*r.Description)
		r.Description = &v
		checkText(errs, "description", v, maxDescriptionLen, true)
	}
	if r.LocationName != nil {
		v := strings.TrimSpace(*r.LocationName)
		r.LocationName = &v
		checkText(errs, "location_name", v, maxLocationLen, true)
	}
	checkCoords(errs, r.Latitude, r.Longitude)
	if r.Status != nil {
		v := strings.ToLower(strings.TrimSpace(*r.Status))
		r.Status = &v
		if !validStatuses[v] {
			errs["status"] = "must be one of: active, monitoring, resolved"
		}
	}
	if r.Tags != nil {
		v := NormalizeTags(*r.Tags)
		r.Tags = &v
		checkTags(errs, v)
	}
	return errs
}

// NormalizeTags lower-cases, trims and de-duplicates tags. Never returns nil.
func NormalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// ValidCoordinate reports whether lat/lng are finite and within range.
func ValidCoordinate(lat, lng float64) bool {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lng) || math.IsInf(lng, 0) {
		return false
	}
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

func checkText(errs map[string]string, field, v string, max int, required bool) {
	switch {
	case v == "" && required:
		errs[field] = "is required"
	case len([]rune(v)) > max:
		errs[field] = fmt.Sprintf("must be at most %d characters", max)
	}
}

func checkCoords(errs map[string]string, lat, lng *float64) {
	if (lat == nil) != (lng == nil) {
		errs["coordinates"] = "latitude and longitude must be provided together"
		return
	}
	if lat != nil && !ValidCoordinate(*lat, *lng) {
		errs["coordinates"] = "latitude must be in [-90, 90] and longitude in [-180, 180]"
	}
}

func checkTags(errs map[string]string, tags []string) {
	if len(tags) > maxTags {
		errs["tags"] = fmt.Sprintf("at most %d tags are allowed", maxTags)
		return
	}
	for _, t := range tags {
		if len([]rune(t)) > maxTagLen {
			errs["tags"] = fmt.Sprintf("each tag must be at most %d characters", maxTagLen)
			return
		}
	}
}
