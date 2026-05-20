# BodyBuddy Architecture and Implementation Presentation Outline

이 문서는 BodyBuddy를 아키텍처, 운영, 복구, 비용 최적화 중심으로 소개하는 발표자료 설계안이다. 서비스 기획 소개를 최소화하고, 실제로 구축하고 검증한 클라우드 인프라 결과를 설득력 있게 보여주는 데 목적이 있다.

## 발표 방향

- 기능 소개보다 운영 가능한 시스템을 만들기 위해 어떤 구조를 선택했는지 보여준다.
- "무엇을 만들었는가"보다 "어떻게 배포하고, 관찰하고, 복구하고, 비용을 줄였는가"에 집중한다.
- 모든 주장은 가능하면 실제 캡처, 로그, 수치, 시연 결과로 뒷받침한다.

## 슬라이드 1. Project Overview

### 제목

`BodyBuddy: EKS 위에서 운영 가능한 비동기 헬스 서비스 만들기`

### 넣을 내용

- 인바디 이미지 업로드를 비동기 분석해 점수와 랭킹으로 연결하는 서비스
- 핵심 목표는 기능 구현보다 EKS 운영, GitOps, 관측성, DR, 비용 최적화 검증
- API와 worker를 SLA 기준으로 분리해 운영

### 추천 시각자료

- 짧은 키워드 배지: `EKS`, `Terraform`, `ArgoCD`, `SQS`, `RDS`, `Redis`, `Karpenter`
- 한 줄 구조 요약 다이어그램

### 발표 포인트

- 이 프로젝트는 단순 CRUD 앱이 아니라 클라우드 운영 역량을 증명하기 위한 실험 환경이었다.
- 기능의 수보다 배포, 장애 대응, 비용 최적화, 복구 자동화에 더 큰 비중을 두었다.

## 슬라이드 2. End-to-End Architecture

### 제목

`전체 아키텍처`

### 넣을 내용

- `user-service`, `score-service`는 API 계층으로 `critical` 노드풀에 배치
- `analysis-worker`, `notification-worker`는 비동기 계층으로 `batch` 노드풀에 배치
- 상태 저장소는 `RDS PostgreSQL`, `Redis`, `S3`
- 운영 계층은 `ArgoCD`, `Prometheus/Grafana`, `Tempo`, `KubeCost`

### 추천 시각자료

- 전체 인프라 다이어그램
- 노드풀을 색으로 구분한 EKS 구조

### 발표 포인트

- 서비스는 도메인보다 트래픽 특성과 SLA 기준으로 분리했다.
- API는 안정성이 중요하므로 on-demand, worker는 지연 허용이 가능하므로 spot 위주로 운영했다.

## 슬라이드 3. Async Processing Flow

### 제목

`업로드에서 점수 반영까지의 비동기 처리 흐름`

### 넣을 내용

1. `POST /uploads` 요청 수신
2. 클라이언트가 presigned URL로 S3에 직접 업로드
3. S3 이벤트가 `analysis-queue`로 전달
4. `analysis-worker`가 OCR mock 처리 후 점수 계산
5. `score-service`가 RDS와 Redis에 점수 반영
6. `notification-queue`를 통해 후속 알림 처리

### 추천 시각자료

- 순서도 또는 화살표 기반 파이프라인 다이어그램

### 발표 포인트

- 사용자 응답 시간과 무거운 처리를 분리해 체감 성능과 안정성을 동시에 가져갔다.
- 큐 기반 구조로 재처리와 장애 격리를 쉽게 만들었다.

## 슬라이드 4. Service Split by SLA

### 제목

`서비스 분리 기준: 도메인보다 SLA`

### 넣을 내용

| 서비스 | 역할 | 호출 방식 | 배치 위치 | 운영 포인트 |
|---|---|---|---|---|
| `user-service` | 인증, 프로필, 업로드 시작점 | 동기 | `critical` | ALB 진입점, 안정성 우선 |
| `score-service` | 점수, 캐릭터, 랭킹 | 동기 | `critical` | read-heavy, HPA 대상 |
| `analysis-worker` | 분석 처리 | 비동기 | `batch` | spot interruption 대응 |
| `notification-worker` | 알림 처리 | 비동기 | `batch` | 지연 허용, 비용 최적화 |

### 추천 시각자료

- 표 하나로 끝내는 것이 가장 깔끔하다.

### 발표 포인트

- 이 분리 덕분에 각 서비스에 서로 다른 배포 전략, 스케일링 전략, 비용 전략을 적용할 수 있었다.

## 슬라이드 5. GitOps Delivery Model

### 제목

`GitHub Actions + ECR + ArgoCD 기반 GitOps 배포`

### 넣을 내용

