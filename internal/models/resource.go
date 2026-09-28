package models

// Resource is a physical asset (shelter, hospital, supply depot...) returned
// from the geospatial search, annotated with its distance from the query point.
type Resource struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Capacity   int     `json:"capacity"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	DistanceKm float64 `json:"distance_km"`
}
