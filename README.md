# Financial Crime Compliance Platform

A portfolio project demonstrating how AML/KYC domain requirements can be translated into an auditable Go backend.

## Current milestone: Customer Risk Assessment and EDD Workflow

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
- initial PostgreSQL schema and Docker packaging.

The running API currently uses an in-memory repository. PostgreSQL persistence is the next implementation step; the migration is included to make the intended storage model reviewable.

## Run locally

```bash
go test ./...
go run ./cmd/api
```

Create a high-risk company:

```bash
curl -i http://localhost:8080/v1/customers \
  -H 'Content-Type: application/json' \
  -H 'X-Actor-ID: analyst@example.test' \
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

1. PostgreSQL repository and transactional audit writes.
2. Authentication, RBAC and maker-checker approval.
3. Transaction ingestion and versioned monitoring rules.
4. Alert investigation and case management.
5. Minimal analyst web interface.

## Important boundary

This is an educational portfolio system built with synthetic data. It is not production compliance software and does not determine whether a person or entity is sanctioned.
