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
Pass `--mosh` as the first argument to use [mosh](https://mosh.org) for the TTY while keeping forwards up with a background `ssh -N` ControlMaster.

Clipboard forwarding needs `python3` on the remote (for the shims).

## Port forwards

OpenSSH-style `-L` and `-R` forwards are supported in all modes and always go through `ssh`:

```sh
# Browse a remote service at http://localhost:8080
slush -L 8080:127.0.0.1:8080 user@host
slush --mosh -L 8080:127.0.0.1:8080 user@host
```

Multiple `-L`/`-R` options may be given. Combined forms (`-L8080:127.0.0.1:8080`) work too.
With `--mosh`, those flags are applied on the background ssh tunnel and are not passed to mosh.

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

`slush [--mosh] validate [host...]` checks the whole path and
reports per check (`ok`/`FAIL`) with a nonzero exit on failure:

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
