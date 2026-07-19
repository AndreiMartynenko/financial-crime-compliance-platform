ALTER TABLE screening_schedules
    ADD COLUMN failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    ADD COLUMN lease_owner TEXT,
    ADD COLUMN lease_until TIMESTAMPTZ;
CREATE INDEX screening_schedules_claim_idx ON screening_schedules(next_run_at, lease_until) WHERE enabled;
