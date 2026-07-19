# Product demo

The demo dataset is synthetic. Do not enter real personal, financial, sanctions, or customer data into a public demonstration environment.

## Start and seed

```bash
POSTGRES_PORT=55432 docker compose up -d --build
DATABASE_URL='postgres://financial_crime:local_development_only@localhost:55432/financial_crime?sslmode=disable' \
CONFIRM_DEMO_SEED=seed \
go run ./cmd/demoseed
```

The explicit confirmation prevents accidental writes. Running the command again is safe: it detects all three stable demo references and makes no changes. If only part of the dataset exists, it stops rather than overwriting or guessing how to repair data.

Open [http://localhost:3000](http://localhost:3000). Local-only accounts are:

| Role | Username | Password |
|---|---|---|
| Analyst | `analyst` | `analyst-demo-only` |
| Reviewer | `reviewer` | `reviewer-demo-only` |
| Administrator | `administrator` | `admin-demo-only` |

Replace these credentials and the imported Keycloak realm outside local development.

## What the dataset contains

- **Harbour Design Studio** — low-risk customer waiting for independent approval.
- **Orion Trading Company** — active company with completed CDD, beneficial owner, verified registry evidence, a high-value payment to a monitored jurisdiction, open alerts, an assigned investigation case, a sanctions match, a notification, and recurring screening.
- **Nadia Karim** — active PEP customer with enhanced-due-diligence context, CDD in review, a PEP screening match, and a notification.

Every write uses the same application services and transactional PostgreSQL repositories as the API. The seed does not bypass risk scoring, maker-checker controls, monitoring rules, or audit-event creation.

## Five-minute walkthrough

1. Sign in as `analyst`. On **Overview**, show operational status, customer counts, alerts, and the live queue.
2. Open **Customers** and choose Orion. In Customer 360, explain the reproducible risk score, linked transactions, alerts, case, and immutable activity timeline.
3. Open **Alerts**, then the linked high-priority investigation case. Show assignment, investigator notes, and the fact that resolving a case atomically closes its linked alert.
4. Open **KYC / CDD**. Show source of wealth, expected activity, beneficial ownership, evidence status, and periodic-review date.
5. Open **Screening** and **Inbox**. Show provider evidence, potential-match workflow, recurring schedule, and operational notification.
6. Sign out and sign in as `reviewer`. Approve Harbour Design Studio and disposition a screening match with a rationale. Point out that the maker cannot approve their own submission.
7. Close with Grafana at [http://localhost:3001](http://localhost:3001) and Jaeger at [http://localhost:16686](http://localhost:16686) to show service health and request traces.

## Suggested narration

“Northstar Compliance OS is a full-stack financial-crime operations platform. It turns customer onboarding, risk assessment, KYC evidence, transaction monitoring, investigations, sanctions/PEP screening, and notifications into one traceable workflow. PostgreSQL commits business state and audit evidence together, Keycloak enforces identity and separation of duties, and the operational stack exposes SLOs, metrics, alerts, and traces. The rules here are demonstrative and versioned; production policy and provider decisions remain governed human responsibilities.”

## Resetting a local demo

The seeder intentionally has no destructive reset flag. To start from a completely clean local environment, explicitly remove the disposable Compose volumes and recreate the stack:

```bash
docker compose down -v
POSTGRES_PORT=55432 docker compose up -d --build
```

`down -v` permanently deletes local PostgreSQL and Keycloak data. Never use it against an environment containing data you need.
