# Demo Video Script

## 목적

3~5분 안에 BodyBuddy가 단순 API 프로젝트가 아니라 EKS 운영, GitOps, 관측성, 비용 최적화, DR을 실제로 검증한 프로젝트임을 보여준다.

## 추천 흐름

### 1. 프로젝트 소개

보여줄 화면:

- Root README
- Architecture diagram

말할 내용:

> BodyBuddy는 인바디 결과 업로드를 점수화하는 서비스입니다. 비즈니스 기능보다 AWS EKS 위에서 비동기 MSA를 운영하는 역량을 보여주기 위해 만들었습니다.

### 2. GitOps와 서비스 상태

보여줄 화면:

- ArgoCD applications
- `kubectl get pods -n bodybuddy -o wide`

말할 내용:

> 배포는 ArgoCD App-of-Apps로 관리합니다. API 서비스는 on-demand 노드에, worker는 spot 노드에 배치했습니다.

### 3. 업로드 비동기 처리

보여줄 화면:

- `POST /api/v1/uploads`
- `analysis-worker` logs
- `GET /api/v1/my/score`

말할 내용:

> 업로드 요청은 바로 큐에 들어가고, worker가 mock OCR과 점수 계산을 수행합니다. 처리 결과는 score-service를 통해 캐릭터 점수에 반영됩니다.

### 4. Spot interruption 복구

보여줄 화면:

- Spot worker node drain evidence
- re-queue 로그
- worker 재스케줄 결과

말할 내용:

> Worker는 spot 노드에서 동작합니다. 처리 중 노드가 내려가도 메시지를 유실하지 않도록 graceful shutdown과 re-queue를 구현했습니다.

### 5. DR 시연 결과

보여줄 화면:

- S3 Lambda recovery log
- RDS PITR available evidence
- RTO/RPO matrix

말할 내용:

> 단일 리전 안에서 S3 삭제와 RDS 데이터 손실을 나눠 복구했습니다. S3는 자동 복구, RDS는 PITR 기반 반자동 복구로 정리했습니다.

### 6. 부하 테스트와 HPA

보여줄 화면:

- k6 ranking-read 결과
- HPA `1 -> 4 replicas`
- load-test report

말할 내용:

> ranking read 부하에서 p95가 튀는 것을 확인한 뒤, metrics-server와 HPA를 적용했습니다. 그 결과 p95가 약 417ms에서 36ms 수준으로 내려갔습니다.

## 마무리 멘트

> 이 프로젝트에서 가장 집중한 것은 기능 개수가 아니라, 장애와 부하가 생겼을 때 시스템이 어떻게 관찰되고 복구되는지를 실제 증거로 남기는 것이었습니다.
