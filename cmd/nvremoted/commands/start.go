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
	srv.ConnectedFunc = handleConnected
	srv.DisconnectedFunc = handleDisconnected
	srv.RegisterMessage("join", func() server.Message { return &joinMessage{} })
	srv.RegisterMessage("protocol_version", func() server.Message { return &protocolVersionMessage{} })
	srv.RegisterMessage("stat", func() server.Message { return &statMessage{} })
	srv.DefaultMessageFunc = handleDefault

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

func handleConnected(c *server.Client) {
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

func handleDisconnected(c *server.Client) {
	nvrd.Kick(c.ID, c.StoppedReason)
}

// joinMessage is sent when a client wants to join a channel.
type joinMessage struct {
	model.DefaultMessage
	Channel        string `json:"channel"`
	ConnectionType string `json:"connection_type"`
}

func (msg joinMessage) Handle(c *server.Client) {
	if msg.Channel == "" {
		c.Send <- model.NewErrorMessage("No channel name")
		return
	}
	if msg.ConnectionType == "" {
		c.Send <- model.NewErrorMessage("No connection type")
		return
	}

	if err := nvrd.JoinChannel(msg.Channel, msg.ConnectionType, c.ID); err != nil {
		c.Send <- model.NewErrorMessage(err.Error())
	}
}

// handleDefault handles unknown messages.
func handleDefault(c *server.Client, msg server.DefaultMessage) {
	if err := nvrd.Send(c.ID, nvremoted.ChannelMessage(msg)); err != nil {
		c.Send <- model.NewErrorMessage(err.Error())
	}
}

// protocolVersionMessage does nothing for now; prevents clients who send it from getting an error.
type protocolVersionMessage struct {
	model.DefaultMessage
	Version int `json:"version"`
}

func (msg protocolVersionMessage) Handle(c *server.Client) {
}

// A statMessage is used to get stats from NVRemoted.
type statMessage struct {
	model.DefaultMessage
	Password string `json:"password"`
}

func (msg statMessage) Handle(c *server.Client) {
	if msg.Password == "" {
		c.Send <- model.NewErrorMessage("No password provided")
		nvrd.Kick(c.ID, "Bad request")
		return
	}

	statsPassword := viper.GetString("server.statsPassword")
	if msg.Password != statsPassword {
		c.Send <- model.NewErrorMessage("Invalid password")
		nvrd.Kick(c.ID, "Bad request")
		return
	}

	stats := nvrd.Stats()
	c.Send <- statsMessage{
		DefaultMessage: model.DefaultMessage{Type: "stats"},
		Stats:          stats,
	}
	nvrd.Kick(c.ID, "Request completed")
}

// A StatsMessage is sent to clients that requested stats.
type statsMessage struct {
	model.DefaultMessage
	Stats nvremoted.Stats `json:"stats"`
}
