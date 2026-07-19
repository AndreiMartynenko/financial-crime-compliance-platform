# Production screening-provider onboarding

FCCP integrates through a provider-neutral HTTPS adapter. A commercial vendor should not be wired directly into domain logic: deploy a small vendor adapter that translates its authentication, request and response formats into the contract below. This keeps screening decisions auditable and makes provider replacement testable.

## Approval checklist

Before transmitting any customer data, record approval for data-processing terms, data residency, sanctions/PEP/adverse-media coverage, update frequency, match methodology, SLA/support escalation, retention/deletion, sub-processors, breach notification and permitted test subjects. Security must approve the endpoint, credential storage, TLS chain and optional client certificate. Compliance must approve thresholds and the interpretation of candidate scores.

Do not use a real person for the conformance check unless legal and compliance have approved that processing. Prefer a vendor-provided synthetic test identity.

## Adapter contract

FCCP sends:

```http
POST /screen
Content-Type: application/json
Accept: application/json
Authorization: Bearer <credential>
X-Correlation-ID: <stable request id>
Idempotency-Key: <same value for every retry>

{"name":"Vendor-approved synthetic name"}
```

The adapter must return exactly one JSON document:

```json
{"candidates":[{"list_type":"sanctions","name":"Matched name","score":98,"reason":"Explainable source/list evidence"}]}
```

`list_type` is `sanctions`, `pep` or `adverse_media`; score is an integer from 0 to 100; name and reason are non-empty. Responses are limited to 1 MiB and 100 candidates. Unknown fields, redirects, trailing JSON, unsupported enum values and incomplete candidates are rejected without retry. Network errors, `429` and 5xx responses use bounded retries and the circuit breaker; other 4xx and contract violations do not.

Delivery is at least once from FCCP to the adapter, so the adapter must cache or deduplicate `Idempotency-Key`. It should propagate `X-Correlation-ID` into vendor logs without logging the submitted name.

## Configuration and conformance

Production requires HTTPS. Plain HTTP is available only behind the explicit `SCREENING_PROVIDER_ALLOW_HTTP=true` local-test override. Optional CA and mTLS files are loaded at startup and must be mounted read-only from the deployment secret store.

```bash
SCREENING_PROVIDER_URL=https://screening-adapter.example.com \
SCREENING_PROVIDER_API_KEY='rotated-secret' \
SCREENING_PROVIDER_NAME='approved-vendor-adapter-v1' \
SCREENING_PROVIDER_CA_FILE=/var/run/secrets/provider/ca.pem \
SCREENING_PROVIDER_CLIENT_CERT_FILE=/var/run/secrets/provider/tls.crt \
SCREENING_PROVIDER_CLIENT_KEY_FILE=/var/run/secrets/provider/tls.key \
SCREENING_PROVIDER_CHECK_NAME='VENDOR SYNTHETIC TEST SUBJECT' \
go run ./cmd/providercheck
```

The checker exercises the real `/screen` contract and prints only provider name and candidate count. Run it from the same network and trust store as staging. Never put credentials, certificates, returned names or raw vendor responses in GitHub artifacts.

## Rollout and rollback

1. Complete vendor and security approval; store evidence outside this repository.
2. Run the conformance check, timeout/retry tests and a controlled staging screening using synthetic data.
3. Confirm provider spans, error counters and circuit-breaker behavior; establish the vendor-specific latency baseline.
4. Enable the provider for a limited staging window. Compare candidate coverage with the deterministic fixture or an approved reference dataset.
5. Rotate the initial credential, document ownership and expiry, then approve production configuration through the protected environment.
6. Roll back by clearing `SCREENING_PROVIDER_URL`; FCCP returns to the deterministic demo provider. This fallback is suitable for demonstrations only and must not be represented as production screening coverage.

Provider outages do not change stored risk-scoring rules. A failed screening run is persisted through worker failure/backoff state and must be operationally investigated rather than silently treated as “no match.”
