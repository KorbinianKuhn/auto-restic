package s3

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/korbiniankuhn/auto-restic/internal/utils"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// queryTimeout applies to single API calls (bucket check, object listing).
// The bucket only holds one object per backup plus its versions, so these
// finish within seconds and 1 minute leaves plenty of headroom.
const queryTimeout = time.Minute

type S3 struct {
	client  *minio.Client
	bucket  string
	timeout time.Duration
}

type Option func(*S3)

// WithTimeout aborts a single upload attempt after timeout. 0 disables the
// timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(s *S3) {
		s.timeout = timeout
	}
}

func Get(accessKey, secretKey, endpoint, bucket string, opts ...Option) (*S3, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: true,
	})

	if err != nil {
		return nil, fmt.Errorf("invalid s3 credentials or endpoint: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	exists, err := c.BucketExists(ctx, bucket)
	err = utils.WrapTimeout(ctx, "s3 bucket check", queryTimeout, err)
	if err != nil {
		return nil, fmt.Errorf("unable to check if bucket exists: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("bucket %s does not exist", bucket)
	}

	s3 := &S3{
		client: c,
		bucket: bucket,
	}
	for _, opt := range opts {
		opt(s3)
	}

	return s3, nil
}

type S3Object struct {
	BackupName     string
	Size           int64
	ExpirationDate time.Time
	CreatedAt      time.Time
	IsLatest       bool
	VersionID      string
	Key            string
	IsDeleteMarker bool
}

func (s3 S3) ListObjects() ([]S3Object, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	objects := []S3Object{}
	for obj := range s3.client.ListObjects(ctx, s3.bucket, minio.ListObjectsOptions{
		WithVersions: true,
		WithMetadata: true,
	}) {
		if obj.Err != nil {
			err := utils.WrapTimeout(ctx, "s3 list objects", queryTimeout, obj.Err)
			return []S3Object{}, fmt.Errorf("failed to list objects: %w", err)
		}

		objects = append(objects, S3Object{
			BackupName:     strings.TrimSuffix(obj.Key, ".tar.gz.age"),
			Size:           obj.Size,
			ExpirationDate: obj.Expiration,
			CreatedAt:      obj.LastModified,
			IsLatest:       obj.IsLatest,
			VersionID:      obj.VersionID,
			Key:            obj.Key,
			IsDeleteMarker: obj.IsDeleteMarker,
		})
	}

	return objects, nil
}

func (s3 S3) StreamUploadFile(ctx context.Context, filename string, reader *io.PipeReader) error {
	ctx, cancel := utils.ContextWithOptionalTimeout(ctx, s3.timeout)
	defer cancel()

	_, err := s3.client.PutObject(ctx, s3.bucket, filename, reader, -1, minio.PutObjectOptions{
		PartSize: 64 * 1024 * 1024, // 64MB parts for multipart upload
	})
	err = utils.WrapTimeout(ctx, "s3 upload", s3.timeout, err)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}
	return nil
}

func (s3 S3) RemoveObject(objectKey, versionID string) error {
	err := s3.client.RemoveObject(context.TODO(), s3.bucket, objectKey, minio.RemoveObjectOptions{
		VersionID: versionID,
	})

	if err != nil {
		return fmt.Errorf("failed to remove S3 object: %w", err)
	}
	return nil
}

func (s3 S3) StreamDownloadFile(objectKey, versionID string) (*minio.Object, error) {
	obj, err := s3.client.GetObject(context.TODO(), s3.bucket, objectKey, minio.GetObjectOptions{
		VersionID: versionID,
	})
	if err != nil {
		return nil, err
	}

	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, err
	}

	return obj, nil
}
