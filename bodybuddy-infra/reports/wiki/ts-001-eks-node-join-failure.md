# TS-001: EKS 노드가 클러스터에 붙지 못한 문제

> **환경**: terraform-aws-modules/eks ~> 20.37, EKS 1.30, ap-northeast-2
> **증상**: `NodeCreationFailure: Instances failed to join the kubernetes cluster`

---

## 증상

`terraform apply`로 EKS managed node group을 처음 생성할 때 아래 에러가 발생했다.

```
Error: waiting for EKS Node Group (bodybuddy-dev-eks:bootstrap-...) create:
unexpected state 'CREATE_FAILED', wanted target 'ACTIVE'.
last error: NodeCreationFailure: Instances failed to join the kubernetes cluster
```

이상한 점이 있었다. EC2 콘솔에서 보면 인스턴스 3개가 `running` 상태였고, `describe-nodegroup`의 `health.issues`에는 1개 인스턴스 ID만 찍혀 있었다. 나머지 2개는 멀쩡히 떠 있었다.

---

## 디버깅 과정

### 1단계 - 인스턴스 콘솔 로그 확인

```bash
aws ec2 get-console-output \
  --instance-id i-09c2381da91e400b5 \
  --region ap-northeast-2 \
  --latest \
  --output text | tail -50
```

결과를 보니 bootstrap 스크립트는 정상 완료됐다.

```
[eks-bootstrap] INFO: complete!
[OK] Started Kubernetes Kubelet.
```

09:02에 부팅 완료, kubelet도 올라왔다. 그런데 09:39까지 37분 동안 클러스터에 등록이 안 됐다. 네트워크 설정이나 IAM 문제가 아니었다.

### 2단계 - launch template 확인

`describe-nodegroup`으로 어떤 launch template을 쓰는지 확인했다.

```bash
aws eks describe-nodegroup \
  --cluster-name bodybuddy-dev-eks \
  --nodegroup-name bootstrap-... \
  --query 'nodegroup.launchTemplate'
```

커스텀 launch template이 붙어 있었고, 해당 LT의 `UserData`가 비어 있었다. 문제의 실마리였다.

### 3단계 - 원인 파악

`terraform-aws-modules/eks` v20에서 managed node group은 기본적으로 **모듈이 자동 생성한 launch template**을 사용한다. 이 LT에는 노드가 EKS 클러스터에 join하기 위한 bootstrap UserData가 포함된다.

그런데 커스텀 LT를 쓰면 UserData를 직접 넣어줘야 한다. 비어있으면 kubelet이 `--node-labels`, `--max-pods`, `--cluster-name` 같은 인자 없이 뜨고, API server 주소도 모른 채 실행된다. 부팅은 되지만 클러스터를 찾지 못해 타임아웃이 난다.

### 4단계 - Security Group 문제 발견

커스텀 LT를 제거(`use_custom_launch_template = false`)해도 한 번 더 실패했다. 추가로 확인해보니 **노드의 Security Group이 컨트롤 플레인과 통신할 수 있는 경로가 없었다.**

EKS managed node group을 생성하면 두 종류의 SG가 존재한다:

| SG | 역할 |
|---|---|
| **Cluster primary SG** | 컨트롤 플레인 ENI에 붙는 SG. 노드와의 양방향 통신 허용 규칙 포함 |
| **Node SG** | 노드 인스턴스에 붙는 SG. 모듈이 별도 생성 |

`terraform-aws-modules/eks` v20은 기본적으로 node group에 cluster primary SG를 **자동으로 붙이지 않는다**. 별도로 `attach_cluster_primary_security_group = true`를 명시해야 한다.

이게 없으면 노드는 떠 있지만 컨트롤 플레인의 API server로 향하는 트래픽이 허용되지 않아 kubelet이 register 요청을 보내도 응답을 받지 못한다.

---

## 해결

`eks/main.tf`의 node group에 두 가지를 추가했다.

