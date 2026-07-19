ALTER TABLE customers
    DROP CONSTRAINT IF EXISTS customers_status_check,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS status;
