# Phase 7 DR Evidence

Phase 7 DR 시연 중 캡처와 원본 증거를 모아두는 폴더다.

Codex가 진행 중에 다음 형식으로 요청하면, 해당 경로와 파일명으로 저장한다.

```text
지금 캡처:
bodybuddy-infra/reports/evidence/phase-7-dr/<section>/<filename>.png
```

## Folder Map

| 폴더 | 용도 |
|---|---|
| `00-overview/` | RTO/RPO 개념, 전체 DR 매트릭스 초안, 아키텍처 개요 |
| `01-s3-auto-recovery/` | S3 삭제 이벤트, EventBridge, Lambda 자동 복구, CloudWatch/Grafana 지표 |
| `02-s3-manual-recovery/` | S3 Versioning 삭제 마커, 이전 버전 수동 복구 |
| `03-rds-pitr/` | RDS 데이터 손실 시뮬레이션, PITR 복원, 검증 쿼리 |
| `04-gitops-k8s-recovery/` | ArgoCD self-heal, Pod/Deployment 삭제 복구, Karpenter node 복구 |
| `99-final-matrix/` | 최종 발표/블로그용 RTO/RPO 표, 요약 대시보드 |

## Naming Rule

파일명은 정렬이 쉽게 앞에 번호를 붙인다.

```text
01-before-failure.png
02-failure-triggered.png
03-recovery-running.png
04-recovery-complete.png
05-metric-rto.png
```

## Capture Tips

- 가능하면 브라우저 전체보다 핵심 패널이 크게 보이게 캡처한다.
- AWS 콘솔 캡처는 계정 ID, 이메일, secret 값이 보이면 가린다.
- Grafana 캡처는 시간 범위와 패널 제목이 보이게 둔다.
- 터미널 캡처보다 가능하면 명령어 출력은 리포트 본문에 텍스트로 남긴다.
