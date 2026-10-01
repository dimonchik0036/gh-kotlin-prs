package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
)

func newConfigCommand(e env, global *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the effective config and where each value comes from",
		Args:  noArgs,
		RunE: func(*cobra.Command, []string) error {
			return printConfig(e, *global)
		},
	}
	path := &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  noArgs,
		RunE: func(*cobra.Command, []string) error {
			p, _ := global.configFile(e)
			writef(e.stdout, "%s\n", p)
			return nil
		},
	}
	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write a config file with every key and its default, commented out",
		Args:  noArgs,
		RunE: func(*cobra.Command, []string) error {
			return initConfig(e, *global, force)
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")
	cmd.AddCommand(path, initCmd)
	return cmd
}

// printConfig lists every key as YAML with its source: default, file or flag, per
// sub-key for the maps.
func printConfig(e env, global globalOptions) error {
	path, from := global.configFile(e)
	cfg, sources, err := config.LoadSources(path)
	if err != nil {
		return err
	}
	if global.icons != "" {
		if _, err := global.iconsFor(cfg); err != nil {
			return err
		}
		cfg.Icons, sources["icons"] = global.icons, config.SourceFlag
	}
	if global.hyperlinks != "" {
		if !config.ValidHyperlinks(global.hyperlinks) {
			return usageError{fmt.Errorf("unknown --hyperlinks %q, want auto, always or never", global.hyperlinks)}
		}
		cfg.Hyperlinks, sources["hyperlinks"] = global.hyperlinks, config.SourceFlag
	}
	state := "found"
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		state = "not found, all defaults"
	}
	writef(e.stdout, "# %s (%s, %s)\n", path, from, state)
	_, _ = io.WriteString(e.stdout, config.Describe(cfg.Values(sources)))
	return nil
}

// initConfig writes the template, refusing to replace a file unless forced.
func initConfig(e env, global globalOptions, force bool) error {
	path, _ := global.configFile(e)
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to overwrite it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(config.Template()), 0o644); err != nil {
		return err
	}
	writef(e.stdout, "wrote %s\n", path)
	return nil
}
