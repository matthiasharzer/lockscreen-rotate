package youtube

import (
	"github.com/matthiasharzer/lockscreen-rotate/cmd/youtube/run"
	"github.com/spf13/cobra"
)

func init() {
	Command.AddCommand(run.Command)
}

var Command = &cobra.Command{
	Use:   "youtube",
	Short: "Rotate lock screen from a YouTube livestream",
}
