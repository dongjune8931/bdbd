# 06. Karpenter + Spot — 비용 최적화와 워크로드 격리

## 개요

06번 작업의 목표는 두 가지다:
1. **비용 최적화**: 워커 Pod을 Spot 인스턴스에서 실행하여 on-demand 대비 70~90% 비용 절감
2. **워크로드 격리**: API(on-demand, critical) vs Worker(spot, batch)로 NodePool을 분리하여 Spot 중단이 API에 영향을 주지 않도록 함

06번 작업 완료 상태:
- critical-pool (on-demand): user-service, score-service 실행
- batch-pool (spot): analysis-worker, notification-worker 실행
- Spot interruption 강제 발생(kubectl drain) → graceful shutdown 로그 확인 → 대체 노드 자동 생성 → worker 재스케줄
- 멱등성 확인: 최종 점수 = 단위 점수 × 처리 횟수 (중복 없음)

---

## 1. Karpenter vs Cluster Autoscaler

### 1.1 Cluster Autoscaler (구 방식)

```
Pod 스케줄 실패 감지
    ↓
어떤 ASG(Auto Scaling Group)를 늘릴지 결정
    ↓
ASG DesiredCapacity +1 → EC2 시작
    ↓
노드 준비 (5~10분)
    ↓
Pod 스케줄
```

**제한사항:**
- ASG 단위로 스케일. ASG에 정의된 인스턴스 타입만 사용 가능
- 인스턴스 타입 변경 시 ASG 설정 수정 필요
- Spot → on-demand 폴백이 복잡 (여러 ASG 필요)
- 노드 통합(consolidation)이 느림

### 1.2 Karpenter (신 방식)

```
Pod의 resource request/limits 분석
    ↓
최적 인스턴스 타입 실시간 결정 (다양한 타입 중 최저 비용)
    ↓
EC2 Fleet API로 직접 인스턴스 시작 (ASG 우회)
    ↓
노드 준비 (2~3분, 더 빠름)
    ↓
Pod 스케줄
```

**장점:**
- 동적 인스턴스 타입 선택 (Pod에 최적화)
- EC2 Fleet API를 직접 사용하여 Spot 시장에서 최저가 인스턴스 선택
- 빠른 노드 통합 (비어있는 노드 자동 제거)
- `NodePool`과 `EC2NodeClass` CRD로 선언적 관리

**📌 개념 설명: NodePool vs NodeGroup (ASG)**
NodeGroup은 미리 정의된 인스턴스 타입과 수량의 박스다. Karpenter의 NodePool은 "이런 요구사항을 만족하는 인스턴스를 알아서 찾아라"는 선언이다.

---

## 2. NodePool 설계

### 2.1 critical-pool (API용, on-demand)

```yaml
# bodybuddy-infra/argocd/apps/karpenter-nodepools.yaml
# (또는 Helm chart로 관리)
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: critical-pool
spec:
  template:
    metadata:
      labels:
        workload-type: critical    # Pod의 nodeSelector 매칭용 라벨
    spec:
      nodeClassRef:
        group: karpenter.k8s.aws
        kind: EC2NodeClass
        name: default
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand"]         # Spot 없음, 안정성 최우선
        - key: node.kubernetes.io/instance-type
          operator: In
          values:
            - t3.medium
            - t3.large
            - m6i.large
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
  limits:
    cpu: "16"      # 이 NodePool 전체 CPU 상한
    memory: "64Gi"
  disruption:
    consolidationPolicy: WhenEmpty      # 비어있을 때만 제거
    consolidateAfter: 30s
```

### 2.2 batch-pool (Worker용, Spot 우선)

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: batch-pool
spec:
  template:
    metadata:
      labels:
        workload-type: batch     # Worker Pod의 nodeSelector 매칭용 라벨
    spec:
      nodeClassRef:
        group: karpenter.k8s.aws
        kind: EC2NodeClass
        name: default
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["spot", "on-demand"]  # spot 우선, 없으면 on-demand 폴백
        - key: node.kubernetes.io/instance-type
          operator: In
          values:
            - t3.medium
            - t3.large
            - m6i.large
            - m6a.large              # 다양한 타입 → Spot 가용성 높아짐
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
  limits:
    cpu: "32"
    memory: "128Gi"
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized   # 덜 쓰이는 노드도 제거
    consolidateAfter: 1m
```

### 2.3 EC2NodeClass

```yaml
apiVersion: karpenter.k8s.aws/v1
kind: EC2NodeClass
metadata:
  name: default
