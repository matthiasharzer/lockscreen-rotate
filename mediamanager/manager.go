package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/matthiasharzer/lockscreen-rotate/logging"
)

//const (
//	MinDownloadInterval = 2 * time.Hour
//	MaxDownloadInterval = 24 * time.Hour
//	CheckTickRate       = 60 * time.Second
//	DownloadTimeout     = 30 * time.Second
//)

type UpdateOptions struct {
	MinDownloadInterval time.Duration
	MaxDownloadInterval time.Duration
	CheckTickRate       time.Duration
	DownloadTimeout     time.Duration
}

func (o UpdateOptions) WithDefaults() UpdateOptions {
	if o.MinDownloadInterval == 0 {
		o.MinDownloadInterval = 2 * time.Hour
	}
	if o.MaxDownloadInterval == 0 {
		o.MaxDownloadInterval = 24 * time.Hour
	}
	if o.CheckTickRate == 0 {
		o.CheckTickRate = 60 * time.Second
	}
	if o.DownloadTimeout == 0 {
		o.DownloadTimeout = 30 * time.Second
	}
	return o
}

func (o UpdateOptions) Validate() error {
	if o.MinDownloadInterval <= 0 {
		return fmt.Errorf("MinDownloadInterval must be greater than zero")
	}
	if o.MaxDownloadInterval <= 0 {
		return fmt.Errorf("MaxDownloadInterval must be greater than zero")
	}
	if o.CheckTickRate <= 0 {
		return fmt.Errorf("CheckTickRate must be greater than zero")
	}
	if o.DownloadTimeout <= 0 {
		return fmt.Errorf("DownloadTimeout must be greater than zero")
	}
	if o.MinDownloadInterval > o.MaxDownloadInterval {
		return fmt.Errorf("MinDownloadInterval cannot be greater than MaxDownloadInterval")
	}
	return nil
}

// Updater is a function signature that defines how a file should be updated.
type Updater func(destPath string) error

// VideoManager handles state, scheduling, file rotation, and event listening.
type VideoManager struct {
	targetSymlinkPath string
	cacheDirectory    string
	isDownloading     atomic.Bool
	updateFile        Updater
	options           UpdateOptions

	mu             sync.RWMutex
	isScreenLocked bool
	pendingVideo   string
}

func NewVideoManager(symlinkPath, cacheDirectory string, downloader Updater, options UpdateOptions) (*VideoManager, error) {
	absSymlinkPath, err := filepath.Abs(symlinkPath)
	if err != nil {
		return nil, fmt.Errorf("could not resolve absolute symlink path: %w", err)
	}

	withDefaultOptions := options.WithDefaults()

	err = withDefaultOptions.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid update options: %w", err)
	}

	return &VideoManager{
		targetSymlinkPath: absSymlinkPath,
		cacheDirectory:    cacheDirectory,
		updateFile:        downloader,
		options:           withDefaultOptions,
		mu:                sync.RWMutex{},
	}, nil
}

func (vm *VideoManager) getLastUpdateTime() time.Time {
	realPath, err := filepath.EvalSymlinks(vm.targetSymlinkPath)
	if err == nil {
		info, err := os.Stat(realPath)
		if err == nil {
			return info.ModTime()
		}
	}
	return time.Time{}
}

// update performs the robust update sequence.
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

	vm.mu.Lock()
	if vm.isScreenLocked {
		logging.Info("screen is currently locked. Caching video for later application", "finalFilename", finalFilename)
		vm.pendingVideo = finalFilename
		vm.mu.Unlock()
		return
	}
	vm.mu.Unlock()

	// Screen is unlocked, apply immediately
	vm.applySymlink(finalFilename)
}

// applySymlink safely updates the symlink and cleans up old files.
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
	vm.cleanupOldVideos(videoPath)
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

func (vm *VideoManager) setLockState(isLocked bool) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.isScreenLocked = isLocked
}

func (vm *VideoManager) handleScreenLocked() {
	vm.mu.Lock()
	var videoToApply string
	if vm.pendingVideo != "" {
		videoToApply = vm.pendingVideo
		vm.pendingVideo = "" // Clear the cache
		logging.Info("Screen unlocked. Found pending cached video to apply", "pendingVideo", videoToApply)
	}
	vm.mu.Unlock()

	if videoToApply != "" {
		vm.applySymlink(videoToApply)
	}
}

// handleScreenUnlocked evaluates if a download should occur upon unlocking.
func (vm *VideoManager) handleScreenUnlocked() {
	now := time.Now()
	last := vm.getLastUpdateTime()

	if last.IsZero() || now.Sub(last) >= vm.options.MinDownloadInterval {
		logging.Info("screen unlocked and minimum interval exceeded. Initiating download", "lastUpdate", last, "now", now)
		go vm.update()
	}
}

// getLockState safely reads the current lock state
func (vm *VideoManager) getLockState() bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.isScreenLocked
}

// initializeLockState fetches the lock status synchronously on startup.
func (vm *VideoManager) initializeLockState(conn *dbus.Conn) {
	obj := conn.Object("org.freedesktop.ScreenSaver", "/ScreenSaver")
	var active bool
	err := obj.Call("org.freedesktop.ScreenSaver.GetActive", 0).Store(&active)
	if err != nil {
		logging.Warn("could not query initial lock state (assuming unlocked)", "error", err)
		vm.setLockState(false)
		return
	}
	vm.setLockState(active)
}

