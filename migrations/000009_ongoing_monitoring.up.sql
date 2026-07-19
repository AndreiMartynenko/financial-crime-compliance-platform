CREATE TABLE screening_schedules (
    customer_id UUID PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    interval_hours INTEGER NOT NULL CHECK (interval_hours BETWEEN 1 AND 8760),
    next_run_at TIMESTAMPTZ NOT NULL,
    last_run_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX screening_schedules_due_idx ON screening_schedules(next_run_at) WHERE enabled;