spec:
  amiFamily: AL2    # Amazon Linux 2

  # Karpenter가 노드를 시작할 서브넷 (private subnet)
  subnetSelectorTerms:
    - tags:
        karpenter.sh/discovery: bodybuddy-dev

  # 보안그룹 (EKS 노드 보안그룹)
  securityGroupSelectorTerms:
    - tags:
        karpenter.sh/discovery: bodybuddy-dev

  # 노드 IAM Role
  role: bodybuddy-dev-karpenter-node

  # EBS 볼륨 암호화 (보안 요구사항)
  blockDeviceMappings:
    - deviceName: /dev/xvda
      ebs:
        volumeSize: 50Gi
        volumeType: gp3
        encrypted: true    # 보안: EBS 암호화
```

---

## 3. nodeSelector로 워크로드 Pinning

### 3.1 API 서비스 → critical-pool

```yaml
# deploy/helm/user-service/templates/deployment.yaml
spec:
  template:
    spec:
      nodeSelector:
        workload-type: critical   # critical-pool 노드에만 스케줄

      # Spot 노드에는 스케줄되지 않도록 toleration 없음
      tolerations: []
```

### 3.2 Worker → batch-pool

```yaml
# deploy/helm/analysis-worker/templates/deployment.yaml
spec:
  template:
    spec:
      nodeSelector:
        workload-type: batch      # batch-pool 노드에만 스케줄

      terminationGracePeriodSeconds: 120   # Spot 인터럽션 대응
```

**📌 개념 설명: nodeSelector vs affinity**
- `nodeSelector`: 단순한 키-값 매칭. "이 라벨이 있는 노드에만"
- `nodeAffinity`: 더 복잡한 규칙. preferred(우선) vs required(필수), operator 지원

이 프로젝트는 단순하게 `nodeSelector`를 사용한다.

---

## 4. Spot 인스턴스란?

### 4.1 개념

**📌 개념 설명: EC2 Spot 인스턴스**
AWS의 여분 컴퓨팅 용량을 경매 방식으로 저렴하게 쓰는 것이다.

| 특성 | On-demand | Spot |
|---|---|---|
| 가격 | 정가 | 정가의 10~30% |
| 안정성 | 언제든 실행 | AWS가 언제든 회수 가능 |
| Notice | 없음 | 회수 2분 전 notice |
| 적합 워크로드 | SLA 있는 서비스 | 중단 허용되는 배치 작업 |

Spot 인스턴스가 회수될 때:
1. `Spot Instance Interruption Warning` EventBridge 이벤트 발생 (2분 전)
2. Karpenter가 SQS 큐를 폴링하여 이 이벤트 수신
3. 해당 노드에 Spot 인터럽션 taint 추가
4. Karpenter가 Pod을 다른 노드로 이동 (eviction)
5. 노드가 SIGTERM 수신 → graceful shutdown 시작
6. Karpenter가 대체 노드를 미리 프로비저닝

### 4.2 Spot으로 얼마나 절약되나?

```
t3.medium on-demand: $0.0416/hr
t3.medium spot:      $0.0125/hr (약 70% 절감)

analysis-worker 2개 pod × 24시간 × 30일
- on-demand: $0.0416 × 2 × 24 × 30 = $59.9/월
- spot:      $0.0125 × 2 × 24 × 30 = $18/월
절감: 약 $42/월 (70%)
```

---

## 5. Spot Interruption 대응 아키텍처

### 5.1 전체 흐름

```
AWS EC2 회수 결정
    │
    │ ① Spot Instance Interruption Warning 이벤트
    ▼
[EventBridge]
    │ ② 이벤트 전달
    ▼
[SQS: Karpenter Interruption Queue]
    │ ③ Karpenter 폴링
    ▼
[Karpenter Controller]
    │ ④ 해당 노드에 Taint 추가 (NoSchedule)
    │ ⑤ 대체 노드 프로비저닝 시작
    │ ⑥ 해당 노드의 Pod eviction (drain)
    ▼
[Pod on 구 노드]
    │ ⑦ SIGTERM 수신
    ▼
[analysis-worker Graceful Shutdown]
    │ ⑧ 신규 SQS 메시지 수신 중단
    │ ⑨ 진행 중인 메시지 처리 완료 대기 (최대 110초)
    │ ⑩ SQS 메시지 삭제 후 종료
    ▼
