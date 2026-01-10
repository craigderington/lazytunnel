package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("tunnelctl version %s\n", version)
		fmt.Println("lazytunnel SSH Tunnel Manager CLI")
	},
}
