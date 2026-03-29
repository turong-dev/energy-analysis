package store

import "context"

// Store is the interface satisfied by both Client and CachedClient.
type Store interface {
	GetJSON(ctx context.Context, key string, v any) error
	GetRaw(ctx context.Context, key string) ([]byte, error)
	PutJSON(ctx context.Context, key string, v any) error
	PutRaw(ctx context.Context, key, contentType string, data []byte) error
	List(ctx context.Context, prefix string) ([]string, error)
	Exists(ctx context.Context, key string) (bool, error)
}