[Pod on 새 노드]
    │ ⑪ 새 노드에서 재스케줄
    │ ⑫ SQS에서 메시지 재수신
    ▼
처리 재개 (멱등성으로 중복 처리 없음)
```

### 5.2 terminationGracePeriodSeconds: 120

```yaml
# deploy/helm/analysis-worker/templates/deployment.yaml
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 120  # Spot notice 120초와 일치

      containers:
        - name: analysis-worker
          # ...
```

```go
// cmd/analysis-worker/main.go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
defer stop()

go worker.Run(ctx)  // SIGTERM 받으면 ctx.Done() 신호

<-ctx.Done()
slog.Info("received shutdown signal, waiting for in-flight messages")

// 110초 내에 처리 완료
shutdownCtx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
defer cancel()
worker.Shutdown(shutdownCtx)
slog.Info("worker stopped gracefully")
```

**왜 110초인가 (다시 정리):**
- Spot notice: 2분 = 120초 전에 발생
- `terminationGracePeriodSeconds: 120` → Kubernetes가 최대 120초 대기
- 110초 shutdown timeout = 120초에서 10초 여유 (Kubernetes 처리 시간)

---

## 6. SQS Visibility Timeout과 멱등성

### 6.1 Spot 중단 시 메시지 흐름

```
analysis-worker가 SQS 메시지 수신
visibility timeout 시작 (30초)
    │
    │ 처리 중 (10초 경과)
    │
    ↓ Spot 인터럽션 발생 → SIGTERM 수신
analysis-worker graceful shutdown 시작
    │
    │ 현재 처리 중인 메시지 완료 시도 (10초 남음)
    │ 완료 → SQS에서 삭제 → 정상
    │ 못 완료 → visibility timeout 만료 → 메시지 다시 보임
    ↓
새 analysis-worker Pod이 SQS 메시지 재수신
    │
    │ 이미 처리된 경우: 멱등성으로 무시
    │ 처리 안 된 경우: 정상 처리
    ↓
중복 없이 정확히 한 번 처리됨 (effectively once)
```

### 6.2 멱등성 구현

```go
// internal/queue/worker.go
func (w *Worker) processMessage(ctx context.Context, msg types.Message) {
    var payload AnalysisPayload
    json.Unmarshal([]byte(*msg.Body), &payload)

    // 멱등성 체크: 이미 처리한 메시지인가?
    key := "processed:analysis:" + payload.UploadID
    set, err := w.redis.SetNX(ctx, key, "1", 24*time.Hour).Result()
    if err != nil {
        slog.WarnContext(ctx, "redis setnx failed, proceeding", "error", err)
        // Redis 장애 시: DB unique constraint이 최후 방어선
    } else if !set {
        // 이미 처리됨 → 메시지 삭제하고 종료
        slog.InfoContext(ctx, "message already processed, skipping",
            "upload_id", payload.UploadID)
        w.deleteMessage(ctx, msg.ReceiptHandle)
        return
    }

    // 실제 처리
    score, err := w.processAnalysis(ctx, payload)
    if err != nil {
        // 실패 시 Redis 키 삭제 → 재시도 가능하게
        w.redis.Del(ctx, key)
        slog.ErrorContext(ctx, "analysis failed", "error", err)
        return
    }

    // score-service 호출
    if err := w.updateScore(ctx, payload.UserID, payload.UploadID, score); err != nil {
        w.redis.Del(ctx, key)
        return
    }

    // 성공 시 SQS 메시지 삭제
    w.deleteMessage(ctx, msg.ReceiptHandle)
}
```

```sql
-- DB 레벨 멱등성: score_history 테이블
-- upload_id UNIQUE constraint
INSERT INTO score_history (user_id, upload_id, score)
VALUES ($1, $2, $3)
ON CONFLICT (upload_id) DO NOTHING;
-- 동일 upload_id가 이미 있으면 조용히 무시
```

---

## 7. Spot Interruption 드릴: kubectl drain

### 7.1 실제 Spot interruption 대신 kubectl drain 사용

AWS Fault Injection Simulator(FIS)를 사용하면 실제 Spot interruption을 강제로 발생시킬 수 있다. 하지만 이 드릴에서는 `kubectl drain`으로 동일한 SIGTERM 흐름을 재현한다.

`kubectl drain`은:
1. 노드에 `NoSchedule` taint 추가
2. 노드의 Pod에 eviction 요청 (SIGTERM)
3. `terminationGracePeriodSeconds` 동안 대기
4. 종료 완료 후 노드가 비워짐

이는 Spot interruption 시 Karpenter가 하는 행동과 동일하다.

### 7.2 드릴 시나리오

```bash
# 1. 사전 상태 확인
kubectl get nodes
# NAME                        STATUS   ROLES
# ip-10-20-11-100.ec2...     Ready    <none>   # critical-pool (API)
# ip-10-20-11-101.ec2...     Ready    <none>   # batch-pool (worker) ← drain 대상

