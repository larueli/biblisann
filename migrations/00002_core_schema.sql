-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

-- unaccent() is only STABLE; an immutable wrapper is required for indexes.
-- +goose StatementBegin
CREATE FUNCTION immutable_unaccent(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
    AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;
-- +goose StatementEnd

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text        NOT NULL CHECK (length(email) BETWEEN 3 AND 320),
    display_name  text        NOT NULL DEFAULT '' CHECK (length(display_name) <= 200),
    password_hash text,
    is_admin      boolean     NOT NULL DEFAULT false,
    locale        text        NOT NULL DEFAULT '',
    settings      jsonb       NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    disabled_at   timestamptz
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- Sessions store only the SHA-256 of the cookie token.
CREATE TABLE sessions (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_seen  timestamptz NOT NULL DEFAULT now(),
    user_agent text        NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE libraries (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    settings   jsonb       NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- role: read, edit, admin. The creator is an admin member.
CREATE TABLE library_members (
    library_id uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('read', 'edit', 'admin')),
    PRIMARY KEY (library_id, user_id)
);
CREATE INDEX library_members_user_idx ON library_members (user_id);

-- Global catalog (ADR 0008).
CREATE TABLE works (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    title      text        NOT NULL,
    authors    text[]      NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE covers (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    sha256       bytea       NOT NULL UNIQUE,
    width        int         NOT NULL,
    height       int         NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE editions (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    work_id        uuid        NOT NULL REFERENCES works (id),
    isbn13         text        UNIQUE CHECK (isbn13 ~ '^[0-9]{13}$'),
    isbn10         text        CHECK (isbn10 ~ '^[0-9]{9}[0-9X]$'),
    ean            text,
    title          text        NOT NULL CHECK (length(title) BETWEEN 1 AND 1000),
    subtitle       text        NOT NULL DEFAULT '',
    authors        text[]      NOT NULL DEFAULT '{}',
    publisher      text        NOT NULL DEFAULT '',
    published_year int,
    language       text        NOT NULL DEFAULT '',
    pages          int,
    description    text        NOT NULL DEFAULT '',
    series_name    text        NOT NULL DEFAULT '',
    series_position double precision,
    cover_id       uuid        REFERENCES covers (id),
    provenance     jsonb       NOT NULL DEFAULT '{}',
    locked_fields  text[]      NOT NULL DEFAULT '{}',
    search_text    text        NOT NULL DEFAULT '',
    created_by     uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX editions_work_idx ON editions (work_id);
CREATE INDEX editions_isbn10_idx ON editions (isbn10);
CREATE INDEX editions_search_trgm_idx ON editions USING gin (search_text gin_trgm_ops);
CREATE INDEX editions_search_fts_idx ON editions USING gin (to_tsvector('simple', search_text));

CREATE TABLE places (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id uuid        NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    parent_id  uuid        REFERENCES places (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    kind       text        NOT NULL DEFAULT 'place',
    position   int         NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX places_library_idx ON places (library_id);

CREATE TABLE instances (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id uuid        NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    edition_id uuid        NOT NULL REFERENCES editions (id),
    place_id   uuid        REFERENCES places (id) ON DELETE SET NULL,
    kind       text        NOT NULL DEFAULT 'print' CHECK (kind IN ('print', 'ebook')),
    condition  text        NOT NULL DEFAULT 'ok',
    notes      text        NOT NULL DEFAULT '' CHECK (length(notes) <= 10000),
    acquired_on date,
    created_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX instances_library_idx ON instances (library_id) WHERE deleted_at IS NULL;
CREATE INDEX instances_edition_idx ON instances (edition_id);

-- Change log feeding the offline index sync: cursor = seq, deletes are tombstones.
CREATE TABLE changes (
    seq        bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_id uuid        NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    entity     text        NOT NULL,
    entity_id  uuid        NOT NULL,
    op         text        NOT NULL CHECK (op IN ('upsert', 'delete')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX changes_library_seq_idx ON changes (library_id, seq);

-- Replay protection for writes the offline client may resend.
CREATE TABLE idempotency_keys (
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key             text        NOT NULL CHECK (length(key) BETWEEN 8 AND 128),
    request_hash    bytea       NOT NULL,
    response_status int         NOT NULL,
    response_body   jsonb       NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

CREATE TABLE provider_cache (
    provider   text        NOT NULL,
    key        text        NOT NULL,
    found      boolean     NOT NULL,
    payload    jsonb       NOT NULL DEFAULT '{}',
    fetched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, key)
);
