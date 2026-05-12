# HANDOFF

이 파일은 Codex와 Claude가 같은 워크스페이스를 번갈아 수정할 때
현재 상태를 빠르게 동기화하기 위한 짧은 작업 로그다.

## Rules

- 긴 설명보다 현재 상태와 다음 액션만 적는다.
- 큰 변경이 끝났을 때만 갱신한다.
- 이미 `AGENTS.md`에 있는 장기 스펙은 여기 중복해서 길게 쓰지 않는다.
- 아래 템플릿을 복사해서 최신 항목을 맨 위에 추가한다.

## Template

```md
## YYYY-MM-DD HH:MM KST
- actor: Codex | Claude
- scope: 작업 범위 한 줄
- done: 실제로 끝낸 것
- validation: 실행/검증 결과
- next: 다음 에이전트가 바로 이어서 할 일
- caution: placeholder, known issue, 건드리면 안 되는 점
```

## 2026-05-12 14:45 KST
- actor: Codex
- scope: Phase 2 EKS/Karpenter Terraform slice
- done: `bodybuddy-infra`에 `eks` 모듈을 실제 `terraform-aws-modules/eks/aws ~> 20.37` 래퍼로 구현했고, `dev` 환경에 wiring 함. `karpenter`는 controller IAM role, node IAM role/instance profile, interruption SQS queue, EventBridge rule/target까지 AWS bootstrap 자원으로 구현함.
- validation: `terraform fmt -recursive bodybuddy-infra`, `terraform -chdir=bodybuddy-infra/terraform/envs/dev init -backend=false -upgrade`, `terraform validate` 통과. `.terraform.lock.hcl` 갱신됨.
- next: `rds`와 `elasticache` 모듈 실제 구현, 이후 `envs/dev` wiring, 그 다음 `terraform plan` 가능한 상태로 진전.
- caution: 아직 `<ACCOUNT_ID>`, `<GITHUB_USER>` placeholder 남아 있음. Karpenter는 아직 Helm/chart 설치나 NodePool CRD 매니페스트까지는 안 들어감. 지금은 AWS bootstrap 자원만 준비된 상태.

## 2026-05-12 15:00 KST
- actor: Codex
- scope: Phase 2 RDS/ElastiCache Terraform slice
- done: `rds` 모듈에 DB subnet group, SG, PostgreSQL instance(`db.t4g.micro`, engine major `16`, managed master password, backup 7일) 구현. `elasticache` 모듈에 subnet group, SG, single-node Redis OSS replication group(`cache.t4g.micro`, engine `7.1`) 구현. 둘 다 `envs/dev`에 wiring 완료.
- validation: `terraform fmt -recursive bodybuddy-infra`, `terraform -chdir=bodybuddy-infra/terraform/envs/dev init -backend=false`, `terraform validate` 통과.
- next: 실제 AWS 값으로 `<ACCOUNT_ID>`, `<GITHUB_USER>` 채우고, credentials 준비 후 `terraform plan` 실행. 그 다음 필요하면 RDS/Redis 세부 튜닝과 IRSA/RDS 연결 정책 정리.
- caution: 현재는 dev 친화적인 단일 노드/단일 AZ 성격(defaults) 위주다. DR drill용 Multi-AZ는 변수로 열어놨지만 기본은 꺼져 있음. `plan/apply` 전 실제 AWS 자격 증명과 backend bootstrap 상태가 필요함.

## 2026-05-12 14:20 KST
- actor: Codex
- scope: Phase 1 로컬 검증 + Phase 2 infra scaffold 시작
- done: `bodybuddy-app` Phase 1을 실제 `docker compose up -d`로 검증했고, `bodybuddy-infra` 레포를 새로 만들고 `terraform/envs/dev`와 초기 모듈(`vpc`, `s3`, `sqs`, `ecr`) 및 나머지 모듈 골격(`eks`, `karpenter`, `rds`, `elasticache`, `iam-irsa`)을 추가함. `.dockerignore`에 `.gocache`도 추가함.
- validation: Phase 1에서 `/healthz`, `/readyz`, `/metrics` 전부 응답 확인. 회원가입 → 업로드 → 5초 후 점수 반영(`total_score=58`) 확인. 이미지 크기 25MB 이하 확인. Phase 2는 `terraform fmt -recursive bodybuddy-infra`, `terraform -chdir=bodybuddy-infra/terraform/envs/dev init -backend=false`, `terraform validate` 통과.
- next: `bodybuddy-infra`에서 EKS wrapper, Karpenter bootstrap path, RDS/ElastiCache 모듈 구현 후 `envs/dev` wiring 이어가기.
- caution: `bodybuddy-infra`에는 `<ACCOUNT_ID>`, `<GITHUB_USER>` placeholder가 남아 있음. `bodybuddy-app`은 Phase 1에서 direct enqueue 시뮬레이션과 LocalStack S3 event wiring이 동시에 있어 malformed analysis 메시지가 남을 수 있지만 다음 Phase blocker는 아님.