kubectl get pods -n bodybuddy -o wide
# analysis-worker-xxx   Running  ip-10-20-11-101...
# notification-worker   Running  ip-10-20-11-101...
# user-service-xxx      Running  ip-10-20-11-100...

# 2. 처리 중인 메시지가 있는 상태 만들기
# SQS에 메시지 3개 발행
for i in 1 2 3; do
  curl -X POST http://<alb>/api/v1/uploads \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"filename":"test.jpg"}'
done

# 3. Batch 노드 drain (Spot interruption 재현)
BATCH_NODE="ip-10-20-11-101.ap-northeast-2.compute.internal"
kubectl drain $BATCH_NODE \
  --ignore-daemonsets \
  --delete-emptydir-data \
  --grace-period=120      # terminationGracePeriodSeconds와 일치

# 4. Worker graceful shutdown 로그 확인 (별도 터미널)
kubectl logs -f analysis-worker-xxx -n bodybuddy
# 2024-01-15T10:23:45Z INFO received shutdown signal
# 2024-01-15T10:23:45Z INFO stopping new message reception
# 2024-01-15T10:23:47Z INFO processing in-flight message upload_id=abc123
# 2024-01-15T10:23:52Z INFO message processed successfully upload_id=abc123
# 2024-01-15T10:23:52Z INFO worker stopped gracefully
```

### 7.3 드릴 결과 확인

```bash
# 5. Karpenter가 새 노드 자동 생성 확인
kubectl get nodes --watch
# ip-10-20-11-101...  NotReady  → Ready → 삭제됨
# ip-10-20-12-50...   Ready     ← 새 batch-pool 노드 자동 생성

# 새 노드 이름 확인 (예: batch-pool-h2l5n → ip-10-20-11-12...)

# 6. Worker 재스케줄 확인
kubectl get pods -n bodybuddy -o wide
# analysis-worker-yyy   Running  ip-10-20-12-50...  ← 새 노드에서 실행

# 7. 멱등성 확인: 최종 점수
curl http://<alb>/api/v1/scores/me -H "Authorization: Bearer $TOKEN"
# {"score": 174}  # = 58 × 3 (메시지 3개 × 단위 점수 58)
# 중복 처리가 있었다면: 58 × 6 = 348 이 됐을 것
```

### 7.4 드릴 결과 해석

**성공 기준:**
1. **Graceful shutdown 로그 확인** → SIGTERM 수신 후 진행 중인 메시지를 완료하고 종료
2. **Karpenter 대체 노드 자동 생성** → batch-pool에 새 Spot 노드 프로비저닝
3. **Worker 재스케줄** → 새 노드에서 analysis-worker, notification-worker 재시작
4. **API는 영향 없음** → user-service, score-service는 critical-pool(on-demand)에서 계속 실행
5. **멱등성 확인** → 최종 점수 = 58 × 3 = 174 (중복 없음)

---

## 8. 면접 Talking Points

### 8.1 "왜 API와 Worker를 다른 NodePool로 분리했나?"

> API 서비스(user-service, score-service)는 사용자가 기다리는 동기 요청을 처리합니다. 응답 시간 SLA가 있어 Spot 인터럽션으로 인한 중단을 허용할 수 없습니다. 반면 분석 워커와 알림 워커는 비동기로 SQS 메시지를 처리하기 때문에 수분의 처리 지연은 허용됩니다. 이 차이를 기반으로 API는 on-demand NodePool에, 워커는 Spot NodePool에 배치했습니다. 결과적으로 워커 비용을 70% 절감하면서도 API 안정성에는 영향이 없습니다.

### 8.2 "Spot 인터럽션 시 데이터 무결성을 어떻게 보장하나?"

> 세 가지 레이어로 보장합니다.
> 첫째, SQS의 visibility timeout입니다. 워커가 메시지를 받아 처리하는 동안 다른 컨슈머에게 보이지 않습니다. 처리 완료 후 삭제하지 않으면 timeout 후 메시지가 다시 보여 다른 워커가 재처리합니다.
> 둘째, Graceful shutdown입니다. SIGTERM 수신 시 신규 메시지 수신을 중단하고 진행 중인 메시지를 110초 내에 완료합니다.
> 셋째, 멱등성입니다. Redis SETNX로 처리된 메시지 ID를 기록하고, DB의 upload_id unique constraint으로 중복 INSERT를 방지합니다. 실제 드릴에서 메시지 3개 처리 후 drain 시 점수가 174점(58×3)으로 중복 없이 정확히 처리됨을 확인했습니다.

### 8.3 "Karpenter의 Spot 폴백 전략은?"

> batch-pool NodePool의 capacity-type에 `["spot", "on-demand"]`를 모두 허용했습니다. Karpenter는 Spot을 우선 시도하고, Spot 용량이 없으면 on-demand로 폴백합니다. 또한 인스턴스 타입을 t3.medium, t3.large, m6i.large, m6a.large처럼 다양하게 허용했습니다. Spot 시장에서 특정 타입이 없어도 다른 타입으로 대체할 수 있어 Spot 가용성이 높아집니다.

---

## 9. Karpenter 노드 통합 (Consolidation)

### 9.1 WhenEmpty vs WhenEmptyOrUnderutilized

```
critical-pool: consolidationPolicy: WhenEmpty
→ 노드가 완전히 비어야만 삭제. 실행 중인 Pod이 있으면 절대 삭제 안 함.
→ API 노드를 함부로 제거하지 않아 안정성 높음.