- 애플리케이션 코드는 GitHub Actions에서 빌드 후 ECR로 push
- 인프라 레포의 Helm values가 배포 소스 오브 트루스 역할 수행
- ArgoCD가 변경을 감지해 자동 sync 및 self-heal 수행
- 직접 `kubectl apply`하는 방식에서 선언형 배포로 전환

### 추천 시각자료

- `GitHub Actions -> ECR -> infra repo -> ArgoCD -> EKS` 흐름도
- self-heal 시연 캡처

### 발표 포인트

- 이미지 빌드와 배포 선언을 분리해 변경 이력을 더 명확히 관리했다.
- 장애 복구도 "명령어"가 아니라 "선언 상태 복원"으로 처리되도록 설계했다.

## 슬라이드 6. Observability Stack

### 제목

`관측성 구성: Metrics, Logs, Traces, Cost`

### 넣을 내용

- Prometheus + Grafana로 RED 메트릭 확인
- CloudWatch Logs를 통해 서비스 로그 수집
- OTel Collector + Tempo로 critical path 추적
- KubeCost로 네임스페이스/노드 비용 가시화

### 추천 시각자료

- 관측성 스택 다이어그램
- Grafana 대시보드 1장

### 발표 포인트

- "배포 성공"만 확인하는 것이 아니라 "서비스가 어떤 상태로 동작하는지"를 수치로 보는 구조를 만들었다.
- 업로드 요청이 어떤 경로를 타고 점수 반영까지 가는지 추적할 수 있도록 구성했다.

## 슬라이드 7. Recovery Scenarios

### 제목

`장애 유형별 복구 전략`

### 넣을 내용

| 장애 유형 | 복구 방식 | 자동화 수준 | 핵심 기술 |
|---|---|---|---|
| Pod 삭제 | ReplicaSet / ArgoCD 복구 | 자동 | Kubernetes, ArgoCD |
| Spot 축출 | graceful shutdown 후 재처리 | 자동 | Karpenter, SQS |
| S3 객체 삭제 | 버전 복구 + Lambda | 자동 | S3 Versioning, Object Lock, EventBridge |
| RDS 데이터 손실 | 특정 시점 복원 | 반자동 | RDS PITR |

### 추천 시각자료

- 표와 작은 아이콘 조합
- [RTO / RPO Matrix](./rto-rpo-matrix.md) 일부 발췌

### 발표 포인트

- 단일 리전이라도 의미 있는 DR은 충분히 설계할 수 있다는 점을 보여준다.
- 복구 전략은 서비스별 특성과 장애 범위에 맞춰 다르게 가져갔다.

## 슬라이드 8. Spot Strategy and Cost Optimization

### 제목

`Karpenter와 Spot을 이용한 비용 최적화`

### 넣을 내용

- worker 계층은 `batch` 노드풀에서 spot 우선 배치
- API 계층은 `critical` 노드풀에서 on-demand 유지
- interruption 발생 시 in-flight 작업 정리 후 다른 worker가 재처리
- 비용 절감과 안정성 사이에서 역할 기반으로 분리 운영

### 추천 시각자료

- 노드 라벨과 파드 배치 캡처
- [Spot Interruption Drill](./06-spot-interruption-drill.md) 핵심 캡처
- [01-workload-placement-before-drill-redo.png](./evidence/07-dr-drill/00-overview/01-workload-placement-before-drill-redo.png)

### 발표 포인트

- 모든 워크로드를 spot에 올리는 대신, 지연 허용 여부를 기준으로 비용 최적화를 적용했다.
- 이 구조는 비용 절감뿐 아니라 운영 의도를 설명하기에도 좋다.

## 슬라이드 9. Load Test and Autoscaling

### 제목

`부하 테스트와 오토스케일링 검증`

### 넣을 내용

- 업로드 burst 테스트로 비동기 파이프라인 처리 안정성 점검
- 랭킹 read-heavy 테스트로 `score-service` HPA scale-out 확인
- metrics-server, HPA, Karpenter까지 실제 동작 검증
- 개선 후 `p95`, error rate, 처리량 기준으로 안정성 확인

### 추천 시각자료

- [Load Test Report](./load-test-report.md) 요약 표
- [01-score-service-hpa-scale-out.png](./evidence/08-load-test/01-score-service-hpa-scale-out.png)
- [02-ranking-read-second-run-result.png](./evidence/08-load-test/02-ranking-read-second-run-result.png)

### 발표 포인트

- 부하 테스트는 단순 성능 자랑보다, 스케일링 정책이 실제로 동작하는지 검증하는 과정이었다.
- HPA target unknown, metrics-server 미구성 같은 실제 운영 문제도 여기서 드러나고 해결됐다.

