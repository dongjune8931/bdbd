# 0007. Scale Score Service With CPU-Based HPA

## Status

Accepted

## Context

The ranking endpoint is read-heavy and can become the easiest API path to saturate during a demo. The service already has low latency under moderate load, but a portfolio project should show how the platform reacts when demand rises.

Before introducing more complex caching or query-level tuning, the first scaling layer should be Kubernetes-native and easy to observe.

## Decision

We scale `score-service` with a CPU-based HorizontalPodAutoscaler:

- Minimum replicas: 1
- Maximum replicas: 4
- Target CPU utilization: 50%
- Metrics source: Kubernetes metrics-server

The deployment keeps CPU requests configured so HPA has a meaningful utilization baseline.

## Alternatives Considered

Custom Prometheus-based scaling was rejected for the first implementation because CPU-based HPA is simpler, easier to explain, and enough for the current read workload.

KEDA was reserved for queue-driven workers where queue depth is a better scaling signal than CPU.

Manual replica changes were rejected because they do not demonstrate platform automation.

## Consequences

This creates a clean scaling story:

- Ranking load increases CPU utilization.
- HPA increases `score-service` replicas.
- Karpenter can add capacity when the cluster needs more room.
- Latency improves once additional replicas become ready.

The tradeoff is that CPU does not always represent user-facing pressure. If the endpoint becomes cache-bound or I/O-bound, a custom metric may be more accurate later.

## Validation

The ranking load test showed the service scaling from 1 replica to 4 replicas. The measured result improved from about 377 requests per second with p95 around 417 ms to about 572 requests per second with p95 around 36 ms and zero request errors.
