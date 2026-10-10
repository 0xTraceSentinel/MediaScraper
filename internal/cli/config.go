package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/0xTraceSentinel/MediaScraper/internal/config"
)

func newConfigCommand(g *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate configuration",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "validate",
			Short: "Validate the config file given with --config",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, err := g.loadConfig(cmd)
				if err != nil {
					return err
				}
				if err := cfg.Validate(); err != nil {
					return fmt.Errorf("invalid configuration:\n%w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "configuration is valid (%d site profiles)\n", len(cfg.Profiles))
				return nil
			},
		},
		&cobra.Command{
			Use:   "defaults",
			Short: "Print the default configuration as YAML",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				enc := yaml.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent(2)
				cfg := config.Default()
				if err := enc.Encode(&cfg); err != nil {
					return err
				}
				return enc.Close()
			},
		},
	)
	return cmd
}
