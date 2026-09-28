-- Runs once on first database initialisation (docker-entrypoint-initdb.d).
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS disasters (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title         TEXT             NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description   TEXT             NOT NULL,
    location_name TEXT             NOT NULL,
    latitude      DOUBLE PRECISION NOT NULL CHECK (latitude  BETWEEN  -90 AND  90),
    longitude     DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    tags          TEXT[]           NOT NULL DEFAULT '{}',
    status        TEXT             NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active', 'monitoring', 'resolved')),
    created_by    TEXT             NOT NULL,
    created_at    TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ      NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS disasters_tags_gin   ON disasters USING GIN (tags);
CREATE INDEX IF NOT EXISTS disasters_created_at ON disasters (created_at DESC);

-- disaster_id NULL => a global resource visible to every disaster's search.
CREATE TABLE IF NOT EXISTS resources (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    disaster_id UUID REFERENCES disasters (id) ON DELETE CASCADE,
    name        TEXT                  NOT NULL,
    type        TEXT                  NOT NULL,
    capacity    INTEGER               NOT NULL DEFAULT 0 CHECK (capacity >= 0),
    location    GEOGRAPHY(POINT,4326) NOT NULL,
    created_at  TIMESTAMPTZ           NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS resources_location_gist ON resources USING GIST (location);
CREATE INDEX IF NOT EXISTS resources_disaster_id   ON resources (disaster_id);

-- Seed data (fixed UUIDs so the README curl examples work verbatim).
INSERT INTO disasters (id, title, description, location_name, latitude, longitude, tags, status, created_by) VALUES
 ('11111111-1111-1111-1111-111111111111', 'Manhattan Flash Flood',
  'Flash flooding in Manhattan, NYC has submerged subway entrances and lower streets.',
  'Manhattan, NYC', 40.7831, -73.9712, ARRAY['flood','urban'], 'active', 'admin'),
 ('22222222-2222-2222-2222-222222222222', 'San Francisco Wildfire',
  'A fast-moving wildfire near San Francisco is threatening residential areas.',
  'San Francisco, CA', 37.7749, -122.4194, ARRAY['fire','evacuation'], 'monitoring', 'contributor')
ON CONFLICT (id) DO NOTHING;

INSERT INTO resources (disaster_id, name, type, capacity, location) VALUES
 ('11111111-1111-1111-1111-111111111111', 'Columbia Presbyterian Field Hospital', 'medical', 120, ST_SetSRID(ST_MakePoint(-73.9442, 40.8402), 4326)::geography),
 ('11111111-1111-1111-1111-111111111111', 'Central Park Relief Shelter',          'shelter', 400, ST_SetSRID(ST_MakePoint(-73.9654, 40.7829), 4326)::geography),
 ('11111111-1111-1111-1111-111111111111', 'Midtown Supply Depot',                 'supplies', 900, ST_SetSRID(ST_MakePoint(-73.9857, 40.7484), 4326)::geography),
 ('11111111-1111-1111-1111-111111111111', 'Brooklyn Navy Yard Staging Area',      'staging', 250, ST_SetSRID(ST_MakePoint(-73.9712, 40.7000), 4326)::geography),
 ('11111111-1111-1111-1111-111111111111', 'Newark Airport Logistics Hub',         'logistics', 1500, ST_SetSRID(ST_MakePoint(-74.1745, 40.6895), 4326)::geography),
 ('22222222-2222-2222-2222-222222222222', 'Golden Gate Park Evacuation Center',   'shelter', 600, ST_SetSRID(ST_MakePoint(-122.4862, 37.7694), 4326)::geography),
 ('22222222-2222-2222-2222-222222222222', 'SF General Trauma Unit',               'medical', 200, ST_SetSRID(ST_MakePoint(-122.4056, 37.7559), 4326)::geography),
 (NULL, 'Regional Red Cross Warehouse (NY)',                                      'supplies', 2000, ST_SetSRID(ST_MakePoint(-73.9903, 40.7580), 4326)::geography);
