BEGIN;

CREATE TABLE sessions (
    session_id VARCHAR(128) PRIMARY KEY,
    wallet_address VARCHAR(42) NOT NULL,
    chain_id BIGINT NOT NULL,
    locale VARCHAR(10) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT sessions_chain_id_check
        CHECK (chain_id = 43113),

    CONSTRAINT sessions_locale_check
        CHECK (locale IN ('vi', 'en'))
);

CREATE INDEX idx_sessions_wallet_created_at
    ON sessions (wallet_address, created_at DESC);


CREATE TABLE messages (
    message_id BIGSERIAL PRIMARY KEY,
    session_id VARCHAR(128) NOT NULL,
    request_id UUID NOT NULL,
    role VARCHAR(20) NOT NULL,
    content TEXT NOT NULL,
    tool_call_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT messages_session_fk
        FOREIGN KEY (session_id)
        REFERENCES sessions(session_id)
        ON DELETE CASCADE,

    CONSTRAINT messages_role_check
        CHECK (role IN ('system', 'user', 'assistant', 'tool'))
);

CREATE INDEX idx_messages_session_created_at
    ON messages (session_id, created_at, message_id);

CREATE INDEX idx_messages_request_id
    ON messages (request_id);


CREATE TABLE tool_calls (
    tool_call_record_id BIGSERIAL PRIMARY KEY,
    session_id VARCHAR(128) NOT NULL,
    request_id UUID NOT NULL,
    tool_call_id VARCHAR(128) NOT NULL,
    tool_name VARCHAR(100) NOT NULL,
    arguments JSONB NOT NULL,
    result JSONB,
    status VARCHAR(20) NOT NULL,
    error_code VARCHAR(100),
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    CONSTRAINT tool_calls_session_fk
        FOREIGN KEY (session_id)
        REFERENCES sessions(session_id)
        ON DELETE CASCADE,

    CONSTRAINT tool_calls_status_check
        CHECK (
            status IN (
                'requested',
                'allowed',
                'denied',
                'succeeded',
                'failed'
            )
        ),

    CONSTRAINT tool_calls_session_call_unique
        UNIQUE (session_id, tool_call_id),

    CONSTRAINT tool_calls_arguments_object_check
        CHECK (jsonb_typeof(arguments) = 'object'),

    CONSTRAINT tool_calls_result_object_check
        CHECK (
            result IS NULL
            OR jsonb_typeof(result) = 'object'
        )
);

CREATE INDEX idx_tool_calls_session_started_at
    ON tool_calls (session_id, started_at DESC);

CREATE INDEX idx_tool_calls_request_id
    ON tool_calls (request_id);

CREATE INDEX idx_tool_calls_status
    ON tool_calls (status);


CREATE TABLE pending_actions (
    pending_action_id UUID PRIMARY KEY,
    session_id VARCHAR(128) NOT NULL,
    request_id UUID NOT NULL,
    tool_call_id VARCHAR(128) NOT NULL,
    wallet_address VARCHAR(42) NOT NULL,
    chain_id BIGINT NOT NULL,
    tool_name VARCHAR(100) NOT NULL,
    normalized_intent_hash CHAR(64) NOT NULL,
    intent JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,
    transaction_hash VARCHAR(66),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT pending_actions_session_fk
        FOREIGN KEY (session_id)
        REFERENCES sessions(session_id)
        ON DELETE CASCADE,

    CONSTRAINT pending_actions_chain_id_check
        CHECK (chain_id = 43113),

    CONSTRAINT pending_actions_status_check
        CHECK (
            status IN (
                'pending',
                'confirmed',
                'rejected',
                'expired',
                'submitted',
                'mined',
                'failed'
            )
        ),

    CONSTRAINT pending_actions_intent_object_check
        CHECK (jsonb_typeof(intent) = 'object'),

    CONSTRAINT pending_actions_intent_hash_check
        CHECK (
            normalized_intent_hash
            ~ '^[0-9a-f]{64}$'
        ),

    CONSTRAINT pending_actions_expiry_check
        CHECK (expires_at > created_at),

    CONSTRAINT pending_actions_session_hash_unique
        UNIQUE (session_id, normalized_intent_hash)
);

CREATE INDEX idx_pending_actions_wallet_status
    ON pending_actions (wallet_address, status);

CREATE INDEX idx_pending_actions_session_created_at
    ON pending_actions (session_id, created_at DESC);

CREATE INDEX idx_pending_actions_expires_at
    ON pending_actions (expires_at)
    WHERE status = 'pending';


CREATE TABLE eval_runs (
    eval_run_id BIGSERIAL PRIMARY KEY,
    case_id VARCHAR(128) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    model VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL,
    request_id UUID,
    expected JSONB NOT NULL,
    actual JSONB,
    latency_ms BIGINT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT eval_runs_status_check
        CHECK (status IN ('passed', 'failed', 'error')),

    CONSTRAINT eval_runs_expected_object_check
        CHECK (jsonb_typeof(expected) = 'object'),

    CONSTRAINT eval_runs_actual_object_check
        CHECK (
            actual IS NULL
            OR jsonb_typeof(actual) = 'object'
        ),

    CONSTRAINT eval_runs_latency_check
        CHECK (latency_ms IS NULL OR latency_ms >= 0)
);

CREATE INDEX idx_eval_runs_case_created_at
    ON eval_runs (case_id, created_at DESC);

CREATE INDEX idx_eval_runs_status_created_at
    ON eval_runs (status, created_at DESC);

COMMIT;