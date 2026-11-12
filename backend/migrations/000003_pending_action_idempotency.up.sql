BEGIN;

ALTER TABLE pending_actions
    ADD COLUMN idempotency_key TEXT,
    ADD COLUMN request_fingerprint CHAR(64),
    ADD CONSTRAINT pending_actions_idempotency_fields_check CHECK (
        (idempotency_key IS NULL AND request_fingerprint IS NULL)
        OR (
            idempotency_key IS NOT NULL
            AND request_fingerprint IS NOT NULL
            AND idempotency_key ~ '^[A-Za-z0-9_-]{1,128}$'
            AND request_fingerprint ~ '^[0-9a-f]{64}$'
        )
    ),
    ADD CONSTRAINT pending_actions_session_idempotency_key_unique
        UNIQUE (session_id, idempotency_key);

COMMIT;