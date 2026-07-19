# Financial Crime Compliance Platform

A portfolio project demonstrating how AML/KYC domain requirements can be translated into an auditable Go backend.

## Current milestone: Maker-checker customer approval

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
- HS256 bearer-token authentication with issuer and expiry validation;
- role-based authorization for `analyst`, `reviewer` and `admin`;
- audit actors derived from the authenticated JWT subject rather than a caller-supplied identity header;
- customer submissions held in `pending_approval` until independent review;
- reviewer/admin approval and rejection actions;
- enforcement that the maker cannot review their own submission;
- transactional customer-state and audit-event updates for every review decision.

The in-memory repository remains available for fast API tests. The running API requires PostgreSQL and reads its connection string from `DATABASE_URL`.

## Run locally

```bash
go test ./...
docker compose up --build
```

The API applies the embedded SQL migration when it starts. For running the API outside Compose:

```bash
DATABASE_URL='postgres://financial_crime:local_development_only@localhost:5432/financial_crime?sslmode=disable' \
JWT_SECRET='replace-with-at-least-32-random-characters' \
go run ./cmd/api
```

Runtime environment variables:

| Variable | Required | Default |
|---|---|---|
| `DATABASE_URL` | yes | none |
| `HTTP_ADDR` | no | `:8080` |
| `HTTP_READ_HEADER_TIMEOUT` | no | `5s` |
| `HTTP_SHUTDOWN_TIMEOUT` | no | `10s` |
| `JWT_SECRET` | yes | none outside Compose |
| `JWT_ISSUER` | no | `financial-crime-compliance-platform` |
| `POSTGRES_PORT` | Compose only | `5432` |
| `API_PORT` | Compose only | `8080` |

`SIGINT` and `SIGTERM` trigger graceful HTTP shutdown before the database pool is closed.

Run the PostgreSQL rollback integration test against a disposable database:

```bash
TEST_DATABASE_URL='postgres://financial_crime:local_development_only@localhost:5432/financial_crime?sslmode=disable' go test ./internal/infrastructure/postgres
```

Protected routes expect an HS256 JWT with these claims:

```json
{
  "sub": "analyst@example.test",
  "role": "analyst",
  "iss": "financial-crime-compliance-platform",
  "exp": 1784451600
}
```

`JWT_SECRET` must contain at least 32 characters. Tokens with the `analyst` or `admin` role can submit customers. Tokens with the `reviewer` or `admin` role can approve or reject them, but the JWT subject must differ from the original maker. In a deployed environment, tokens should be issued by the configured identity provider. Compose uses an explicitly non-production development secret unless `JWT_SECRET` is supplied.

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

1. Transaction ingestion and versioned monitoring rules.
2. Alert investigation and case management.
3. Minimal analyst web interface.

## Important boundary

This is an educational portfolio system built with synthetic data. It is not production compliance software and does not determine whether a person or entity is sanctioned.
