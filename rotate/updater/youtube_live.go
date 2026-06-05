package updater

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/matthiasharzer/lockscreen-rotate/logging"
	"github.com/matthiasharzer/lockscreen-rotate/util/fsutil"
)

func YouTubeLive(liveURL string, offsetStartFromLive, offsetEndFromLive time.Duration) (Updater, error) {
	if offsetStartFromLive > 0 {
		return nil, errors.New("offsetEndFromLive must be less than 0")
	}
	if offsetEndFromLive > 0 {
		return nil, errors.New("offsetEndFromLive must be less than or equal to 0")
	}
	if offsetStartFromLive >= offsetEndFromLive {
		return nil, errors.New("offsetStartFromLive must be less than offsetEndFromLive")
	}

	offsetStartFromLiveSecondsPositive := -int(offsetStartFromLive.Seconds())
	offsetEndFromLiveSecondsPositive := -int(offsetEndFromLive.Seconds())

	return func(destinationFilePath string) error {
		startStr := fmt.Sprintf("now  - PT%dS", offsetStartFromLiveSecondsPositive)
		endStr := "now"
		if offsetEndFromLiveSecondsPositive > 0 {
			endStr = fmt.Sprintf("now - PT%dS", offsetEndFromLiveSecondsPositive)
		}
		interval := fmt.Sprintf("%s/%s", startStr, endStr)

		duration := offsetStartFromLiveSecondsPositive - offsetEndFromLiveSecondsPositive
		logging.Info("preparing to download a segment", "liveURL", liveURL, "duration", duration, "start", startStr, "end", endStr, "interval", interval)

		tmpDir, err := os.MkdirTemp("", "ytpb-*")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		ext := filepath.Ext(destinationFilePath)
		tmpFileOutput := filepath.Join(tmpDir, "temp_segment"+ext)
		tmpFile := fmt.Sprintf("%s.mkv", tmpFileOutput) // ytpb adds .mkv extension by default

		cmd := exec.Command("ytpb", "download", liveURL, "--interval", interval, "--output", tmpFileOutput)

		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ytpb failed: %w\nCLI Output:\n%s", err, string(output))
		}

		if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
			return errors.New("ytpb completed, but the output video file is missing")
		}

		finalDir := filepath.Dir(destinationFilePath)
		err = os.MkdirAll(finalDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}

		err = fsutil.MoveFile(tmpFile, destinationFilePath)
		if err != nil {
			return fmt.Errorf("failed to move file to final destination: %w", err)
		}

		logging.Info("successfully downloaded segment", "source", liveURL, "destination", destinationFilePath, "duration", duration)
		return nil
	}, nil

}
