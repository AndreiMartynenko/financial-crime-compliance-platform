DROP INDEX IF EXISTS screening_schedules_claim_idx;
ALTER TABLE screening_schedules DROP COLUMN IF EXISTS lease_until, DROP COLUMN IF EXISTS lease_owner, DROP COLUMN IF EXISTS failure_count;
