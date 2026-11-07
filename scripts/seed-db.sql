BEGIN;

INSERT INTO sessions (
    session_id,
    wallet_address,
    chain_id,
    locale
)
VALUES (
    'dev2-demo-session',
    '0x0000000000000000000000000000000000000001',
    43113,
    'vi'
)
ON CONFLICT (session_id) DO NOTHING;

-- Stop if this identifier already belongs to another wallet or chain.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM sessions
        WHERE session_id = 'dev2-demo-session'
          AND wallet_address =
              '0x0000000000000000000000000000000000000001'
          AND chain_id = 43113
    ) THEN
        RAISE EXCEPTION 'Demo session ownership mismatch';
    END IF;
END
$$;

-- Serialize repeated seed runs for this session.
SELECT session_id
FROM sessions
WHERE session_id = 'dev2-demo-session'
FOR UPDATE;

INSERT INTO messages (
    session_id,
    request_id,
    role,
    content
)
SELECT
    'dev2-demo-session',
    '11111111-1111-4111-8111-111111111111'::uuid,
    'user',
    'Đây là tin nhắn mẫu để kiểm tra lịch sử hội thoại.'
WHERE NOT EXISTS (
    SELECT 1
    FROM messages
    WHERE session_id = 'dev2-demo-session'
      AND request_id =
          '11111111-1111-4111-8111-111111111111'::uuid
      AND role = 'user'
);

INSERT INTO messages (
    session_id,
    request_id,
    role,
    content
)
SELECT
    'dev2-demo-session',
    '11111111-1111-4111-8111-111111111111'::uuid,
    'assistant',
    'Dữ liệu demo đã được lưu trong PostgreSQL. Đây không phải kết quả truy vấn blockchain.'
WHERE NOT EXISTS (
    SELECT 1
    FROM messages
    WHERE session_id = 'dev2-demo-session'
      AND request_id =
          '11111111-1111-4111-8111-111111111111'::uuid
      AND role = 'assistant'
);

COMMIT;