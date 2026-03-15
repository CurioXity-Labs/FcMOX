package vmmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

func copyAndResizeImage(srcPath, dstPath string, targetSize int64) (extendedRootfsPath string, err error) {
	ctx := context.Background()
	// 1. Open source file
	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// 2. Stat the source to get current size and file permissions
	srcInfo, err := src.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat source file: %w", err)
	}

	// 3. Create destination file preserving source permissions
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}

	// Handle destination closure safely, capturing write-flush errors
	defer func() {
		if closeErr := dst.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close destination file securely: %w", closeErr)
		}
	}()

	// 4. Perform optimized copy (uses copy_file_range on Linux)
	copiedBytes, err := io.Copy(dst, src)
	if err != nil {
		return "", fmt.Errorf("failed during data copy: %w", err)
	}

	// 5. Extend the file if a larger target size is requested
	if targetSize > copiedBytes {
		if err := dst.Truncate(targetSize); err != nil {
			return "", fmt.Errorf("failed to truncate/resize file: %w", err)
		}
	}

	// 6. Sync to disk. Crucial for filesystem images to prevent corruption.
	if err := dst.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync data to disk: %w", err)
	}

	if err := expandExt4FS(ctx, dstPath); err != nil {
		return "", fmt.Errorf("failed to expand filesystem: %w", err)
	}

	return dstPath, nil
}

// ExpandExt4FS checks the filesystem integrity and expands it to fill the file.
func expandExt4FS(ctx context.Context, imagePath string) error {
	// 1. Enforce a timeout. Filesystem operations on large files can take time,
	// but they shouldn't block our Go application forever.
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// 2. Run e2fsck (Filesystem Check)
	// -p: Automatic repair (no interactive prompts)
	// -f: Force checking even if the filesystem seems clean
	fsckCmd := exec.CommandContext(cmdCtx, "e2fsck", "-p", "-f", imagePath)
	var fsckErrBuf bytes.Buffer
	fsckCmd.Stderr = &fsckErrBuf

	if err := fsckCmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// e2fsck exit codes are bitmasks.
			// 0 = Clean, 1 = Errors corrected (acceptable for us to proceed).
			// Anything higher (4 = uncorrected errors, 8 = operational error) is fatal.
			if exitErr.ExitCode() > 1 {
				return fmt.Errorf("e2fsck failed (code %d): %s", exitErr.ExitCode(), fsckErrBuf.String())
			}
		} else {
			return fmt.Errorf("failed to execute e2fsck: %w", err)
		}
	}

	// 3. Run resize2fs
	// Without a size argument, resize2fs automatically expands to the file's boundaries.
	resizeCmd := exec.CommandContext(cmdCtx, "resize2fs", imagePath)
	var resizeErrBuf bytes.Buffer
	resizeCmd.Stderr = &resizeErrBuf

	if err := resizeCmd.Run(); err != nil {
		// Context errors (like DeadlineExceeded) will be caught here too.
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("resize2fs operation timed out")
		}
		return fmt.Errorf("resize2fs failed: %w - %s", err, resizeErrBuf.String())
	}

	return nil
}
