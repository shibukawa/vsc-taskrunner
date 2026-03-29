package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const apiTokenObjectName = "api-tokens.json"

type ObjectAPITokenStore struct {
	client     *s3.Client
	bucket     string
	key        string
	maxRetries int
}

func NewObjectAPITokenStore(ctx context.Context, options ObjectIndexStoreOptions) (*ObjectAPITokenStore, error) {
	client, err := newS3Client(ctx, options)
	if err != nil {
		return nil, err
	}
	maxRetries := options.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}
	return &ObjectAPITokenStore{
		client:     client,
		bucket:     options.Bucket,
		key:        joinObjectKey(options.Prefix, apiTokenObjectName),
		maxRetries: maxRetries,
	}, nil
}

func (s *ObjectAPITokenStore) ReadAll(ctx context.Context) ([]*APITokenRecord, error) {
	records, _, err := s.readAll(ctx)
	return records, err
}

func (s *ObjectAPITokenStore) UpdateAll(ctx context.Context, fn func([]*APITokenRecord) ([]*APITokenRecord, error)) error {
	for attempt := 0; attempt < s.maxRetries; attempt++ {
		records, version, err := s.readAll(ctx)
		if err != nil {
			return err
		}
		updated, err := fn(records)
		if err != nil {
			return err
		}
		if err := s.writeAll(ctx, updated, version); err != nil {
			if isCASConflict(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("update api token store: compare-and-swap retries exhausted")
}

func (s *ObjectAPITokenStore) readAll(ctx context.Context) ([]*APITokenRecord, string, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	})
	if err != nil {
		if isObjectNotFound(err) {
			return []*APITokenRecord{}, "", nil
		}
		return nil, "", fmt.Errorf("get api token object: %w", err)
	}
	defer output.Body.Close()
	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read api token object: %w", err)
	}
	if len(data) == 0 {
		return []*APITokenRecord{}, normalizeETag(aws.ToString(output.ETag)), nil
	}
	var records []*APITokenRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, "", fmt.Errorf("parse api token object: %w", err)
	}
	if records == nil {
		records = []*APITokenRecord{}
	}
	return records, normalizeETag(aws.ToString(output.ETag)), nil
}

func (s *ObjectAPITokenStore) writeAll(ctx context.Context, records []*APITokenRecord, version string) error {
	body, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal api token object: %w", err)
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
		return fmt.Errorf("put api token object: %w", err)
	}
	return nil
}
