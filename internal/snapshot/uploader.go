// Package snapshot provides S3-compatible snapshot upload and pre-signed URL generation.
// When S3 is not configured (empty bucket), the NoopUploader is used and all
// S3 operations are skipped, keeping the system in local-only mode.
package snapshot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/hyperengineering/engram/internal/config"
)

// ErrNotConfigured is returned when S3 snapshot storage is not configured.
var ErrNotConfigured = errors.New("snapshot storage not configured")

// Uploader uploads snapshots and generates pre-signed download URLs.
type Uploader interface {
	// Upload uploads a snapshot file for the given store to S3.
	Upload(ctx context.Context, storeID string, filePath string) error

	// PresignedURL returns a pre-signed URL for downloading the snapshot.
	// Returns ErrNotConfigured when S3 is not configured.
	PresignedURL(ctx context.Context, storeID string) (url string, expiry time.Time, err error)
}

// s3Client defines the minimal minio.Client operations used by S3Uploader.
// This interface enables testing with mock implementations.
type s3Client interface {
	FPutObject(ctx context.Context, bucket, objectName, filePath string, opts interface{}) error
	PresignedGetObject(ctx context.Context, bucket, objectName string, expiry time.Duration) (*url.URL, error)
}

// minioClientWrapper wraps *minio.Client to satisfy the s3Client interface.
// This is necessary because minio.Client methods have concrete option types
// that differ from our simplified interface.
type minioClientWrapper struct {
	client *minio.Client
}

func (w *minioClientWrapper) FPutObject(ctx context.Context, bucket, objectName, filePath string, opts interface{}) error {
	putOpts := minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	}
	_, err := w.client.FPutObject(ctx, bucket, objectName, filePath, putOpts)
	return err
}

func (w *minioClientWrapper) PresignedGetObject(ctx context.Context, bucket, objectName string, expiry time.Duration) (*url.URL, error) {
	return w.client.PresignedGetObject(ctx, bucket, objectName, expiry, nil)
}

// S3Uploader uploads snapshots to S3-compatible storage.
type S3Uploader struct {
	client    s3Client
	bucket    string
	urlExpiry time.Duration
}

// Upload uploads the snapshot file at filePath for the given store.
func (u *S3Uploader) Upload(ctx context.Context, storeID string, filePath string) error {
	key := objectKey(storeID)
	if err := u.client.FPutObject(ctx, u.bucket, key, filePath, nil); err != nil {
		return fmt.Errorf("upload snapshot to S3: %w", err)
	}
	return nil
}

// PresignedURL returns a pre-signed GET URL for the snapshot.
func (u *S3Uploader) PresignedURL(ctx context.Context, storeID string) (string, time.Time, error) {
	key := objectKey(storeID)
	presigned, err := u.client.PresignedGetObject(ctx, u.bucket, key, u.urlExpiry)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate pre-signed URL: %w", err)
	}
	expiry := time.Now().Add(u.urlExpiry)
	return presigned.String(), expiry, nil
}

// NoopUploader is used when S3 storage is not configured.
// Upload is a no-op and PresignedURL returns ErrNotConfigured.
type NoopUploader struct{}

// Upload is a no-op when S3 is not configured.
func (u *NoopUploader) Upload(ctx context.Context, storeID string, filePath string) error {
	return nil
}

// PresignedURL returns ErrNotConfigured when S3 is not configured.
func (u *NoopUploader) PresignedURL(ctx context.Context, storeID string) (string, time.Time, error) {
	return "", time.Time{}, ErrNotConfigured
}

// NewUploader creates the appropriate Uploader based on configuration.
// Returns NoopUploader when bucket is empty, S3Uploader otherwise.
func NewUploader(cfg config.SnapshotStorageConfig) (Uploader, error) {
	if cfg.Bucket == "" {
		return &NoopUploader{}, nil
	}

	useSSL := true
	if cfg.UseSSL != nil {
		useSSL = *cfg.UseSSL
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: useSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}

	return &S3Uploader{
		client:    &minioClientWrapper{client: client},
		bucket:    cfg.Bucket,
		urlExpiry: time.Duration(cfg.URLExpiry),
	}, nil
}

// objectKey returns the S3 object key for a store's snapshot.
// Convention: {store_id}/snapshot/current.db
func objectKey(storeID string) string {
	return storeID + "/snapshot/current.db"
}
