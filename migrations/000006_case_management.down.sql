DROP TABLE IF EXISTS case_comments;
DROP TABLE IF EXISTS investigation_cases;
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_aggregate_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_aggregate_type_check
    CHECK (aggregate_type IN ('customer', 'transaction', 'alert'));
