package snapshot

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/config"
)

// --- NoopUploader Tests ---

func TestNoopUploader_Upload_IsNoOp(t *testing.T) {
	u := &NoopUploader{}
	err := u.Upload(context.Background(), "store-1", "/some/path")
	if err != nil {
		t.Errorf("NoopUploader.Upload() should not error, got %v", err)
	}
}

func TestNoopUploader_PresignedURL_ReturnsErrNotConfigured(t *testing.T) {
	u := &NoopUploader{}
	_, _, err := u.PresignedURL(context.Background(), "store-1")
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("NoopUploader.PresignedURL() should return ErrNotConfigured, got %v", err)
	}
}

// --- NewUploader factory tests ---

func TestNewUploader_EmptyBucket_ReturnsNoopUploader(t *testing.T) {
	cfg := config.SnapshotStorageConfig{
		Bucket: "", // Empty = not configured
	}

	u, err := NewUploader(cfg)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}

	_, ok := u.(*NoopUploader)
	if !ok {
		t.Errorf("expected *NoopUploader, got %T", u)
	}
}

func TestNewUploader_WithBucket_ReturnsS3Uploader(t *testing.T) {
	boolTrue := true
	cfg := config.SnapshotStorageConfig{
		Bucket:    "test-bucket",
		Endpoint:  "localhost:9000",
		Region:    "us-east-1",
		UseSSL:    &boolTrue,
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		URLExpiry: config.Duration(15 * time.Minute),
	}

	u, err := NewUploader(cfg)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}

	_, ok := u.(*S3Uploader)
	if !ok {
		t.Errorf("expected *S3Uploader, got %T", u)
	}
}

func TestNewUploader_UseSSLNil_DefaultsTrue(t *testing.T) {
	cfg := config.SnapshotStorageConfig{
		Bucket:    "test-bucket",
		Endpoint:  "localhost:9000",
		Region:    "us-east-1",
		UseSSL:    nil, // nil = defaults to true
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		URLExpiry: config.Duration(15 * time.Minute),
	}

	u, err := NewUploader(cfg)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}

	s3u, ok := u.(*S3Uploader)
	if !ok {
		t.Fatalf("expected *S3Uploader, got %T", u)
	}
	if s3u.bucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", s3u.bucket, "test-bucket")
	}
}

// --- S3Uploader with mock client tests ---

// mockS3Client implements s3Client for testing.
type mockS3Client struct {
	uploadCalled    bool
	uploadErr       error
	presignCalled   bool
	presignURL      *url.URL
	presignErr      error
	lastBucket      string
	lastObjectName  string
	lastFilePath    string
}

func (m *mockS3Client) FPutObject(ctx context.Context, bucket, objectName, filePath string, opts interface{}) error {
	m.uploadCalled = true
	m.lastBucket = bucket
	m.lastObjectName = objectName
	m.lastFilePath = filePath
	return m.uploadErr
}

func (m *mockS3Client) PresignedGetObject(ctx context.Context, bucket, objectName string, expiry time.Duration) (*url.URL, error) {
	m.presignCalled = true
	m.lastBucket = bucket
	m.lastObjectName = objectName
	if m.presignErr != nil {
		return nil, m.presignErr
	}
	if m.presignURL != nil {
		return m.presignURL, nil
	}
	u, _ := url.Parse("https://s3.example.com/" + bucket + "/" + objectName + "?presigned=true")
	return u, nil
}

func TestS3Uploader_Upload_Success(t *testing.T) {
	// Create a temp file to upload
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "current.db")
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	mock := &mockS3Client{}
	u := &S3Uploader{
		client:    mock,
		bucket:    "test-bucket",
		urlExpiry: 15 * time.Minute,
	}

	err := u.Upload(context.Background(), "my-store", filePath)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if !mock.uploadCalled {
		t.Error("expected FPutObject to be called")
	}
	if mock.lastBucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", mock.lastBucket, "test-bucket")
	}
	if mock.lastObjectName != "my-store/snapshot/current.db" {
		t.Errorf("objectName = %q, want %q", mock.lastObjectName, "my-store/snapshot/current.db")
	}
	if mock.lastFilePath != filePath {
		t.Errorf("filePath = %q, want %q", mock.lastFilePath, filePath)
	}
}

