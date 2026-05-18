# RDS PITR Restore Runbook

## 목적

BodyBuddy dev 환경에서 RDS PostgreSQL 데이터 손실이 발생했을 때, 특정 시점으로 복원 인스턴스를 생성하고 검증하는 절차를 정리한다.

이 문서는 2026-05-18 1차 드릴에서 확인된 실제 문제점까지 반영한다.

핵심 교훈:

1. `LatestRestorableTime`는 삭제 직후 즉시 원하는 시점까지 따라오지 않을 수 있다.
2. restore 시 `DBSubnetGroup`를 명시하지 않으면 복원 인스턴스가 `default` subnet group으로 올라가 운영 경로 검증이 꼬일 수 있다.

---

## 대상 환경

- 리전: `ap-northeast-2`
- Source DB instance: `bodybuddy-dev-postgres`
- 엔진: `PostgreSQL`
- 기본 프로파일: `terraform-bodybuddy`

---

## 사전 조건

아래 조건을 먼저 확인한다.

1. source DB instance가 `available` 상태다.
2. automated backup retention이 1일 이상이며, dev 기준 현재는 `7 days`다.
3. `LatestRestorableTime`가 존재한다.
4. 복원 후 검증에 사용할 네트워크 경로를 알고 있다.
   - EKS private subnet에서 붙을 것인지
   - 별도 bastion에서 붙을 것인지

사전 확인 예시:

```bash
aws rds describe-db-instances \
  --db-instance-identifier bodybuddy-dev-postgres \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy \
  --query 'DBInstances[0].{Status:DBInstanceStatus,BackupRetentionPeriod:BackupRetentionPeriod,LatestRestorableTime:LatestRestorableTime,DBSubnetGroup:DBSubnetGroup.DBSubnetGroupName}'
```

---

## 복원 시점 결정

### 원칙

- 복원 시점은 "정상 데이터가 존재했던 시각"과 "손실 작업이 일어나기 전 시각" 사이여야 한다.
- 삭제 직후 바로 복원하려 하지 말고, 먼저 `LatestRestorableTime`가 그 시점까지 올라왔는지 확인한다.

확인 명령어:

```bash
aws rds describe-db-instances \
  --db-instance-identifier bodybuddy-dev-postgres \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy \
  --query 'DBInstances[0].LatestRestorableTime'
```

### 주의

아래 에러는 PITR 로그 지연일 가능성이 높다.

```text
The specified instance cannot be restored to a time later than <timestamp>.
```

이 경우 restore 시점을 바꾸기보다, 먼저 `LatestRestorableTime`가 충분히 따라왔는지 다시 본다.

---

## 권장 복원 절차

### 1. 복원 인스턴스 식별자 결정

예시:

- `bodybuddy-dev-postgres-pitr-20260518`

같은 날 여러 번 시도할 수 있으므로, 필요하면 뒤에 시각이나 suffix를 붙인다.

### 2. source DB와 같은 네트워크 배치 확인

중요:

- restore 시에는 가능한 한 source DB와 동일한 `DB subnet group`을 명시한다.
- 그렇지 않으면 AWS 기본값(`default`)으로 복원되어 현재 EKS private subnet 경로와 어긋날 수 있다.

source DB subnet group 확인:

```bash
aws rds describe-db-instances \
  --db-instance-identifier bodybuddy-dev-postgres \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy \
  --query 'DBInstances[0].DBSubnetGroup.DBSubnetGroupName'
```

### 3. PITR 실행

예시 템플릿:

```bash
aws rds restore-db-instance-to-point-in-time \
  --source-db-instance-identifier bodybuddy-dev-postgres \
  --target-db-instance-identifier <target-db-instance-id> \
  --restore-time <UTC-restore-time> \
  --db-instance-class db.t4g.micro \
  --db-subnet-group-name <source-subnet-group-name> \
  --no-multi-az \
  --no-publicly-accessible \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

메모:

- dev에서는 비용 때문에 `db.t4g.micro`, `--no-multi-az` 유지
- 프로덕션 성격 시연이라면 same-class restore 여부를 별도 판단

### 4. 상태 추적

```bash
aws rds describe-db-instances \
  --db-instance-identifier <target-db-instance-id> \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy \
  --query 'DBInstances[0].{Status:DBInstanceStatus,Endpoint:Endpoint.Address,DBSubnetGroup:DBSubnetGroup.DBSubnetGroupName}'
```

상태가 다음처럼 변할 수 있다.

- `creating`
- `configuring-enhanced-monitoring`
- `backing-up`
- `available`

최종 판정 기준:

1. `Status: available`
2. `Endpoint` 값 생성

---

## 검증 절차

### EKS 경유 검증

복원 인스턴스가 같은 private subnet 경로에 있다면, 임시 `psql-client` Pod나 기존 debug pod에서 접속 검증할 수 있다.

예시:

```bash
kubectl exec -n bodybuddy psql-client -- \
  psql \
  -h <restored-endpoint> \
  -U bodybuddy \
  -d bodybuddy \
  -c '\dt'
```

이후 복구 대상 레코드를 조회한다.

```sql
SELECT u.id, u.email, c.name, c.level, c.total_score
FROM users u
JOIN characters c ON c.user_id = u.id
WHERE u.email = 'phase7-pitr-20260518@example.com';
```

### 검증이 바로 안 붙는 경우

다음 항목을 먼저 의심한다.

1. 복원 인스턴스가 `default` subnet group으로 생성됨
2. SG가 현재 EKS 워커 노드에서 오는 접근을 허용하지 않음
3. source와 다른 VPC/라우팅 경로로 올라감

이 경우는 "복원 실패"가 아니라 "복원 인스턴스 검증 경로가 잘못 배치된 상태"로 분리해서 해석한다.

---

## 2026-05-18 드릴에서 확인된 실제 이슈

### 1. 스키마가 비어 있었음

초기 접속 시 `public` 스키마에 앱 테이블이 없었다.

대응:

- `bodybuddy-app/migrations/000001_init.up.sql`를 수동 적용
- 이후 `users`, `characters`, `score_history`, `inbody_uploads` 생성 확인

### 2. restore time가 허용되지 않았음

원인:

- `LatestRestorableTime`가 아직 해당 시점까지 따라오지 않았음

대응:

- restore time를 바로 바꾸지 않고, `LatestRestorableTime`를 재조회해 허용 범위 확인 후 재시도

### 3. 복원본이 `default` subnet group으로 생성됨

원인:

- restore 시 subnet group 명시 없음

영향:

- 현재 EKS private subnet 경로에서 즉시 접속 검증하기 어려울 수 있음

재발 방지:

- source DB의 subnet group 이름을 restore 명령어에 명시

---

## 성공 판정 기준

최소 성공:

1. PITR 요청이 수락된다.
2. 복원 인스턴스가 `available` 상태로 도달한다.

권장 성공:

1. `available` 도달
2. 네트워크 경로 검증 성공
3. 삭제된 테스트 레코드 조회 성공

---

## 후처리

dev 드릴이 끝나면 복원 인스턴스는 비용 절감을 위해 정리한다.

예시:

```bash
aws rds delete-db-instance \
  --db-instance-identifier <target-db-instance-id> \
  --skip-final-snapshot \
  --delete-automated-backups \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

주의:

- 운영 환경이라면 final snapshot 정책을 별도 검토
- dev 드릴에서는 복원본을 오래 유지하지 않는 것이 비용상 유리
