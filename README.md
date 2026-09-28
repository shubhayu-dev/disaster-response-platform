# Disaster Response Coordination Platform

Go 1.23 backend: disaster CRUD, resilient location resolution, PostGIS radius search,
Redis cache-aside reports, WebSocket broadcasts and header-based authorization.

## Run

```bash
docker compose up --build          # API on :8080, Postgres/PostGIS, Redis
```
Configuration lives in `.env` (git-ignored); start from the template: `cp .env.example .env`
(every variable has a default, so this is optional; set `LLM_API_KEY` to enable LLM extraction).

End-to-end smoke test (Python, optional; Go dependencies stay in `go.mod`):

```bash
python3 -m venv venv && source venv/bin/activate
pip install -r requirements.txt
python scripts/smoke_test.py
```
`seed.sql` is mounted into `docker-entrypoint-initdb.d`, so it runs on the first boot of
the DB volume (`docker compose down -v` to re-seed). The Dockerfile runs `go mod tidy`;
run it locally once and commit `go.sum` for reproducible builds. Tests: `go test ./...`.

## Architecture

```
cmd/api            wiring, graceful shutdown
internal/config    env config
internal/models    domain types + request validation
internal/store     pgx/PostGIS queries (Postgres)
internal/cache     Cache interface + Redis impl
internal/geo       Resolver: mockable LLM Extractor + deterministic gazetteer fallback
internal/reports   mock external feed, normalisation, cache-aside Service
internal/ws        mutex-guarded Hub, ping/pong, slow-client eviction
internal/auth      X-User-Role middleware + Require(action)
internal/handlers  chi router and HTTP handlers (depend on small interfaces)
```

Key decisions:
- **Handlers depend on interfaces** (`Store`, `Broadcaster`, `LocationResolver`, `ReportService`), so each layer is unit-testable with fakes.
- **Location resolution** (`POST /disasters`): explicit `latitude`+`longitude` > explicit `location_name` (geocoded via gazetteer) > extraction from `description`. Extraction calls the `geo.Extractor` function (OpenAI-compatible HTTP client when `LLM_API_KEY` is set, 5s timeout). On error, timeout, or unknown answer it falls back to a longest-match, word-boundary scan of the gazetteer (`Manhattan, NYC` -> 40.7831, -73.9712). With no key it is fully offline. Nothing resolvable -> `422`.
- **Cache-aside** (`GET /disasters/{id}/reports`): key `disaster:<id>:reports`, TTL 300s. Redis errors are logged and bypassed (never a 500); feed failure -> `502`. Responses carry `cached` and an `X-Cache: HIT|MISS` header. Update/delete invalidate the key.
- **WebSocket** (`/ws`): `Hub` guards its client set with a `sync.RWMutex`; each client has a buffered send channel and a write pump. Broadcasts never block: a full buffer evicts that client. Create and update emit `{"event":"disaster_updated","data":<disaster>}`.
- **Authorization**: `X-User-Role: admin` may do everything; `contributor` may read/create/update; anything else (missing, unknown) is a read-only viewer. Forbidden actions return `403`. `X-User-ID` (optional) populates `created_by`, defaulting to the role name.

## Geospatial search

`resources.location` is `GEOGRAPHY(POINT,4326)`, so distances are metres on the WGS84 spheroid, not degrees. Note `ST_MakePoint(lng, lat)` takes longitude first.

```sql
WITH origin AS (SELECT ST_SetSRID(ST_MakePoint($lng,$lat),4326)::geography AS pt)
SELECT r.*, ST_Distance(r.location, o.pt)/1000.0 AS distance_km
FROM resources r, origin o
WHERE (r.disaster_id = $id OR r.disaster_id IS NULL)   -- disaster-specific + global
  AND ST_DWithin(r.location, o.pt, $radius_km * 1000)  -- strict radius, GIST-index assisted
ORDER BY ST_Distance(r.location, o.pt);
```
`ST_DWithin` is used instead of `ST_Distance(...) < r` because it can use the GIST index. `lat`/`lng` default to the disaster's coordinates; `radius` defaults to 10 km (max 1000).

## API

| Method | Path | Role |
|---|---|---|
| POST | `/disasters` | contributor, admin |
| GET | `/disasters?tags=flood,fire&limit=&offset=` (any-match) | any |
| GET | `/disasters/{id}` | any |
| PATCH | `/disasters/{id}` | contributor, admin |
| DELETE | `/disasters/{id}` | admin |
| GET | `/disasters/{id}/resources?lat=&lng=&radius=` | any |
| GET | `/disasters/{id}/reports` | any |
| WS | `/ws`, GET `/healthz` | any |

```bash
curl -s -X POST localhost:8080/disasters -H 'X-User-Role: contributor' -H 'Content-Type: application/json' \
  -d '{"title":"Subway flooding","description":"Water main burst in Manhattan, NYC","tags":["flood"]}'
curl -s 'localhost:8080/disasters?tags=flood'
curl -s 'localhost:8080/disasters/11111111-1111-1111-1111-111111111111/resources?lat=40.7831&lng=-73.9712&radius=5'
curl -si localhost:8080/disasters/11111111-1111-1111-1111-111111111111/reports   # X-Cache: MISS, then HIT
curl -si -X DELETE -H 'X-User-Role: contributor' localhost:8080/disasters/<id>   # 403
npx wscat -c ws://localhost:8080/ws
```

## Trade-offs

Gazetteer geocoding stands in for a real geocoder; there are no migrations tool (schema lives in `seed.sql`); role headers are trusted, so put real auth (JWT) in front for production; WebSocket origin checking is permissive.
