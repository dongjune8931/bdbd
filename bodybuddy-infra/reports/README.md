# BodyBuddy Reports

이 폴더는 BodyBuddy 인프라 실험과 시연 증거를 모아두는 공간이다. 목적은 단순 기록이 아니라, 면접과 블로그에서 바로 근거로 사용할 수 있는 자료를 남기는 것이다.

## 핵심 보고서

| 문서 | 내용 |
|---|---|
| [Presentation Outline](./presentation-architecture-implementation-outline.md) | 아키텍처, 운영, 복구, 비용 최적화 중심 발표 설계안 |
| [Diagram Blueprints](./presentation-diagram-blueprints.md) | 전체 아키텍처, GitOps, DR, 서비스 흐름 다이어그램 초안 |
| [Monitoring Scorecard Plan](./monitoring-performance-scorecard.md) | 커스텀 메트릭, Grafana 대시보드, before/after 성능 개선 장표 설계 |
| [RTO / RPO Matrix](./rto-rpo-matrix.md) | 장애 유형별 복구 목표와 관찰값 |
| [Load Test Report](./load-test-report.md) | k6 부하 테스트와 HPA 튜닝 결과 |
| [Spot Interruption Drill](./06-spot-interruption-drill.md) | worker spot 노드 축출, re-queue, 재처리 검증 |
| [DR Drill](./07-dr-drill.md) | S3 자동 복구와 RDS PITR 검증 |
| [Demo Video Script](./demo-video-script.md) | 3~5분 데모 영상 구성안 |

## 증거 자료

| 경로 | 내용 |
|---|---|
| [evidence/07-dr-drill](./evidence/07-dr-drill/README.md) | S3, RDS, Spot interruption 캡처 |
| [evidence/08-load-test](./evidence/08-load-test/) | HPA scale-out, k6 결과 캡처 |

## 블로그 초안

| 초안 | 주제 |
|---|---|
| [01. EKS MSA GitOps Observability](./blog-drafts/01-eks-msa-gitops-observability.md) | EKS 위 MSA, GitOps, 관측성 |
| [02. Karpenter Spot Cost Optimization](./blog-drafts/02-karpenter-spot-cost-optimization.md) | Karpenter, Spot, worker interruption 대응 |
| [03. Single Region DR](./blog-drafts/03-single-region-dr-s3-rds.md) | S3 자동 복구와 RDS PITR |

## 학습 노트와 트러블슈팅

| 경로 | 내용 |
|---|---|
| [study](./study/) | 구축 흐름별 학습 노트 |
| [wiki](./wiki/) | 주요 트러블슈팅 기록 |

## 운영 원칙

- 새 시연을 하면 먼저 `evidence/`에 캡처를 넣고, 그 다음 관련 보고서에 링크한다.
- 파일명은 정렬하기 쉽게 숫자로 시작한다.
- 보고서에는 가능하면 명령어 출력, 로그, 측정값을 함께 남긴다.
- 로컬 핸드오프는 `HANDOFF.md`에만 남기고, 공개 가능한 자료는 이 폴더에 정리한다.