batch-pool: consolidationPolicy: WhenEmptyOrUnderutilized
→ 노드가 비어있거나 충분히 활용되지 않을 때 삭제.
→ 워커가 줄어들면 노드도 자동으로 줄어들어 비용 절감.
```

**📌 개념 설명: Consolidation**
여러 노드에 분산된 Pod을 더 적은 노드로 모으고, 빈 노드를 삭제하는 과정이다. 예를 들어:
- 노드 3개, 각 30% 사용률
- Karpenter가 Pod을 노드 1~2개로 재배치
- 노드 3을 삭제
→ 비용 절감

`consolidateAfter: 1m`: 1분간 통합 조건을 만족하면 실행.

---

## 10. Karpenter NodeClaim 모니터링

```bash
# Karpenter가 관리하는 노드 확인
kubectl get nodeclaim

# NAME                   TYPE         ZONE              NODE
# critical-pool-xxxxx    t3.medium    ap-northeast-2a   ip-10-20-11-100...
# batch-pool-yyyyy       t3.medium    ap-northeast-2a   ip-10-20-11-101...

# NodePool 사용량 확인
kubectl get nodepool

# NAME            READY   NODES   CPU    MEMORY
# critical-pool   True    1       2      4Gi
# batch-pool      True    1       2      4Gi

# Karpenter 이벤트 확인
kubectl events --for NodePool/batch-pool -n karpenter
```

---

## 핵심 요약

- **Karpenter vs Cluster Autoscaler**: ASG 단위 스케일 vs Pod 요구사항 기반 동적 인스턴스 선택. Karpenter가 더 빠르고 비용 효율적.
- **NodePool 2개 분리**: critical(on-demand, API) vs batch(spot, worker). 트래픽 특성(SLA 유무)이 설계 근거.
- **EC2NodeClass**: AMI, subnet, securityGroup, EBS 암호화 설정. Karpenter가 노드를 시작할 때 사용하는 설정.
- **Spot 절감**: on-demand 대비 70~90% 절감. t3.medium 기준 월 ~$42 절감 (worker 2개 기준).
- **Interruption 대응 3단계**: EventBridge → SQS → Karpenter 감지 → SIGTERM → Graceful shutdown. 2분 notice 안에 완료.
- **terminationGracePeriodSeconds: 120 + 110초 timeout**: Spot notice 120초에 맞춘 설정.
- **SQS at-least-once**: 메시지가 두 번 전달될 수 있음. Redis SETNX + DB unique constraint으로 중복 방지.
- **kubectl drain으로 드릴**: 실제 FIS 없이도 SIGTERM 흐름 재현 가능. graceful shutdown 로그, 대체 노드 생성, 멱등성을 모두 검증.
- **드릴 결과**: 점수 174 = 58 × 3. 중복 처리 없이 멱등성 동작 확인. API(user-service, score-service)는 critical-pool에서 영향 없이 계속 실행.
