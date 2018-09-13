NVRemoted is an implementation of the [NVDA Remote][] server in Go.

To use:

* `go install github.com/n0ot/nvremoted/cmd/nvremoted`
* Create a directory ".nvremoted" in your home directory, and copy example.conf to $HOME/.nvremoted/conf.
* Open $HOME/.nvremoted/conf, and follow the instructions in the file.
* As NVDA Remote only uses TLS, you need to point NVRemoted at a certificate and private key.
    Self signed certificates can be generated with openssl,
    and signed certificates can be gotten from [Let's Encrypt][].
* Run `nvremoted`

[NVDA Remote]: https://www.nvdaremote.com
[Let's Encrypt]: https://letsencrypt.org