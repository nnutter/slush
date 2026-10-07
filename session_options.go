package main

// sessionOptions separates shared connection and forwarding options from the
// arguments used by the selected terminal client.
type sessionOptions struct {
	mode     clientMode
	args     []string
	forwards []string
	connOpts []string
}

func sessionOptionsFromArgs(mode clientMode, args []string) (sessionOptions, error) {
	forwards, rest, err := takeSSHForwards(args)
	if err != nil {
		return sessionOptions{}, err
	}
	options := sessionOptions{mode: mode, args: rest, forwards: forwards}
	if mode == modeSSH {
		options.connOpts, err = sshConnOpts(rest)
		if err != nil {
			return sessionOptions{}, err
		}
	}
	return options, nil
}
