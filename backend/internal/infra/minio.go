// Object storage retains original uploads independently of SQL metadata and vectors.
package infra

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/Emulisy/SupaAgent/internal/config"
)

// NewMinIO configures static S3 credentials and TLS from application settings.
// It checks the document bucket and creates it when absent, returning initialization
// or bucket-access errors before document operations begin.
func NewMinIO(ctx context.Context, cfg *config.MinIO) (*minio.Client, error) {
	cli, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("new minio client: %w", err)
	}

	exists, err := cli.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket %s: %w", cfg.Bucket, err)
		}
	}
	return cli, nil
}

// ObjectKey derives an upload key from the knowledge-base ID, document ID, and
// filename. The service persists this key so the worker can retrieve the original.
func ObjectKey(kbID, docID int64, filename string) string {
	return fmt.Sprintf("docs/%d/%d_%s", kbID, docID, filename)
}
