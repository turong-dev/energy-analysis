package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"energy-utility/internal/config"
)

type Client struct {
	s3     *s3.Client
	bucket string
}

// New builds an S3 client using the default AWS credential chain
// (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY env vars, ~/.aws/credentials, etc.)
// with a custom endpoint for R2 or other S3-compatible providers.
func New(ctx context.Context, cfg config.S3Config) (*Client, error) {
	awscfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	s3client := s3.NewFromConfig(awscfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = true
	})
	return &Client{s3: s3client, bucket: cfg.Bucket}, nil
}

// PutJSON serialises v as JSON and writes it to key.
func (c *Client) PutJSON(ctx context.Context, key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	return err
}

// PutRaw writes raw bytes to key with the given content type.
func (c *Client) PutRaw(ctx context.Context, key, contentType string, data []byte) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}

// GetJSON fetches key and deserialises the JSON body into v.
func (c *Client) GetJSON(ctx context.Context, key string, v any) error {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	defer out.Body.Close()
	return json.NewDecoder(out.Body).Decode(v)
}

// GetRaw fetches key and returns the raw bytes.
func (c *Client) GetRaw(ctx context.Context, key string) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// IsNotFound reports whether err is a NoSuchKey (object not found) error.
func IsNotFound(err error) bool {
	var nsk *types.NoSuchKey
	return errors.As(err, &nsk)
}

// List returns all object keys under prefix.
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})
	var keys []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}
	return keys, nil
}

// Exists returns true if key exists in the bucket.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}