func TestS3Uploader_Upload_Error(t *testing.T) {
	mock := &mockS3Client{
		uploadErr: errors.New("network timeout"),
	}
	u := &S3Uploader{
		client:    mock,
		bucket:    "test-bucket",
		urlExpiry: 15 * time.Minute,
	}

	err := u.Upload(context.Background(), "store-1", "/path/to/file.db")
	if err == nil {
		t.Fatal("Upload() expected error, got nil")
	}
	if !errors.Is(err, mock.uploadErr) {
		t.Errorf("expected wrapped network timeout error, got %v", err)
	}
}

func TestS3Uploader_PresignedURL_Success(t *testing.T) {
	expectedURL, _ := url.Parse("https://s3.example.com/bucket/store-1/snapshot/current.db?token=abc")
	mock := &mockS3Client{
		presignURL: expectedURL,
	}
	u := &S3Uploader{
		client:    mock,
		bucket:    "test-bucket",
		urlExpiry: 15 * time.Minute,
	}

	urlStr, expiry, err := u.PresignedURL(context.Background(), "store-1")
	if err != nil {
		t.Fatalf("PresignedURL() error = %v", err)
	}

	if urlStr != expectedURL.String() {
		t.Errorf("url = %q, want %q", urlStr, expectedURL.String())
	}

	// Expiry should be approximately 15 minutes from now
	expectedExpiry := time.Now().Add(15 * time.Minute)
	if expiry.Before(expectedExpiry.Add(-1*time.Second)) || expiry.After(expectedExpiry.Add(1*time.Second)) {
		t.Errorf("expiry = %v, want approximately %v", expiry, expectedExpiry)
	}

	if !mock.presignCalled {
		t.Error("expected PresignedGetObject to be called")
	}
	if mock.lastObjectName != "store-1/snapshot/current.db" {
		t.Errorf("objectName = %q, want %q", mock.lastObjectName, "store-1/snapshot/current.db")
	}
}

func TestS3Uploader_PresignedURL_Error(t *testing.T) {
	mock := &mockS3Client{
		presignErr: errors.New("access denied"),
	}
	u := &S3Uploader{
		client:    mock,
		bucket:    "test-bucket",
		urlExpiry: 15 * time.Minute,
	}

	_, _, err := u.PresignedURL(context.Background(), "store-1")
	if err == nil {
		t.Fatal("PresignedURL() expected error, got nil")
	}
}

func TestStripScheme(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		wantHost   string
		wantSSL    bool
	}{
		{"bare host", "s3.example.com", "s3.example.com", true},
		{"bare host:port", "minio:9000", "minio:9000", true},
		{"https URL", "https://s3.example.com", "s3.example.com", true},
		{"http URL", "http://minio:9000", "minio:9000", false},
		{"https with port", "https://s3.example.com:443", "s3.example.com:443", true},
		{"http with port", "http://localhost:9000", "localhost:9000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssl := true
			got := stripScheme(tt.endpoint, &ssl)
			if got != tt.wantHost {
				t.Errorf("stripScheme(%q) host = %q, want %q", tt.endpoint, got, tt.wantHost)
			}
			if ssl != tt.wantSSL {
				t.Errorf("stripScheme(%q) ssl = %v, want %v", tt.endpoint, ssl, tt.wantSSL)
			}
		})
	}
}

func TestS3Uploader_ObjectKey_Format(t *testing.T) {
	// Verify the key convention: {store_id}/snapshot/current.db
	tests := []struct {
		storeID string
		want    string
	}{
		{"default", "default/snapshot/current.db"},
		{"my-project", "my-project/snapshot/current.db"},
		{"org/project", "org/project/snapshot/current.db"},
	}

	for _, tt := range tests {
		got := objectKey(tt.storeID)
		if got != tt.want {
			t.Errorf("objectKey(%q) = %q, want %q", tt.storeID, got, tt.want)
		}
	}
}
