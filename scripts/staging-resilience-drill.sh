#!/bin/sh
set -eu

: "${BASE_URL:?BASE_URL is required, for example https://staging.example.com}"
: "${KUBE_NAMESPACE:=fccp-staging}"
: "${API_DEPLOYMENT:=api}"
: "${POD_SELECTOR:=app=fccp-api}"
: "${RECOVERY_TIMEOUT_SECONDS:=120}"
: "${EVIDENCE_FILE:=resilience-evidence.json}"

if [ "${CONFIRM_RESILIENCE_DRILL:-}" != "run" ]; then
  echo "Refusing to disrupt staging: set CONFIRM_RESILIENCE_DRILL=run" >&2
  exit 1
fi

case "$KUBE_NAMESPACE" in
  *prod*)
    echo "Refusing to run against production-like namespace $KUBE_NAMESPACE" >&2
    exit 1
    ;;
esac

command -v kubectl >/dev/null
command -v curl >/dev/null
command -v jq >/dev/null

base_url=${BASE_URL%/}
ready_url="$base_url/readyz"
started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
started_epoch=$(date +%s)

desired=$(kubectl -n "$KUBE_NAMESPACE" get deployment "$API_DEPLOYMENT" -o jsonpath='{.spec.replicas}')
available_before=$(kubectl -n "$KUBE_NAMESPACE" get deployment "$API_DEPLOYMENT" -o jsonpath='{.status.availableReplicas}')
if [ "${desired:-0}" -lt 2 ] || [ "${available_before:-0}" -lt 2 ]; then
  echo "At least two desired and available API replicas are required before the drill" >&2
  exit 1
fi
if ! curl --fail --silent --show-error --max-time 5 "$ready_url" >/dev/null; then
  echo "Readiness endpoint is not healthy before the drill: $ready_url" >&2
  exit 1
fi

pod=$(kubectl -n "$KUBE_NAMESPACE" get pods -l "$POD_SELECTOR" \
  --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}')
if [ -z "$pod" ]; then
  echo "No running API pod found" >&2
  exit 1
fi

kubectl -n "$KUBE_NAMESPACE" delete pod "$pod" --wait=false >/dev/null

availability_checks=0
availability_failures=0
recovered=false
deadline=$((started_epoch + RECOVERY_TIMEOUT_SECONDS))
while [ "$(date +%s)" -le "$deadline" ]; do
  availability_checks=$((availability_checks + 1))
  if ! curl --fail --silent --show-error --max-time 2 "$ready_url" >/dev/null 2>&1; then
    availability_failures=$((availability_failures + 1))
  fi

  available_now=$(kubectl -n "$KUBE_NAMESPACE" get deployment "$API_DEPLOYMENT" -o jsonpath='{.status.availableReplicas}')
  old_pod_exists=$(kubectl -n "$KUBE_NAMESPACE" get pod "$pod" --ignore-not-found -o name)
  if [ "${available_now:-0}" -ge "$desired" ] && [ -z "$old_pod_exists" ]; then
    recovered=true
    break
  fi
  sleep 2
done

completed_epoch=$(date +%s)
completed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
recovery_seconds=$((completed_epoch - started_epoch))
available_after=$(kubectl -n "$KUBE_NAMESPACE" get deployment "$API_DEPLOYMENT" -o jsonpath='{.status.availableReplicas}')

jq -n \
  --arg started_at "$started_at" \
  --arg completed_at "$completed_at" \
  --arg namespace "$KUBE_NAMESPACE" \
  --arg deployment "$API_DEPLOYMENT" \
  --arg deleted_pod "$pod" \
  --arg ready_url "$ready_url" \
  --argjson desired_replicas "$desired" \
  --argjson available_before "$available_before" \
  --argjson available_after "${available_after:-0}" \
  --argjson recovery_seconds "$recovery_seconds" \
  --argjson recovery_timeout_seconds "$RECOVERY_TIMEOUT_SECONDS" \
  --argjson availability_checks "$availability_checks" \
  --argjson availability_failures "$availability_failures" \
  --argjson recovered "$recovered" \
  '{schema_version:1, started_at:$started_at, completed_at:$completed_at,
    target:{namespace:$namespace, deployment:$deployment, readiness_url:$ready_url},
    disruption:{type:"single_pod_deletion", deleted_pod:$deleted_pod},
    replicas:{desired:$desired_replicas, available_before:$available_before, available_after:$available_after},
    measurement:{recovery_seconds:$recovery_seconds, recovery_timeout_seconds:$recovery_timeout_seconds,
      availability_checks:$availability_checks, availability_failures:$availability_failures},
    passed:$recovered}' >"$EVIDENCE_FILE"

cat "$EVIDENCE_FILE"
if [ "$recovered" != "true" ]; then
  echo "API deployment did not recover within ${RECOVERY_TIMEOUT_SECONDS}s" >&2
  exit 1
fi

echo "Staging resilience drill passed in ${recovery_seconds}s; evidence: $EVIDENCE_FILE"
