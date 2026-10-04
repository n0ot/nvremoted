NVRemoted is an implementation of the [NVDA Remote][] server in Go.

`server.timeBetweenPings` controls server ping delivery only (seconds; 0 disables
pings). NVDA Remote clients do not acknowledge these pings, so valid idle
connections are allowed. `server.pingsUntilTimeout` and `--pings-until-timeout`
are deprecated and ignored, including existing nonzero values. They no longer
impose an inactivity timeout.

TCP keepalive is enabled for both TCP and TLS connections independently of
these settings, with a 15-second idle period. Probe intervals, retry counts,
and the time to detect a lost peer depend on the listener and operating system;
this is not a 15-second disconnect timeout. Transport errors and disconnects
stop the client and remove its channel membership. Keepalive cannot detect a
stalled client application while its operating system still responds to TCP.

To use:

* `go install github.com/n0ot/nvremoted/cmd/nvremoted`
* Create a directory, $HOME/.config/nvremoted, and copy examples/nvremoted.toml there.
* Open $HOME/.config/nvremoted/nvremoted.toml, and follow the instructions in the file.
* As NVDA Remote only uses TLS, you need to point NVRemoted at a certificate and private key.
    Self signed certificates can be generated with openssl,
    and signed certificates can be gotten from [Let's Encrypt][].
* Run `nvremoted start`

#### Building
Install [Mage][], then use

    mage build

to build, or

    mage install

to install.

`GOOS` and `GOARCH` work as you'd expect.

[NVDA Remote]: https://www.nvdaremote.com
[Let's Encrypt]: https://letsencrypt.org
[Mage]: https://github.com/magefile/mage