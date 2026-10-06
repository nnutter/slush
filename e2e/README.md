# End-to-end contracts

The E2E workflow runs the same black-box suite against real SSH servers over SSH and mosh.
Only the client desktop CLI tools are fake: they store clipboard bytes and record browser arguments in files under `/tmp`.
The suite does not emulate slush's protocol, provisioning, SSH, or mosh.

## OS pairs

| Client | Server | Environment |
| --- | --- | --- |
| Linux | Linux | Ubuntu runner and an Ubuntu Docker container |
| macOS | macOS | macOS runner and an isolated local sshd |
| macOS | Linux | macOS runner and an Ubuntu QEMU guest |
| Linux | macOS | Ubuntu QEMU guest and the macOS runner |

Each pair runs through both transports, and every job is gating.
The macOS-to-macOS pair shares a kernel and loopback network; the heterogeneous pairs provide independent kernels and exercise the reverse SSH connection across the guest network.
The Linux guest uses software emulation, not nested hardware virtualization or publicly reachable runners.
Guest boot and package installation can take approximately 15 minutes.

## Protected behavior

- `validate` connects, provisions, and checks clipboard forwarding.
- Every provisioned copy and paste name transfers exact bytes, including NULs, quotes, Unicode, and trailing newlines.
- Every open name and the exported `BROWSER` deliver exact URL arguments to the client backend.
- Remote file paths are rejected, and dry-run does not launch the browser.
- A wrong protocol token is rejected without poisoning a subsequent authenticated request.
- New sessions reuse existing installations and recover from a stale installation version.
- Clipboard and browser backend failures reach the remote command.
- Both `-L` and `-R` forwards carry real bytes.
- SSH preserves the remote command's exit status and honors explicit port and identity options.
- Signal termination releases the local listener and the extra reverse forward.
- Interactive login shells can copy and paste after shell startup.
- Linux client selection exercises Wayland, xclip, and xsel backends.

A `validate` invocation is an SSH probe regardless of mode; it is not evidence that a mosh session works.
The suite starts actual mosh clients and servers with a controlling PTY and a nonzero terminal size.
Assertions on remote success markers and independent client-side files prevent a clean transport exit from hiding a remote command failure.

## Fixtures and diagnostics

Keys, SSH configuration, native server caches, and desktop-tool records reside under `/tmp`.
The test SSH launcher adds an isolated `-F` configuration and then execs the real SSH executable.
It does not fake SSH responses or edit the user's SSH configuration.
The Linux guest is disposable and receives its own cloud-init configuration.

The workflow prints the server and guest boot logs during cleanup.
The suite prints terminal output and its fixture directory, and fails on a bounded session timeout rather than waiting indefinitely.

To run the suite against an existing test endpoint, supply an absolute slush binary path, an isolated SSH configuration, a host alias, and a transport:

```sh
python3 e2e/suite.py /tmp/slush /tmp/test-ssh-config test-host ssh
python3 e2e/suite.py /tmp/slush /tmp/test-ssh-config test-host mosh
```

Use a disposable endpoint with Python 3 and mosh installed, and isolate its `XDG_CACHE_HOME`.
The suite intentionally changes the provisioned shim version file to test recovery.
It also requires the clipboard port and test forward ports to be free.
