package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type ObjectRunStore struct {
	local  *LocalRunStore
	client *s3.Client
	bucket string
	prefix string
}

func NewObjectRunStore(ctx context.Context, historyDir string, options ObjectIndexStoreOptions) (*ObjectRunStore, error) {
	client, err := newS3Client(ctx, options)
	if err != nil {
		return nil, err
	}
	return &ObjectRunStore{
		local:  NewLocalRunStore(historyDir),
		client: client,
		bucket: options.Bucket,
		prefix: strings.Trim(options.Prefix, "/"),
	}, nil
}

func (s *ObjectRunStore) RunDir(runID string) string {
	return s.local.RunDir(runID)
}

func (s *ObjectRunStore) LogPath(runID string) string {
	return s.local.LogPath(runID)
}

func (s *ObjectRunStore) TaskLogPath(runID, taskLabel string) string {
	return s.local.TaskLogPath(runID, taskLabel)
}

func (s *ObjectRunStore) WorktreePath(runID string) string {
	return s.local.WorktreePath(runID)
}

func (s *ObjectRunStore) ArtifactDir(runID string) string {
	return s.local.ArtifactDir(runID)
}

func (s *ObjectRunStore) MetaPath(runID string) string {
	return s.local.MetaPath(runID)
}

func (s *ObjectRunStore) WriteMeta(meta *RunMeta) error {
	return s.local.WriteMeta(meta)
}

func (s *ObjectRunStore) ListMetas(ctx context.Context) ([]*RunMeta, error) {
	metasByID := map[string]*RunMeta{}

	prefix := joinObjectKey(s.prefix, "runs") + "/"
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list run metadata objects: %w", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if !strings.HasSuffix(key, "/meta.yaml") {
				continue
			}
			runID := strings.TrimSuffix(strings.TrimPrefix(key, prefix), "/meta.yaml")
			if strings.Contains(runID, "/") || runID == "" {
				continue
			}
			data, err := s.readObject(ctx, key)
			if err != nil {
				return nil, err
			}
			meta, err := parseRunMeta(data, runID)
			if err != nil {
				return nil, err
			}
			metasByID[runID] = meta
		}
	}

	localMetas, err := s.local.ListMetas(ctx)
	if err != nil {
		return nil, err
	}
	for _, meta := range localMetas {
		metasByID[meta.RunID] = meta
	}

	metas := make([]*RunMeta, 0, len(metasByID))
	for _, meta := range metasByID {
		metas = append(metas, meta)
	}
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].StartTime.Equal(metas[j].StartTime) {
			return metas[i].RunID < metas[j].RunID
		}
		return metas[i].StartTime.Before(metas[j].StartTime)
	})
	return metas, nil
}

func (s *ObjectRunStore) ReadMeta(runID string) (*RunMeta, error) {
	if meta, err := s.local.ReadMeta(runID); err == nil {
		return meta, nil
	}
	data, err := s.readObject(context.Background(), s.runObjectKey(runID, "meta.yaml"))
	if err != nil {
		return nil, err
	}
	return parseRunMeta(data, runID)
}

func (s *ObjectRunStore) ReadLog(runID string) ([]byte, error) {
	if data, err := s.local.ReadLog(runID); err == nil {
		return data, nil
	}
	return s.readObject(context.Background(), s.runObjectKey(runID, "stdout.log"))
}

func (s *ObjectRunStore) ReadTaskLog(runID, taskLabel string) ([]byte, error) {
	if data, err := s.local.ReadTaskLog(runID, taskLabel); err == nil {
		return data, nil
	}
	return s.readObject(context.Background(), s.runObjectKey(runID, filepath.ToSlash(filepath.Join("tasks", sanitizeTaskLabel(taskLabel)+".log"))))
}

func (s *ObjectRunStore) TailLog(runID string, byteOffset int64) ([]byte, error) {
	if data, err := s.local.TailLog(runID, byteOffset); err == nil {
		return data, nil
	}
	data, err := s.ReadLog(runID)
	if err != nil {
		return nil, err
	}
	if byteOffset <= 0 || byteOffset >= int64(len(data)) {
		return data, nil
	}
	return data[byteOffset:], nil
}

