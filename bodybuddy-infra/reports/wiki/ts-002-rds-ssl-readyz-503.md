# TS-002: EKS 배포 후 /readyz 503 — RDS SSL 연결 실패

> **환경**: EKS 1.30, RDS PostgreSQL 16 (AWS 관리형), pgx/v5, Go 1.24
> **증상**: `/healthz` 200인데 `/readyz`만 503

---

## 증상

`user-service`를 EKS에 처음 배포했을 때 pod가 뜨긴 하는데 readiness probe가 계속 실패했다.

```bash
kubectl describe pod user-service-xxx -n bodybuddy
# Readiness probe failed: HTTP probe failed with statuscode: 503
```

이상한 점은 `/healthz`는 200 OK인데 `/readyz`만 503이었다. 서비스 자체는 살아있는데 ready 상태가 안 된다는 뜻이었다.

---

## 디버깅 과정

### 1단계 - 로그 확인

```bash
kubectl logs deploy/user-service -n bodybuddy
```

롤링 업데이트 중이라 이전 pod 로그가 섞여서 나왔다. 에러 메시지를 명확히 보기 어려웠다.

### 2단계 - port-forward로 직접 확인

```bash
kubectl port-forward deploy/user-service 8080:8080 -n bodybuddy
curl -i http://localhost:8080/readyz
```

응답 본문에서 원인을 찾았다.

```json
{
  "status": "error",
  "error": "db ping: FATAL: no pg_hba.conf entry for host '10.20.x.x', user 'bodybuddy', database 'bodybuddy', no encryption"
}
```

**"no encryption"** — RDS가 SSL 연결을 요구하는데 연결 시도가 평문으로 갔다.

### 3단계 - 원인 파악

`internal/config/config.go`의 DSN 생성 함수를 보니 `sslmode`가 하드코딩돼 있었다.

```go
func (c *Common) DSN() string {
    return fmt.Sprintf(
        "host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
        c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName,
    )
}
```

`sslmode=disable`이었다. 로컬 docker-compose의 postgres는 SSL을 강제하지 않아서 이 설정으로 잘 돌아갔지만, AWS RDS는 달랐다.

---

## 원인: AWS RDS의 기본 SSL 정책

AWS RDS PostgreSQL은 **기본적으로 SSL 연결을 강제**한다. 정확히는 `rds.force_ssl` 파라미터가 기본값 `1`(강제)로 설정된 파라미터 그룹이 적용된다.

`pg_hba.conf` 관점에서 보면 RDS는 내부적으로 다음과 같이 설정되어 있다.

```
# TYPE  DATABASE  USER  ADDRESS  METHOD
hostssl all       all   all      md5   # SSL 연결만 허용
```

`sslmode=disable`로 연결을 시도하면 이 규칙에 매칭되지 않아 `no pg_hba.conf entry` 에러가 발생한다.

로컬 postgres는 기본 설정이 `host all all all md5` (SSL 비강제)라서 문제가 없었지만, RDS에 올리는 순간 이 차이가 드러났다.

---

## 해결

### 1. DSN의 sslmode를 환경변수로 분리

```go
type Common struct {
    // ...
    DBSSLMode string `envconfig:"DB_SSL_MODE" default:"disable"`
}

func (c *Common) DSN() string {
    return fmt.Sprintf(
        "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
        c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
    )
}
```

### 2. Helm values에서 환경별로 다르게 주입

```yaml
# values.yaml (로컬/기본값)
env:
  DB_SSL_MODE: disable

# EKS 배포 시
env:
  DB_SSL_MODE: require
```

배포 후 `/readyz` 200 OK로 바뀌었다.

---

## 딥다이브: PostgreSQL SSL 모드의 종류

PostgreSQL 클라이언트(pgx 포함)의 `sslmode`에는 여러 단계가 있다. 보안 수준 순으로 정리하면:

| sslmode | 동작 | 서버 인증서 검증 |
|---|---|---|
| `disable` | SSL 사용 안 함 | - |
| `allow` | 서버가 요구하면 SSL 사용 | 안 함 |
| `prefer` | 가능하면 SSL, 아니면 평문 (기본값) | 안 함 |
| `require` | 반드시 SSL | 안 함 (연결은 암호화) |
| `verify-ca` | SSL + CA 인증서로 서버 검증 | CA 검증 |
| `verify-full` | SSL + CA + 호스트명까지 검증 | CA + 호스트명 검증 |

### RDS에서 `require` vs `verify-full`

`require`는 연결 자체는 암호화되지만 서버 인증서를 검증하지 않는다. MITM(중간자 공격)에 이론적으로 취약하다.

`verify-full`을 쓰려면 AWS RDS의 CA 인증서를 다운받아 컨테이너에 포함시키거나 마운트해야 한다.

```bash
# AWS RDS CA 인증서 다운로드
wget https://truststore.pki.rds.amazonaws.com/ap-northeast-2/ap-northeast-2-bundle.pem
```

```go
// verify-full 설정 시
connConfig.TLSConfig = &tls.Config{
    RootCAs:    certPool, // RDS CA 인증서
    ServerName: rdsEndpoint,
}
```

이 프로젝트에서는 VPC 내부 통신(Pod → RDS)이라 네트워크 레벨에서 이미 격리되어 있고, `require`로 전송 암호화는 보장되므로 `require`로 충분하다고 판단했다. 퍼블릭 엔드포인트에 노출된다면 `verify-full`을 써야 한다.

### `pg_hba.conf`란

PostgreSQL이 클라이언트 연결을 허용할지 판단하는 규칙 파일이다. 형식은 다음과 같다.

```
TYPE  DATABASE  USER  ADDRESS  METHOD
```

RDS는 이 파일을 직접 수정할 수 없고, `rds.force_ssl` 파라미터로 SSL 강제 여부를 제어한다. `rds.force_ssl=1`이면 RDS가 내부적으로 `hostssl` 항목만 남겨두어 평문 연결을 거부한다.

### 환경별 sslmode 전략

| 환경 | sslmode | 이유 |
|---|---|---|
| 로컬 docker-compose | `disable` | 개발 편의, 로컬 postgres는 SSL 미강제 |
| EKS + RDS | `require` | RDS 기본 SSL 강제, VPC 내부 통신 |
| 퍼블릭 노출 + RDS | `verify-full` | MITM 방지 |

---

## 면접 포인트

> **"readiness probe가 실패했는데 어떻게 디버깅했나요?"**

`kubectl logs`가 롤링 업데이트로 이전 pod 로그를 섞어서 보여줘서 `kubectl port-forward`로 직접 엔드포인트를 찔러봤다. 응답 본문에 실제 에러 메시지(`no pg_hba.conf entry ... no encryption`)가 있어서 바로 원인을 찾을 수 있었다.

> **"로컬에서는 됐는데 EKS에 올리니까 DB 연결이 안 됐다면 어떤 걸 의심하나요?"**

가장 먼저 SSL 설정 차이를 본다. 로컬 postgres는 SSL을 강제하지 않지만 AWS RDS는 `rds.force_ssl=1`이 기본값이라 `sslmode=disable`로 연결하면 거부된다. 환경변수로 분리해서 환경마다 다르게 주입하는 것이 정석이다.

> **"sslmode=require와 verify-full의 차이가 뭔가요?"**

`require`는 연결을 암호화하지만 서버 인증서를 검증하지 않는다. `verify-full`은 CA 인증서로 서버를 검증해서 MITM을 방어한다. VPC 내부 통신이면 `require`로 충분하고, 퍼블릭 엔드포인트라면 `verify-full`을 써야 한다.
