ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_aggregate_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_aggregate_type_check
    CHECK (aggregate_type IN ('customer', 'transaction', 'alert', 'case'));

CREATE TABLE investigation_cases (
    id UUID PRIMARY KEY,
    alert_id UUID NOT NULL UNIQUE REFERENCES alerts(id),
    customer_id UUID NOT NULL REFERENCES customers(id),
    title TEXT NOT NULL,
    priority TEXT NOT NULL CHECK (priority IN ('low', 'medium', 'high')),
    status TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'resolved')),
    assigned_to TEXT,
    resolution TEXT,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    resolved_by TEXT,
    resolved_at TIMESTAMPTZ
);

CREATE INDEX investigation_cases_status_updated_idx ON investigation_cases (status, updated_at DESC);
CREATE INDEX investigation_cases_assignee_updated_idx ON investigation_cases (assigned_to, updated_at DESC);

CREATE TABLE case_comments (
    id UUID PRIMARY KEY,
    case_id UUID NOT NULL REFERENCES investigation_cases(id) ON DELETE CASCADE,
    author TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX case_comments_case_created_idx ON case_comments (case_id, created_at, id);
