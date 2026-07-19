CREATE TABLE notifications (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    match_id UUID NOT NULL REFERENCES screening_matches(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('screening_match')),
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    read BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    read_by TEXT,
    read_at TIMESTAMPTZ
);
CREATE INDEX notifications_unread_created_idx ON notifications(read, created_at DESC);
