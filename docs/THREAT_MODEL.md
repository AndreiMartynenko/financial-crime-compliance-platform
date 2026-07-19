# FCCP threat model

## Scope and assets

The protected assets are customer identity and KYC data, risk decisions, screening matches, investigation records, audit history, access tokens, provider credentials and notification destinations. Trust boundaries exist between the browser and Nginx, Nginx and the API, the API and PostgreSQL/Keycloak, and the API and external screening or webhook providers.

## Principal threats and controls

| Threat | Existing control | Remaining production action |
|---|---|---|
| Account takeover or token forgery | OIDC Authorization Code + PKCE, RS256/JWKS validation, issuer and authorized-party checks | Enforce MFA and short token lifetimes in the production identity provider |
| Unauthorized compliance action | Role-based API authorization and maker-checker separation | Periodically review role assignments and export access decisions to the SIEM |
| Sensitive-data disclosure | Minimal structured logs, no passwords in the application, restrictive response headers | TLS everywhere, field-level classification, encryption-key rotation and DLP review |
| Request flooding | Per-client token-bucket limiting and bounded request bodies | Apply edge/WAF limits and distributed rate limiting across replicas |
| Audit or state tampering | PostgreSQL transactions, append-only audit events and actor attribution | Restrict database roles and archive signed audit exports to immutable storage |
| Duplicate background processing | PostgreSQL leases and `SKIP LOCKED` claims | Monitor lease age and test failover under multiple production replicas |
| SSRF through provider/webhook configuration | Destinations are server-side environment configuration, not caller input | Allow-list hosts and enforce egress policy in the deployment network |
| Secret leakage | Secrets are environment-driven and excluded from API responses | Use a managed secret store; never use Compose demo credentials outside local development |
| Supply-chain compromise | Go tests/vet, npm audit, govulncheck and Trivy CI scanning | Pin deployment images by digest and review automated dependency updates |
| Metrics/operations disclosure | `/metrics` uses a separate bearer credential | Keep Prometheus, Grafana and Jaeger on a private operations network with SSO |

## Security boundaries

- Local Compose credentials and anonymous Grafana access are explicitly demo-only.
- The deterministic screening dataset and rules are synthetic and are not legal determinations.
- Rate limiting in one API process is not a substitute for an edge or distributed limiter.
- PostgreSQL backup encryption, restore testing and production network policies belong to the deployment milestone.

Review this model whenever a new external integration, privileged role, sensitive data category or public endpoint is introduced.
