# LitmusChaos Dev Drill Report

## Summary

This report records one-off LitmusChaos drills run against the BodyBuddy dev EKS environment.

The goal is to measure recovery behavior under controlled Kubernetes failures, not to claim production chaos engineering maturity.

## Environment

| Item | Value |
|---|---|
| Date | TBD |
| Cluster | `bodybuddy-dev` |
| Region | `ap-northeast-2` |
| LitmusChaos version | TBD |
| BodyBuddy image tags | TBD |
| Load profile | TBD |

## Baseline

| Metric | Value |
|---|---|
| API error rate | TBD |
| API p95 latency | TBD |
| Analysis queue depth | TBD |
| Analysis DLQ depth | TBD |
| Worker replicas | TBD |
| Score-service replicas | TBD |

## Drill Results

| Drill | Hypothesis | Result | RTO | Data loss | Duplicate side effect | Evidence |
|---|---|---|---|---|---|---|
| `analysis-worker` pod delete | SQS re-delivery and idempotency recover in-flight work | TBD | TBD | TBD | TBD | TBD |
| `score-service` pod delete | Readiness and retries recover sync dependency failures | TBD | TBD | TBD | TBD | TBD |
| worker-to-score latency | Latency is observable without retry storms | TBD | TBD | TBD | TBD | TBD |

## Observations

- TBD

## Tuning Notes

- TBD

## Resume Bullet Candidate

> EKS dev 환경에서 LitmusChaos 기반 장애 주입 실험을 설계하고, Pod 장애 및 네트워크 지연 시나리오에서 SQS 재처리, Kubernetes self-healing, OTel/Grafana 기반 RTO와 데이터 유실 여부를 실측했습니다.
