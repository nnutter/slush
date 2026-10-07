package command

// sessionOptions separates shared connection and forwarding options from the
// arguments used by the selected terminal client.
type sessionOptions struct {
	mode         clientMode
	args         []string
	forwards     []string
	connOpts     []string
	forwardAgent bool
}
