ALTER TABLE notification_outbox
    ADD COLUMN channel TEXT NOT NULL DEFAULT 'webhook'
    CHECK (channel IN ('webhook', 'email'));

CREATE TABLE notification_preferences (
    actor_subject TEXT PRIMARY KEY,
    email_address TEXT NOT NULL DEFAULT '',
    email_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (NOT email_enabled OR email_address <> '')
);

CREATE INDEX notification_preferences_email_idx
    ON notification_preferences(email_enabled)
    WHERE email_enabled;
