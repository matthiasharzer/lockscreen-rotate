package rotate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/matthiasharzer/lockscreen-rotate/lockscreen"
	"github.com/matthiasharzer/lockscreen-rotate/logging"
	"github.com/matthiasharzer/lockscreen-rotate/rotate/updater"
	"github.com/matthiasharzer/lockscreen-rotate/trigger"
)

type Schedule struct {
	MinUpdateDelay time.Duration
	MaxUpdateDelay time.Duration
}

type Manager struct {
	targetSymlinkPath string
	cacheDirectory    string
	isDownloading     atomic.Bool
	updateFile        updater.Updater
	lockscreenState   *lockscreen.State
	trigger           trigger.Trigger

	pendingVideo string
}

func NewRotateManager(
	symlinkPath,
	cacheDirectory string,
	updater updater.Updater,
	lockscreenState *lockscreen.State,
	trigger trigger.Trigger,
) (*Manager, error) {
	absSymlinkPath, err := filepath.Abs(symlinkPath)
	if err != nil {
		return nil, fmt.Errorf("could not resolve absolute symlink path: %w", err)
	}

	return &Manager{
		targetSymlinkPath: absSymlinkPath,
		cacheDirectory:    cacheDirectory,
		updateFile:        updater,
		lockscreenState:   lockscreenState,
		trigger:           trigger,
	}, nil
}

func (m *Manager) isScreenLocked() bool {
	if m.lockscreenState == nil {
		logging.Warn("lockscreen state is nil. Assuming screen is unlocked")
		return false
	}
	return m.lockscreenState.IsLocked()
}

func (m *Manager) update() {
	if !m.isDownloading.CompareAndSwap(false, true) {
		logging.Info("download already in progress. Skipping")
		return
	}
	defer m.isDownloading.Store(false)

	timestamp := time.Now().Unix()
	finalFilename := filepath.Join(m.cacheDirectory, fmt.Sprintf("file_%d", timestamp))
	tempFilename := finalFilename + ".part"

	err := m.updateFile(tempFilename)
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

	if m.isScreenLocked() {
		m.postponeVideo(finalFilename)
		return
	}

	// Screen is unlocked, apply immediately
	m.applySymlink(finalFilename)
	m.cleanupOldVideos(finalFilename)
}

func (m *Manager) postponeVideo(videoPath string) {
	logging.Info("postponing video application until screen is unlocked", "videoPath", videoPath)
	m.pendingVideo = videoPath
}

func (m *Manager) applySymlink(videoPath string) {
	tempSymlink := m.targetSymlinkPath + ".tmp"
	_ = os.Remove(tempSymlink) // Ensure no leftover temp symlink exists

	err := os.Symlink(videoPath, tempSymlink)
	if err != nil {
		logging.Error("failed to create temporary symlink", "error", err)
		return
	}

	err = os.Rename(tempSymlink, m.targetSymlinkPath)
	if err != nil {
		logging.Error("failed to atomically update symlink", "error", err)
		return
	}

	currentTime := time.Now()
	err = os.Chtimes(videoPath, currentTime, currentTime)
	if err != nil {
		logging.Warn("failed to update file timestamps", "error", err)
	}
	logging.Info("symlink updated successfully", "symlinkPath", m.targetSymlinkPath, "videoPath", videoPath)
}

func (m *Manager) cleanupOldVideos(keepFile string) {
	entries, err := os.ReadDir(m.cacheDirectory)
	if err != nil {
		logging.Error("failed to read storage directory for cleanup", "error", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filePath := filepath.Join(m.cacheDirectory, entry.Name())
		isKnownFile := filePath == keepFile || filePath == m.pendingVideo
		if isKnownFile {
			continue
		}

		err = os.Remove(filePath)
		if err == nil {
			logging.Info("removed old video file during cleanup", "filePath", filePath)
		}
	}
}

func (m *Manager) applyPendingVideo() {
	if m.pendingVideo == "" {
		return
	}
	logging.Info("applying pending video", "pendingVideo", m.pendingVideo)
	m.applySymlink(m.pendingVideo)
	m.pendingVideo = ""
}

func (m *Manager) Run(ctx context.Context) error {
	isScreenLockedSub := m.lockscreenState.Subscribe()
	defer m.lockscreenState.Unsubscribe(isScreenLockedSub)

	for {
		select {
		case <-m.trigger:
			logging.Info("trigger event received. Initiating update.")
			go m.update()
		case isLocked := <-isScreenLockedSub:
			hasPendingVideo := m.pendingVideo != ""
			if !isLocked && hasPendingVideo {
				logging.Info("screen unlocked and pending video exists. Applying pending video.")
				m.applyPendingVideo()
			}
		case <-ctx.Done():
			return nil
		}
	}
}
