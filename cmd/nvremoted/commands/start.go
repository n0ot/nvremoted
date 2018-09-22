// Copyright © 2018 Niko Carpenter <nikoacarpenter@gmail.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package commands

import (
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/n0ot/nvremoted/pkg/model"
	"github.com/n0ot/nvremoted/pkg/nvremoted"
	"github.com/n0ot/nvremoted/pkg/server"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	log        *logrus.Logger
	nvrd       *nvremoted.NVRD
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
	viper.SetDefault("nvremoted.warnIfNotEncrypted", true)
	viper.SetDefault("tls.useTls", true)
}

func runServer(cmd *cobra.Command, args []string) {
	log = logrus.New()
	log.Out = os.Stderr
	log.Formatter = new(logrus.TextFormatter)
	log.Level = logrus.DebugLevel
	log.WithFields(logrus.Fields(viper.AllSettings())).Debug("Configuration")

	motdFile := os.ExpandEnv(viper.GetString("nvremoted.motdFile"))
	if motdBuf, err := ioutil.ReadFile(motdFile); err == nil {
		motd = string(motdBuf)
	}

	nvrd = nvremoted.New(log, strings.TrimSpace(motd), viper.GetBool("nvremoted.warnIfNotEncrypted"))

	srv := server.New(log)
	srv.TimeBetweenPings = viper.GetDuration("server.timeBetweenPings") * time.Second
	srv.PingsUntilTimeout = viper.GetInt("server.pingsUntilTimeout")
	srv.ConnectedHandler = server.MessageHandlerFunc(handleConnected)
	srv.DisconnectedHandler = server.MessageHandlerFunc(handleDisconnected)
	srv.HandleMessage("join", server.MessageHandlerFunc(handleJoined))
	srv.HandleMessage("protocol_version", server.MessageHandlerFunc(handleProtocolVersion))
	srv.HandleMessage("stat", server.MessageHandlerFunc(handleStat))
	srv.DefaultHandler = server.MessageHandlerFunc(handleDefault)

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

func handleConnected(c *server.Client, msg model.Message) {
	nc := &nvremoted.Client{
		ID:   c.ID,
		Send: c.Send,
	}
	if err := nvrd.AddClient(nc); err != nil {
		log.WithFields(logrus.Fields{
			"server_client":    c,
			"nvremoted_client": nc,
			"error":            err,
		}).Warn("Error adding client to the NVRemoted service")
	}
}

func handleDisconnected(c *server.Client, msg model.Message) {
	nvrd.Kick(c.ID, c.StoppedReason)
}

func handleJoined(c *server.Client, msg model.Message) {
	name, _ := msg["channel"].(string)
	connectionType, _ := msg["connection_type"].(string)
	if name == "" {
		c.Send <- model.ErrorMessage("No channel name")
		return
	}
	if connectionType == "" {
		c.Send <- model.ErrorMessage("No connection type")
		return
	}

	if err := nvrd.JoinChannel(name, connectionType, c.ID); err != nil {
		c.Send <- model.ErrorMessage(err.Error())
	}
}

func handleDefault(c *server.Client, msg model.Message) {
	if err := nvrd.Send(c.ID, msg); err != nil {
		c.Send <- model.ErrorMessage(err.Error())
	}
}

// handleProtocolVersion does nothing for now; prevents clients who send it from getting an error.
func handleProtocolVersion(c *server.Client, msg model.Message) {
}

func handleStat(c *server.Client, msg model.Message) {
	password, _ := msg["password"].(string)
	if password == "" {
		c.Send <- model.Message{
			"type":  "stats",
			"error": "No password provided",
		}
		nvrd.Kick(c.ID, "Bad request")
		return
	}

	statsPassword := viper.GetString("server.statsPassword")
	if password != statsPassword {
		c.Send <- model.Message{
			"type":  "stats",
			"error": "Invalid password",
		}
		nvrd.Kick(c.ID, "Bad request")
		return
	}

	stats := nvrd.Stats()
	c.Send <- model.Message{
		"type":  "stats",
		"stats": stats,
	}
	nvrd.Kick(c.ID, "Request completed")
}
