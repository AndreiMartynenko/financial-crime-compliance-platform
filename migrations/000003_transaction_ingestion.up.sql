ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_aggregate_id_fkey;

ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS aggregate_type TEXT NOT NULL DEFAULT 'customer';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'audit_events_aggregate_type_check'
    ) THEN
        ALTER TABLE audit_events ADD CONSTRAINT audit_events_aggregate_type_check
            CHECK (aggregate_type IN ('customer', 'transaction'));
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY,
    external_ref TEXT NOT NULL,
    customer_id UUID NOT NULL REFERENCES customers(id),
    direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency CHAR(3) NOT NULL,
    counterparty_country CHAR(2) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    ingested_by TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS transactions_external_ref_unique
    ON transactions (external_ref)
    WHERE external_ref <> '';

CREATE INDEX IF NOT EXISTS transactions_customer_occurred_idx
    ON transactions (customer_id, occurred_at DESC);
