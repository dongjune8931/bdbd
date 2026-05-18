# RTO / RPO Matrix

## 목적

이 문서는 BodyBuddy dev 환경에서 수행한 DR 드릴 결과를 기준으로, 장애 유형별 복구 목표와 실제 관찰값을 한 곳에 모아두는 기준 문서다.

숫자는 "이론상 기대치"가 아니라 가능한 한 실제 시연과 로그, 캡처, CLI 출력에 근거해 채운다.

관련 상세 보고서:

- [DR Drill - S3 Auto Recovery and RDS PITR](./07-dr-drill.md)
- [Karpenter + Spot Drill](./06-spot-interruption-drill.md)

---

## Matrix

| 장애 유형 | RTO 목표 | RTO 실측 | RPO 목표 | RPO 실측 | 자동화 | 근거 |
|---|---:|---:|---:|---:|---|---|
| Pod 다운 | 30초 | 미측정 | 0 | 0 | 자동 | Kubernetes self-heal, 향후 별도 GitOps/K8s 복구 드릴로 보강 |
| Node 다운 / Spot interruption | 2분 | 드릴 완료, 상세 수치 후속 보강 | 0 | 0 | 자동 | [06-spot-interruption-drill.md](./06-spot-interruption-drill.md) |
| Deployment 강제 삭제 | 5분 | 미측정 | 0 | 0 | 자동 | ArgoCD self-heal 시연 이력 있음, 별도 evidence 보강 필요 |
| S3 객체 삭제 | 2분 | 약 1.247초 | 0 | 0 | 자동 | Lambda 로그 `duration_ms=1247`, CloudWatch metric `S3AutoRecoveryRecoveredObjects` |
| RDS 데이터 손실 | 15분 | 1차 드릴에서 `available` 도달 확인, 정밀 분 단위 후속 보강 필요 | 5분 | 수 분 지연 관찰 | 반자동 | PITR restore 성공, `LatestRestorableTime` 지연 관찰 |

---

## Notes

### S3 객체 삭제

- 테스트 객체: `<S3_TEST_OBJECT_KEY>`
- 삭제 후 Lambda가 delete marker를 제거하고 원본 객체를 자동 복구했다.
- 복구 성공 로그:

```json
{
  "recovered": true,
  "duration_ms": 1247
}
```

- 이 드릴에서는 객체 본문 손실 없이 최신 버전이 다시 복귀했으므로, RPO는 사실상 `0`으로 본다.

### RDS 데이터 손실

- 테스트 레코드 `<PITR_TEST_EMAIL>` 생성 후 삭제
- `restore-db-instance-to-point-in-time`로 `bodybuddy-dev-postgres-pitr-20260518` 복원 인스턴스 생성
- 최종 `available` 상태 도달까지는 확인했지만, 이번 1차 드릴에서는 시작/종료 시각을 별도 타이머로 엄밀하게 재지 않아 분 단위 최종 수치는 후속 보정이 필요하다.
- 다만 다음 두 가지 운영 포인트는 확실히 확인했다.
  - `LatestRestorableTime`는 삭제 직후 원하는 시점까지 바로 따라오지 않는다.
  - restore 시 subnet group을 명시하지 않으면 `default` subnet group으로 복원되어 운영 검증 경로가 어긋날 수 있다.

---

## Next Update Rule

이 매트릭스는 아래 경우에 즉시 갱신한다.

1. 새 DR 드릴로 기존 수치보다 더 정확한 실측값을 확보했을 때
2. runbook 개선으로 복구 절차가 바뀌었을 때
3. 시연 환경이 변경되어 목표치나 검증 방식이 바뀌었을 때
