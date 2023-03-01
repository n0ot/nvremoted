// Copyright © 2023 Niko Carpenter <niko@nikocarpenter.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package commands

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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
	statsHost              string
	statsPort              string
	skipTLSVerification    bool
	statsServerCertificate string
	statsPassword          string
	promptForPassword      bool
)

// statsCmd represents the stats command
var statsCmd = &cobra.Command{
	Use:   "stats [host]",
	Short: "Print stats from an NVRemoted server",
	Long: `stats queries an NVRemoted server for running stats.

If the host is omitted, the local nvremoted server will be queried.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host := "127.0.0.1"
		if len(args) > 0 {
			host = args[0]
			if disableTLS {
				fmt.Fprintln(os.Stderr, "Warning: TLS is disabled. All traffic including your stats password will be sent in the clear.")
			} else if skipTLSVerification {
				fmt.Fprintln(os.Stderr, "Warning: skipping TLS verification is insecure.")
			}
		} else {
			// Use the options from the local server's configuration.
			if _, port, err := net.SplitHostPort(viper.GetString("server.bind")); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: cannot determine local server port from config; using \"%s\"\n", statsPort)
			} else {
				statsPort = port
			}
			disableTLS = !viper.GetBool("tls.useTls")
			skipTLSVerification = true
			statsPassword = viper.GetString("server.statsPassword")
			if !disableTLS {
				fmt.Fprintln(os.Stderr, "Skipping TLS verification for local server query")
			}
		}
		return getStats(host)
	},
}

func init() {
	RootCmd.AddCommand(statsCmd)
	statsCmd.Flags().StringVarP(&statsPort, "port", "P", "6837", "port of the server to query stats for")
	statsCmd.Flags().BoolVarP(&disableTLS, "disable-tls", "d", false, "disable connecting over TLS")
	statsCmd.Flags().BoolVarP(&skipTLSVerification, "no-tls-verify", "n", false, "skip TLS verification\n    This is insecure, an attacker can get your password, and you should only use this for testing")
	statsCmd.Flags().StringVarP(&statsServerCertificate, "server-certificate", "s", "", "file containing the PEM encoded certificate to use for server verification, instead of the system's certificate store")
	statsCmd.Flags().BoolVarP(&promptForPassword, "prompt-for-password", "p", false, "prompt for the server's stats password\n    If unset, the password is the same as the local server's.")

	viper.SetDefault("server.statsPassword", "")
}

func getStats(statsHost string) error {
	if promptForPassword {
		fmt.Printf("Password: ")
		pass, err := gopass.GetPasswd()
		if err != nil {
			return err
		}
		statsPassword = string(pass)
	}

	if statsPassword == "" {
		statsPassword = os.Getenv("NVREMOTED_STATS_PASSWORD")
	}

	if statsPassword == "" {
		return errors.New("A stats password is required")
	}

	var conn net.Conn
	var err error
	statsAddr := net.JoinHostPort(statsHost, statsPort)
	if disableTLS {
		conn, err = net.Dial("tcp", statsAddr)
	} else {
		var certPool *x509.CertPool
		if statsServerCertificate != "" {
			cert, err := ioutil.ReadFile(statsServerCertificate)
			if err != nil {
				return errors.Wrap(err, "Open server certificate")
			}
			certPool = x509.NewCertPool()
			certPool.AppendCertsFromPEM(cert)
		}

		conn, err = tls.Dial("tcp", statsAddr, &tls.Config{
			InsecureSkipVerify: skipTLSVerification,
			RootCAs:            certPool,
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
		Password: statsPassword,
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
			if err == io.EOF {
				return errors.New("Connection closed by remote host")
			}
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
			fmt.Printf("MOTD: %s\n\n", msg.MOTD)

		case *server.ClientErrorResponse:
			return errors.Errorf("Server returned an error: %s", msg.Error)

		case *server.ClientStatsResponse:
			// Don't display the default port in the output.
			friendlyAddr := statsHost
			if statsPort != "6837" {
				friendlyAddr = statsAddr
			}
			fmt.Printf(`Stats for %s:
Uptime: %s
Number of channels: %d (%d serving clients using end-to-end encryption),
Max channels: %d on %s

Number of clients: %d
Max clients: %d on %s
`, friendlyAddr, msg.Stats.Uptime,
				msg.Stats.NumChannels, msg.Stats.NumE2eChannels,
				msg.Stats.MaxChannels, msg.Stats.MaxChannelsTime,
				msg.Stats.NumClients,
				msg.Stats.MaxClients, msg.Stats.MaxClientsTime)
			return nil
		}
	}
}
