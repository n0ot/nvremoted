// Copyright © 2018 Niko Carpenter <nikoacarpenter@gmail.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package commands

import (
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/n0ot/nvremoted/pkg/server"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	log        *logrus.Logger
	motd       string
	disableTLS bool
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the NVRemoted server",
	Run:   runServer,
}

func init() {
	RootCmd.AddCommand(startCmd)

	startCmd.Flags().StringP("bind", "b", "127.0.0.1:6837", "Bind the server to host:port. Leave host empty to bind to all interfaces.")
	viper.BindPFlag("server.bind", startCmd.Flags().Lookup("bind"))
	startCmd.Flags().IntP("time-between-pings", "t", 30, "How often pings should be sent in seconds (0 disables)")
	viper.BindPFlag("server.timeBetweenPings", startCmd.Flags().Lookup("time-between-pings"))
	startCmd.Flags().IntP("pings-until-timeout", "p", 2, "Number of pings that can pass before inactive clients are dropped (0 disables timeout)")
	viper.BindPFlag("server.pingsUntilTimeout", startCmd.Flags().Lookup("pings-until-timeout"))
	startCmd.Flags().BoolVarP(&disableTLS, "disable-tls", "d", false, "Overrides config option to enable TLS")

	viper.SetDefault("server.statsPassword", "")
	viper.SetDefault("tls.useTls", true)
}

func runServer(cmd *cobra.Command, args []string) {
	log = logrus.New()
	log.Out = os.Stderr
	log.Formatter = new(logrus.TextFormatter)
	log.Level = logrus.DebugLevel

	motdFile := os.ExpandEnv(viper.GetString("nvremoted.motdFile"))
	if motdBuf, err := ioutil.ReadFile(motdFile); err == nil {
		motd = string(motdBuf)
	}

	srv := &server.Server{
		TimeBetweenPings:  viper.GetDuration("server.timeBetweenPings") * time.Second,
		PingsUntilTimeout: viper.GetInt("server.pingsUntilTimeout"),
		MOTD:              strings.TrimSpace(motd),
		StatsPassword:     viper.GetString("server.statsPassword"),
		Log:               log,
	}

	bindAddr := viper.GetString("server.bind")
	certFile := os.ExpandEnv(viper.GetString("tls.certFile"))
	keyFile := os.ExpandEnv(viper.GetString("tls.keyFile"))
	useTLS := viper.GetBool("tls.useTls")

	log.Info("Starting NVRemoted")
	if useTLS && !disableTLS {
		log.Fatal(srv.ListenAndServeTLS(bindAddr, certFile, keyFile))
	} else {
		log.Fatal(srv.ListenAndServe(bindAddr))
	}
}
