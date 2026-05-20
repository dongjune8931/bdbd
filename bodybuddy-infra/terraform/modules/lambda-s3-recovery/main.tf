resource "aws_s3_bucket_notification" "eventbridge" {
  bucket      = var.bucket_name
  eventbridge = true
}

locals {
  email_alerts_enabled = var.alert_email_from != null && length(var.alert_email_to) > 0
}

data "aws_iam_policy_document" "assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.function_name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume_role.json
  tags               = var.tags
}

data "aws_iam_policy_document" "this" {
  statement {
    sid    = "WriteLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["${aws_cloudwatch_log_group.this.arn}:*"]
  }

  statement {
    sid    = "InspectObjectVersions"
    effect = "Allow"
    actions = [
      "s3:ListBucketVersions",
    ]
    resources = [var.bucket_arn]
  }

  statement {
    sid    = "RemoveDeleteMarkers"
    effect = "Allow"
    actions = [
      "s3:DeleteObjectVersion",
      "s3:BypassGovernanceRetention",
    ]
    resources = ["${var.bucket_arn}/*"]
  }

  statement {
    sid       = "PublishRecoveryMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = [var.metric_namespace]
    }
  }

  dynamic "statement" {
    for_each = local.email_alerts_enabled ? [1] : []

    content {
      sid    = "SendSesRecoveryAlerts"
      effect = "Allow"
      actions = [
        "ses:SendEmail",
        "ses:SendRawEmail",
      ]
      resources = ["*"]
    }
  }
}

resource "aws_iam_policy" "this" {
  name   = "${var.function_name}-policy"
  policy = data.aws_iam_policy_document.this.json
  tags   = var.tags
}

resource "aws_iam_role_policy_attachment" "this" {
  role       = aws_iam_role.this.name
  policy_arn = aws_iam_policy.this.arn
}

resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.function_name}"
  retention_in_days = 7
  tags              = var.tags
}

resource "aws_lambda_function" "this" {
  function_name    = var.function_name
  description      = "Restores S3 objects by removing latest delete markers after delete events."
  role             = aws_iam_role.this.arn
  filename         = var.package_path
  source_code_hash = filebase64sha256(var.package_path)
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  memory_size      = var.memory_size
  timeout          = var.timeout

  environment {
    variables = {
      BUCKET_NAME      = var.bucket_name
      METRIC_NAMESPACE = var.metric_namespace
      ALERT_EMAIL_FROM = var.alert_email_from != null ? var.alert_email_from : ""
      ALERT_EMAIL_TO   = join(",", var.alert_email_to)
    }
  }

  depends_on = [
    aws_cloudwatch_log_group.this,
    aws_iam_role_policy_attachment.this,
  ]

  tags = var.tags
}

resource "aws_cloudwatch_event_rule" "s3_object_deleted" {
  name        = "${var.function_name}-s3-object-deleted"
  description = "Routes object deleted events from the protected S3 bucket to the recovery Lambda."

  event_pattern = jsonencode({
    source      = ["aws.s3"]
    detail-type = ["Object Deleted"]
    detail = {
      bucket = {
        name = [var.bucket_name]
      }
    }
  })

  tags = var.tags
}

resource "aws_cloudwatch_event_target" "this" {
  rule      = aws_cloudwatch_event_rule.s3_object_deleted.name
  target_id = var.function_name
  arn       = aws_lambda_function.this.arn
}

resource "aws_lambda_permission" "eventbridge" {
  statement_id  = "AllowExecutionFromEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.s3_object_deleted.arn
}
