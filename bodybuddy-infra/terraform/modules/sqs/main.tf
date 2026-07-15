resource "aws_sqs_queue" "analysis_dlq" {
  name = "${var.analysis_queue_name}-dlq"
  tags = merge(var.tags, {
    Name = "${var.analysis_queue_name}-dlq"
  })
}

resource "aws_sqs_queue" "notification_dlq" {
  name = "${var.notification_queue_name}-dlq"
  tags = merge(var.tags, {
    Name = "${var.notification_queue_name}-dlq"
  })
}

resource "aws_sqs_queue" "analysis" {
  name                       = var.analysis_queue_name
  visibility_timeout_seconds = 90

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.analysis_dlq.arn
    maxReceiveCount     = var.max_receive_count
  })

  tags = merge(var.tags, {
    Name = var.analysis_queue_name
  })
}

resource "aws_sqs_queue" "notification" {
  name                       = var.notification_queue_name
  visibility_timeout_seconds = 60

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.notification_dlq.arn
    maxReceiveCount     = var.max_receive_count
  })

  tags = merge(var.tags, {
    Name = var.notification_queue_name
  })
}

resource "aws_cloudwatch_event_rule" "analysis_object_created" {
  name        = "${var.analysis_queue_name}-object-created"
  description = "Routes completed BodyBuddy image uploads to the analysis queue."

  event_pattern = jsonencode({
    source      = ["aws.s3"]
    detail-type = ["Object Created"]
    detail = {
      bucket = {
        name = [var.analysis_source_bucket_name]
      }
      object = {
        key = [{ prefix = "uploads/" }]
      }
    }
  })

  tags = var.tags
}

resource "aws_cloudwatch_event_target" "analysis_queue" {
  rule      = aws_cloudwatch_event_rule.analysis_object_created.name
  target_id = "analysis-queue"
  arn       = aws_sqs_queue.analysis.arn
}

data "aws_iam_policy_document" "analysis_eventbridge" {
  statement {
    sid     = "AllowEventBridgeObjectCreated"
    effect  = "Allow"
    actions = ["sqs:SendMessage"]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    resources = [aws_sqs_queue.analysis.arn]

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.analysis_object_created.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "analysis_eventbridge" {
  queue_url = aws_sqs_queue.analysis.id
  policy    = data.aws_iam_policy_document.analysis_eventbridge.json
}
