package queue

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// Message is a simplified SQS message.
type Message struct {
	MessageID     string
	ReceiptHandle string
	Body          string
	Attributes    map[string]string
}

// Client wraps the AWS SQS client.
type Client struct {
	svc *sqs.Client
}

// New creates a new SQS client.
// endpointURL, if non-empty, overrides the endpoint (used for LocalStack).
func New(ctx context.Context, region, endpointURL string) (*Client, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}

	var sqsOpts []func(*sqs.Options)
	if endpointURL != "" {
		// Override endpoint for LocalStack or other compatible services.
		sqsOpts = append(sqsOpts, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
		})
	}

	return &Client{svc: sqs.NewFromConfig(cfg, sqsOpts...)}, nil
}

// ReceiveMessages polls the queue for up to maxMessages messages.
// waitTimeSeconds enables long polling (0 = short poll).
func (c *Client) ReceiveMessages(ctx context.Context, queueURL string, maxMessages, waitTimeSeconds, visibilityTimeout int32) ([]Message, error) {
	out, err := c.svc.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     waitTimeSeconds,
		VisibilityTimeout:   visibilityTimeout,
		MessageAttributeNames: []string{"All"},
	})
	if err != nil {
		return nil, fmt.Errorf("sqs receive message: %w", err)
	}

	msgs := make([]Message, 0, len(out.Messages))
	for _, m := range out.Messages {
		msg := Message{
			MessageID:     aws.ToString(m.MessageId),
			ReceiptHandle: aws.ToString(m.ReceiptHandle),
			Body:          aws.ToString(m.Body),
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// DeleteMessage removes a successfully processed message from the queue.
func (c *Client) DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error {
	_, err := c.svc.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		return fmt.Errorf("sqs delete message: %w", err)
	}
	return nil
}

// SendMessage publishes a message to the queue.
func (c *Client) SendMessage(ctx context.Context, queueURL, body string, attrs map[string]types.MessageAttributeValue) (string, error) {
	input := &sqs.SendMessageInput{
		QueueUrl:          aws.String(queueURL),
		MessageBody:       aws.String(body),
	}
	if len(attrs) > 0 {
		input.MessageAttributes = attrs
	}

	out, err := c.svc.SendMessage(ctx, input)
	if err != nil {
		return "", fmt.Errorf("sqs send message: %w", err)
	}
	return aws.ToString(out.MessageId), nil
}

// ChangeMessageVisibility extends the visibility timeout for an in-flight message.
// Call this periodically for long-running tasks to prevent re-delivery.
func (c *Client) ChangeMessageVisibility(ctx context.Context, queueURL, receiptHandle string, visibilityTimeout int32) error {
	_, err := c.svc.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(queueURL),
		ReceiptHandle:     aws.String(receiptHandle),
		VisibilityTimeout: visibilityTimeout,
	})
	if err != nil {
		return fmt.Errorf("sqs change visibility: %w", err)
	}
	return nil
}
