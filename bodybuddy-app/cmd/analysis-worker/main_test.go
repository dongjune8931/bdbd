package main

import "testing"

func TestDecodeAnalysisPayloadDirect(t *testing.T) {
	payload, err := decodeAnalysisPayload(`{"user_id":"user-a","upload_id":"upload-a","s3_key":"uploads/user-a/upload-a"}`)
	if err != nil {
		t.Fatalf("decode direct payload: %v", err)
	}
	if payload.UserID != "user-a" || payload.UploadID != "upload-a" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestDecodeAnalysisPayloadS3Notification(t *testing.T) {
	body := `{"Records":[{"s3":{"object":{"key":"uploads%2Fuser-a%2Fupload-a"}}}]}`
	payload, err := decodeAnalysisPayload(body)
	if err != nil {
		t.Fatalf("decode S3 notification: %v", err)
	}
	if payload.S3Key != "uploads/user-a/upload-a" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestDecodeAnalysisPayloadEventBridge(t *testing.T) {
	body := `{"detail":{"object":{"key":"uploads/user-b/upload-b"}}}`
	payload, err := decodeAnalysisPayload(body)
	if err != nil {
		t.Fatalf("decode EventBridge event: %v", err)
	}
	if payload.UserID != "user-b" || payload.UploadID != "upload-b" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestDecodeAnalysisPayloadRejectsUnexpectedKey(t *testing.T) {
	_, err := decodeAnalysisPayload(`{"detail":{"object":{"key":"other/user/upload"}}}`)
	if err == nil {
		t.Fatal("expected unsupported key error")
	}
}
