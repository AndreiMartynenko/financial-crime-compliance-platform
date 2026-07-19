# Database backup and disaster recovery

This runbook covers the PostgreSQL system of record. Identity-provider, Kubernetes and observability recovery must follow their providers' own backup procedures. All examples require PostgreSQL 17 client tools and TLS-enabled connection strings outside local development.

## Objectives and ownership

- Target recovery point objective (RPO): 24 hours with daily logical backups; production should additionally enable managed PostgreSQL point-in-time recovery for a materially lower RPO.
- Target recovery time objective (RTO): four hours after infrastructure and credentials are available.
- The on-call platform engineer executes recovery. A compliance owner validates customer, audit and case data before traffic returns.
- Run a restore drill in an isolated staging account at least quarterly and after material schema or backup changes.

These are engineering targets, not guarantees. They become service commitments only after repeated timed drills on the selected cloud provider.

## Staging resilience drill

The manual **Staging resilience drill** GitHub workflow requires approval through the `staging-resilience` environment. It verifies that at least two API replicas are healthy, deletes exactly one non-production API pod, continuously samples `/readyz`, measures full replica recovery, and retains a machine-readable evidence artifact for 365 days. It refuses namespaces containing `prod` or `production`.

Configure the environment with `KUBE_CONFIG`, `STAGING_DATABASE_URL`, and the `STAGING_BASE_URL` variable. The database credential needs `CREATEDB` only when the optional disposable restore drill is selected. Start with the default 120-second recovery threshold; tighten it only after measured staging history supports the change.

The same pod-recovery check can be run by an operator:

```bash
BASE_URL='https://staging.example.com' \
KUBE_NAMESPACE=fccp-staging \
CONFIRM_RESILIENCE_DRILL=run \
EVIDENCE_FILE=resilience-evidence.json \
./scripts/staging-resilience-drill.sh
```

This is deliberately a single-pod availability exercise, not proof of database, availability-zone, region, identity-provider, or third-party-provider failover. Those failure modes require provider-specific staging exercises after cloud selection.

## Backup policy

Run `scripts/postgres-backup.sh` from an isolated job with a read-only backup credential. Upload both the `.dump` and `.sha256` files to encrypted, versioned object storage using immutable retention. Do not store dumps in Git or container filesystems.

Recommended starting retention is 35 daily backups and 12 month-end backups. Alert on a missing backup, checksum failure, upload failure or a backup older than 26 hours. Restrict download and deletion to separate operational roles and audit every access.

```bash
DATABASE_URL='postgres://backup_user:...@database.example.com:5432/fccp?sslmode=verify-full' \
BACKUP_DIR=/secure/temporary/path \
./scripts/postgres-backup.sh
```

The script uses PostgreSQL's consistent logical snapshot, writes a private custom-format dump to a temporary filename, validates its catalog, atomically renames it and generates a SHA-256 checksum. It refuses to overwrite an artifact with the same timestamp.

Local development uses the matching PostgreSQL 17 client image, so host client tools are not required:

```bash
docker compose --profile ops run --rm db-tools /scripts/postgres-backup.sh
```

## Restore drill

The verification script creates a disposable database on the same server, validates the checksum, restores transactionally, checks critical tables and the migration ledger, then drops the database. The credential therefore needs `CREATEDB` for drills.

```bash
DATABASE_URL='postgres://dr_operator:...@staging-db.example.com:5432/fccp?sslmode=verify-full' \
./scripts/postgres-verify-backup.sh /secure/temporary/path/fccp-20260719T120000Z.dump
```

For a local dump created in `./backups`:

```bash
docker compose --profile ops run --rm db-tools \
  /scripts/postgres-verify-backup.sh /backups/fccp-20260719T120000Z.dump
```

Never run a drill against the production server when isolation policy forbids temporary databases. Restore into a dedicated recovery instance instead.

Each drill record should include the commit SHA, operator/approver, timestamps, recovery duration, availability failures, restored backup identity, validation result, and links to incident or follow-up work. GitHub workflow artifacts provide the raw JSON evidence; export them to the approved long-term audit store before artifact expiry.

## Incident recovery

1. Declare the incident, stop API and worker writes, preserve logs and record the recovery decision.
2. Provision an isolated PostgreSQL 17 recovery target with encryption, network restrictions and monitoring.
3. Select the newest valid backup consistent with the incident boundary; verify its checksum and provenance.
4. Restore with an explicit destructive-operation acknowledgement:

   ```bash
   RESTORE_DATABASE_URL='postgres://restore_operator:...@recovery-db.example.com:5432/fccp?sslmode=verify-full' \
   CONFIRM_DATABASE_RESTORE=restore \
   ./scripts/postgres-restore.sh /secure/temporary/path/fccp-20260719T120000Z.dump
   ```

5. Start one API replica so embedded migrations can apply. Confirm `/readyz`, migration count, customer totals, recent immutable audit events, open cases and screening schedules.
6. Have the compliance owner approve validation evidence. Rotate affected credentials, switch application traffic, then scale workers and replicas gradually.
7. Preserve the failed database and recovery evidence according to incident-retention policy. Complete a post-incident review and update this runbook.

The restore command uses `--clean` and replaces objects in its target database. It deliberately refuses to run without both the matching checksum file and `CONFIRM_DATABASE_RESTORE=restore`.
