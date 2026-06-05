package run

import (
	"errors"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/matthiasharzer/lockscreen-rotate/lockscreen"
	"github.com/matthiasharzer/lockscreen-rotate/rotate"
	"github.com/matthiasharzer/lockscreen-rotate/rotate/updater"
	"github.com/matthiasharzer/lockscreen-rotate/trigger"
	"github.com/spf13/cobra"
)

var minUpdateDelay time.Duration
var maxUpdateDelay time.Duration
var symlink string
var offsetStartFromLive time.Duration
var offsetEndFromLive time.Duration
var url string

func init() {
	Command.Flags().DurationVar(&minUpdateDelay, "min-update-delay", 30*time.Minute, "Minimum delay between lock screen updates")
	Command.Flags().DurationVar(&maxUpdateDelay, "max-update-delay", 2*time.Hour, "Maximum delay between lock screen updates")
	Command.Flags().StringVarP(&symlink, "symlink", "s", "", "Path to a symlink that will point to the current lock screen image (optional)")
	Command.Flags().DurationVarP(&offsetStartFromLive, "offset-start-from-live", "", 0, "Offset from the live edge to start fetching thumbnails (e.g., -5m for 5 minutes behind live)")
	Command.Flags().DurationVarP(&offsetEndFromLive, "offset-end-from-live", "", 0, "Offset from the live edge to stop fetching thumbnails (e.g., -1m for 1 minute behind live)")
	Command.Flags().StringVarP(&url, "url", "u", "", "URL of the YouTube livestream to fetch thumbnails from")

	err := Command.MarkFlagRequired("url")
	if err != nil {
		panic(err)
	}
	err = Command.MarkFlagRequired("symlink")
	if err != nil {
		panic(err)
	}
	err = Command.MarkFlagRequired("offset-start-from-live")
	if err != nil {
		panic(err)
	}
}

var Command = &cobra.Command{
	Use:   "run",
	Short: "Rotate lock screen from a YouTube livestream",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if minUpdateDelay <= 0 {
			return errors.New("min-update-delay must be greater than 0")
		}
		if maxUpdateDelay <= 0 {
			return errors.New("max-update-delay must be greater than 0")
		}
		if minUpdateDelay > maxUpdateDelay {
			return errors.New("min-update-delay cannot be greater than max-update-delay")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		u, err := updater.YouTubeLive(url, offsetStartFromLive, offsetEndFromLive)
		if err != nil {
			return err
		}

		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			return err
		}
		state, err := lockscreen.NewState(conn)
		if err != nil {
			return err
		}

		trigger, err := trigger.ScheduledScreenUnlock(state, minUpdateDelay, maxUpdateDelay)
		if err != nil {
			return err
		}

		systemCacheDirectory, err := os.UserCacheDir()
		if err != nil {
			return err
		}

		cacheDirectory := systemCacheDirectory + "/lockscreen-rotate"
		err = os.MkdirAll(cacheDirectory, os.ModePerm)
		if err != nil {
			return err
		}

		manager, err := rotate.NewRotateManager(symlink, cacheDirectory, u, state, trigger)
		if err != nil {
			return err
		}

		err = manager.Run(cmd.Context())

		return nil
	},
}
