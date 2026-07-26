package cli

import (
	"runtime/debug"

	"github.com/datapointchris/goselfupdate"
	"github.com/datapointchris/goselfupdate/cobracmd"
	"github.com/spf13/cobra"
)

// version is set at build time via ldflags. Falls back to Go module info.
var version = ""

func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func updateCmd() *cobra.Command {
	return cobracmd.New(goselfupdate.Config{
		Owner:   "datapointchris",
		Repo:    "todoui",
		Binary:  "todoui",
		Version: Version(),
	})
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the current version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(Version())
		},
	}
}
