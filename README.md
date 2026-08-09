# slush

`ssh`/`et`/`mosh` wrapper that starts a local [Lemonade](https://github.com/lemonade-command/lemonade) server, forwards port 2489 with a reverse tunnel, and stops Lemonade when the session ends.

By default `slush` invokes `ssh` with `-R 2489:127.0.0.1:2489`.
Pass `--et` as the first argument to invoke [Eternal Terminal](https://mistertea.github.io/EternalTerminal/) for the TTY while keeping forwards up with a background `ssh -N` ControlMaster.
Pass `--mosh` as the first argument to use [mosh](https://mosh.org) for the TTY the same way.
`--et` and `--mosh` are mutually exclusive.

## Port forwards

OpenSSH-style `-L` and `-R` forwards are supported in all modes and always go through `ssh`:

```sh
# Browse a remote service at http://localhost:8080
slush -L 8080:127.0.0.1:8080 user@host
slush --et -L 8080:127.0.0.1:8080 user@host
slush --mosh -L 8080:127.0.0.1:8080 user@host
```

Multiple `-L`/`-R` options may be given. Combined forms (`-L8080:127.0.0.1:8080`) work too.
With `--et`/`--mosh`, those flags are applied on the background ssh tunnel and are not passed to `et`/`mosh`.
