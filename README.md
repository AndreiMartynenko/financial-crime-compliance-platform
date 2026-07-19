# Financial Crime Compliance Platform

A portfolio project demonstrating how AML/KYC domain requirements can be translated into an auditable Go backend.

## Current milestone: Durable ongoing-monitoring jobs

The first vertical slice accepts a customer, evaluates explicit risk factors, assigns a reproducible risk rating and due-diligence route, and records an audit event.

Implemented:

- individual and company onboarding;
- versioned, explainable customer-risk rules;
- low, medium and high risk ratings;
- standard and enhanced due-diligence routing;
- blocking of potential sanctions matches pending human review;
- actor-attributed audit events;
- HTTP validation and structured errors;
- OpenAPI 3.1 contract in `docs/openapi.yaml`;
- unit and API-level tests;
- PostgreSQL-backed runtime and Docker packaging;
- atomic customer and audit-event writes in one database transaction;
- embedded, idempotent schema migration at startup;
- OIDC Authorization Code + PKCE login with RS256/JWKS token validation;
- role-based authorization for `analyst`, `reviewer` and `admin`;
- audit actors derived from the authenticated JWT subject rather than a caller-supplied identity header;
- customer submissions held in `pending_approval` until independent review;
- reviewer/admin approval and rejection actions;
- enforcement that the maker cannot review their own submission;
- transactional customer-state and audit-event updates for every review decision;
- ingestion of inbound and outbound customer transactions;
- integer minor-unit monetary amounts without floating-point rounding;
- enforcement that transactions belong to active customers;
- atomic transaction and `transaction.ingested` audit-event persistence;
- deterministic transaction-monitoring rules with stored rule versions;
- explainable alerts with rule and reason codes;
- atomic transaction, alert, and audit-event persistence;
- role-protected alert listing and closure workflow;
- migration ledger protected by a PostgreSQL advisory lock;
- separate liveness and database-backed readiness probes;
- GitHub Actions verification with PostgreSQL integration tests, race detection, vet and container build;
- concurrency-safe idempotent transaction ingestion with replay and payload-conflict detection;
- cursor-paginated read APIs for customers, transactions, alerts and audit trails;
- responsive React operations portal delivered through a same-origin Nginx reverse proxy;
- role-aware customer onboarding with explicit compliance risk-factor capture;
- active-customer transaction entry with precise major-to-minor unit conversion;
- immediate transaction-monitoring result feedback and refreshed alert queues;
- alert-linked investigation cases with priority, assignment and lifecycle status;
- investigator comments and an immutable case audit timeline;
- atomic case resolution, linked-alert closure and dual audit-event persistence;
- a role-aware investigation workspace in the React operations portal;
- a unified customer activity stream spanning customer, transaction, alert and case events;
- Customer 360 views with explainable risk reasons, linked work and transaction history;
- a filterable audit explorer for reconstructing the complete customer lifecycle;
- structured CDD profiles with source of wealth, business purpose and expected activity;
- beneficial-owner capture including ownership percentage, country and PEP indicator;
- KYC document evidence with independent reviewer verification or rejection;
- periodic review scheduling and a dedicated role-aware KYC/CDD workspace.
- provider-independent customer and beneficial-owner screening;
- a deterministic local demo provider for sanctions, PEP and adverse-media candidates;
- transactional persistence of screening runs, potential matches and audit events;
- reviewer/admin match confirmation and false-positive disposition with mandatory rationale;
- an immutable screening history and role-aware screening workspace.
- configurable recurring screening schedules per customer;
- an in-process background worker for due sanctions, PEP and adverse-media re-screening;
- persisted next/last-run state and operational error visibility;
- audited enable, pause and cadence changes in the screening workspace.
- atomic PostgreSQL job claiming with `FOR UPDATE SKIP LOCKED`;
- expiring worker leases for crash recovery and safe multi-instance processing;
- exponential retry backoff with a 24-hour cap and persisted failure counters.

The in-memory repository remains available for fast API tests. The running API requires PostgreSQL and reads its connection string from `DATABASE_URL`.

## Run locally

```bash
go test ./...
docker compose up --build
```