func (s *ObjectRunStore) ListWorktreeFiles(runID string) ([]string, error) {
	if files, err := s.local.ListWorktreeFiles(runID); err == nil {
		return files, nil
	}
	prefix := s.runObjectKey(runID, "worktree/")
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	var files []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list worktree objects %s: %w", runID, err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if strings.HasSuffix(key, "/") {
				continue
			}
			rel := strings.TrimPrefix(key, prefix)
			if rel == "" {
				continue
			}
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	return files, nil
}

func (s *ObjectRunStore) ReadWorktreeFile(runID, filePath string) ([]byte, error) {
	if data, err := s.local.ReadWorktreeFile(runID, filePath); err == nil {
		return data, nil
	}
	return s.readObject(context.Background(), s.runObjectKey(runID, filepath.ToSlash(filepath.Join("worktree", filePath))))
}

func (s *ObjectRunStore) ListArtifactFiles(runID string) ([]string, error) {
	if files, err := s.local.ListArtifactFiles(runID); err == nil {
		return files, nil
	}
	prefix := s.runObjectKey(runID, "artifacts/")
	return s.listFilesUnderPrefix(prefix)
}

func (s *ObjectRunStore) StatArtifactFile(runID, filePath string) (ArtifactFileInfo, error) {
	if info, err := s.local.StatArtifactFile(runID, filePath); err == nil {
		return info, nil
	}
	output, err := s.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.runObjectKey(runID, filepath.ToSlash(filepath.Join("artifacts", filePath)))),
	})
	if err != nil {
		return ArtifactFileInfo{}, fmt.Errorf("head object %s: %w", filePath, err)
	}
	createdAt := time.Time{}
	if output.LastModified != nil {
		createdAt = *output.LastModified
	}
	sizeBytes := int64(0)
	if output.ContentLength != nil {
		sizeBytes = *output.ContentLength
	}
	return ArtifactFileInfo{
		SizeBytes: sizeBytes,
		CreatedAt: createdAt,
	}, nil
}

func (s *ObjectRunStore) ReadArtifactFile(runID, filePath string) ([]byte, error) {
	if data, err := s.local.ReadArtifactFile(runID, filePath); err == nil {
		return data, nil
	}
	return s.readObject(context.Background(), s.runObjectKey(runID, filepath.ToSlash(filepath.Join("artifacts", filePath))))
}

func (s *ObjectRunStore) DeleteRun(runID string) error {
	_ = s.local.DeleteRun(runID)
	return s.deleteRemoteRun(runID)
}

func (s *ObjectRunStore) deleteRemoteRun(runID string) error {
	prefix := s.runObjectKey(runID, "")
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return fmt.Errorf("list run objects %s: %w", runID, err)
		}
		for _, object := range page.Contents {
			if _, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    object.Key,
			}); err != nil {
				return fmt.Errorf("delete run object %s: %w", aws.ToString(object.Key), err)
			}
		}
	}
	return nil
}

func (s *ObjectRunStore) FinalizeRun(runID string) error {
	root := s.local.RunDir(runID)
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	if err := s.deleteRemoteRun(runID); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = s.client.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket:      aws.String(s.bucket),
			Key:         aws.String(s.runObjectKey(runID, filepath.ToSlash(rel))),
			Body:        bytes.NewReader(data),
			ContentType: aws.String(contentTypeForRunFile(rel)),
		})
		if err != nil {
			return fmt.Errorf("upload run object %s: %w", rel, err)
		}
		return nil
	})
}

func (s *ObjectRunStore) runObjectKey(runID, rel string) string {
	base := joinObjectKey(s.prefix, filepath.ToSlash(filepath.Join("runs", runID)))
	if rel == "" {
		return base
	}
	return base + "/" + strings.TrimPrefix(rel, "/")
}

func (s *ObjectRunStore) readObject(ctx context.Context, key string) ([]byte, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	defer output.Body.Close()
	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, fmt.Errorf("read object %s: %w", key, err)
	}
	return data, nil
}

func (s *ObjectRunStore) listFilesUnderPrefix(prefix string) ([]string, error) {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	var files []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list objects %s: %w", prefix, err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if strings.HasSuffix(key, "/") {
				continue
			}
			rel := strings.TrimPrefix(key, prefix)
			if rel == "" {
				continue
			}
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	return files, nil
}

func contentTypeForRunFile(path string) string {
	switch {
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
		return "application/x-yaml"
	case strings.HasSuffix(path, ".log"), strings.HasSuffix(path, ".txt"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
