BEGIN;

ALTER TABLE tool_calls
    DROP CONSTRAINT tool_calls_session_call_unique;

ALTER TABLE tool_calls
    ADD CONSTRAINT tool_calls_request_call_unique
    UNIQUE (request_id, tool_call_id);

COMMIT;