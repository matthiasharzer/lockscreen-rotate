package main

import (
	"fmt"
	"os"

	"github.com/matthiasharzer/lockscreen-rotate/cmd/version"

	"github.com/spf13/cobra"
)

var rootCommand = &cobra.Command{
	Use: "lockscreen-rotate",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCommand.AddCommand(version.Command)
}

func main() {
	err := rootCommand.Execute()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
