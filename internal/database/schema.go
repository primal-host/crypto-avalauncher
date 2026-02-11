package database

const schema = `
CREATE TABLE IF NOT EXISTS hosts (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    ssh_addr    TEXT NOT NULL DEFAULT '',
    labels      JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'unknown',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS nodes (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    host_id       BIGINT NOT NULL REFERENCES hosts(id),
    image         TEXT NOT NULL DEFAULT 'avaplatform/avalanchego:latest',
    node_id       TEXT NOT NULL DEFAULT '',
    staking_cert  TEXT NOT NULL DEFAULT '',
    staking_key   TEXT NOT NULL DEFAULT '',
    container_id  TEXT NOT NULL DEFAULT '',
    http_port     INT NOT NULL DEFAULT 9650,
    staking_port  INT NOT NULL DEFAULT 9651,
    status        TEXT NOT NULL DEFAULT 'stopped',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS l1s (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    subnet_id      TEXT NOT NULL DEFAULT '',
    blockchain_id  TEXT NOT NULL DEFAULT '',
    vm             TEXT NOT NULL DEFAULT '',
    chain_config   JSONB NOT NULL DEFAULT '{}',
    status         TEXT NOT NULL DEFAULT 'pending',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS l1_validators (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    l1_id     BIGINT NOT NULL REFERENCES l1s(id),
    node_id   BIGINT NOT NULL REFERENCES nodes(id),
    weight    BIGINT NOT NULL DEFAULT 100,
    tx_id     TEXT NOT NULL DEFAULT '',
    UNIQUE(l1_id, node_id)
);

CREATE TABLE IF NOT EXISTS events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type  TEXT NOT NULL,
    target      TEXT NOT NULL DEFAULT '',
    message     TEXT NOT NULL DEFAULT '',
    details     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_events_created_at ON events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_target ON events (target);

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS network TEXT NOT NULL DEFAULT '';
`
