package pmtiles

import (
	"context"
	"fmt"
	"gocloud.dev/blob"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Bucket interface {
	Close() error
	NewRangeReader(ctx context.Context, key string, offset int64, length int64) (io.ReadCloser, error)
}

type HTTPBucket struct {
	baseURL string
}

func (b HTTPBucket) NewRangeReader(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	reqURL := b.baseURL + "/" + key

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	return resp.Body, nil
}

func (b HTTPBucket) Close() error {
	return nil
}

type BucketAdapter struct {
	Bucket *blob.Bucket
}

func (ba BucketAdapter) NewRangeReader(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	reader, err := ba.Bucket.NewRangeReader(ctx, key, offset, length, nil)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func (ba BucketAdapter) Close() error {
	return ba.Bucket.Close()
}

func NormalizeBucketKey(bucket string, prefix string, key string) (string, string, error) {
	if bucket == "" {
		if strings.HasPrefix(key, "http") {
			u, err := url.Parse(key)
			if err != nil {
				return "", "", err
			}
			dir, file := path.Split(u.Path)
			if strings.HasSuffix(dir, "/") {
				dir = dir[:len(dir)-1]
			}
			return u.Scheme + "://" + u.Host + dir, file, nil
		} else {
			fileprotocol := "file://"
			if string(os.PathSeparator) != "/" {
				fileprotocol += "/"
			}
			if prefix != "" {
				abs, err := filepath.Abs(prefix)
				if err != nil {
					return "", "", err
				}
				return fileprotocol + filepath.ToSlash(abs), key, nil
			}
			abs, err := filepath.Abs(key)
			if err != nil {
				return "", "", err
			}
			return fileprotocol + filepath.ToSlash(filepath.Dir(abs)), filepath.Base(abs), nil
		}
	}

	return bucket, key, nil
}

func OpenBucket(ctx context.Context, bucketURL string, bucketPrefix string) (Bucket, error) {
	if strings.HasPrefix(bucketURL, "http") {
		bucket := HTTPBucket{bucketURL}
		return bucket, nil
	} else {
		bucket, err := blob.OpenBucket(ctx, bucketURL)
		if err != nil {
			return nil, err
		}
		if bucketPrefix != "" && bucketPrefix != "/" && bucketPrefix != "." {
			bucket = blob.PrefixedBucket(bucket, path.Clean(bucketPrefix)+string(os.PathSeparator))
		}
		wrappedBucket := BucketAdapter{bucket}
		return wrappedBucket, err
	}
}
