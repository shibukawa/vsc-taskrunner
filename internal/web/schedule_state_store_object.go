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

type ObjectScheduleStateStore struct {
	client     *s3.Client
	bucket     string
	key        string
	maxRetries int
}

func NewObjectScheduleStateStore(ctx context.Context, options ObjectIndexStoreOptions) (*ObjectScheduleStateStore, error) {
	client, err := newS3Client(ctx, options)
	if err != nil {
		return nil, err
	}
	maxRetries := options.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}
	return &ObjectScheduleStateStore{
		client:     client,
		bucket:     options.Bucket,
		key:        joinObjectKey(options.Prefix, scheduleStateObjectName),
		maxRetries: maxRetries,
	}, nil
}

func (s *ObjectScheduleStateStore) ReadState(ctx context.Context) (*ScheduleStateIndex, error) {
	state, _, err := s.readState(ctx)
	return state, err
}

func (s *ObjectScheduleStateStore) UpdateState(ctx context.Context, fn func(*ScheduleStateIndex) error) error {
	for attempt := 0; attempt < s.maxRetries; attempt++ {
		state, version, err := s.readState(ctx)
		if err != nil {
			return err
		}
		if err := fn(state); err != nil {
			return err
		}
		if err := s.writeState(ctx, state, version); err != nil {
			if isCASConflict(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("update schedule state: compare-and-swap retries exhausted")
}

func (s *ObjectScheduleStateStore) readState(ctx context.Context) (*ScheduleStateIndex, string, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	})
	if err != nil {
		if isObjectNotFound(err) {
			return newScheduleStateIndex(), "", nil
		}
		return nil, "", fmt.Errorf("get schedule state object: %w", err)
	}
	defer output.Body.Close()
	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read schedule state object: %w", err)
	}
	if len(data) == 0 {
		return newScheduleStateIndex(), normalizeETag(aws.ToString(output.ETag)), nil
	}
	var state ScheduleStateIndex
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, "", fmt.Errorf("parse schedule state object: %w", err)
	}
	if state.Items == nil {
		state.Items = make(map[string]ScheduleState)
	}
	return &state, normalizeETag(aws.ToString(output.ETag)), nil
}

func (s *ObjectScheduleStateStore) writeState(ctx context.Context, state *ScheduleStateIndex, version string) error {
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schedule state object: %w", err)
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
		return fmt.Errorf("put schedule state object: %w", err)
	}
	return nil
}
