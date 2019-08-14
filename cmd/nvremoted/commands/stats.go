// Copyright © 2018 Niko Carpenter <nikoacarpenter@gmail.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package commands

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/n0ot/nvremoted/pkg/server"
	"github.com/pkg/errors"

	"github.com/howeyc/gopass"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	statsAddr           string
	skipTLSVerification bool
	promptForPassword   bool
)

// statsCmd represents the stats command
var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Print stats from an NVRemoted server",
	Long: `stats connects to an NVRemoted server and prints stats about that server.

In order to connect, the stats password must be provided.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := getStats(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	RootCmd.AddCommand(statsCmd)
	statsCmd.Flags().StringVarP(&statsAddr, "address", "a", "127.0.0.1:6837", "Address of the server to query stats for.")
	statsCmd.Flags().BoolVarP(&disableTLS, "disable-tls", "d", false, "Disables connecting over TLS")
	statsCmd.Flags().BoolVarP(&skipTLSVerification, "no-tls-verify", "n", false, "Skips TLS verification. This is insecure, an attacker can get your password, and you should only use this for testing")
	statsCmd.Flags().BoolVarP(&promptForPassword, "prompt-for-password", "p", false, "Prompt for the server's stats password. If unset, the password is the same as the local server's.")

	viper.SetDefault("server.statsPassword", "")
}

func getStats() error {
	password := viper.GetString("server.statsPassword")
	if promptForPassword {
		fmt.Printf("Password: ")
		pass, err := gopass.GetPasswd()
		if err != nil {
			return err
		}
		password = string(pass)
	}
	if password == "" {
		return errors.New("No stats password specified")
	}

	var conn net.Conn
	var err error
	if disableTLS {
		conn, err = net.Dial("tcp", statsAddr)
	} else {
		conn, err = tls.Dial("tcp", statsAddr, &tls.Config{
			InsecureSkipVerify: skipTLSVerification,
		})
	}
	if err != nil {
		return errors.Wrap(err, "Connect to NVRemoted server")
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	var raw json.RawMessage

	err = enc.Encode(server.ClientStatMessage{
		GenericClientMessage: server.GenericClientMessage{
			Type: "stat",
		},
		Password: password,
	})
	if err != nil {
		return errors.Wrap(err, "Request stats")
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	messages := map[string]func() server.Message{
		"motd":  func() server.Message { return &server.ClientMOTDResponse{} },
		"error": func() server.Message { return &server.ClientErrorResponse{} },
		"stats": func() server.Message { return &server.ClientStatsResponse{} },
	}

	for {
		if err := dec.Decode(&raw); err != nil {
			return errors.Wrap(err, "Get stats response from server")
		}
		var unknownMSG server.GenericClientResponse
		if err := json.Unmarshal(raw, &unknownMSG); err != nil {
			return errors.Wrap(err, "Get stats response from server")
		}
		if messages[unknownMSG.Type] == nil {
			// Ignore all unknown messages
			continue
		}

		msg := messages[unknownMSG.Type]()
		if err := json.Unmarshal(raw, &msg); err != nil {
			return errors.Wrap(err, "Get stats response from server")
		}

		switch msg := msg.(type) {
		case *server.ClientMOTDResponse:
			fmt.Printf("MOTD: %s\n", msg.MOTD)

		case *server.ClientErrorResponse:
			return errors.Errorf("Server returned an error: %s", msg.Error)

		case *server.ClientStatsResponse:
			fmt.Printf(`Stats for NVRemoted server at %s:
Uptime: %s
Number of channels: %d (%d serving clients using end-to-end encryption),
Max channels: %d on %s

Number of clients: %d
Max clients: %d on %s
`, statsAddr, msg.Stats.Uptime,
				msg.Stats.NumChannels, msg.Stats.NumE2eChannels,
				msg.Stats.MaxChannels, msg.Stats.MaxChannelsTime,
				msg.Stats.NumClients,
				msg.Stats.MaxClients, msg.Stats.MaxClientsTime)
			return nil
		}
	}
}
