NVRemoted is an implementation of the [NVDA Remote][] server in Go.

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