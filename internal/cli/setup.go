package cli

import "github.com/davidalecrim1/amx/internal/setup"

func (a *App) setupCmd(args []string) int {
	dry := false
	for _, arg := range args {
		if arg == "--dry-run" {
			dry = true
		}
	}
	if err := setup.Run(setup.Options{DryRun: dry, Out: a.Stdout}); err != nil {
		return a.fail("%v", err)
	}
	return 0
}
