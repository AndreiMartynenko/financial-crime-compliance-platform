CREATE TABLE IF NOT EXISTS customers (
    id UUID PRIMARY KEY,
    external_ref TEXT NOT NULL,
    customer_type TEXT NOT NULL CHECK (customer_type IN ('individual', 'company')),
    legal_name TEXT NOT NULL,
    country_code CHAR(2) NOT NULL,
    risk_factors JSONB NOT NULL,
    risk_score INTEGER NOT NULL CHECK (risk_score >= 0),
    risk_rating TEXT NOT NULL CHECK (risk_rating IN ('low', 'medium', 'high')),
    due_diligence TEXT NOT NULL CHECK (due_diligence IN ('standard', 'enhanced', 'blocked_pending_review')),
    risk_reasons JSONB NOT NULL,
    risk_rule_version TEXT NOT NULL,
    risk_assessed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS customers_external_ref_unique
    ON customers (external_ref)
    WHERE external_ref <> '';

CREATE TABLE IF NOT EXISTS audit_events (
    id UUID PRIMARY KEY,
    aggregate_id UUID NOT NULL REFERENCES customers(id),
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS audit_events_aggregate_time_idx
    ON audit_events (aggregate_id, occurred_at);
