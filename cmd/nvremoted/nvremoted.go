package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strings"
	"time"

	"github.com/n0ot/nvremoted"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var Name = "NVRemoted"
var Version = "unset"

func init() {
	// Try to get the user's home directory
	usr, err := user.Current()
	if err != nil {
		log.Fatalf("Cannot get a user to fetch the home directory\n")
	}
	if usr.HomeDir == "" {
		log.Fatalf("Cannot get home directory. Reading the wrong configuration file could yield undefined results:\n")
	}

	configFile := flag.String("config", path.Join(usr.HomeDir, ".nvremoted", "conf"), "Location of configuration file")
	var getVersion bool
	flag.BoolVar(&getVersion, "version", false, "Print version and exit")
	flag.Parse()

	if getVersion {
		fmt.Printf("%s version %s\n", Name, Version)
		os.Exit(0)
	}

	viper.SetConfigFile(*configFile)
	viper.SetConfigType("toml")
	viper.SetDefault("tls.useTls", true)
	viper.SetDefault("timeBetweenPings", 10)
	viper.SetDefault("pingsUntilTimeout", 2)
	viper.SetDefault("warnIfNotEncrypted", true)
	err = viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Cannot read configuration: %s\n", err)
	}
}

func main() {
	motdFile := os.ExpandEnv(viper.GetString("motdFile"))
	motd, err := ioutil.ReadFile(motdFile)
	if err != nil {
		log.Fatalf("Error reading motd from file. If you don't want a message of the day, create an empty file: %s\n", err)
	}

	config := &nvremoted.ServerConfig{
		BindAddr:           viper.GetString("bindAddr"),
		TimeBetweenPings:   viper.GetDuration("timeBetweenPings") * time.Second,
		PingsUntilTimeout:  viper.GetInt("pingsUntilTimeout"),
		ServerName:         viper.GetString("serverName"),
		Motd:               strings.TrimSpace(string(motd)),
		WarnIfNotEncrypted: viper.GetBool("warnIfNotEncrypted"),
		UseTLS:             viper.GetBool("tls.useTls"),
		CertFile:           os.ExpandEnv(viper.GetString("tls.certFile")),
		KeyFile:            os.ExpandEnv(viper.GetString("tls.keyFile")),
	}

	log.Printf("Starting %s", Name)
	server := nvremoted.NewServer(config)
	server.Start()
}
