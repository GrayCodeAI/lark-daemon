package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store defines the file storage interface.
type Store interface {
	// Save saves a file from reader and returns the storage path.
	Save(ctx context.Context, path string, r io.Reader) error
	// Open opens a file for reading.
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	// Delete removes a file.
	Delete(ctx context.Context, path string) error
	// URL returns the public URL for a file path.
	URL(ctx context.Context, path string) string
}

// Config holds storage configuration.
type Config struct {
	Type       string // "local" or "s3"
	LocalDir   string // base directory for local storage
	S3Bucket   string // S3 bucket name
	S3Region   string // S3 region
	S3Endpoint string // S3 endpoint (for MinIO/compatible)
	S3Key      string // S3 access key
	S3Secret   string // S3 secret key
}

// NewStore creates a Store based on config.
func NewStore(cfg Config) (Store, error) {
	switch cfg.Type {
	case "s3":
		return newS3Store(cfg)
	default:
		return newLocalStore(cfg)
	}
}

// --- Local Store ---

type localStore struct {
	baseDir string
}

func newLocalStore(cfg Config) (*localStore, error) {
	dir := cfg.LocalDir
	if dir == "" {
		dir = "data/files"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &localStore{baseDir: dir}, nil
}

func (s *localStore) fullPath(path string) string {
	return filepath.Join(s.baseDir, path)
}

func (s *localStore) Save(ctx context.Context, path string, r io.Reader) error {
	full := s.fullPath(path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	f, err := os.Create(full)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (s *localStore) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	return os.Open(s.fullPath(path))
}

func (s *localStore) Delete(ctx context.Context, path string) error {
	return os.Remove(s.fullPath(path))
}

func (s *localStore) URL(ctx context.Context, path string) string {
	return "" // local files served directly by the server
}

// --- S3 Store (stub for future implementation) ---

type s3Store struct {
	bucket   string
	region   string
	endpoint string
	key      string
	secret   string
}

func newS3Store(cfg Config) (*s3Store, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required for s3 storage")
	}
	return &s3Store{
		bucket:   cfg.S3Bucket,
		region:   cfg.S3Region,
		endpoint: cfg.S3Endpoint,
		key:      cfg.S3Key,
		secret:   cfg.S3Secret,
	}, nil
}

func (s *s3Store) Save(ctx context.Context, path string, r io.Reader) error {
	return fmt.Errorf("S3 storage not yet implemented")
}

func (s *s3Store) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("S3 storage not yet implemented")
}

func (s *s3Store) Delete(ctx context.Context, path string) error {
	return fmt.Errorf("S3 storage not yet implemented")
}

func (s *s3Store) URL(ctx context.Context, path string) string {
	if s.endpoint != "" {
		return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, path)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, path)
}
