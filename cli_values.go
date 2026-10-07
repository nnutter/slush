package main

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

var (
	_ pflag.Value = (*clientMode)(nil)
	_ pflag.Value = (*tcpPort)(nil)
	_ pflag.Value = (*forwardValues)(nil)
)

func (m clientMode) String() string {
	if m == modeMosh {
		return "mosh"
	}
	return "ssh"
}

func (m *clientMode) Set(value string) error {
	switch value {
	case "ssh":
		*m = modeSSH
	case "mosh":
		*m = modeMosh
	default:
		return fmt.Errorf("unknown transport %q; choose ssh or mosh", value)
	}
	return nil
}

func (*clientMode) Type() string { return "transport" }

type tcpPort uint16

func (p tcpPort) String() string {
	if p == 0 {
		return ""
	}
	return strconv.Itoa(int(p))
}

func (p *tcpPort) Set(value string) error {
	if !decimal(value) {
		return fmt.Errorf("port %q must be a decimal number between 1 and 65535", value)
	}
	n, err := strconv.ParseUint(value, 10, 16)
	if err != nil || n == 0 {
		return fmt.Errorf("port %q must be between 1 and 65535", value)
	}
	*p = tcpPort(n)
	return nil
}

func (*tcpPort) Type() string { return "port" }

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type portForward struct {
	bindAddress string
	listenPort  tcpPort
	host        string
	port        tcpPort
}

func (f portForward) String() string {
	return net.JoinHostPort(f.bindAddress, f.listenPort.String()) + ":" + net.JoinHostPort(f.host, f.port.String())
}

// forwardValues appends one structured TCP forward per flag occurrence.
// Commas are not list separators, unlike pflag's StringSlice.
type forwardValues []portForward

func (v *forwardValues) Set(value string) error {
	forward, err := parsePortForward(value)
	if err != nil {
		return fmt.Errorf("%s; use PORT, PORT:PORT, PORT:HOST:PORT, or BIND:PORT:HOST:PORT (bracket IPv6 addresses)", err)
	}
	*v = append(*v, forward)
	return nil
}

func (v *forwardValues) String() string {
	values := make([]string, len(*v))
	for i, forward := range *v {
		values[i] = forward.String()
	}
	return strings.Join(values, ", ")
}

func (*forwardValues) Type() string { return "forward" }

func parsePortForward(value string) (portForward, error) {
	fields, err := forwardFields(value)
	if err != nil {
		return portForward{}, err
	}
	f := portForward{bindAddress: "localhost", host: "localhost"}
	if len(fields) > 1 && !decimal(fields[0]) {
		f.bindAddress, err = forwardHost(fields[0])
		if err != nil {
			return portForward{}, fmt.Errorf("bind address: %w", err)
		}
		fields = fields[1:]
	}
	if err := f.listenPort.Set(fields[0]); err != nil {
		return portForward{}, fmt.Errorf("listen port: %w", err)
	}
	f.port = f.listenPort
	switch len(fields) {
	case 1:
	case 2:
		if decimal(fields[1]) {
			if err := f.port.Set(fields[1]); err != nil {
				return portForward{}, fmt.Errorf("destination port: %w", err)
			}
		} else {
			f.host, err = forwardHost(fields[1])
		}
	case 3:
		f.host, err = forwardHost(fields[1])
		if err == nil && fields[2] != "" {
			if portErr := f.port.Set(fields[2]); portErr != nil {
				return portForward{}, fmt.Errorf("destination port: %w", portErr)
			}
		}
	default:
		return portForward{}, fmt.Errorf("too many components in forward %q", value)
	}
	if err != nil {
		return portForward{}, fmt.Errorf("destination host: %w", err)
	}
	return f, nil
}

func forwardFields(value string) ([]string, error) {
	var fields []string
	start, bracketed := 0, false
	for i, r := range value {
		switch r {
		case '[':
			if bracketed || i != start {
				return nil, fmt.Errorf("unexpected '[' in forward %q", value)
			}
			bracketed = true
		case ']':
			if !bracketed {
				return nil, fmt.Errorf("unexpected ']' in forward %q", value)
			}
			bracketed = false
		case ':':
			if !bracketed {
				fields = append(fields, value[start:i])
				start = i + 1
			}
		}
	}
	if bracketed {
		return nil, fmt.Errorf("missing ']' in forward %q", value)
	}
	return append(fields, value[start:]), nil
}

func forwardHost(value string) (string, error) {
	if value == "" {
		return "localhost", nil
	}
	if strings.HasPrefix(value, "[") {
		if !strings.HasSuffix(value, "]") {
			return "", fmt.Errorf("invalid bracketed address %q", value)
		}
		addr, err := netip.ParseAddr(value[1 : len(value)-1])
		if err != nil || !addr.Is6() {
			return "", fmt.Errorf("%q is not a bracketed IPv6 address", value)
		}
		return value[1 : len(value)-1], nil
	}
	if strings.ContainsAny(value, "[]:/, \t\r\n") {
		return "", fmt.Errorf("invalid TCP host %q", value)
	}
	return value, nil
}
