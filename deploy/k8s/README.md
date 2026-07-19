# Kubernetes staging deployment

The manifests target a Kubernetes 1.27+ cluster with Nginx Ingress, cert-manager, Metrics Server, an external PostgreSQL database, an external OIDC provider and an OTLP collector. They intentionally do not install stateful infrastructure or commit credentials.

## Required GitHub environment

Create a protected GitHub environment named `staging` with required reviewers. Add these secrets directly in GitHub:

- `KUBE_CONFIG`: base64-encoded, namespace-scoped kubeconfig;
- `STAGING_DATABASE_URL`: TLS-enabled PostgreSQL connection string;
- `STAGING_METRICS_TOKEN`: high-entropy Prometheus bearer token.

Replace `staging.fccp.example.com` and identity-provider URLs in `overlays/staging` before the first deployment. Configure the OIDC client redirect URI as `https://<host>/`.

## Validate and deploy

```bash
kubectl kustomize deploy/k8s/overlays/staging
kubectl apply --server-side --dry-run=server -k deploy/k8s/overlays/staging
```

The `Deploy staging` GitHub workflow builds immutable SHA-tagged API and Web images, pushes them to GHCR, materializes `fccp-secrets` from protected GitHub secrets, applies the overlay, pins both deployments to the SHA images and waits for rollout completion.

Migrations remain safe under two API replicas because the embedded migrator uses a PostgreSQL advisory transaction lock. Roll back application code with the previous workflow SHA or `kubectl rollout undo`; database migrations are forward-only during automated deployment and require an explicit reviewed recovery plan.
