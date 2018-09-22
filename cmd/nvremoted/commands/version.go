// Copyright © 2018 Niko Carpenter <nikoacarpenter@gmail.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the version of NVRemoted.
var Version = "unset"

// Copyright is the copyright including authors of NVRemoted.
var Copyright = "Copyright © 2018 Niko Carpenter <nikoacarpenter@gmail.com>"

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of NVRemoted",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("NVRemoted version %s\n%s\n", Version, Copyright)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
