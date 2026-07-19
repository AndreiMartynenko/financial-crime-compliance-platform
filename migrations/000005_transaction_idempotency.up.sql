ALTER TABLE transactions ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

UPDATE transactions
SET idempotency_key = 'legacy:' || id::text
WHERE idempotency_key IS NULL;

ALTER TABLE transactions ALTER COLUMN idempotency_key SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS transactions_idempotency_key_unique
    ON transactions (idempotency_key);
