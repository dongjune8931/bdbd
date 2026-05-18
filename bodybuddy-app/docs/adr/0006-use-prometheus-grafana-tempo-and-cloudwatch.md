# 0006. Use Prometheus, Grafana, Tempo, and CloudWatch Logs

## Status

Accepted

## Context

The project needs observability that is broad enough for platform storytelling but small enough to operate during short-lived dev environments. The target signals are service health, latency, error rate, worker behavior, queue processing, traces through the upload workflow, and cost visibility.

Logs, metrics, traces, and cost data should be available without turning the project into an observability platform project of its own.

## Decision

We use the following observability split:

- Prometheus and Grafana for Kubernetes and service metrics.
- Alertmanager for rule-based alerting.
- CloudWatch Logs as the log backend.
- OpenTelemetry Collector and Tempo for distributed tracing on the upload to analysis to score path.
- KubeCost for namespace and workload cost visibility.

We intentionally trace only the critical asynchronous workflow instead of instrumenting every endpoint.

## Alternatives Considered

Loki was rejected to reduce operational overhead. CloudWatch Logs already exists in the AWS environment and is enough for this project.

Tracing every request was rejected because it would add noise without improving the core story.

Using only application logs was rejected because it would not prove autoscaling behavior, RED metrics, queue health, or cost impact.

## Consequences

This gives the project a balanced observability setup:

- Prometheus shows service and infrastructure behavior.
- Grafana gives a single place to present metrics and cost views.
- Tempo provides a focused distributed trace for the most important workflow.
- CloudWatch keeps log storage simple and AWS-native.

The tradeoff is that log querying is less Kubernetes-native than a Loki setup. That is acceptable because the project optimizes for clarity and operating cost.

## Validation

The cluster has successfully run Prometheus, Grafana, Alertmanager, Tempo, OpenTelemetry Collector, and KubeCost. Service metrics were used to validate HPA behavior, while worker logs were used to validate queue processing and graceful shutdown during node disruption.
