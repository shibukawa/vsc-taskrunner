package tasks

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/creack/pty"
)

func (r *Runner) runCommand(ctx context.Context, cmd *exec.Cmd, outputWriter io.Writer) (*os.ProcessState, error) {
	if !r.shouldUsePTY() {
		cmd.Stdout = outputWriter
		cmd.Stderr = outputWriter
		err := cmd.Run()
		return cmd.ProcessState, err
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		cmd.Stdout = outputWriter
		cmd.Stderr = outputWriter
		err = cmd.Run()
		return cmd.ProcessState, err
	}
	defer ptmx.Close()

	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(outputWriter, ptmx)
		copyDone <- normalizePTYCopyError(copyErr)
	}()

	if r.options.Input != nil {
		go func(input io.Reader) {
			_, _ = io.Copy(ptmx, input)
		}(r.options.Input)
	}

	waitErr := cmd.Wait()
	_ = ptmx.Close()
	copyErr := <-copyDone
	if copyErr != nil && waitErr == nil && !errors.Is(ctx.Err(), context.Canceled) {
		return cmd.ProcessState, copyErr
	}
	return cmd.ProcessState, waitErr
}

func normalizePTYCopyError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
		return nil
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.EIO {
		return nil
	}
	if strings.Contains(err.Error(), "input/output error") {
		return nil
	}
	return err
}
