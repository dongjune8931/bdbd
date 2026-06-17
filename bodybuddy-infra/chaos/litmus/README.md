# LitmusChaos Dev Drill

This directory contains one-off LitmusChaos drill manifests for the BodyBuddy dev EKS environment.

The goal is not to run LitmusChaos as a permanent platform component. Use these manifests only when running a measured resilience drill, then clean up the Litmus resources.

## Scope

| Drill | Target | Question |
|---|---|---|
| `01-analysis-worker-pod-delete.yaml` | `analysis-worker` deployment | Does SQS re-delivery complete without message loss or duplicate score writes? |
| `02-score-service-pod-delete.yaml` | `score-service` deployment | Do readiness, retries, and Kubernetes self-healing recover the sync dependency? |
| `03-analysis-worker-to-score-service-latency.yaml` | `analysis-worker` pods | Does `analysis-worker -> score-service` latency show up in metrics/traces without retry storms? |

## Prerequisites

- BodyBuddy dev EKS environment is running.
- ArgoCD apps are `Synced` and `Healthy`.
- Prometheus/Grafana and CloudWatch logs are available.
- LitmusChaos 3.x is installed in the `litmus` namespace.
- The required Litmus faults are installed from ChaosHub:
  - `pod-delete`
  - `pod-network-latency` for the optional latency drill

## Install LitmusChaos

```bash
helm repo add litmuschaos https://litmuschaos.github.io/litmus-helm/
helm repo update
kubectl create namespace litmus
helm upgrade --install chaos litmuschaos/litmus --namespace litmus
kubectl get pods -n litmus
```

If you need the UI locally:

```bash
kubectl port-forward svc/chaos-litmus-frontend-service 9091:9091 -n litmus
```

## Run a Drill

Run only one experiment at a time.

```bash
kubectl apply -f bodybuddy-infra/chaos/litmus/experiments/01-analysis-worker-pod-delete.yaml
kubectl get chaosengine,chaosresult -n litmus
kubectl describe chaosengine analysis-worker-pod-delete -n litmus
```

Record the result in `bodybuddy-infra/reports/09-litmuschaos-dev-drill.md`.

## Cleanup

```bash
kubectl delete -f bodybuddy-infra/chaos/litmus/experiments/01-analysis-worker-pod-delete.yaml
kubectl delete -f bodybuddy-infra/chaos/litmus/experiments/02-score-service-pod-delete.yaml --ignore-not-found
kubectl delete -f bodybuddy-infra/chaos/litmus/experiments/03-analysis-worker-to-score-service-latency.yaml --ignore-not-found
helm uninstall chaos -n litmus
kubectl delete namespace litmus
```
