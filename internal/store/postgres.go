// Package store implements persistence on PostgreSQL/PostGIS via pgx.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/disaster-response/internal/models"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ListFilter narrows and paginates disaster listings.
type ListFilter struct {
	Tags   []string // match disasters having ANY of these tags
	Limit  int
	Offset int
}

// Postgres is the pgx-backed store.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres connects (retrying while the database boots) and returns a store.
func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	for attempt := 1; ; attempt++ {
		if err = pool.Ping(ctx); err == nil {
			return &Postgres{pool: pool}, nil
		}
		if attempt >= 15 {
			pool.Close()
			return nil, fmt.Errorf("ping database after %d attempts: %w", attempt, err)
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Close releases the pool.
func (p *Postgres) Close() { p.pool.Close() }

// Ping checks database connectivity.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

const disasterCols = `id::text, title, description, location_name, latitude, longitude, tags, status, created_by, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanDisaster(s scanner) (models.Disaster, error) {
	var d models.Disaster
	err := s.Scan(&d.ID, &d.Title, &d.Description, &d.LocationName, &d.Latitude, &d.Longitude,
		&d.Tags, &d.Status, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
	if d.Tags == nil {
		d.Tags = []string{}
	}
	return d, err
}

// CreateDisaster inserts a disaster and returns the stored row.
func (p *Postgres) CreateDisaster(ctx context.Context, d models.Disaster) (models.Disaster, error) {
	const q = `INSERT INTO disasters (title, description, location_name, latitude, longitude, tags, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING ` + disasterCols
	out, err := scanDisaster(p.pool.QueryRow(ctx, q, d.Title, d.Description, d.LocationName,
		d.Latitude, d.Longitude, d.Tags, d.Status, d.CreatedBy))
	if err != nil {
		return models.Disaster{}, fmt.Errorf("insert disaster: %w", err)
	}
	return out, nil
}

// ListDisasters returns disasters newest-first, optionally filtered by tags (&&, any-match).
func (p *Postgres) ListDisasters(ctx context.Context, f ListFilter) ([]models.Disaster, error) {
	const q = `SELECT ` + disasterCols + ` FROM disasters
		WHERE ($1::text[] IS NULL OR tags && $1::text[])
		ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`
	var tags any // untyped nil => SQL NULL => no tag filter
	if len(f.Tags) > 0 {
		tags = f.Tags
	}
	rows, err := p.pool.Query(ctx, q, tags, f.Limit, f.Offset)
	if err != nil {
		return nil, fmt.Errorf("query disasters: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (models.Disaster, error) { return scanDisaster(r) })
	if err != nil {
		return nil, fmt.Errorf("scan disasters: %w", err)
	}
	if out == nil {
		out = []models.Disaster{}
	}
	return out, nil
}

// GetDisaster fetches one disaster by id.
func (p *Postgres) GetDisaster(ctx context.Context, id string) (models.Disaster, error) {
	d, err := scanDisaster(p.pool.QueryRow(ctx, `SELECT `+disasterCols+` FROM disasters WHERE id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Disaster{}, ErrNotFound
	}
	if err != nil {
		return models.Disaster{}, fmt.Errorf("get disaster: %w", err)
	}
	return d, nil
}

// UpdateDisaster applies a partial update (NULL parameter => keep column value).
func (p *Postgres) UpdateDisaster(ctx context.Context, id string, req models.UpdateDisasterRequest) (models.Disaster, error) {
	const q = `UPDATE disasters SET
			title         = COALESCE($2, title),
			description   = COALESCE($3, description),
			location_name = COALESCE($4, location_name),
			latitude      = COALESCE($5, latitude),
			longitude     = COALESCE($6, longitude),
			tags          = COALESCE($7::text[], tags),
			status        = COALESCE($8, status),
			updated_at    = now()
		WHERE id = $1::uuid RETURNING ` + disasterCols
	var tags []string // nil => NULL => unchanged
	if req.Tags != nil {
		tags = *req.Tags
	}
	d, err := scanDisaster(p.pool.QueryRow(ctx, q, id, req.Title, req.Description, req.LocationName,
		req.Latitude, req.Longitude, tags, req.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Disaster{}, ErrNotFound
	}
	if err != nil {
		return models.Disaster{}, fmt.Errorf("update disaster: %w", err)
	}
	return d, nil
}

// DeleteDisaster removes a disaster (resources cascade).
func (p *Postgres) DeleteDisaster(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM disasters WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete disaster: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// NearbyResources returns resources for the disaster (or global ones with a NULL
// disaster_id) strictly within radiusKm of (lat, lng), nearest first.
//
// resources.location is GEOGRAPHY, so ST_DWithin and ST_Distance operate in
// metres on the spheroid; ST_DWithin is index-assisted by the GIST index.
func (p *Postgres) NearbyResources(ctx context.Context, disasterID string, lat, lng, radiusKm float64) ([]models.Resource, error) {
	const q = `
		WITH origin AS (
			SELECT ST_SetSRID(ST_MakePoint($3::float8, $2::float8), 4326)::geography AS pt
		)
		SELECT r.id::text, r.name, r.type, r.capacity,
		       ST_Y(r.location::geometry) AS latitude,
		       ST_X(r.location::geometry) AS longitude,
		       ROUND((ST_Distance(r.location, o.pt) / 1000.0)::numeric, 3)::float8 AS distance_km
		FROM resources r, origin o
		WHERE (r.disaster_id = $1::uuid OR r.disaster_id IS NULL)
		  AND ST_DWithin(r.location, o.pt, $4::float8 * 1000.0)
		ORDER BY ST_Distance(r.location, o.pt), r.id
		LIMIT 200`
	rows, err := p.pool.Query(ctx, q, disasterID, lat, lng, radiusKm)
	if err != nil {
		return nil, fmt.Errorf("query resources: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (models.Resource, error) {
		var res models.Resource
		err := r.Scan(&res.ID, &res.Name, &res.Type, &res.Capacity, &res.Latitude, &res.Longitude, &res.DistanceKm)
		return res, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan resources: %w", err)
	}
	if out == nil {
		out = []models.Resource{}
	}
	return out, nil
}
