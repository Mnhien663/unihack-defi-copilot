BEGIN;

ALTER TABLE pending_actions
    DROP CONSTRAINT pending_actions_session_idempotency_key_unique,
    DROP CONSTRAINT pending_actions_idempotency_fields_check,
    DROP COLUMN request_fingerprint,
    DROP COLUMN idempotency_key;

COMMIT;