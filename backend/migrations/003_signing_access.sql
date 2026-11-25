-- +goose Up
CREATE TABLE signing_links (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recipient_id bigint NOT NULL REFERENCES recipients (id),
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE signing_sessions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    signing_link_id bigint NOT NULL REFERENCES signing_links (id),
    session_token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    email_verified_at timestamptz
);

CREATE TABLE signing_email_codes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id bigint NOT NULL REFERENCES signing_sessions (id),
    code_digest bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    used_at timestamptz
);

-- +goose Down
DROP TABLE signing_email_codes;
DROP TABLE signing_sessions;
DROP TABLE signing_links;
