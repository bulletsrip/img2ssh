package cmd

import (
	"fmt"
	"os"

	"github.com/bulletsrip/img2ssh/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run the clipboard watch loop (managed by the OS at login)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := daemon.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return nil
	},
}
