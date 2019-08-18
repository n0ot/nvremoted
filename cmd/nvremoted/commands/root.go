// Copyright © 2019 Niko Carpenter <nikoacarpenter@gmail.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"path"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgDir string

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "nvremoted",
	Short: "NVDA Remote server",
	Long: `NVRemoted is a server for NVDA Remote.

This application relays messages for NVDA Remote clients,
and prints usage stats for other NVRemoted servers.`,
	SilenceErrors:     true,
	SilenceUsage:      true,
	DisableAutoGenTag: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	RootCmd.PersistentFlags().StringVar(&cfgDir, "config", "", "config directory (default is $HOME/.config/nvremoted)")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgDir == "" {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search for config in $HOME/.config/nvremoted
		cfgDir = path.Join(home, ".config", "nvremoted")
	}

	viper.AddConfigPath(cfgDir)
	viper.SetConfigName("nvremoted")

	os.Setenv("CONFDIR", cfgDir)

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config file: %s\n", err)
		os.Exit(1)
	}
}