Open the analyst website at [http://localhost:3000](http://localhost:3000). The API remains available at `http://localhost:8080`; browser requests use `/api` through the website reverse proxy.

Analysts and administrators can register customers from **Customers → New customer**. After an independent reviewer activates a customer, analysts and administrators can ingest and monitor payments from **Customers → Add transaction**. Reviewer-only controls are hidden from unauthorized roles, while the API remains the authoritative authorization boundary.

The API applies the embedded SQL migration when it starts. For running the API outside Compose:

```bash
DATABASE_URL='postgres://financial_crime:local_development_only@localhost:5432/financial_crime?sslmode=disable' \
JWT_ISSUER='http://localhost:8081/realms/fccp' \
JWT_JWKS_URL='http://localhost:8081/realms/fccp/protocol/openid-connect/certs' \
go run ./cmd/api
```

Runtime environment variables:

| Variable | Required | Default |
|---|---|---|
| `DATABASE_URL` | yes | none |
| `HTTP_ADDR` | no | `:8080` |
| `HTTP_READ_HEADER_TIMEOUT` | no | `5s` |
| `HTTP_SHUTDOWN_TIMEOUT` | no | `10s` |
| `SCREENING_WORKER_INTERVAL` | no | `1m` |
| `SCREENING_JOB_LEASE` | no | `5m` |
| `JWT_ISSUER` | yes | Keycloak realm URL in Compose |
| `JWT_JWKS_URL` | yes | internal Keycloak JWKS URL in Compose |
| `JWT_AUTHORIZED_PARTY` | no | `fccp-web` |
| `POSTGRES_PORT` | Compose only | `5432` |
| `API_PORT` | Compose only | `8080` |
| `WEB_PORT` | Compose only | `3000` |
| `KEYCLOAK_PORT` | Compose only | `8081` |

`SIGINT` and `SIGTERM` trigger graceful HTTP shutdown before the database pool is closed.

`GET /healthz` is a process liveness probe. `GET /readyz` verifies that PostgreSQL is reachable and returns `503` when the API should be removed from service. Database migrations are recorded in `schema_migrations` and serialized with a PostgreSQL advisory transaction lock, so concurrent application starts cannot apply the same migration twice.

Run the PostgreSQL persistence and rollback integration tests against a disposable database:

```bash
TEST_DATABASE_URL='postgres://financial_crime:local_development_only@localhost:5432/financial_crime?sslmode=disable' go test ./internal/infrastructure/postgres
```

Protected routes expect an identity-provider RS256 JWT. Roles are read from standard Keycloak realm access claims:

```json
{
  "sub": "analyst@example.test",
  "azp": "fccp-web",
  "iss": "http://localhost:8081/realms/fccp",
  "realm_access": {"roles": ["analyst"]},
  "exp": 1784451600
}
```

The website uses Authorization Code with PKCE and never handles user passwords. The API fetches rotating public keys from JWKS and validates the RS256 signature, issuer, expiry, not-before time and authorized party. Demo-only local users are `analyst / analyst-demo-only`, `reviewer / reviewer-demo-only`, and `administrator / admin-demo-only`; replace the imported realm and credentials outside local development.

Create a high-risk company using a signed token:

```bash
curl -i http://localhost:8080/v1/customers \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $JWT" \
  -d '{
    "external_ref": "CRM-1001",
    "type": "company",
    "legal_name": "Example Payments Ltd",
    "country_code": "GB",
    "risk_factors": {
      "country_risk": "high",
      "pep": true,
      "sanctions_potential_match": false,
      "high_risk_industry": false,
      "complex_ownership": true,
      "source_of_funds_verified": true
    }
  }'
```

The response has `status: "pending_approval"`. A different authenticated user reviews it:

```bash
curl -i -X POST http://localhost:8080/v1/customers/$CUSTOMER_ID/approve \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $REVIEWER_JWT" \
  -d '{"reason":"Identity and ownership evidence verified"}'
```

Use `/reject` instead of `/approve` to reject a pending submission. Approval/rejection and its audit event are committed in one PostgreSQL transaction.

Ingest a transaction for an active customer:

```bash
curl -i http://localhost:8080/v1/transactions \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $JWT" \
  -H 'Idempotency-Key: payment-PAY-1001' \
  -d '{
    "external_ref": "PAY-1001",
    "customer_id": "'$CUSTOMER_ID'",
    "direction": "outbound",
    "amount_minor": 1250000,
    "currency": "GBP",
    "counterparty_country": "IR",
    "occurred_at": "2026-07-19T12:00:00Z"
  }'
```

`amount_minor` is expressed in the currency's minor unit—for example, `125050` GBP represents GBP 1,250.50. The response includes both the transaction and any alerts raised. Transaction, alerts, and audit events share one PostgreSQL transaction.

Retrying the same request with the same `Idempotency-Key` returns the original response with HTTP `200` and `Idempotency-Replayed: true`; it does not create another transaction, alert, or audit event. Reusing the key with a different payload returns HTTP `409`. PostgreSQL advisory locks serialize concurrent requests for the same key, while a unique index provides a second database-level guarantee.

## Transaction-monitoring rules

Rules are deterministic and store `transaction-monitoring-v1` on every alert, so historical decisions remain explainable after future rule changes.

| Rule | Trigger | Reason code |
|---|---|---|
| `large_transaction` | Amount is at least 1,000,000 minor units | `amount_threshold_exceeded` |
| `high_risk_counterparty_country` | Counterparty country is in the configured product-risk list | `counterparty_country_high_risk` |

These thresholds and country classifications are synthetic product assumptions for this educational demo, not legal or sanctions determinations.

List open alerts and close one after review:

```bash
curl -s 'http://localhost:8080/v1/alerts?status=open' \
  -H "Authorization: Bearer $JWT"

curl -i -X POST http://localhost:8080/v1/alerts/$ALERT_ID/close \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $REVIEWER_JWT" \
  -d '{"reason":"Reviewed and explained by expected customer activity"}'
```

## Risk model

The model is deliberately deterministic and explainable. Each assessment stores the rule version and individual reason codes. The example weights are product-design assumptions for this portfolio project, not legal advice or a production AML policy.

| Factor | Points |
|---|---:|
| Medium-risk country | 15 |
| High-risk country | 35 |
| PEP | 35 |
| High-risk industry | 20 |
| Complex ownership | 20 |
| Source of funds not verified | 20 |
| Potential sanctions match | 100 and block pending review |

Scores below 20 are low risk, 20-49 medium risk, and 50 or above high risk. A potential sanctions match always creates a high-risk, blocked-pending-review result.

## Planned milestones

1. Production-grade external screening-provider adapter and notification integrations.
2. Prometheus metrics, distributed tracing and alerting dashboards.
3. Deployment hardening, secrets management, backups and disaster-recovery procedures.

## Important boundary

This is an educational portfolio system built with synthetic data. It is not production compliance software and does not determine whether a person or entity is sanctioned.
