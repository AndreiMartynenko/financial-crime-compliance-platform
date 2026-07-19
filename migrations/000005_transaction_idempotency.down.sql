DROP INDEX IF EXISTS transactions_idempotency_key_unique;
ALTER TABLE transactions DROP COLUMN IF EXISTS idempotency_key;
