# slush

`ssh`/`mosh` wrapper with clipboard and URL forwarding that just
works: `slush user@host`, then `pbcopy`, `pbpaste`, and `open` on the
remote reach your local clipboard and browser.

How it works: slush starts a clipboard server on 127.0.0.1:2489,
forwards the remote loopback to it with a reverse tunnel, provisions
clipboard shims into `~/.cache/slush/bin` on the remote, and wraps
the remote environment (`PATH` shadow, `BROWSER=slush-open`, session
token) without touching your dotfiles. When the session ends, the
tunnel and server go with it.

By default `slush` invokes `ssh` with a held ControlMaster plus
`-R 2489:127.0.0.1:2489`.
Select [mosh](https://mosh.org) with `--transport mosh`, or use the `--mosh` shorthand.
Slush owns its options and uses the same connection and forwarding settings for both transports.
Use `--help`, `--version`, or `completion` for CLI help, build information, and shell completions.

```sh
slush --transport ssh user@host
slush --transport mosh -p 2222 -i ./key user@host
slush validate --transport mosh user@host
```

`-p`/`--port` selects the SSH connection port in either mode, not a mosh UDP port.
`-i`/`--identity` selects an SSH identity file, and `-F`/`--config` selects an SSH configuration file.
Otherwise, SSH configuration supplies connection defaults.
Slush rejects unknown flags before the host instead of passing them to a native client.
Arguments after the host form a remote shell command and are not parsed as slush options.
Use `slush -- validate` to connect to a host literally named `validate`.

Clipboard forwarding needs `python3` on the remote (for the shims).

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

- `127.0.0.1:2489` speaks the slush clipboard protocol, routed to
  the local machine. One request per TCP connection:
  `SLUSH1 <token> HELLO` → `OK slush-clipboard 1`,
  `SLUSH1 <token> COPY <n>` + bytes → `OK`,
  `SLUSH1 <token> PASTE` → `OK <n>` + bytes,
  `SLUSH1 <token> OPEN <n>` + URL → `OK`.
  Failures answer `ERR <message>`; anything unparsable gets an
  `ERR`, never a hang.
- The session token is available as `$SLUSH_TOKEN`, and
  `SLUSH=1` marks a wrapped session. Without them (stray shells), fall back to `~/.cache/slush/slush-env`.
- Detecting slush: send `SLUSH1 x HELLO` and look for the `OK`/`ERR`
  shape, or probe for the listener without handshaking (bind
  attempt, `lsof`, `ss -ltn`). A bare TCP connect is harmless but
  proves nothing.
