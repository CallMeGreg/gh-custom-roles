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
	rootCmd.PersistentFlags().StringVarP(&opts.hostname, "hostname", "u", "", "GitHub hostname (default: github.com)")
	rootCmd.PersistentFlags().StringVarP(&opts.enterprise, "enterprise", "e", "", "GitHub enterprise slug (default: github)")
	rootCmd.PersistentFlags().StringVarP(&opts.org, "org", "o", "", "Target a single organization")
	rootCmd.PersistentFlags().BoolVarP(&opts.allOrgs, "all-orgs", "a", false, "Target all organizations in the enterprise")
	rootCmd.PersistentFlags().StringVarP(&opts.orgsCSVPath, "orgs-csv", "c", "", "CSV file path with organizations to target")
	rootCmd.MarkFlagsMutuallyExclusive("org", "all-orgs", "orgs-csv")

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
