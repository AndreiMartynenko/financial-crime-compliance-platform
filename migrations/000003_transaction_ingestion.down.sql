DROP TABLE IF EXISTS transactions;
DELETE FROM audit_events WHERE aggregate_type = 'transaction';
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_aggregate_type_check;
ALTER TABLE audit_events DROP COLUMN IF EXISTS aggregate_type;
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_aggregate_id_fkey
    FOREIGN KEY (aggregate_id) REFERENCES customers(id);
