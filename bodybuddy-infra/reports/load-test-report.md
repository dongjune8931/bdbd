# Load Test Report

## 목적

이 문서는 BodyBuddy dev 환경에서 수행한 부하 테스트 결과를 기록하는 템플릿이다.

챕터 8의 목표는 단순히 "버틴다"를 보여주는 것이 아니라, 아래를 숫자로 남기는 것이다.

1. 업로드 부하 시 `user-service -> SQS -> analysis-worker` 경로가 어디서 병목이 생기는지
2. 랭킹 조회 부하 시 `score-service`가 Redis 캐시를 얼마나 잘 활용하는지
3. HPA와 Karpenter가 실제로 Pod와 노드를 확장하는지
4. 1차 측정 후 어떤 튜닝을 했고, 2차에서 얼마나 나아졌는지

---

## 시나리오

### 1. Upload Burst

- 스크립트: `bodybuddy-app/test/load/upload-burst.js`
- 대상: `POST /api/v1/uploads`
- 목적:
  - `user-service` 응답 지연/오류율 측정
  - analysis queue 적재량 증가 확인
  - `analysis-worker` 처리 backlog 및 batch node 확장 관찰

### 2. Ranking Read

- 스크립트: `bodybuddy-app/test/load/ranking-read.js`
- 대상: `GET /api/v1/ranking`
- 목적:
  - `score-service` read-heavy 경로 응답시간 측정
  - Redis 캐시 기반 랭킹 조회 성능 확인
  - HPA/CPU 반응 관찰

---

## 실행 커맨드

### Upload Burst

```bash
cd /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-app
K6_BASE_URL=http://localhost:8080 \
K6_TOKEN=<JWT_TOKEN> \
make load-upload-smoke
```

### Ranking Read

```bash
cd /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-app
K6_SCORE_BASE_URL=http://localhost:8081 \
make load-ranking-smoke
```

---

## 측정 포인트

### 애플리케이션

- `user-service`
  - request rate
  - error rate
  - p50 / p95 / p99
- `score-service`
  - request rate
  - error rate
  - p50 / p95 / p99
- `analysis-worker`
  - queue backlog
  - 처리량
  - 처리 지연

### 인프라

- HPA scale out 여부
- Karpenter node scale out 여부
- batch node / critical node 분리 유지 여부
- CPU / memory saturation

---

## 결과 표

### 1차 측정

| 시나리오 | RPS/VUs | p50 | p95 | p99 | Error Rate | 비고 |
|---|---:|---:|---:|---:|---:|---|
| Upload Burst | 미입력 | - | - | - | - | |
| Ranking Read | 미입력 | - | - | - | - | |

### 튜닝 내용

- 미입력

### 2차 측정

| 시나리오 | RPS/VUs | p50 | p95 | p99 | Error Rate | 비고 |
|---|---:|---:|---:|---:|---:|---|
| Upload Burst | 미입력 | - | - | - | - | |
| Ranking Read | 미입력 | - | - | - | - | |

---

## 관찰 캡처

추가 예정:

- Grafana HPA 그래프
- Karpenter node scale out 장면
- queue depth 변화
- k6 결과 요약

---

## 해석

작성 예정:

- 1차 병목
- 어떤 튜닝이 효과 있었는지
- 이후 운영 관점에서 무엇을 더 보강할지
