# slush

`ssh`/`mosh` wrapper with clipboard and URL forwarding that just
works: `slush user@host`, then `pbcopy`, `pbpaste`, and `open` on the
remote reach your local clipboard and browser.

How it works: slush listens on an ephemeral port on 127.0.0.1 and
forwards a matching remote loopback port back to that listener. Each
session gets its own port, so more than one slush can be connected
at the same time. It provisions clipboard shims into
`~/.cache/slush/bin` on the remote, and wraps the remote environment
(`PATH` shadow, `BROWSER=slush-open`, `SLUSH_PORT`, session token)
without touching your dotfiles. When the session ends, the tunnel
and server go with it.

The default `--transport auto` selects [Mosh](https://mosh.org) when local `mosh` and remote `mosh-server` are available; otherwise, it selects SSH.
Use `--transport ssh` to force SSH, or `--transport mosh` to force Mosh without fallback.
Authentication failures, connection failures, and failed availability probes are errors, not reasons to fall back.
Once Mosh starts, slush does not retry a failed session over SSH or execute the remote command again.
Both transports use a held SSH ControlMaster. After it is up, slush
adds a reverse forward for that session's clipboard port.
`validate` uses the same automatic selection and supports the same explicit transport options.
Slush owns its options and uses the same connection and forwarding settings for both transports.
Use `--help`, `--version`, or `completion` for CLI help, build information, and shell completions.

```sh
slush user@host
slush --transport ssh user@host
slush --transport mosh -p 2222 -i ./key user@host
slush validate --transport mosh user@host
```

`-p`/`--port` selects the SSH connection port in either mode, not a mosh UDP port.
`-i`/`--identity` selects an SSH identity file, and `-F`/`--config` selects an SSH configuration file.
Otherwise, SSH configuration supplies connection defaults.
Slush rejects unknown flags before the host instead of passing them to a native client.
Arguments after the host form a remote shell command and are not parsed as slush options.
Use `slush -- validate` or `slush -- install` to connect to a host literally named `validate` or `install`.

Clipboard forwarding needs `python3` on the remote (for the shims).

## Shell completion

Generate a completion script with `slush completion bash`, `slush completion zsh`, `slush completion fish`, or `slush completion powershell`.
For example, enable completion in the current Bash session:

```sh
source <(slush completion bash)
```

Host completion works for `slush`, `slush validate`, and `slush install`, including a typed username such as `alice@`.
Suggestions combine literal `Host` aliases from `~/.ssh/config` and its `Include` files with readable hosts from `~/.ssh/known_hosts`.
Use `-F` to select a different config file for alias suggestions.
Relative include paths resolve from `~/.ssh`, as in OpenSSH.
Completion collects aliases without evaluating `Host` or `Match` conditions or executing commands.
Wildcard aliases, negated aliases, hashed hosts, and revoked known-host entries are not suggested.
Hosts stored as `[host]:port` appear as `host` only, so use SSH config or `-p` to select the port.
Missing or unreadable files do not prevent completion from using other sources.
Host suggestions stop after the host argument.

## Port forwards

Use `-L`/`--local-forward` to listen locally and connect from the remote machine.
Use `-R`/`--remote-forward` to listen remotely and connect from the local machine.
Both transports carry these TCP forwards through the held SSH connection.

```sh
slush -L 8080 user@host
slush --transport mosh -L 8080:80 user@host
slush -R 9000:localhost:3000 user@host
```

Omitted bind addresses and destination hosts mean `localhost` on their respective machines.
If only one port is given, both ends use that port.

| Specification | Listen address | Destination |
| --- | --- | --- |
| `8080` | `localhost:8080` | `localhost:8080` |
| `8080:80` | `localhost:8080` | `localhost:80` |
| `8080:db` | `localhost:8080` | `db:8080` |
| `8080:db:5432` | `localhost:8080` | `db:5432` |
| `127.0.0.1:8080:80` | `127.0.0.1:8080` | `localhost:80` |
| `127.0.0.1:8080:db:5432` | `127.0.0.1:8080` | `db:5432` |
| `[::1]:8080:[::1]:80` | `[::1]:8080` | `[::1]:80` |

Empty address fields also mean `localhost`, as in `:8080::80`.
Bracket IPv6 addresses so their colons are not field separators.
Repeat `-L` or `-R` for multiple forwards, or use combined forms such as `-L8080`.
Ports must be between 1 and 65535.
Unix socket forwards, dynamic port allocation, and arbitrary native transport flags are not supported.

## SSH agent forwarding

Enable agent forwarding with `-A` or `--forward-agent` in either transport:

```sh
slush -A user@host
slush --transport mosh -A -L 8080 user@host
slush validate -A user@host
```

Agent forwarding is disabled by default, even if SSH configuration enables it.
Slush checks the local agent before connecting and checks the remote socket during setup.
The held SSH connection keeps that socket usable after mosh bootstrap exits.
Mosh's default address discovery uses a separate bootstrap connection.
Slush gives the mosh session the held connection's socket instead of the short-lived bootstrap socket.
The socket and TCP forwards are removed when the held connection closes.
An empty local agent is valid, so you can load keys during the session.

Agent forwarding requires a local SSH agent and `ssh-add`.
The server must permit `AllowAgentForwarding`.
Validation also needs `ssh-add` on the remote machine.
Forward only to trusted hosts because remote programs can request signatures from your local agent.
Slush does not copy private keys to the server.

## Clipboard and open

On the remote, the usual names all forward to your local machine:

- copy: `pbcopy`, `wl-copy`, `xclip`, `xsel` (stdin → local clipboard)
- paste: `pbpaste`, `wl-paste`, `xclip -o`, `xsel --output` (local clipboard → stdout)
- open: `xdg-open`, `open`, `slush-open` (URL → local browser)

Names are dispatched by `argv[0]`, so mixed pairs interoperate:
`wl-copy` on a Linux remote lands in `pbcopy` on a macOS client
and vice versa. Locally, slush shells out to the platform tools
(`pbcopy`/`pbpaste`/`open` on macOS, `wl-copy`/`wl-paste` with
`xclip`/`xsel` fallbacks and `xdg-open` on Linux), so local
configuration is honored.

`BROWSER=slush-open` is exported remotely, so `gh browse`,
`git web--browse`, and other `BROWSER`-aware tools open locally
too. Only URLs open remotely: remote *file* paths are rejected
with an error instead of opening the wrong machine's files. For
files, use the editor's own remoting — e.g. `zed ssh://host/path`
composes with slush untouched, since Zed tunnels over your ssh
connection independently.

`http://localhost:port` URLs open as-is on the client, so pair
them with your own `-L` forward when the service lives remotely.

## Install wrappers for existing remote servers

Use `install` when a remote server, such as Herdr, started before your slush connection.
The command installs the clipboard and URL wrappers into `~/.slush/bin`:

```sh
slush install user@host
slush install -p 2222 -i ./key user@host
```

Prepend `~/.slush/bin` to `PATH` on the remote host so the existing server finds the wrappers.

Installation requires remote Python 3 and uses SSH, including connection options `-p`, `-i`, and `-F`.
It creates `~/.slush/bin` if needed and replaces existing command names there.
It does not change startup files, session credentials, or the server's environment.
Transport and forwarding options (`--transport`, `-A`, `-L`, `-R`) are not supported by `install`.
For browser-aware tools, configure `BROWSER=slush-open` if their environment does not already select that command.
Installation cannot change the environment of an existing process.

Connect through slush as usual and keep that connection active while you use the remote server.
Installed wrappers use `SLUSH_TOKEN` and `SLUSH_PORT` from their environment when present.
Missing values come from the remote session file at `${XDG_CACHE_HOME:-$HOME/.cache}/slush/slush-env`.
A process without those variables therefore uses the newest slush session for that remote account and cache directory.
Reconnecting does not require another installation.
A process with inherited credentials still selects its original session, which can become unavailable after disconnection.
When no token is available or the server cannot be reached, wrappers pass through to the native tool with the same name and arguments.
`slush-open` has no native namesake and still fails, as do server errors.

## Validate

`slush validate [options] HOST` checks the whole path and reports each check (`ok`/`FAIL`).
It returns a nonzero exit status if a check fails.
It accepts the same connection, transport, and forwarding options as a session:

```sh
slush validate user@host
```

It verifies the tunnel, shim version, transport, session
environment, clipboard round-trips in both directions, open
dry-run, and prints a client/remote platform report. Sessions
degrade with a warning when provisioning fails; validate is where
forwarding is enforced. Use it after changing anything here, and
point remote tools at it when they misbehave.

## Remote integration contract

Programs running on the remote (Herdr, plugins, scripts) can rely
on this while a slush session is connected:

- `127.0.0.1:$SLUSH_PORT` speaks the slush clipboard protocol, routed
  to the local machine. The port is chosen per session, so simultaneous
  slush connections do not share a listener. One request per TCP connection:
  `SLUSH1 <token> HELLO` → `OK slush-clipboard 1`,
  `SLUSH1 <token> COPY <n>` + bytes → `OK`,
  `SLUSH1 <token> PASTE` → `OK <n>` + bytes,
  `SLUSH1 <token> OPEN <n>` + URL → `OK`.
  Failures answer `ERR <message>`; anything unparsable gets an
  `ERR`, never a hang.
- The session token is `$SLUSH_TOKEN`, the clipboard port is
  `$SLUSH_PORT`, and `SLUSH=1` marks a wrapped session. Shells started
  by that session keep those values. A shell that has neither variable
  falls back to `~/.cache/slush/slush-env`, which records the newest
  session on that remote account.
- Detecting slush: send `SLUSH1 x HELLO` to `127.0.0.1:$SLUSH_PORT` and
  look for the `OK`/`ERR` shape, or probe for that listener without
  handshaking (bind attempt, `lsof`, `ss -ltn`). A bare TCP connect is
  harmless but proves nothing.
