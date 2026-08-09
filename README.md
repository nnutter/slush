# slush

`ssh`/`et`/`mosh` wrapper that starts a local [Lemonade](https://github.com/lemonade-command/lemonade) server, forwards port 2489 with a reverse tunnel, and stops Lemonade when the session ends.

By default `slush` invokes `ssh` with `-R 2489:127.0.0.1:2489`.
Pass `--et` as the first argument to invoke [Eternal Terminal](https://mistertea.github.io/EternalTerminal/) instead (`et -r 2489:2489`).
Pass `--mosh` as the first argument to use [mosh](https://mosh.org) for the TTY while keeping the Lemonade forward up with a background `ssh -N -R 2489:127.0.0.1:2489` ControlMaster.
`--et` and `--mosh` are mutually exclusive.
