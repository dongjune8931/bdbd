# S3 Mass Delete Recovery Runbook

## 목적

BodyBuddy dev 환경의 이미지 버킷에서 대량 삭제가 발생했을 때, Versioning과 자동 복구 Lambda를 기준으로 어떤 순서로 확인하고 복구할지 정리한다.

이 문서는 "단일 객체 삭제 자동 복구" 시연을 바탕으로, 대량 삭제 상황으로 확장한 운영 절차다.

---

## 대상 환경

- 버킷: `bodybuddy-dev-inbody`
- 리전: `ap-northeast-2`
- 자동 복구 Lambda: `bodybuddy-dev-s3-auto-recovery`
- EventBridge rule: `bodybuddy-dev-s3-auto-recovery-s3-object-deleted`

---

## 기본 원칙

1. 먼저 **대량 삭제가 실제로 발생했는지** 확인한다.
2. 자동 복구가 커버하는 범위와, 수동 개입이 필요한 범위를 나눈다.
3. 객체 본문 삭제가 아니라 delete marker 생성이라면, 우선 Versioning 기준으로 복구한다.
4. 복구 절차 중 원본 증거는 남긴다.

---

## 사전 확인

### 1. Versioning 활성화 여부

```bash
aws s3api get-bucket-versioning \
  --bucket bodybuddy-dev-inbody \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

### 2. Lambda 상태 확인

```bash
aws lambda get-function-configuration \
  --function-name bodybuddy-dev-s3-auto-recovery \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

### 3. EventBridge rule 확인

```bash
aws events describe-rule \
  --name bodybuddy-dev-s3-auto-recovery-s3-object-deleted \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

---

## 장애 확인 절차

### 1. 삭제 이벤트 범위 파악

가능하면 prefix를 기준으로 먼저 좁힌다.

```bash
aws s3api list-object-versions \
  --bucket bodybuddy-dev-inbody \
  --prefix <prefix> \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

중점 확인 항목:

- `DeleteMarkers`
- `IsLatest: true`
- 복구 대상 key 수

### 2. Lambda 로그 확인

```bash
aws logs describe-log-streams \
  --log-group-name /aws/lambda/bodybuddy-dev-s3-auto-recovery \
  --order-by LastEventTime \
  --descending \
  --max-items 3 \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

이후 최신 stream에서 복구 로그를 본다.

```bash
aws logs get-log-events \
  --log-group-name /aws/lambda/bodybuddy-dev-s3-auto-recovery \
  --log-stream-name <latest-stream> \
  --limit 100 \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

중점 확인 항목:

- `recovered: true`
- `recovered: false`
- `duration_ms`
- 특정 prefix/key에 실패가 반복되는지

### 3. CloudWatch metric 확인

```bash
aws cloudwatch get-metric-statistics \
  --namespace BodyBuddy/DR \
  --metric-name S3AutoRecoveryRecoveredObjects \
  --start-time <start-utc> \
  --end-time <end-utc> \
  --period 60 \
  --statistics Sum \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

필요 시 실패 메트릭도 확인한다.

- `S3AutoRecoveryFailures`
- `S3AutoRecoveryDurationMilliseconds`

---

## 복구 절차

### A. 자동 복구가 이미 처리 중인 경우

조건:

- Lambda 로그에 `recovered: true`가 정상적으로 찍힘
- `list-object-versions`에서 최신 버전이 다시 일반 객체로 복귀

조치:

1. 추가 수동 삭제/수정 작업을 멈춘다.
2. 자동 복구가 끝날 때까지 key 범위별로 결과만 확인한다.
3. 증거 캡처와 metric만 남긴다.

### B. delete marker가 남아 있고 자동 복구가 실패한 경우

수동으로 delete marker를 제거한다.

1. 대상 key의 delete marker version id 확인

```bash
aws s3api list-object-versions \
  --bucket bodybuddy-dev-inbody \
  --prefix <object-key> \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

2. delete marker 제거

```bash
aws s3api delete-object \
  --bucket bodybuddy-dev-inbody \
  --key <object-key> \
  --version-id <delete-marker-version-id> \
  --bypass-governance-retention \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

3. 최신 버전이 다시 객체로 복귀했는지 재확인

```bash
aws s3api list-object-versions \
  --bucket bodybuddy-dev-inbody \
  --prefix <object-key> \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

### C. prefix 단위 다건 삭제 복구

대량 삭제에서는 개별 key를 모두 수동 처리하는 대신, 아래 전략 중 하나를 선택한다.

1. Lambda가 대부분 처리하고, 실패 key만 수동 보정
2. Inventory 또는 외부 목록이 있다면 삭제된 key 목록 기준으로 delete marker 일괄 제거

dev 환경에서는 1번이 현실적이다.

---

## 성공 판정 기준

최소 성공:

1. 삭제된 key가 `list-object-versions` 기준 다시 최신 객체로 복귀
2. Lambda 로그 혹은 수동 조치로 복구 경로가 확인됨

권장 성공:

1. CloudWatch metric에 복구 건수 기록
2. 복구 대상 key 수와 실제 복구 완료 key 수 일치
3. 실패 key가 있다면 별도 목록화

---

## 트러블슈팅 포인트

### 1. Lambda는 돌았는데 `recovered: false`만 찍힘

가능한 원인:

- 이미 다른 실행이 delete marker를 제거함
- 실제 삭제 이벤트가 아닌 후속 이벤트가 재유입됨

해석:

- 단독으로는 실패 의미가 아니다
- 최신 버전 상태와 함께 봐야 한다

### 2. delete marker는 보이는데 복구가 안 됨

가능한 원인:

- EventBridge rule 비활성화
- Lambda 권한 문제
- Object Lock / Governance 조건 충돌

조치:

1. EventBridge rule 상태 확인
2. Lambda 로그와 실패 메트릭 확인
3. 필요한 경우 `--bypass-governance-retention`으로 수동 제거

### 3. 실제 객체 본문 손실

Versioning이 켜져 있어도 본문 자체가 이전 버전 없이 사라진 경우는 다른 문제다.

이 경우:

1. 남아 있는 이전 버전 존재 여부 확인
2. 없으면 별도 백업/외부 원본 경로 확인

---

## 후처리

복구가 끝나면 아래를 남긴다.

1. 영향받은 prefix/key 범위
2. 자동 복구 성공 건수
3. 수동 개입 건수
4. Lambda duration 및 failure 유무
5. 캡처/CLI 증거 경로
