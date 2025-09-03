package s3

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3 struct {
	client *minio.Client
	bucket string
}

func Get(accessKey, secretKey, endpoint, bucket string) (*S3, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: true,
	})

	if err != nil {
		return nil, fmt.Errorf("invalid s3 credentials or endpoint: %w", err)
	}

	exists, err := c.BucketExists(context.TODO(), bucket)
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
}

func (s3 S3) ListObjects() ([]S3Object, error) {
	objects := []S3Object{}
	for obj := range s3.client.ListObjects(context.TODO(), s3.bucket, minio.ListObjectsOptions{
		WithVersions: true,
		WithMetadata: true,
	}) {
		if obj.Err != nil {
			return []S3Object{}, fmt.Errorf("failed to list objects: %w", obj.Err)
		}

		objects = append(objects, S3Object{
			BackupName:     strings.TrimSuffix(obj.Key, ".tar.gz.age"),
			Size:           obj.Size,
			ExpirationDate: obj.Expiration,
			CreatedAt:      obj.LastModified,
			IsLatest:       obj.IsLatest,
			VersionID:      obj.VersionID,
			Key:            obj.Key,
		})
	}

	return objects, nil
}

func (s3 S3) UploadFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Upload the file to the bucket
	_, err = s3.client.PutObject(context.TODO(), s3.bucket, fileInfo.Name(), file, fileInfo.Size(), minio.PutObjectOptions{})
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

func (s3 S3) DownloadFile(objectKey, versionID, filePath string) error {
	err := s3.client.FGetObject(context.TODO(), s3.bucket, objectKey, filePath, minio.GetObjectOptions{
		VersionID: versionID,
	})
	if err != nil {
		return fmt.Errorf("failed to download S3 object: %w", err)
	}
	return nil
}
