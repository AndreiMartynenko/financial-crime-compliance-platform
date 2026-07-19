# Service-level objectives and performance gate

These objectives define the initial staging reliability contract for the authenticated `/v1` API. They are engineering targets until a representative staging environment has accumulated at least 30 days of telemetry.

## Indicators and objectives

| Indicator | Initial objective | Measurement |
|---|---:|---|
| Availability | 99.5% over a rolling 30-day window | Non-5xx authenticated API responses divided by all authenticated API responses. Caller-caused 4xx responses remain available service. |
| Latency | 95% of authenticated API requests below 500 ms | Prometheus histogram across normalized `/v1` routes, measured at the API boundary. |
| Notification durability | No outbox item pending for more than 15 minutes | Existing outbox backlog gauge and delivery-failure counters. |
| Screening freshness | No enabled schedule more than one cadence late | Persisted next-run state; a dedicated overdue-schedule metric remains a production-provider follow-up. |

The availability objective allows approximately 3 hours 39 minutes of server-error time per 30 days if traffic is uniform. Multi-window burn-rate alerts page on rapid consumption and warn on slower sustained consumption. Deployments should stop when the fast-burn alert is active or the performance gate fails.

## Load-test workload

`tests/load/smoke.js` authenticates through a dedicated least-privilege Keycloak service account and exercises the same customer, alert, notification and preference reads used by the operations dashboard. Defaults are 10 iterations per second for one minute, with four authenticated requests per iteration.

```bash
docker run --rm --network host \
  -e API_URL=http://127.0.0.1:8080 \
  -e OIDC_TOKEN_URL=http://127.0.0.1:8081/realms/fccp/protocol/openid-connect/token \
  -v "$PWD/tests/load:/scripts:ro" \
  grafana/k6:0.57.0 run /scripts/smoke.js
```

For rates above five iterations per second, restart the local API with appropriately higher `HTTP_RATE_LIMIT_RPS` and `HTTP_RATE_LIMIT_BURST`; the automated performance workflow uses 200/400 so that it measures application latency rather than the single-client edge limit.

The gate fails below 99% successful checks, at 1% failed workload requests, or when workload p95 exceeds 500 ms. Increase `FCCP_RATE` and `FCCP_DURATION` only after establishing a clean baseline. Do not interpret a laptop result as production capacity.

## Release policy

The `Performance gate` workflow runs weekly and on demand against an isolated Compose stack, exports the k6 summary as a build artifact and fails on threshold violation. Before a public release, require the normal CI workflow and a recent successful performance gate, confirm no SLO burn alert is firing, verify backup recency and record the deployed immutable image SHA.
