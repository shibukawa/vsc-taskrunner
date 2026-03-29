package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type ObjectIndexStoreOptions struct {
	Endpoint       string
	Bucket         string
	Region         string
	AccessKey      string
	SecretKey      string
	Prefix         string
	ForcePathStyle bool
	MaxRetries     int
}

type ObjectIndexStore struct {
	client     *s3.Client
	bucket     string
	key        string
	maxRetries int
}

func NewObjectIndexStore(ctx context.Context, options ObjectIndexStoreOptions) (*ObjectIndexStore, error) {
	client, err := newS3Client(ctx, options)
	if err != nil {
		return nil, err
	}
	maxRetries := options.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}
	return &ObjectIndexStore{
		client:     client,
		bucket:     options.Bucket,
		key:        joinObjectKey(options.Prefix, historyIndexObjectName),
		maxRetries: maxRetries,
	}, nil
}

func (s *ObjectIndexStore) ReadIndex(ctx context.Context) (*RunHistoryIndex, error) {
	index, _, err := s.readIndex(ctx)
	return index, err
}

func (s *ObjectIndexStore) UpdateIndex(ctx context.Context, fn func(*RunHistoryIndex) error) error {
	for attempt := 0; attempt < s.maxRetries; attempt++ {
		index, version, err := s.readIndex(ctx)
		if err != nil {
			return err
		}
		if err := fn(index); err != nil {
			return err
		}
		if err := s.writeIndex(ctx, index, version); err != nil {
			if isCASConflict(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("update history index: compare-and-swap retries exhausted")
}

func (s *ObjectIndexStore) readIndex(ctx context.Context) (*RunHistoryIndex, string, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	})
	if err != nil {
		if isObjectNotFound(err) {
			return newRunHistoryIndex(), "", nil
		}
		return nil, "", fmt.Errorf("get history index object: %w", err)
	}
	defer output.Body.Close()
	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read history index object: %w", err)
	}
	if len(data) == 0 {
		return newRunHistoryIndex(), aws.ToString(output.ETag), nil
	}
	var index RunHistoryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, "", fmt.Errorf("parse history index object: %w", err)
	}
	if index.Groups == nil {
		index.Groups = make(map[string]*RunHistoryGroup)
	}
	return &index, normalizeETag(aws.ToString(output.ETag)), nil
}

func (s *ObjectIndexStore) writeIndex(ctx context.Context, index *RunHistoryIndex, version string) error {
	body, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal history index object: %w", err)
	}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json"),
	}
	version = normalizeETag(version)
	if version == "" {
		input.IfNoneMatch = aws.String("*")
	} else {
		input.IfMatch = aws.String(version)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("put history index object: %w", err)
	}
	return nil
}

func joinObjectKey(prefix, name string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func normalizeETag(value string) string {
	return strings.Trim(value, "\"")
}

func isObjectNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "NoSuchKey" || code == "NotFound" || code == "NoSuchBucket"
	}
	return false
}

func isCASConflict(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "PreconditionFailed" || code == "ConditionalRequestConflict" {
			return true
		}
	}
	return strings.Contains(err.Error(), "status code: 412") || strings.Contains(err.Error(), "status code: 409")
}
