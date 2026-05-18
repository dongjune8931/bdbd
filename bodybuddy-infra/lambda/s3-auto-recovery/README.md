# S3 Auto Recovery Lambda

S3 `Object Deleted` EventBridge 이벤트를 받아 최신 delete marker를 제거한다.
Versioning이 켜진 버킷에서는 delete marker를 제거하면 직전 object version이 다시 current version이 되므로, 소규모 삭제 사고를 자동 복구하는 DR 데모로 사용할 수 있다.

## Build

```bash
/Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/lambda/s3-auto-recovery/build.sh
```

생성물:

```text
dist/bootstrap.zip
```
