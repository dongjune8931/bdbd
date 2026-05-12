#!/bin/bash
# LocalStack 초기화 스크립트 - S3 버킷, SQS 큐 생성

set -e

REGION="ap-northeast-2"
ACCOUNT_ID="000000000000"

echo "=== LocalStack init: creating AWS resources ==="

# S3 버킷 생성
awslocal s3 mb s3://bodybuddy-inbody --region "$REGION"
awslocal s3api put-bucket-versioning \
  --bucket bodybuddy-inbody \
  --versioning-configuration Status=Enabled

echo "✓ S3 bucket: bodybuddy-inbody"

# SQS 큐 생성 (DLQ 먼저)
awslocal sqs create-queue \
  --queue-name analysis-queue-dlq \
  --region "$REGION"

awslocal sqs create-queue \
  --queue-name notification-queue-dlq \
  --region "$REGION"

# 분석 큐 (DLQ 연결)
ANALYSIS_DLQ_ARN="arn:aws:sqs:${REGION}:${ACCOUNT_ID}:analysis-queue-dlq"
awslocal sqs create-queue \
  --queue-name analysis-queue \
  --region "$REGION" \
  --attributes "{\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"${ANALYSIS_DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"3\\\"}\"}"

# 알림 큐 (DLQ 연결)
NOTIFICATION_DLQ_ARN="arn:aws:sqs:${REGION}:${ACCOUNT_ID}:notification-queue-dlq"
awslocal sqs create-queue \
  --queue-name notification-queue \
  --region "$REGION" \
  --attributes "{\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"${NOTIFICATION_DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"3\\\"}\"}"

echo "✓ SQS queues: analysis-queue, notification-queue (with DLQs)"

# S3 → SQS 이벤트 알림 설정
# ObjectCreated 이벤트 시 analysis-queue로 메시지 전달
ANALYSIS_QUEUE_ARN="arn:aws:sqs:${REGION}:${ACCOUNT_ID}:analysis-queue"

awslocal s3api put-bucket-notification-configuration \
  --bucket bodybuddy-inbody \
  --notification-configuration "{
    \"QueueConfigurations\": [
      {
        \"QueueArn\": \"${ANALYSIS_QUEUE_ARN}\",
        \"Events\": [\"s3:ObjectCreated:*\"],
        \"Filter\": {
          \"Key\": {
            \"FilterRules\": [
              {\"Name\": \"prefix\", \"Value\": \"uploads/\"}
            ]
          }
        }
      }
    ]
  }"

echo "✓ S3 event notification → analysis-queue configured"

# SES 이메일 주소 인증 (LocalStack에서는 자동 인증)
awslocal ses verify-email-identity \
  --email-address noreply@bodybuddy.local \
  --region "$REGION" 2>/dev/null || true

echo "✓ SES email identity verified"
echo "=== LocalStack init complete ==="
