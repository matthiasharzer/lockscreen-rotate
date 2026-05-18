package mediamanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/matthiasharzer/lockscreen-rotate/lockscreen"
	"github.com/matthiasharzer/lockscreen-rotate/logging"
	"github.com/matthiasharzer/lockscreen-rotate/trigger"
)

type Schedule struct {
	MinUpdateDelay time.Duration
	MaxUpdateDelay time.Duration
}

type Updater func(destPath string) error

type VideoManager struct {
	targetSymlinkPath string
	cacheDirectory    string
	isDownloading     atomic.Bool
	updateFile        Updater
	lockscreenState   *lockscreen.State
	trigger           trigger.Trigger

	pendingVideo string
}

func NewVideoManager(
	symlinkPath,
	cacheDirectory string,
	downloader Updater,
	lockscreenState *lockscreen.State,
	trigger trigger.Trigger,
) (*VideoManager, error) {
	absSymlinkPath, err := filepath.Abs(symlinkPath)
	if err != nil {
		return nil, fmt.Errorf("could not resolve absolute symlink path: %w", err)
	}

	return &VideoManager{
		targetSymlinkPath: absSymlinkPath,
		cacheDirectory:    cacheDirectory,
		updateFile:        downloader,
		lockscreenState:   lockscreenState,
		trigger:           trigger,
	}, nil
}

func (vm *VideoManager) isScreenLocked() bool {
	if vm.lockscreenState == nil {
		logging.Warn("lockscreen state is nil. Assuming screen is unlocked")
		return false
	}
	return vm.lockscreenState.IsLocked()
}

func (vm *VideoManager) update() {
	if !vm.isDownloading.CompareAndSwap(false, true) {
		logging.Info("download already in progress. Skipping")
		return
	}
	defer vm.isDownloading.Store(false)

	timestamp := time.Now().Unix()
	finalFilename := filepath.Join(vm.cacheDirectory, fmt.Sprintf("file_%d", timestamp))
	tempFilename := finalFilename + ".part"

	err := vm.updateFile(tempFilename)
	if err != nil {
		logging.Error("failed to download video", "error", err)
		_ = os.Remove(tempFilename)
		logging.Info("symlink remains unchanged. Will retry on next interval or unlock event")
		return
	}

	err = os.Rename(tempFilename, finalFilename)
	if err != nil {
		logging.Error("failed to finalize video file", "error", err)
		return
	}

	logging.Info("download successful", "finalFilename", finalFilename)

	if vm.isScreenLocked() {
		vm.postponeVideo(finalFilename)
		return
	}

	// Screen is unlocked, apply immediately
	vm.applySymlink(finalFilename)
	vm.cleanupOldVideos(finalFilename)
}

func (vm *VideoManager) postponeVideo(videoPath string) {
	logging.Info("postponing video application until screen is unlocked", "videoPath", videoPath)
	vm.pendingVideo = videoPath
}

func (vm *VideoManager) applySymlink(videoPath string) {
	tempSymlink := vm.targetSymlinkPath + ".tmp"
	_ = os.Remove(tempSymlink) // Ensure no leftover temp symlink exists

	err := os.Symlink(videoPath, tempSymlink)
	if err != nil {
		logging.Error("failed to create temporary symlink", "error", err)
		return
	}

	err = os.Rename(tempSymlink, vm.targetSymlinkPath)
	if err != nil {
		logging.Error("failed to atomically update symlink", "error", err)
		return
	}

	currentTime := time.Now()
	err = os.Chtimes(videoPath, currentTime, currentTime)
	if err != nil {
		logging.Warn("failed to update file timestamps", "error", err)
	}
	logging.Info("symlink updated successfully", "symlinkPath", vm.targetSymlinkPath, "videoPath", videoPath)
}

func (vm *VideoManager) cleanupOldVideos(keepFile string) {
	entries, err := os.ReadDir(vm.cacheDirectory)
	if err != nil {
		logging.Error("failed to read storage directory for cleanup", "error", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filePath := filepath.Join(vm.cacheDirectory, entry.Name())
		isKnownFile := filePath == keepFile || filePath == vm.pendingVideo
		if isKnownFile {
			continue
		}

		err = os.Remove(filePath)
		if err == nil {
			logging.Info("removed old video file during cleanup", "filePath", filePath)
		}
	}
}

func (vm *VideoManager) applyPendingVideo() {
	if vm.pendingVideo == "" {
		return
	}
	logging.Info("applying pending video", "pendingVideo", vm.pendingVideo)
	vm.applySymlink(vm.pendingVideo)
	vm.pendingVideo = ""
}

func (vm *VideoManager) Run(ctx context.Context) error {
	isScreenLockedSub := vm.lockscreenState.Subscribe()
	defer vm.lockscreenState.Unsubscribe(isScreenLockedSub)

	for {
		select {
		case <-vm.trigger:
			logging.Info("trigger event received. Initiating update.")
			go vm.update()
		case isLocked := <-isScreenLockedSub:
			hasPendingVideo := vm.pendingVideo != ""
			if !isLocked && hasPendingVideo {
				logging.Info("screen unlocked and pending video exists. Applying pending video.")
				vm.applyPendingVideo()
			}
		case <-ctx.Done():
			return nil
		}
	}
}
