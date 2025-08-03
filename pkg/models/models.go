package models

import (
	"net"
	"time"

	api "natpunch/proto/gen/go"
)

// ClientInfo is the truct to store client info
type ClientInfo struct {
	Addr      *net.UDPAddr
	LocalAddr *net.UDPAddr
	Type      api.NatType
	KeepAlive time.Time
}

// LobbyInfo represents a game lobby
type LobbyInfo struct {
	ID           string
	Name         string
	HostClientID string
	MaxPlayers   uint32
	Members      map[string]*ClientInfo
	CreatedAt    time.Time
}
