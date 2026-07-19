CREATE TABLE customer_due_diligence (
 customer_id UUID PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
 source_of_wealth TEXT NOT NULL, business_purpose TEXT NOT NULL,
 expected_monthly_volume_minor BIGINT NOT NULL CHECK(expected_monthly_volume_minor>=0),
 currency TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN('incomplete','in_review','complete')),
 next_review_at TIMESTAMPTZ, updated_by TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE beneficial_owners (
 id UUID PRIMARY KEY, customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 full_name TEXT NOT NULL, ownership_percent INTEGER NOT NULL CHECK(ownership_percent BETWEEN 1 AND 100),
 country_code CHAR(2) NOT NULL, pep BOOLEAN NOT NULL, created_by TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX beneficial_owners_customer_idx ON beneficial_owners(customer_id,created_at);
CREATE TABLE kyc_documents (
 id UUID PRIMARY KEY, customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 document_type TEXT NOT NULL, reference TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN('pending','verified','rejected')),
 expires_at TIMESTAMPTZ, created_by TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
 verified_by TEXT, verified_at TIMESTAMPTZ
);
CREATE INDEX kyc_documents_customer_idx ON kyc_documents(customer_id,created_at);
