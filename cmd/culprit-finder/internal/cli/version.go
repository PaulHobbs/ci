package cli

import (
	"fmt"

	"github.com/example/turboci-lite/cmd/culprit-finder/internal/ui"
	"github.com/spf13/cobra"
)

const (
	version = "1.0.0"
	banner  = `
   _____      _            _ _     _____ _           _
  / ____|    | |          (_) |   |  __ (_)         | |
 | |    _   _| |_ __  _ __ _| |_  | |__) |_ __   __| | ___ _ __
 | |   | | | | | '_ \| '__| | __| |  ___/| '_ \ / _' |/ _ \ '__|
 | |___| |_| | | |_) | |  | | |_  | |    | | | | (_| |  __/ |
  \_____\__,_|_| .__/|_|  |_|\__| |_|    |_| |_|\__,_|\___|_|
               | |
               |_|
`
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the version of culprit-finder.`,
	Run:   runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion(cmd *cobra.Command, args []string) {
	fmt.Println(banner)
	ui.PrintInfo(fmt.Sprintf("Version: %s", version))
	ui.PrintInfo("Flake-aware non-adaptive group testing for finding culprit commits")
	ui.PrintInfo("")
	ui.PrintInfo("For help: culprit-finder --help")
	ui.PrintInfo("Documentation: https://github.com/example/turboci-lite")
}
