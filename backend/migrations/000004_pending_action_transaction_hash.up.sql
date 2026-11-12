BEGIN;

CREATE UNIQUE INDEX pending_actions_chain_tx_hash_unique
    ON pending_actions (chain_id, lower(transaction_hash))
    WHERE transaction_hash IS NOT NULL;

COMMIT;