```hcl
eks_managed_node_groups = {
  bootstrap = {
    ami_type                              = "AL2_x86_64"
    use_custom_launch_template            = false   # 모듈 기본 LT 사용
    attach_cluster_primary_security_group = true    # 핵심 수정
    disk_size                             = 20
    # ...
  }
}
```

`terraform apply` 후 노드가 정상적으로 `Ready` 상태로 올라왔다.

```bash
kubectl get nodes
# NAME                                            STATUS   VERSION
# ip-10-20-xx-xxx.ap-northeast-2.compute.internal Ready    v1.30.14-eks-ecaa3a6
```

---

## 딥다이브: EKS 컨트롤 플레인 ↔ 노드 통신 구조

EKS에서 컨트롤 플레인과 노드가 어떻게 통신하는지 이해하면 이 문제가 왜 생기는지 명확해진다.

### EKS의 네트워크 구조

EKS 컨트롤 플레인(API server, etcd 등)은 AWS가 관리하는 별도 VPC에서 실행된다. 사용자 VPC에는 **Cross-Account ENI**가 생성되고, 이 ENI를 통해 컨트롤 플레인과 노드가 통신한다.

```
[AWS 관리 VPC]          [사용자 VPC]
 API Server  ←→  ENI (Cluster Primary SG 부착)  ←→  노드
```

이 ENI에 붙는 SG가 **Cluster Primary Security Group**이다.

### Cluster Primary SG의 역할

Cluster Primary SG는 생성 시 다음 규칙을 가진다:

- **inbound**: Cluster Primary SG 자신으로부터의 모든 트래픽 허용 (self-referencing)
- **outbound**: 모든 트래픽 허용

즉, 노드 인스턴스에 Cluster Primary SG가 붙어 있으면 컨트롤 플레인 ENI ↔ 노드 간 통신이 self-referencing 규칙으로 허용된다.

### `attach_cluster_primary_security_group = false`(기본값)일 때

노드에는 모듈이 별도 생성한 Node SG만 붙는다. 이 경우 컨트롤 플레인 ENI와 노드 사이에 통신 경로를 열어주는 추가 SG 규칙이 필요한데, 명시하지 않으면 kubelet → API server 방향의 트래픽이 차단된다.

```
컨트롤 플레인 ENI (Cluster Primary SG)
      ↕ ← 여기가 막힘
노드 (Node SG만 있음, Cluster Primary SG 없음)
```

### `attach_cluster_primary_security_group = true`일 때

노드에 Node SG + Cluster Primary SG 두 개가 모두 붙는다. Self-referencing 규칙이 적용되어 통신이 열린다.

```
컨트롤 플레인 ENI (Cluster Primary SG)
      ↕ ← self-referencing으로 허용
노드 (Node SG + Cluster Primary SG)
```

### 언제 false로 두는가

Cluster Primary SG를 붙이지 않고 Node SG에 직접 규칙을 추가해서 관리하는 방식도 있다. 세밀한 트래픽 제어가 필요할 때 사용하지만, 설정이 복잡해지고 실수하기 쉽다. 처음 구성에서는 `true`로 두는 게 안전하다.

---

## 면접 포인트

> **"EKS 노드가 클러스터에 join을 못 했는데 어떻게 디버깅했나요?"**

1. 인스턴스 콘솔 로그로 OS/bootstrap 레벨 문제인지 먼저 확인
2. bootstrap 정상이면 네트워크/SG 문제로 좁힘
3. `attach_cluster_primary_security_group` 미설정으로 컨트롤 플레인 ENI와 통신 경로가 없었음을 파악

> **"EKS 컨트롤 플레인과 노드는 어떻게 통신하나요?"**

컨트롤 플레인은 AWS 관리 VPC에 있고 사용자 VPC에 Cross-Account ENI를 통해 연결된다. 이 ENI에 붙은 Cluster Primary SG를 노드에도 함께 attach하면 self-referencing 규칙으로 양방향 통신이 허용된다.
