// Package tiering implements the S3 object-store adapter.
package tiering

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"io"
	"strings"
	"time"
)

type S3 struct {
	client         *s3.Client
	bucket, prefix string
}

// NewS3 uses the default AWS credential chain. An endpoint enables path-style
// requests for local S3-compatible servers. Each request has a 30-second deadline.
func NewS3(ctx context.Context, bucket, prefix, endpoint string) (*S3, error) {
	if bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})
	return &S3{client: client, bucket: bucket, prefix: strings.Trim(prefix, "/")}, nil
}
func (s *S3) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return s.prefix + "/" + k
}
func (s *S3) Put(ctx context.Context, k string, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(k)), Body: bytes.NewReader(b)})
	return err
}
func (s *S3) Range(ctx context.Context, k string, offset int64, length int) ([]byte, error) {
	if offset < 0 || length <= 0 {
		return nil, fmt.Errorf("invalid range")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(k)), Range: aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+int64(length)-1))})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	if out.ContentRange == nil || !strings.HasPrefix(*out.ContentRange, fmt.Sprintf("bytes %d-%d/", offset, offset+int64(length)-1)) {
		return nil, fmt.Errorf("server did not honor byte range")
	}
	b, err := io.ReadAll(io.LimitReader(out.Body, int64(length)+1))
	if err == nil && len(b) != length {
		err = io.ErrUnexpectedEOF
	}
	return b, err
}
