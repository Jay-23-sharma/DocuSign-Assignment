-- +goose Up
CREATE TABLE document_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint NOT NULL REFERENCES documents (id),
    recipient_id bigint,
    actor_user_id bigint REFERENCES users (id),
    event_type text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (document_id, recipient_id) REFERENCES recipients (document_id, id)
);

-- +goose Down
DROP TABLE document_events;
