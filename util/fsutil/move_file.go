package fsutil

import (
	"io"
	"os"
)

// MoveFile moves a file from sourcePath to destPath. Works across different filesystems
func MoveFile(sourcePath, destPath string) error {
	err := os.Rename(sourcePath, destPath)
	if err == nil {
		return nil
	}

	// If rename fails (likely cross-device link error), fallback to copy
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	if err := destFile.Sync(); err != nil {
		return err
	}

	srcFile.Close()
	destFile.Close()

	return os.Remove(sourcePath)
}