func (vm *VideoManager) onDbusSignal(sig *dbus.Signal) {
	isUnlockEvent := sig.Name == "org.freedesktop.ScreenSaver.ActiveChanged" && len(sig.Body) > 0
	if !isUnlockEvent {
		return
	}

	isLocked, ok := sig.Body[0].(bool)
	if !ok {
		logging.Warn("received ActiveChanged signal with unexpected body", "body", sig.Body)
		return
	}

	vm.setLockState(isLocked)

	if isLocked {
		vm.handleScreenLocked()
	} else {
		vm.handleScreenUnlocked()
	}
}

// startUnlockListener registers the D-Bus signal and spawns a background routine to process events.
func (vm *VideoManager) startUnlockListener(conn *dbus.Conn) error {
	err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.ScreenSaver"),
		dbus.WithMatchMember("ActiveChanged"),
	)
	if err != nil {
		return fmt.Errorf("failed to add match signal: %w", err)
	}

	sigChan := make(chan *dbus.Signal, 10)
	conn.Signal(sigChan)

	// Spawn a background goroutine dedicated strictly to D-Bus events
	go func() {
		logging.Info("Listening for D-Bus ScreenSaver unlock events...")
		for sig := range sigChan {
			vm.onDbusSignal(sig)
		}
	}()

	return nil
}

// checkMaxInterval triggers a download if the maximum time threshold is exceeded.
func (vm *VideoManager) checkMaxInterval() {
	now := time.Now()
	last := vm.getLastUpdateTime()

	if last.IsZero() || now.Sub(last) >= vm.options.MaxDownloadInterval {
		isLocked := vm.getLockState()
		if isLocked {
			return
		}

		logging.Info("maximum interval exceeded. Initiating download", "lastUpdate", last, "now", now)
		go vm.update()
	}
}

func (vm *VideoManager) Run(ctx context.Context, conn *dbus.Conn) error {
	if conn != nil {
		vm.initializeLockState(conn)

		err := vm.startUnlockListener(conn)
		if err != nil {
			return fmt.Errorf("failed to start unlock listener: %w", err)
		}
	}

	ticker := time.NewTicker(vm.options.CheckTickRate)
	defer ticker.Stop()

	// Trigger initial max interval check immediately on startup
	vm.checkMaxInterval()

	for {
		select {
		case <-ticker.C:
			vm.checkMaxInterval()
		case <-ctx.Done():
			return nil
		}
	}
}

// --- Pluggable Downloaders ---

func NativeHTTPDownloader(destPath string) error {
	client := &http.Client{}
	resp, err := client.Get("https://static.taptwice.dev/cyno.png")
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bad HTTP status code: %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("could not create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func ExternalCurlDownloader(url string, destPath string) error {
	cmd := exec.Command("curl", "-sL", "-o", destPath, url)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("curl failed to download file: %w", err)
	}
	return nil
}

// --- Main execution ---

func main() {
	os.MkdirAll("./cache", os.ModePerm)
	vm, err := NewVideoManager("./target.jpg", "./cache", NativeHTTPDownloader, UpdateOptions{
		MinDownloadInterval: 2 * time.Hour,
		MaxDownloadInterval: 24 * time.Hour,
		CheckTickRate:       60 * time.Second,
		DownloadTimeout:     30 * time.Second,
	})
	if err != nil {
		logging.Error("Initialization failed", "error", err)
		os.Exit(1)
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		logging.Warn("Failed to connect to D-Bus. Fallback to timer-only mode.", "error", err)
	} else {
		defer conn.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = vm.Run(ctx, conn)
	if err != nil {
		logging.Error("VideoManager encountered an error", "error", err)
		os.Exit(1)
	}

	//urlPtr := flag.String("url", "", "URL of the MP4 video to download (required)")
	//symlinkPtr := flag.String("symlink", "", "Path where the symlink should be created (required)")
	//useCurlPtr := flag.Bool("use-curl", false, "Use curl instead of native Go HTTP client")
	//flag.Parse()
	//
	//if *urlPtr == "" || *symlinkPtr == "" {
	//	flag.Usage()
	//	os.Exit(1)
	//}
	//
	//var selectedDownloader Updater
	//if *useCurlPtr {
	//	selectedDownloader = ExternalCurlDownloader
	//	log.Println("Using external curl downloader.")
	//} else {
	//	selectedDownloader = NativeHTTPDownloader
	//	log.Println("Using native Go HTTP downloader.")
	//}
	//
	//vm, err := NewVideoManager(*urlPtr, *symlinkPtr, selectedDownloader)
	//if err != nil {
	//	log.Fatalf("Initialization failed: %v", err)
	//}
	//
	//// Connect to D-Bus and hand off responsibility to the VideoManager
	//conn, err := dbus.ConnectSessionBus()
	//if err != nil {
	//	log.Printf("Failed to connect to D-Bus: %v. Fallback to timer-only mode.\n", err)
	//} else {
	//	defer conn.Close()
	//	vm.initializeLockState(conn)
	//	if err := vm.startUnlockListener(conn); err != nil {
	//		log.Printf("Failed to start D-Bus unlock listener: %v", err)
	//	}
	//}
	//
	//log.Printf("Starting daemon. Monitoring: %s\n", *urlPtr)
	//
	//// Trigger initial max interval check
	//vm.checkMaxInterval()
	//
	//// The Main Event Loop is now simply a timer.
	//// D-Bus events are handled autonomously by the struct's background goroutine.
	//ticker := time.NewTicker(CheckTickRate)
	//defer ticker.Stop()
	//
	//// range over ticker.C blocks and iterates every time the ticker ticks
	//for range ticker.C {
	//	vm.checkMaxInterval()
	//}
}
