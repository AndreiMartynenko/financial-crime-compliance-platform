ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT 'legacy-migration',
    ADD COLUMN IF NOT EXISTS reviewed_by TEXT,
    ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'customers_status_check'
    ) THEN
        ALTER TABLE customers ADD CONSTRAINT customers_status_check
            CHECK (status IN ('pending_approval', 'active', 'rejected'));
    END IF;
END
$$;
