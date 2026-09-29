package cmd

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/bulletsrip/img2ssh/internal/config"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail the daemon log",
	RunE: func(cmd *cobra.Command, args []string) error {
		logPath, err := config.LogPath()
		if err != nil {
			return err
		}
		return followLog(cmd.Context(), logPath, os.Stdout)
	},
}

func followLog(ctx context.Context, path string, output io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := io.Copy(output, f); err != nil && err != io.EOF {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, err := f.Stat()
			if err != nil {
				return err
			}
			offset, err := f.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}
			if info.Size() < offset {
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return err
				}
			}
		}
	}
}
