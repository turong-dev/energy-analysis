package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// CachedClient wraps a Client and caches GetJSON/GetRaw results to local disk.
// Cache files mirror the S3 key structure under the configured directory.
// Writes (PutJSON/PutRaw) go to S3 and also update the local cache.
// List and Exists always delegate to S3.
type CachedClient struct {
	s3  *Client
	dir string
}

// NewCached returns a CachedClient that stores files under dir.
func NewCached(s3 *Client, dir string) *CachedClient {
	return &CachedClient{s3: s3, dir: dir}
}

func (c *CachedClient) cachePath(key string) string {
	return filepath.Join(c.dir, filepath.FromSlash(key))
}

// GetJSON checks the local cache first. On a miss it fetches from S3 and
// writes the raw JSON to the cache before returning.
func (c *CachedClient) GetJSON(ctx context.Context, key string, v any) error {
	path := c.cachePath(key)
	if data, err := os.ReadFile(path); err == nil {
		return json.Unmarshal(data, v)
	}
	// Fetch from S3, capturing the raw bytes so we can cache them.
	data, err := c.s3.GetRaw(ctx, key)
	if err != nil {
		return err
	}
	if jsonErr := json.Unmarshal(data, v); jsonErr != nil {
		return jsonErr
	}
	_ = writeFile(path, data)
	return nil
}

// GetRaw checks the local cache first; on a miss fetches from S3 and caches.
func (c *CachedClient) GetRaw(ctx context.Context, key string) ([]byte, error) {
	path := c.cachePath(key)
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	data, err := c.s3.GetRaw(ctx, key)
	if err != nil {
		return nil, err
	}
	_ = writeFile(path, data)
	return data, nil
}

// PutJSON writes to S3 and updates the local cache on success.
func (c *CachedClient) PutJSON(ctx context.Context, key string, v any) error {
	if err := c.s3.PutJSON(ctx, key, v); err != nil {
		return err
	}
	if data, err := json.Marshal(v); err == nil {
		_ = writeFile(c.cachePath(key), data)
	}
	return nil
}

// PutRaw writes to S3 and updates the local cache on success.
func (c *CachedClient) PutRaw(ctx context.Context, key, contentType string, data []byte) error {
	if err := c.s3.PutRaw(ctx, key, contentType, data); err != nil {
		return err
	}
	_ = writeFile(c.cachePath(key), data)
	return nil
}

// List always delegates to S3.
func (c *CachedClient) List(ctx context.Context, prefix string) ([]string, error) {
	return c.s3.List(ctx, prefix)
}

// Exists always delegates to S3.
func (c *CachedClient) Exists(ctx context.Context, key string) (bool, error) {
	return c.s3.Exists(ctx, key)
}

// writeFile writes data to path, creating parent directories as needed.
// Errors are intentionally ignored by callers — cache writes are best-effort.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

