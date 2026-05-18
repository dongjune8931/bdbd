# Architecture Decision Records

이 디렉터리는 BodyBuddy에서 중요한 기술 결정을 기록한다.

각 문서는 단순한 선택 목록이 아니라, 당시의 제약과 대안, 선택 이유, 검증 결과를 함께 남긴다. 목적은 나중에 코드를 처음 보는 사람이 "왜 이렇게 되어 있는지"를 빠르게 이해하게 하는 것이다.

## Records

| ID | Decision |
|---|---|
| [0001](./0001-split-services-by-traffic-and-sla.md) | 서비스 분리를 트래픽 특성과 SLA 기준으로 한다 |
| [0002](./0002-use-sqs-for-async-workflow.md) | 비동기 워크플로에 SQS를 사용한다 |
| [0003](./0003-run-api-on-on-demand-and-workers-on-spot.md) | API는 on-demand, worker는 Spot 노드에 배치한다 |
| [0004](./0004-use-argocd-and-explicit-image-tags.md) | ArgoCD GitOps와 명시적 이미지 태그 업데이트를 사용한다 |
| [0005](./0005-use-single-region-dr-with-s3-versioning-and-rds-pitr.md) | 단일 리전에서 S3 Versioning과 RDS PITR 중심으로 복구한다 |
| [0006](./0006-use-prometheus-grafana-tempo-and-cloudwatch.md) | 관측성은 Prometheus, Grafana, Tempo, CloudWatch 조합으로 구성한다 |
| [0007](./0007-scale-score-service-with-cpu-hpa.md) | score-service는 CPU 기반 HPA로 확장한다 |