## 슬라이드 10. Troubleshooting Highlights

### 제목

`실제 운영 중 마주친 문제와 해결`

### 넣을 내용

| 문제 | 원인 | 해결 |
|---|---|---|
| HPA가 `cpu: <unknown>` 상태 | metrics-server 부재, ArgoCD project 허용 범위 누락 | metrics-server 앱 추가 및 project 허용 설정 |
| destroy가 VPC 단계에서 멈춤 | ALB, EKS 보안 그룹, worker 인스턴스 잔재 | 잔여 리소스 추적 후 정리 |
| S3 버킷 삭제 실패 | Object Lock Governance와 버전 객체 잔존 | 버전 삭제 + governance bypass |
| ECR repository 삭제 실패 | 이미지가 남아 있어 non-empty 상태 | `force` 삭제로 정리 |

### 추천 시각자료

- `before / after` 상태 캡처
- 로그 한 줄과 명령어 결과 스니펫

### 발표 포인트

- 이 장표는 프로젝트의 깊이를 가장 잘 보여준다.
- "무엇을 만들었는지"보다 "문제가 생겼을 때 어떻게 원인을 추적했는지"가 면접에서 더 강하다.

## 슬라이드 11. Disaster Recovery Evidence

### 제목

`DR 검증 결과`

### 넣을 내용

- S3 자동 복구: 삭제 후 Lambda가 버전 복원
- RDS PITR: 손실 시점 이전으로 새 인스턴스 복원
- Spot worker drain: 메시지 재수신과 재처리 확인
- 각 시나리오별 RTO/RPO를 실제 측정치로 기록

### 추천 시각자료

- [DR Drill](./07-dr-drill.md) 요약
- [03-lambda-recovery-log-console.png](./evidence/07-dr-drill/01-s3-auto-recovery/03-lambda-recovery-log-console.png)
- [04-cloudwatch-recovered-metric.png](./evidence/07-dr-drill/01-s3-auto-recovery/04-cloudwatch-recovered-metric.png)
- [05-pitr-instance-available.png](./evidence/07-dr-drill/03-rds-pitr/05-pitr-instance-available.png)

### 발표 포인트

- "백업이 있다"보다 "실제로 복원해봤다"가 훨씬 중요하다.
- DR은 문서만 만든 것이 아니라 시연과 로그, 결과 기록까지 남겼다.

## 슬라이드 12. What This Project Proved

### 제목

`이 프로젝트를 통해 증명한 것`

### 넣을 내용

- Terraform으로 AWS 기반 인프라를 선언적으로 구축하고 반복 재생성할 수 있다.
- ArgoCD 기반 GitOps와 self-heal 복구 흐름을 운영에 적용할 수 있다.
- Karpenter와 Spot 전략으로 비용을 줄이면서 worker 안정성을 유지할 수 있다.
- 메트릭, 로그, 트레이스, 비용을 함께 보는 운영 체계를 만들 수 있다.
- 단일 리전에서도 의미 있는 DR 시나리오를 구성하고 검증할 수 있다.

### 추천 시각자료

- 핵심 키워드 5개를 카드 형태로 정리
- 마지막 한 줄 메시지

### 발표 포인트

- 이 프로젝트의 핵심은 기능 데모가 아니라 운영 가능한 시스템을 끝까지 만들어 본 경험이다.
- 발표 마지막은 "클라우드 환경에서 실제로 부딪히는 문제를 설계, 배포, 관측, 복구까지 연결해 봤다"는 메시지로 마무리한다.

## 장표 제작 팁

- 한 장에 메시지는 하나만 둔다.
- 아키텍처 장표는 텍스트보다 박스와 화살표 위주로 단순하게 그린다.
- 수치 장표는 `before / after`, `목표 / 실측` 구조를 우선 사용한다.
- 로그 캡처는 전체 화면보다 핵심 3~5줄이 보이게 잘라서 넣는다.
- GitOps, DR, HPA 같은 장표는 "구성 설명"보다 "검증 결과"를 크게 배치한다.

## 우선 제작 순서

1. 전체 아키텍처 다이어그램
2. 비동기 처리 흐름 다이어그램
3. GitOps 배포 흐름
4. 장애 복구 표
5. 부하 테스트 결과표
6. 트러블슈팅 요약 장표

## 같이 쓰면 좋은 근거 문서

- [Architecture Components](./architecture-components.md)
- [Load Test Report](./load-test-report.md)
- [RTO / RPO Matrix](./rto-rpo-matrix.md)
- [Spot Interruption Drill](./06-spot-interruption-drill.md)
- [DR Drill](./07-dr-drill.md)
- [Demo Video Script](./demo-video-script.md)
