package web

import (
	"context"
	"time"
)

type ArtifactFileInfo struct {
	SizeBytes int64
	CreatedAt time.Time
}

type RunStore interface {
	RunDir(runID string) string
	LogPath(runID string) string
	TaskLogPath(runID, taskLabel string) string
	WorktreePath(runID string) string
	ArtifactDir(runID string) string
	MetaPath(runID string) string

	WriteMeta(meta *RunMeta) error
	ListMetas(ctx context.Context) ([]*RunMeta, error)
	ReadMeta(runID string) (*RunMeta, error)
	ReadLog(runID string) ([]byte, error)
	ReadTaskLog(runID, taskLabel string) ([]byte, error)
	TailLog(runID string, byteOffset int64) ([]byte, error)
	ListWorktreeFiles(runID string) ([]string, error)
	ReadWorktreeFile(runID, filePath string) ([]byte, error)
	ListArtifactFiles(runID string) ([]string, error)
	StatArtifactFile(runID, filePath string) (ArtifactFileInfo, error)
	ReadArtifactFile(runID, filePath string) ([]byte, error)
	// ReadWorktreeZip returns the worktree as a zip archive bytes.
	ReadWorktreeZip(runID string) ([]byte, error)
	// PresignWorktreeURL returns a presigned URL to download the worktree zip when supported.
	PresignWorktreeURL(runID string, expiry time.Duration) (string, error)
	DeleteRun(runID string) error
	FinalizeRun(runID string) error
}
