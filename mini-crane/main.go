package main

import (
	"mini-crane/cmd/export"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	streams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}
	rootCmd := &cobra.Command{
		Use:   "mini-crane",
		Short: "Small crane style learning CLI",
	}

	rootCmd.AddCommand(export.NewExportCommand(streams))
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
