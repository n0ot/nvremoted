// Copyright © 2019 Niko Carpenter <nikoacarpenter@gmail.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package server

// A Message encapsulates events passed through the server.
type Message interface {
	Name() string
}
