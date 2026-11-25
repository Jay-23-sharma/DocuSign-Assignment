-- +goose Up
CREATE TABLE documents (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id bigint NOT NULL REFERENCES users (id),
    title text NOT NULL,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN (
        'draft', 'sent', 'viewed', 'signed', 'completed', 'declined', 'voided', 'expired'
    )),
    email_subject text,
    email_message text,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    completed_at timestamptz
);

CREATE TABLE document_files (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint NOT NULL REFERENCES documents (id),
    kind text NOT NULL CHECK (kind IN ('original', 'completed')),
    storage_key text NOT NULL UNIQUE,
    original_filename text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    page_count integer NOT NULL CHECK (page_count > 0),
    sha256 bytea NOT NULL CHECK (octet_length(sha256) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_id, kind)
);

CREATE TABLE recipients (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint NOT NULL REFERENCES documents (id),
    name text NOT NULL,
    email text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'viewed', 'signed', 'declined')),
    first_viewed_at timestamptz,
    signed_at timestamptz,
    declined_at timestamptz,
    UNIQUE (document_id, id)
);

CREATE TABLE document_fields (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint NOT NULL REFERENCES documents (id),
    recipient_id bigint NOT NULL,
    field_type text NOT NULL CHECK (field_type IN (
        'signature', 'initials', 'name', 'date', 'email', 'text', 'checkbox'
    )),
    page_number integer NOT NULL CHECK (page_number >= 1),
    x numeric NOT NULL CHECK (x >= 0),
    y numeric NOT NULL CHECK (y >= 0),
    width numeric NOT NULL CHECK (width > 0),
    height numeric NOT NULL CHECK (height > 0),
    required boolean NOT NULL DEFAULT true,
    text_value text,
    checked_value boolean,
    signature_method text CHECK (signature_method IN ('typed', 'drawn', 'uploaded')),
    signature_storage_key text,
    submitted_at timestamptz,
    CHECK (x + width <= 1),
    CHECK (y + height <= 1),
    FOREIGN KEY (document_id, recipient_id) REFERENCES recipients (document_id, id)
);

-- +goose Down
DROP TABLE document_fields;
DROP TABLE recipients;
DROP TABLE document_files;
DROP TABLE documents;
