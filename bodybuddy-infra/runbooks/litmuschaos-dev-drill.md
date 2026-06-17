# LitmusChaos Dev Drill Runbook

## Purpose

Use LitmusChaos as a temporary dev-environment drill tool to validate how BodyBuddy recovers from Kubernetes workload failures.

This is not a production chaos program. The evidence should support the portfolio story: SQS re-delivery, Kubernetes self-healing, retry behavior, and Grafana/OTel visibility were measured under controlled failure.

## Safety Rules

- Run only in the `dev` EKS environment.
- Run one experiment at a time.
- Keep k6 load low enough to avoid unnecessary AWS cost.
- Confirm DLQ depth is zero before and after each drill.
- Remove LitmusChaos after the drill.

## Preflight

```bash
kubectl get applications -n bodybuddy-system
kubectl get pods -n bodybuddy
kubectl get hpa,scaledobject -n bodybuddy
kubectl get pods -n litmus
```

Capture baseline:

```bash
kubectl get pods -n bodybuddy -o wide
kubectl logs -n bodybuddy deploy/analysis-worker --since=10m
kubectl logs -n bodybuddy deploy/score-service --since=10m
```

Record SQS queue attributes:

```bash
aws sqs get-queue-attributes \
  --queue-url <analysis-queue-url> \
  --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed
```

## Drill 1: analysis-worker Pod Delete

Hypothesis: if an `analysis-worker` pod dies while processing a message, SQS re-delivery and idempotency prevent message loss and duplicate score writes.

```bash
kubectl apply -f bodybuddy-infra/chaos/litmus/experiments/01-analysis-worker-pod-delete.yaml
kubectl get chaosengine,chaosresult -n litmus
kubectl get pods -n bodybuddy -l app.kubernetes.io/name=analysis-worker -w
```

Measure:

- Time from pod kill to ready worker count restored
- `ApproximateNumberOfMessagesNotVisible` movement
- DLQ message count
- Duplicate score history rows for the same upload id

## Drill 2: score-service Pod Delete

Hypothesis: if `score-service` disappears, readiness and worker retries prevent long-lived bad traffic and the dependency recovers without data loss.

```bash
kubectl apply -f bodybuddy-infra/chaos/litmus/experiments/02-score-service-pod-delete.yaml
kubectl get chaosengine,chaosresult -n litmus
kubectl get pods -n bodybuddy -l app.kubernetes.io/name=score-service -w
```

Measure:

- API 5xx duration
- p95/p99 latency spike
- Worker retry/error log count
- Time until `score-service` is ready again

## Optional Drill: analysis-worker to score-service Latency

Hypothesis: artificial latency between `analysis-worker` and `score-service` is visible in traces/metrics and does not create retry storms.

```bash
kubectl apply -f bodybuddy-infra/chaos/litmus/experiments/03-analysis-worker-to-score-service-latency.yaml
kubectl get chaosengine,chaosresult -n litmus
```

Measure:

- p95/p99 request duration
- Retry count
- Queue depth peak
- Trace span duration for the worker-to-score call

## Cleanup

```bash
kubectl delete -f bodybuddy-infra/chaos/litmus/experiments/01-analysis-worker-pod-delete.yaml --ignore-not-found
kubectl delete -f bodybuddy-infra/chaos/litmus/experiments/02-score-service-pod-delete.yaml --ignore-not-found
kubectl delete -f bodybuddy-infra/chaos/litmus/experiments/03-analysis-worker-to-score-service-latency.yaml --ignore-not-found
helm uninstall chaos -n litmus
kubectl delete namespace litmus
```

## Evidence

Store screenshots and command outputs under:

```text
bodybuddy-infra/reports/evidence/09-litmuschaos-drill/
```

Then fill in:

```text
bodybuddy-infra/reports/09-litmuschaos-dev-drill.md
```
