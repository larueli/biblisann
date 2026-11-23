-- +goose Up

-- ---- sharing: groups, custom permission profiles
CREATE TABLE groups (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    external_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX groups_name_key ON groups (lower(name));
CREATE TABLE group_members (
    group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user_idx ON group_members (user_id);

-- A profile is a named set of permissions defined per library.
CREATE TABLE permission_profiles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id  uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    permissions text[] NOT NULL,
    UNIQUE (library_id, name)
);

ALTER TABLE library_members DROP CONSTRAINT library_members_role_check;
ALTER TABLE library_members ADD CONSTRAINT library_members_role_check CHECK (role IN ('read', 'edit', 'admin', 'custom'));
ALTER TABLE library_members ADD COLUMN profile_id uuid REFERENCES permission_profiles (id) ON DELETE CASCADE;
ALTER TABLE library_members ADD CONSTRAINT library_members_profile_check CHECK ((role = 'custom') = (profile_id IS NOT NULL));

CREATE TABLE library_group_shares (
    library_id uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    group_id   uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('read', 'edit', 'admin', 'custom')),
    profile_id uuid REFERENCES permission_profiles (id) ON DELETE CASCADE,
    PRIMARY KEY (library_id, group_id),
    CHECK ((role = 'custom') = (profile_id IS NOT NULL))
);

-- ---- reading status and grades (per user, per work)
CREATE TABLE reading_states (
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    work_id     uuid NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    status      text NOT NULL CHECK (status IN ('to_read', 'reading', 'read', 'abandoned')),
    started_on  date,
    finished_on date,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, work_id)
);
CREATE INDEX reading_states_status_idx ON reading_states (user_id, status);

CREATE TABLE grades (
    user_id    uuid     NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    work_id    uuid     NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    grade      smallint CHECK (grade BETWEEN 1 AND 10),
    comment    text     NOT NULL DEFAULT '' CHECK (length(comment) <= 10000),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, work_id)
);

-- Grades from outside (critics, providers): shown separately from local grades.
CREATE TABLE external_grades (
    work_id    uuid NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    source     text NOT NULL,
    grade      real NOT NULL,
    scale      real NOT NULL DEFAULT 10,
    votes      int,
    url        text NOT NULL DEFAULT '',
    fetched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (work_id, source)
);

-- ---- lists
CREATE TABLE lists (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid        REFERENCES users (id) ON DELETE CASCADE,
    library_id uuid        REFERENCES libraries (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    kind       text        NOT NULL DEFAULT 'custom' CHECK (kind IN ('custom', 'wishlist')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((owner_id IS NULL) <> (library_id IS NULL))
);
CREATE UNIQUE INDEX lists_one_wishlist_idx ON lists (owner_id) WHERE kind = 'wishlist';
CREATE INDEX lists_library_idx ON lists (library_id);

CREATE TABLE list_items (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    list_id         uuid        NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    edition_id      uuid        REFERENCES editions (id) ON DELETE SET NULL,
    title           text        NOT NULL CHECK (length(title) BETWEEN 1 AND 1000),
    authors         text[]      NOT NULL DEFAULT '{}',
    isbn13          text,
    series_name     text        NOT NULL DEFAULT '',
    series_position double precision,
    note            text        NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    added_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX list_items_list_idx ON list_items (list_id, added_at);

-- ---- loans
CREATE TABLE contacts (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name     text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    email    text NOT NULL DEFAULT '',
    notes    text NOT NULL DEFAULT ''
);
CREATE INDEX contacts_owner_idx ON contacts (owner_id);

-- direction lent: one of my copies goes to a contact. borrowed: a book (or a library item) I
-- must give back; it has no instance of mine.
CREATE TABLE loans (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id   uuid        NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    owner_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    direction    text        NOT NULL CHECK (direction IN ('lent', 'borrowed')),
    instance_id  uuid        REFERENCES instances (id) ON DELETE SET NULL,
    contact_id   uuid        REFERENCES contacts (id) ON DELETE SET NULL,
    counterparty text        NOT NULL DEFAULT '' CHECK (length(counterparty) <= 200),
    title        text        NOT NULL CHECK (length(title) BETWEEN 1 AND 1000),
    lent_on      date        NOT NULL DEFAULT current_date,
    due_on       date,
    returned_on  date,
    notes        text        NOT NULL DEFAULT '' CHECK (length(notes) <= 5000),
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK ((direction = 'lent' AND instance_id IS NOT NULL) OR direction = 'borrowed')
);
CREATE INDEX loans_open_idx ON loans (owner_id) WHERE returned_on IS NULL;
-- A copy can only be out once at a time.
CREATE UNIQUE INDEX loans_one_open_per_instance_idx ON loans (instance_id) WHERE returned_on IS NULL AND instance_id IS NOT NULL;

CREATE TABLE loan_notifications (
    loan_id uuid NOT NULL REFERENCES loans (id) ON DELETE CASCADE,
    kind    text NOT NULL, -- due_soon, due_today, overdue
    bucket  int  NOT NULL DEFAULT 0, -- overdue repeats weekly: week number
    sent_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (loan_id, kind, bucket)
);

-- ---- notification channels (where a user wants to be notified)
CREATE TABLE notification_channels (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('email', 'webpush', 'webhook', 'ntfy')),
    target     text NOT NULL CHECK (length(target) <= 2000), -- address, push endpoint, URL
    config     jsonb NOT NULL DEFAULT '{}',                  -- keys for webpush, secret for webhook
    enabled    boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_channels_user_idx ON notification_channels (user_id);

-- ---- tokens: personal access tokens (scoped, optionally limited to libraries) and ICS feed tokens
CREATE TABLE access_tokens (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    kind        text        NOT NULL DEFAULT 'pat' CHECK (kind IN ('pat', 'feed')),
    token_hash  bytea       NOT NULL UNIQUE,
    scopes      text[]      NOT NULL DEFAULT '{}',
    library_ids uuid[],     -- NULL: every library the user can access
    expires_at  timestamptz,
    last_used   timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX access_tokens_user_idx ON access_tokens (user_id);

-- Every (user, library) pair with access, directly or through a group.
CREATE VIEW user_library_access AS
    SELECT user_id, library_id FROM library_members
    UNION
    SELECT gm.user_id, s.library_id FROM library_group_shares s JOIN group_members gm ON gm.group_id = s.group_id;
