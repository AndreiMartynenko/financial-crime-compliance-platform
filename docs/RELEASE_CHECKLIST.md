# Release checklist

Use this checklist for a versioned release candidate. A passing build is necessary but does not by itself authorize a production compliance deployment.

## Scope and approval

- [ ] Choose a semantic version and write release notes, known limitations, and migration impact.
- [ ] Link approved requirements and record the accountable release owner.
- [ ] Confirm that risk/monitoring rule changes have independent compliance review.
- [ ] Confirm privacy, retention, licensing, and jurisdiction decisions for the target environment.
- [ ] Obtain production change approval and define the maintenance window.

## Verification

- [ ] `go test -race -count=1 ./...` and `go vet ./...` pass.
- [ ] PostgreSQL integration tests run against a disposable database.
- [ ] `npm ci`, production Web build, and high-severity dependency audit pass.
- [ ] Compose, Kubernetes, Prometheus rules, and k6 scenario validate.
- [ ] Go, npm, and container vulnerability gates pass or have approved exceptions.
- [ ] Authenticated performance gate meets the documented p95 latency and availability SLOs.
- [ ] Provider conformance check passes with production-like TLS and credentials.
- [ ] Demo walkthrough passes using a clean database and synthetic data.

## Data and recovery

- [ ] Schema migration is tested on a production-sized staging snapshot.
- [ ] Backup completes, checksum verifies, and a timed disposable restore drill passes.
- [ ] Recovery point and recovery time results are recorded against objectives.
- [ ] Rollback compatibility is understood; irreversible migrations have an approved forward-fix plan.

## Deployment readiness

- [ ] Images are immutable, SBOMs and SHA-256 checksums exist, and provenance attestations are available.
- [ ] Secrets come from the environment's secret store; repository and image scans find no credentials.
- [ ] OIDC issuer, authorized parties, roles, MFA/session policy, and break-glass access are verified.
- [ ] HTTPS certificates, DNS, ingress, NetworkPolicy, rate limits, and outbound allowlists are verified.
- [ ] Metrics authentication, dashboards, alert routing, logs, tracing, and on-call ownership are verified.
- [ ] Database capacity, API autoscaling, disruption budgets, and dependency quotas are reviewed.

## Rollout and rollback

- [ ] Record the previous immutable image digests and database version.
- [ ] Deploy to staging, run smoke and critical-journey tests, then obtain environment approval.
- [ ] Roll out gradually and watch readiness, 5xx rate, p95 latency, worker failures, and outbox backlog.
- [ ] Stop rollout on SLO burn or data-integrity signals; retain evidence and invoke the incident runbook.
- [ ] Roll back application images only when schema compatibility is confirmed; otherwise use the approved forward fix or restore procedure.
- [ ] Complete post-deployment validation and record release evidence.

## GitHub release

Pushing a tag matching `v*.*.*` runs the release workflow. It verifies the project, builds API and Web images, creates a deployment bundle, generates an SPDX SBOM and SHA-256 checksums, attests build provenance, publishes immutable GHCR tags, and creates the GitHub Release. Create the tag only after every applicable item above is complete.
