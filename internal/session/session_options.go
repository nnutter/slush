package session

import (
	"net"
	"slices"
	"strconv"
	"strings"
)

// Transport selects the terminal client. The zero value detects available
// Mosh and otherwise uses SSH. Both clients use SSH for forwarding.
type Transport int

const (
	Auto Transport = iota
	SSH
	Mosh
)

// Forward describes a TCP listener and its destination.
type Forward struct {
	BindAddress     string
	ListenPort      uint16
	DestinationHost string
	DestinationPort uint16
}

// String renders the TCP forwarding specification used by SSH.
func (f Forward) String() string {
	return net.JoinHostPort(f.BindAddress, strconv.Itoa(int(f.ListenPort))) + ":" +
		net.JoinHostPort(f.DestinationHost, strconv.Itoa(int(f.DestinationPort)))
}

// Options contains transport-independent session configuration.
type Options struct {
	Transport      Transport
	Host           string
	Command        []string
	SSHPort        uint16
	IdentityFile   string
	ConfigFile     string
	ForwardAgent   bool
	LocalForwards  []Forward
	RemoteForwards []Forward
	ProtocolPort   int
}

// sessionOptions holds translated native arguments inside the session package.
type sessionOptions struct {
	mode         Transport
	args         []string
	forwards     []string
	connOpts     []string
	forwardAgent bool
	protocolPort int
}

func (o Options) nativeOptions() sessionOptions {
	connOpts := []string{"-o", "ForwardAgent=no"}
	if o.ForwardAgent {
		connOpts[1] = "ForwardAgent=yes"
	}
	if o.SSHPort != 0 {
		connOpts = append(connOpts, "-p", strconv.Itoa(int(o.SSHPort)))
	}
	if o.IdentityFile != "" {
		connOpts = append(connOpts, "-i", o.IdentityFile)
	}
	if o.ConfigFile != "" {
		connOpts = append(connOpts, "-F", o.ConfigFile)
	}
	var forwards []string
	for _, group := range []struct {
		flag   string
		values []Forward
	}{{"-L", o.LocalForwards}, {"-R", o.RemoteForwards}} {
		for _, forward := range group.values {
			forwards = append(forwards, group.flag, forward.String())
		}
	}
	args := append([]string{o.Host}, o.Command...)
	if o.Transport != Mosh {
		args = slices.Concat(connOpts, args)
	} else if len(o.Command) > 0 {
		args = []string{o.Host, "sh", "-c", strings.Join(o.Command, " ")}
	}
	return sessionOptions{
		mode: o.Transport, args: args, forwards: forwards, connOpts: connOpts,
		forwardAgent: o.ForwardAgent, protocolPort: o.ProtocolPort,
	}
}
