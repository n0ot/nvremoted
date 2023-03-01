// Copyright © 2023 Niko Carpenter <niko@nikocarpenter.com>
//
// This source code is governed by the MIT license, which can be found in the LICENSE file.

package server

import (
	"sync"
	"time"
)

type registry struct {
	lock            sync.RWMutex // Protects the entire registry
	clients         map[uint64]channelMember
	channels        map[string]*channel
	statsPassword   string
	createdTime     time.Time
	numE2eChannels  int
	maxChannels     int
	maxChannelsTime time.Time
	maxClients      int
	maxClientsTime  time.Time
}

// Stats contains summary information about a registry.
type Stats struct {
	Uptime          time.Duration `json:"uptime"`
	NumChannels     int           `json:"num_channels"`
	NumE2eChannels  int           `json:"num_e2e_channels"`
	MaxChannels     int           `json:"max_channels"`
	MaxChannelsTime time.Time     `json:"max_channels_at"`
	NumClients      int           `json:"num_clients"`
	MaxClients      int           `json:"max_clients"`
	MaxClientsTime  time.Time     `json:"max_clients_at"`
}

// Stats gets stats for this registry.
func (reg *registry) Stats() Stats {
	reg.lock.RLock()
	defer reg.lock.RUnlock()

	return Stats{
		Uptime:          time.Since(reg.createdTime),
		NumChannels:     len(reg.channels),
		NumE2eChannels:  reg.numE2eChannels,
		MaxChannels:     reg.maxChannels,
		MaxChannelsTime: reg.maxChannelsTime,
		NumClients:      len(reg.clients),
		MaxClients:      reg.maxClients,
		MaxClientsTime:  reg.maxClientsTime,
	}
}
