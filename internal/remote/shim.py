#!/usr/bin/env python3
"""slush remote endpoint: clipboard/open client over the slush tunnel.

Installed as argv[0]-dispatched symlinks (pbcopy, wl-paste, xdg-open,
...); forwards to the clipboard server at 127.0.0.1:PORT using the
session token from $SLUSH_TOKEN (or the provisioned env file).
"""
import os
import socket
import sys

MAGIC = "SLUSH1"
DEFAULT_PORT = 2489

COPY_NAMES = {"pbcopy", "wl-copy", "xclip", "xsel"}
PASTE_NAMES = {"pbpaste", "wl-paste"}
OPEN_NAMES = {"xdg-open", "open", "slush-open", "sensible-browser"}

# Flags that flip xclip/xsel from copy (stdin) to paste (stdout).
PASTE_FLAGS = {"-o", "-out", "--output"}


def fail(msg):
    sys.stderr.write("slush: %s\n" % msg)
    sys.exit(1)


def shim_dir():
    cache = os.environ.get("XDG_CACHE_HOME") or os.path.join(
        os.path.expanduser("~"), ".cache"
    )
    return os.path.join(cache, "slush")


def load_token_port():
    token = os.environ.get("SLUSH_TOKEN", "")
    port = os.environ.get("SLUSH_PORT", "")
    if not token or not port:
        try:
            with open(os.path.join(shim_dir(), "slush-env")) as f:
                for line in f:
                    name, eq, value = line.partition("=")
                    if not eq or name.strip().startswith("#"):
                        continue
                    if name.strip() == "SLUSH_TOKEN" and not token:
                        token = value.strip()
                    elif name.strip() == "SLUSH_PORT" and not port:
                        port = value.strip()
        except OSError:
            pass
    if not token:
        fail("no session token (reconnect with slush, or export SLUSH_TOKEN)")
    try:
        return token, int(port or DEFAULT_PORT)
    except ValueError:
        fail("bad SLUSH_PORT %r" % (port,))


def exchange(token, port, verb, payload=None):
    """Send one request; return (response line, response body)."""
    if payload is None:
        header = "%s %s %s\n" % (MAGIC, token, verb)
    else:
        header = "%s %s %s %d\n" % (MAGIC, token, verb, len(payload))
    try:
        sock = socket.create_connection(("127.0.0.1", port), timeout=10)
    except OSError as e:
        fail(
            "cannot reach clipboard server on 127.0.0.1:%d: %s "
            "(is slush connected?)" % (port, e)
        )
    with sock:
        stream = sock.makefile("rb")
        sock.sendall(header.encode())
        if payload:
            sock.sendall(payload)
        line = stream.readline().decode("utf-8", "replace").strip()
        body = b""
        fields = line.split(" ")
        if len(fields) == 2 and fields[0] == "OK":
            try:
                body = stream.read(int(fields[1]))
            except ValueError:
                pass
    return line, body


def server_error(line):
    if line.startswith("ERR "):
        return line[4:]
    if line:
        return "unexpected server response %r" % (line,)
    return "empty server response"


def do_copy(token, port):
    data = sys.stdin.buffer.read()
    line, _ = exchange(token, port, "COPY", data)
    if line != "OK":
        fail(server_error(line))


def do_paste(token, port):
    line, body = exchange(token, port, "PASTE")
    if not line.startswith("OK"):
        fail(server_error(line))
    sys.stdout.buffer.write(body)


def do_open(token, port, target):
    line, _ = exchange(token, port, "OPEN", target.encode())
    if line != "OK":
        fail(server_error(line))


def first_operand(argv):
    for arg in argv[1:]:
        if arg == "--dry-run":
            continue
        if not arg.startswith("-") or arg == "-":
            return arg
    return ""


def main(argv):
    name = os.path.basename(argv[0])
    if name in OPEN_NAMES:
        target = first_operand(argv)
        if not target:
            fail("usage: %s [--dry-run] <url>" % name)
        if "--dry-run" in argv:
            sys.stdout.write("OPEN %s\n" % target)
            return 0
    token, port = load_token_port()
    if name in COPY_NAMES:
        if name in ("xclip", "xsel") and PASTE_FLAGS.intersection(argv[1:]):
            do_paste(token, port)
        else:
            do_copy(token, port)
    elif name in PASTE_NAMES:
        do_paste(token, port)
    elif name in OPEN_NAMES:
        do_open(token, port, target)
    else:
        fail("unknown shim name %r" % (name,))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
