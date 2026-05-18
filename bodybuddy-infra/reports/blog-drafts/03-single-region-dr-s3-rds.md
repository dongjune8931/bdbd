# 단일 리전에서 진지하게 DR 하기

## 한 줄 요약

멀티 리전을 하지 않아도 DR 이야기를 만들 수 있다. 중요한 것은 복구 대상을 명확히 나누고, 실제 복구 시간을 측정하는 것이다.

## 전제

BodyBuddy는 dev 환경에서 운영되는 사이드 프로젝트다. 그래서 멀티 리전 active-active 구성은 비용과 복잡도 대비 과하다.

대신 단일 리전 안에서 실제로 자주 만날 수 있는 장애를 다뤘다.

- S3 객체 삭제
- RDS 데이터 손실
- Pod/Node 장애
- Deployment 삭제

## S3 자동 복구

S3는 Versioning을 켜고, 삭제 이벤트를 EventBridge로 받아 Lambda가 delete marker를 제거하도록 구성했다.

흐름은 단순하다.

```text
DeleteObject
  -> EventBridge Object Deleted
  -> Lambda
  -> latest delete marker 확인
  -> delete marker 제거
  -> 이전 객체 버전이 최신 상태로 복귀
```

시연에서는 Lambda 로그에서 다음 결과를 확인했다.

```json
{
  "recovered": true,
  "duration_ms": 1247
}
```

이 시나리오에서는 객체 본문이 사라진 것이 아니라 delete marker가 최신 상태가 된 것이므로, version history가 보존되는 한 RPO는 0으로 볼 수 있다.

## RDS PITR

RDS는 automated backup을 7일 보관하도록 두고, 테스트 레코드를 만든 뒤 삭제했다. 이후 point-in-time restore로 새 DB 인스턴스를 생성했다.

여기서 중요한 배움이 있었다.

- `LatestRestorableTime`은 삭제 직후 원하는 시점까지 바로 따라오지 않는다.
- restore 시 subnet group을 명시하지 않으면 `default` subnet group으로 복원될 수 있다.
- "복원 인스턴스가 available"과 "애플리케이션에서 바로 검증 가능"은 다르다.

이건 실패가 아니라 좋은 런북 재료였다. 실제 운영에서도 복원 명령 자체보다 네트워크 placement와 검증 경로가 더 중요하기 때문이다.

## RTO/RPO 매트릭스

최종적으로 장애 유형별로 목표와 관찰값을 분리했다.

| 장애 유형 | RTO 목표 | RPO 목표 | 자동화 |
|---|---:|---:|---|
| Pod 다운 | 30초 | 0 | 자동 |
| Node 다운 / Spot interruption | 2분 | 0 | 자동 |
| Deployment 강제 삭제 | 5분 | 0 | 자동 |
| S3 객체 삭제 | 2분 | 0 | 자동 |
| RDS 데이터 손실 | 15분 | 5분 | 반자동 |

## 면접에서 말할 포인트

이 프로젝트의 DR 이야기는 이렇게 요약할 수 있다.

> 멀티 리전은 의도적으로 제외했고, 대신 단일 리전에서 실제 데이터 손실 시나리오를 S3와 RDS로 나눠 복구했다. S3는 자동 복구, RDS는 PITR 기반 반자동 복구로 설계했고, 각 시나리오의 RTO/RPO를 문서화했다.

## 남은 개선

- RDS restore 명령에 DB subnet group과 security group을 명시
- 복원 인스턴스에 실제 검증 쿼리까지 자동화
- DR 대시보드에 RTO/RPO 패널 추가
