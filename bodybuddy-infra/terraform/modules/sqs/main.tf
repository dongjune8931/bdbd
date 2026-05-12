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
