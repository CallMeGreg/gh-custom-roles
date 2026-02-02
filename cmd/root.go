package cmd

import (
	"os"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "custom-roles",
	Short: "Create custom repository roles in GitHub organizations",
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
}

func init() {
	// Root command flags (persistent for all subcommands)
	rootCmd.PersistentFlags().StringVarP(&opts.hostname, "hostname", "u", "", "GitHub hostname")
	rootCmd.PersistentFlags().StringVarP(&opts.enterprise, "enterprise", "e", "", "GitHub enterprise slug")
	rootCmd.PersistentFlags().StringVarP(&opts.org, "org", "o", "", "Target a single organization")
	rootCmd.PersistentFlags().BoolVarP(&opts.allOrgs, "all-orgs", "a", false, "Target all organizations in the enterprise")
	rootCmd.PersistentFlags().StringVarP(&opts.orgsCSVPath, "orgs-csv", "c", "", "CSV file path with organizations to target")
	rootCmd.PersistentFlags().IntVarP(&opts.concurrency, "concurrency", "x", 1, "Number of parallel requests (1-20, mutually exclusive with --delay)")
	rootCmd.PersistentFlags().IntVarP(&opts.delay, "delay", "w", 0, "Seconds to wait between role creations (mutually exclusive with --concurrency)")
	rootCmd.MarkFlagsMutuallyExclusive("org", "all-orgs", "orgs-csv")
	rootCmd.MarkFlagsMutuallyExclusive("delay", "concurrency")

	// Register create command
	rootCmd.AddCommand(createCmd)
}

// Execute initializes and runs the command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		pterm.Error.Printfln("Error: %v", err)
		os.Exit(1)
	}
}
