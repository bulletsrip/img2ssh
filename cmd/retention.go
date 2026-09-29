package cmd

import (
	"fmt"
	"strconv"

	"github.com/bulletsrip/img2ssh/internal/config"
	"github.com/spf13/cobra"
)

var retentionCmd = &cobra.Command{
	Use:   "retention [count]",
	Short: "Show or set the number of remote images to keep",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Printf("Keep last %d images\n", cfg.Settings.KeepLastNFiles)
			return nil
		}
		count, err := strconv.Atoi(args[0])
		if err != nil || count < 1 || count > 10 {
			return fmt.Errorf("retention must be a number from 1 to 10")
		}
		cfg.Settings.KeepLastNFiles = count
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("Now keeping the last %d images\n", count)
		return nil
	},
}
