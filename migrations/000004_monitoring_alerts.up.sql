ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_aggregate_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_aggregate_type_check
    CHECK (aggregate_type IN ('customer', 'transaction', 'alert'));

CREATE TABLE IF NOT EXISTS alerts (
    id UUID PRIMARY KEY,
    transaction_id UUID NOT NULL REFERENCES transactions(id),
    customer_id UUID NOT NULL REFERENCES customers(id),
    rule_code TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('medium', 'high')),
    status TEXT NOT NULL CHECK (status IN ('open', 'closed')),
    reason_code TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    closed_by TEXT,
    closure_reason TEXT,
    UNIQUE (transaction_id, rule_code, rule_version)
);

CREATE INDEX IF NOT EXISTS alerts_status_created_idx
    ON alerts (status, created_at DESC);

CREATE INDEX IF NOT EXISTS alerts_customer_created_idx
    ON alerts (customer_id, created_at DESC);
