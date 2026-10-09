#!/usr/bin/env python3
"""Black-box slush contracts. Only desktop CLI edges are file-backed fakes.

Usage: suite.py /absolute/slush /absolute/ssh-config host ssh|mosh
All fixtures live under /tmp. SSH launcher only adds -F; it execs real ssh.
"""
import base64
import errno
import faulthandler
import fcntl
import hashlib
import json
import os
from pathlib import Path
import select
import shlex
import shutil
import signal
import socket
import sys
import struct
import subprocess
import termios
import tempfile
import threading
import time

SLUSH, CONFIG, HOST, MODE = sys.argv[1:]
ROOT = Path(tempfile.mkdtemp(prefix="slush-e2e-", dir="/tmp"))
ENV = dict(os.environ, TERM="xterm", E2E_ROOT=str(ROOT))
BIN = ROOT / "bin"
BIN.mkdir()
SSH = shutil.which("ssh")
(BIN / "ssh").write_text(
    '#!/bin/sh\ncase " $* " in *" -N "*) printf \'%s\' "$$" > '
    + shlex.quote(str(ROOT / 'master-pid')) + ' ;; esac\n'
    + 'case "$*" in *"command -v mosh-server"*) if [ -e '
    + shlex.quote(str(ROOT / 'kill-master')) + ' ]; then kill "$(/bin/cat '
    + shlex.quote(str(ROOT / 'master-pid')) + ')"; /bin/sleep 0.3; fi ;; esac\n'
    + "case \"$*\" in *mosh-server*new*) printf '%s\\n' \"$$\" >> "
    + shlex.quote(str(ROOT / 'bootstrap-pids')) + ' ;; esac\nexec '
    + shlex.quote(SSH) + " -F " + shlex.quote(CONFIG) + ' "$@"\n')
(BIN / "ssh").chmod(0o755)
MOSH = shutil.which("mosh")
(BIN / "mosh").write_text(
    "#!/bin/sh\nprintf 'started\\n' >> " + shlex.quote(str(ROOT / 'mosh-clients'))
    + '\nexec ' + shlex.quote(MOSH) + ' "$@"\n')
(BIN / "mosh").chmod(0o755)
ENV["PATH"] = str(BIN) + os.pathsep + ENV["PATH"]
ENV["WAYLAND_DISPLAY"] = "e2e"
# Native platform names with independent file I/O, not a protocol mock.
BACKEND = '''#!PYTHON
import hashlib, json, os, pathlib, sys
root = pathlib.Path(os.environ['E2E_ROOT'])
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
if (root / 'fail').exists():
    sys.exit(23)
if name in ('open', 'xdg-open'):
    record = {'op': 'open', 'args': args}
else:
    paste = name in ('pbpaste', 'wl-paste') or any(a in args for a in ('-o', '--output'))
    if paste:
        data = (root / 'clipboard').read_bytes()
        sys.stdout.buffer.write(data)
    else:
        data = sys.stdin.buffer.read()
        (root / 'clipboard').write_bytes(data)
    record = {'op': 'paste' if paste else 'copy', 'sha256': hashlib.sha256(data).hexdigest()}
with (root / 'calls').open('a') as f:
    f.write(json.dumps(record) + '\\n')
'''.replace("PYTHON", sys.executable)
for name in ("pbcopy", "pbpaste", "wl-copy", "wl-paste", "xclip", "xsel", "open", "xdg-open"):
    path = BIN / name
    path.write_text(BACKEND)
    path.chmod(0o755)


def session(command, timeout=90, extra=(), raw=False, tick=None):
    if raw:
        argv = [SLUSH, *extra, HOST, command]
    else:
        argv = [SLUSH, '--transport', MODE, *extra, HOST, command]
    return terminal(argv, timeout, tick)


def terminal(argv, timeout, tick=None, shell_input=None):
    # Do not run Python after forking the driver on macOS. Spawn a fresh
    # interpreter that attaches the controlling terminal, then execs slush.
    print('start - terminal', argv[1], 'timeout', timeout, flush=True)
    faulthandler.dump_traceback_later(timeout + 35, exit=True)
    fd, slave = os.openpty()
    try:
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 40, 120, 0, 0))
        helper = ('import os, sys, fcntl, termios; '
                  'os.setsid(); fcntl.ioctl(0, termios.TIOCSCTTY, 0); '
                  'os.execvpe(sys.argv[1], sys.argv[1:], os.environ)')
        actions = [(os.POSIX_SPAWN_DUP2, slave, target) for target in (0, 1, 2)]
        pid = os.posix_spawn(sys.executable, [sys.executable, '-c', helper, *argv],
                             ENV, file_actions=actions)
    except BaseException:
        os.close(fd)
        faulthandler.cancel_dump_traceback_later()
        raise
    finally:
        os.close(slave)
    print('started - terminal child', pid, flush=True)
    os.set_blocking(fd, False)
    output = bytearray()
    pending_input = bytearray()
    started = time.monotonic()
    deadline = started + timeout
    input_sent = False
    next_probe = started + 2
    status = None
    eof = False
    try:
        while time.monotonic() < deadline:
            if shell_input is not None and not input_sent:
                if b'SHELL_READY' in output:
                    pending_input.extend(shell_input.encode())
                    input_sent = True
                elif time.monotonic() >= next_probe:
                    # Harmless probes survive slow startup and terminal flushes.
                    # The marker is not present literally in the echoed input.
                    pending_input.extend(b"printf 'SHELL_%s\\n' READY\n")
                    next_probe = time.monotonic() + 2
            if pending_input:
                try:
                    written = os.write(fd, pending_input)
                    del pending_input[:written]
                except BlockingIOError:
                    pass
            if tick:
                reply = tick(bytes(output), pid)
                if reply:
                    pending_input.extend(reply)
            if eof:
                time.sleep(0.05)
            elif select.select([fd], [], [], 0.1)[0]:
                try:
                    chunk = os.read(fd, 65536)
                    if not chunk:
                        eof = True
                    output.extend(chunk)
                except BlockingIOError:
                    pass
                except OSError as e:
                    if e.errno != errno.EIO:
                        raise
                    # PTY EOF can precede waitpid reporting child exit.
                    eof = True
            done, result = os.waitpid(pid, os.WNOHANG)
            if done:
                status = result
                break
        if status is None:
            done, result = os.waitpid(pid, os.WNOHANG)
            if done:
                status = result
        if status is None:
            print(output.decode(errors='replace'), flush=True)
            print('FAIL - terminal deadline; process states follow', flush=True)
            subprocess.run(['ps', '-axo', 'pid,ppid,pgid,stat,wchan,comm'], timeout=5,
                           stdin=subprocess.DEVNULL, check=False)
            os.kill(pid, signal.SIGTERM)
            # Allow slush to forward the signal and clean up its master.
            end = time.monotonic() + 10
            while time.monotonic() < end:
                done, result = os.waitpid(pid, os.WNOHANG)
                if done:
                    status = result
                    break
                time.sleep(0.1)
            if status is None:
                os.kill(pid, signal.SIGKILL)
                end = time.monotonic() + 5
                while time.monotonic() < end:
                    done, result = os.waitpid(pid, os.WNOHANG)
                    if done:
                        status = result
                        break
                    time.sleep(0.1)
                if status is None:
                    print('FAIL - child did not exit after SIGKILL', pid, flush=True)
                    subprocess.run(['ps', '-axo', 'pid,ppid,pgid,stat,wchan,comm'], timeout=5,
                                   stdin=subprocess.DEVNULL, check=False)
            raise AssertionError("session timed out: " + repr(argv) + "\n" + output.decode(errors="replace"))
        # Drain remaining output after process exit.
        drain_deadline = time.monotonic() + 2
        while time.monotonic() < drain_deadline and select.select([fd], [], [], 0)[0]:
            try:
                chunk = os.read(fd, 65536)
                if not chunk:
                    break
                output.extend(chunk)
            except OSError:
                break
        text = output.decode(errors="replace")
        print(text, flush=True)
        print('finish - terminal', argv[1], 'status', os.waitstatus_to_exitcode(status), flush=True)
        return os.waitstatus_to_exitcode(status), text
    finally:
        faulthandler.cancel_dump_traceback_later()
        os.close(fd)


def python_command(script):
    return "python3 -c " + shlex.quote(script)


def checked(script):
    code, output = session(python_command(script + '\nprint("SLUSH_E2E_PASS", flush=True)'))
    assert code == 0 and "SLUSH_E2E_PASS" in output, (code, output)
    assert "continuing without clipboard forwarding" not in output, output
    # Listener teardown must occur before slush exits, not eventually.
    with socket.socket() as s:
        assert s.connect_ex(("127.0.0.1", 2489)) != 0, "orphaned local server"
    return output


PAYLOAD = bytes(range(256)) * 4096 + b"\nquotes ' \\\" unicode \xe2\x98\x83\n"
DIGEST = hashlib.sha256(PAYLOAD).hexdigest()
# Generate the payload remotely instead of exceeding SSH's command-size limit.
REMOTE_PAYLOAD = 'bytes(range(256)) * 4096 + ' + repr(PAYLOAD[256 * 4096:])
REMOTE = '''import hashlib, os, pathlib, socket, subprocess
cache = pathlib.Path(os.environ.get('XDG_CACHE_HOME') or pathlib.Path.home() / '.cache')
assert (cache / 'slush/slush-env').stat().st_mode & 0o077 == 0, 'session token is not private'
assert os.environ['SLUSH'] == '1'
assert os.environ['BROWSER'] == 'slush-open'
assert os.environ['SLUSH_TOKEN']
payload = PAYLOAD
copies = [['pbcopy'], ['wl-copy'], ['xclip', '-selection', 'clipboard'], ['xsel', '--clipboard', '--input']]
pastes = [['pbpaste'], ['wl-paste'], ['xclip', '-selection', 'clipboard', '-o'], ['xsel', '--clipboard', '--output']]
for copy in copies:
    subprocess.run(copy, input=payload, check=True)
    for paste in pastes:
        assert subprocess.check_output(paste) == payload, paste
for name in ['open', 'xdg-open', 'slush-open', 'sensible-browser', os.environ['BROWSER']]:
    subprocess.run([name, 'https://example.com/path?q=a%20b&x=1'], check=True)
assert subprocess.run(['slush-open', '/tmp/remote-only-file']).returncode != 0
assert subprocess.check_output(['slush-open', '--dry-run', 'https://example.com/dry']).strip() == b'OPEN https://example.com/dry'
# An incorrect token must fail, and must not poison the next valid request.
for token, expected in [('wrong-token', b'ERR'), (os.environ['SLUSH_TOKEN'], b'OK')]:
    with socket.create_connection(('127.0.0.1', 2489), timeout=5) as conn:
        conn.sendall(('SLUSH1 ' + token + ' HELLO\\n').encode())
        assert conn.recv(4096).startswith(expected)
'''.replace("PAYLOAD", REMOTE_PAYLOAD)

try:
    # Auto must launch real Mosh when both native binaries are available.
    marker = ROOT / 'mosh-clients'
    code, output = terminal([SLUSH, HOST,
                             "printf 'AUTO_AVAILABLE_PASS\\n'"], 90)
    assert code == 0 and 'AUTO_AVAILABLE_PASS' in output and marker.exists(), output
    marker.unlink()
    # Explicit SSH must not launch Mosh, even when it is available.
    code, output = terminal([SLUSH, '--transport', 'ssh', HOST,
                             "printf 'FORCED_SSH_PASS\\n'"], 90)
    assert code == 0 and 'FORCED_SSH_PASS' in output and not marker.exists(), output
    code, output = terminal([SLUSH, 'validate', HOST], 120)
    assert code == 0 and 'all 9 checks passed' in output, output
    # Remove only the local Mosh launcher from the isolated client PATH.
    original_path = ENV['PATH']
    (BIN / 'mosh').rename(BIN / 'hidden-mosh')
    ENV['PATH'] = str(BIN)
    try:
        code, output = terminal([SLUSH, HOST,
                                 "printf 'AUTO_SSH_PASS\\n'; exit 37"], 90)
        assert code == 37 and 'AUTO_SSH_PASS' in output and not marker.exists(), output
        code, output = terminal([SLUSH, 'validate', HOST], 120)
        assert code == 0 and 'all 10 checks passed' in output, output
    finally:
        ENV['PATH'] = original_path
        (BIN / 'hidden-mosh').rename(BIN / 'mosh')
    # Losing the held master must not reconnect to discover capabilities.
    (ROOT / 'kill-master').touch()
    try:
        code, output = terminal([SLUSH, '--transport', 'auto', HOST,
                                 "printf 'NEVER_FALLBACK\\n'"], 90)
        assert code != 0 and 'detect remote mosh availability' in output.lower(), output
        assert 'NEVER_FALLBACK' not in output and not marker.exists(), output
    finally:
        (ROOT / 'kill-master').unlink()
    # The native fixture also has a real account without mosh-server on PATH.
    if HOST == 'native':
        code, output = terminal([SLUSH, 'native-ssh',
                                 "printf 'REMOTE_SSH_PASS\\n'; exit 37"], 90)
        assert code == 37 and 'REMOTE_SSH_PASS' in output and not marker.exists(), output
        code, output = terminal([SLUSH, 'validate', 'native-ssh'], 120)
        assert code == 0 and 'all 10 checks passed' in output, output
        code, output = terminal([SLUSH, '--transport', 'mosh', 'native-ssh',
                                 "printf 'NEVER_EXECUTED\\n'"], 90)
        assert code != 0 and 'NEVER_EXECUTED' not in output and marker.exists(), output
        marker.unlink()
    # validate itself is a real SSH probe, not mosh coverage.
    code, output = terminal([SLUSH, "validate", '--transport', 'ssh', HOST], 120)
    assert code == 0 and "all 10 checks passed" in output, output
    (ROOT / "calls").write_text("")
    # Simulate permissions left by an older installation; refresh must repair.
    subprocess.run([SSH, '-F', CONFIG, HOST,
                    'chmod 644 "${XDG_CACHE_HOME:-$HOME/.cache}/slush/slush-env"'],
                   check=True, timeout=60, stdin=subprocess.DEVNULL)
    checked(REMOTE)
    records = [json.loads(line) for line in (ROOT / "calls").read_text().splitlines()]
    assert sum(r["op"] == "copy" for r in records) == 4, records
    assert sum(r["op"] == "paste" for r in records) == 16, records
    assert all(r["sha256"] == DIGEST for r in records if "sha256" in r), records
    opens = [r["args"] for r in records if r["op"] == "open"]
    assert opens == [["https://example.com/path?q=a%20b&x=1"]] * 5, opens
    assert (ROOT / "clipboard").read_bytes() == PAYLOAD
    # A reused installation still works in a fresh session with a new token.
    checked("import subprocess\nassert subprocess.check_output(['pbpaste']) == " + REMOTE_PAYLOAD)
    # Force the upgrade path, then prove installed commands still work.
    subprocess.run([SSH, '-F', CONFIG, HOST,
                    'printf stale > "${XDG_CACHE_HOME:-$HOME/.cache}/slush/VERSION"'],
                   check=True, timeout=60, stdin=subprocess.DEVNULL)
    checked("import subprocess\nassert subprocess.check_output(['pbpaste']) == " + REMOTE_PAYLOAD)
    (ROOT / "fail").touch()
    checked("import subprocess\nfor args in [['pbcopy'], ['pbpaste'], ['slush-open', 'https://example.com/fail']]:\n    assert subprocess.run(args, input=b'x').returncode != 0, args")
    (ROOT / "fail").unlink()
    # Both -L and -R must carry actual bytes, not just accepted flags.
    with socket.socket() as reverse:
        reverse.bind(('127.0.0.1', 0))
        reverse.listen(1)
        reverse.settimeout(30)
        reverse_port = reverse.getsockname()[1]
        def echo_reverse():
            with reverse.accept()[0] as conn:
                conn.sendall(conn.recv(4096))
        worker = threading.Thread(target=echo_reverse, daemon=True)
        worker.start()
        with socket.socket() as reserve:
            reserve.bind(('127.0.0.1', 0))
            local_port = reserve.getsockname()[1]
        sent = [False]
        def forward_tick(output, pid):
            if not sent[0] and b'FORWARD_READY' in output:
                with socket.create_connection(('127.0.0.1', local_port), timeout=5) as conn:
                    conn.sendall(b'local-forward')
                    assert conn.recv(4096) == b'local-forward'
                sent[0] = True
        script = '''import socket
with socket.socket() as listener:
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(('127.0.0.1', 39091))
    listener.listen(1)
    listener.settimeout(30)
    with socket.create_connection(('127.0.0.1', 39093), timeout=5) as conn:
        conn.sendall(b'reverse-forward')
        assert conn.recv(4096) == b'reverse-forward'
    print('FORWARD_READY', flush=True)
    with listener.accept()[0] as conn:
        conn.sendall(conn.recv(4096))
print('FORWARD_PASS', flush=True)
'''
        code, output = session(python_command(script), extra=(
            '-L', str(local_port) + ':39091',
            '-R', '39093:' + str(reverse_port)), tick=forward_tick)
        worker.join(5)
        assert code == 0 and sent[0] and 'FORWARD_PASS' in output and not worker.is_alive(), output
    # SIGTERM must tear down the server and reverse listener too.
    stopped = [False]
    def stop_tick(output, pid):
        if not stopped[0] and b'SIGNAL_READY' in output:
            os.kill(pid, signal.SIGTERM)
            stopped[0] = True
    session(python_command("import time\nprint('SIGNAL_READY', flush=True)\ntime.sleep(60)"),
            extra=('-R', '39093:127.0.0.1:9'), timeout=30, tick=stop_tick)
    assert stopped[0]
    with socket.socket() as conn:
        assert conn.connect_ex(('127.0.0.1', 2489)) != 0, 'orphan after SIGTERM'
    checked("import socket\nwith socket.socket() as listener:\n    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)\n    listener.bind(('127.0.0.1', 39093))")
    if MODE == "ssh":
        code, output = session("exit 37")
        assert code == 37, (code, output)
    # Both transports use the same normalized SSH connection options.
    config = subprocess.check_output([SSH, '-G', '-F', CONFIG, HOST],
                                     text=True, timeout=60, stdin=subprocess.DEVNULL)
    resolved = {}
    for line in config.splitlines():
        key, _, value = line.partition(' ')
        resolved.setdefault(key, value)
    code, output = terminal([SLUSH, '--transport', MODE,
                             '-p', resolved['port'], '-i', resolved['identityfile'], '-F', CONFIG,
                             resolved['user'] + '@' + resolved['hostname'],
                             "printf 'OPTIONS_%s\\n' PASS"], 90)
    assert code == 0 and 'OPTIONS_PASS' in output, output
    # A real agent must sign after mosh's bootstrap SSH has exited.
    agent_key = ROOT / 'agent-key'
    subprocess.run(['ssh-keygen', '-q', '-t', 'ed25519', '-f', str(agent_key), '-N', ''],
                   check=True, timeout=30, stdin=subprocess.DEVNULL)
    agent_socket = ROOT / 'agent.sock'
    agent = subprocess.Popen(['ssh-agent', '-D', '-a', str(agent_socket)],
                             stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                             stderr=subprocess.DEVNULL)
    old_socket = ENV.get('SSH_AUTH_SOCK')
    try:
        deadline = time.monotonic() + 10
        while not agent_socket.exists() and time.monotonic() < deadline:
            assert agent.poll() is None, 'fixture ssh-agent exited'
            time.sleep(0.05)
        assert agent_socket.exists(), 'fixture ssh-agent did not start'
        ENV['SSH_AUTH_SOCK'] = str(agent_socket)
        subprocess.run(['ssh-add', str(agent_key)], env=ENV, check=True,
                       timeout=30, stdin=subprocess.DEVNULL)
        agent_config = ROOT / 'agent-config'
        agent_config.write_text(Path(CONFIG).read_text() + '\nHost *\n  ForwardAgent yes\n')
        code, output = terminal([SLUSH, 'validate', '--transport', MODE, '-A', '-F', str(agent_config), HOST], 120)
        assert code == 0 and 'ok - SSH agent forwarding' in output, output
        code, output = session(python_command(
            'import os\nassert not os.environ.get("SSH_AUTH_SOCK")\nprint("AGENT_DISABLED")'),
            extra=('-F', str(agent_config)))
        assert code == 0 and 'AGENT_DISABLED' in output, output
        state = '/tmp/' + ROOT.name + '-agent-state'
        public_key = agent_key.with_suffix('.pub').read_text()
        script = ('import os, pathlib, subprocess, sys, tempfile\n'
                  'print("AGENT_WAIT", flush=True)\n'
                  'assert sys.stdin.readline().strip() == "continue"\n'
                  'with tempfile.TemporaryDirectory(prefix="slush-agent-") as folder:\n'
                  '    key = pathlib.Path(folder) / "key.pub"\n'
                  '    key.write_text(' + repr(public_key) + ')\n'
                  '    subprocess.run(["ssh-add", "-T", str(key)], check=True, timeout=10)\n'
                  'pathlib.Path(' + repr(state) + ').write_text(os.environ["SSH_AUTH_SOCK"])\n'
                  'print("AGENT_PASS", flush=True)\n')
        (ROOT / 'bootstrap-pids').unlink(missing_ok=True)
        released = False

        def after_bootstrap(output, _pid):
            global released
            if released or b'AGENT_WAIT' not in output:
                return
            if MODE == 'mosh':
                records = ROOT / 'bootstrap-pids'
                if not records.exists():
                    return
                for bootstrap in records.read_text().splitlines():
                    try:
                        os.kill(int(bootstrap), 0)
                        return
                    except ProcessLookupError:
                        pass
            released = True
            return b'continue\n'

        code, output = terminal([SLUSH, '--transport', MODE, '-A', '-F', str(agent_config),
                                 HOST, python_command(script)], 90, tick=after_bootstrap)
        assert released and code == 0 and 'AGENT_PASS' in output, output
        cleanup = ('import pathlib, time\nstate = pathlib.Path(' + repr(state) + ')\n'
                   'socket = pathlib.Path(state.read_text())\nend = time.monotonic() + 5\n'
                   'while socket.exists() and time.monotonic() < end: time.sleep(0.05)\n'
                   'assert not socket.exists(), "orphaned remote agent socket"\nstate.unlink()')
        subprocess.run([SSH, '-F', CONFIG, HOST, python_command(cleanup)],
                       check=True, timeout=30, stdin=subprocess.DEVNULL)
        ENV['SSH_AUTH_SOCK'] = str(ROOT / 'missing-agent.sock')
        code, output = session('true', extra=('-A',))
        assert code != 0 and 'local ssh agent is unavailable' in output.lower(), output
    finally:
        if old_socket is None:
            ENV.pop('SSH_AUTH_SOCK', None)
        else:
            ENV['SSH_AUTH_SOCK'] = old_socket
        agent.terminate()
        try:
            agent.wait(timeout=5)
        except subprocess.TimeoutExpired:
            agent.kill()
            agent.wait(timeout=5)
    # A no-command session must preserve forwarding through login startup.
    argv = [SLUSH, '--transport', MODE, HOST]
    code, output = terminal(argv, 60, shell_input=(
        'test "$SLUSH" = 1 && printf interactive-payload | pbcopy && '
        'test "$(pbpaste)" = interactive-payload && '
        'printf "INTERACTIVE_%s\\n" PASS; exit\n'))
    assert code == 0 and 'INTERACTIVE_PASS' in output, output
    assert (ROOT / 'clipboard').read_bytes() == b'interactive-payload'
    # Zsh must retain user startup files while restoring shim priority after
    # a login profile resets PATH. This isolated account really uses zsh.
    dotdir = '/tmp/' + ROOT.name + '-zsh'
    prepare = '''import pathlib, shlex
print('start - zsh fixture preparation', flush=True)
root = pathlib.Path(__DOTDIR__)
root.mkdir()
(root / '.zshenv').write_text('export E2E_PROFILE=seen\\n')
(root / '.zprofile').write_text('export E2E_LOGIN_PROFILE=seen PATH=/usr/bin:/bin\\n')
(root / '.zshrc').write_text('export E2E_RC=seen\\n')
(root / '.zlogin').write_text('export E2E_LOGIN=seen PATH=/usr/bin:/bin\\n')
(pathlib.Path.home() / '.zshenv').write_text('export ZDOTDIR=' + shlex.quote(str(root)) + '\\nsource "$ZDOTDIR/.zshenv"\\n')
print('finish - zsh fixture preparation', flush=True)
'''.replace('__DOTDIR__', repr(dotdir))
    zsh_host = HOST + '-zsh'
    # Resolve the interpreter itself, not macOS's /usr/bin/python3 launcher.
    python = subprocess.check_output([SSH, '-F', CONFIG, HOST,
                                      "python3 -c 'import sys; print(sys.executable)'"],
                                     text=True, timeout=60, stdin=subprocess.DEVNULL).strip()
    print('info - fixture interpreter', python, flush=True)
    subprocess.run([SSH, '-F', CONFIG, zsh_host,
                    shlex.quote(python) + ' -c ' + shlex.quote(prepare)],
                   check=True, timeout=60, stdin=subprocess.DEVNULL)
    argv = [SLUSH, '--transport', MODE, zsh_host]
    code, output = terminal(argv, 90, shell_input=(
        'test "$SLUSH" = 1 && test "$E2E_PROFILE" = seen && '
        'test "$E2E_LOGIN_PROFILE" = seen && test "$E2E_RC" = seen && '
        'test "$E2E_LOGIN" = seen && '
        'printf zsh-payload | pbcopy && test "$(pbpaste)" = zsh-payload && '
        'printf "ZSH_%s\\n" PASS; exit\n'))
    assert code == 0 and 'ZSH_PASS' in output, output
    assert (ROOT / 'clipboard').read_bytes() == b'zsh-payload'
    # Repeat against every Linux native backend selection path.
    if sys.platform != "darwin":
        ENV.pop("WAYLAND_DISPLAY", None)
        checked("import subprocess\nsubprocess.run(['pbcopy'], input=b'x11', check=True)\nassert subprocess.check_output(['pbpaste']) == b'x11'")
        (BIN / "xclip").unlink()
        checked("import subprocess\nsubprocess.run(['pbcopy'], input=b'xsel', check=True)\nassert subprocess.check_output(['pbpaste']) == b'xsel'")
    print("PASS: " + sys.platform + " client -> " + HOST + " via " + MODE, flush=True)
finally:
    # Keep logs on failure for CI diagnostics; no HOME or dotfile changes.
    print("Fixtures and logs: " + str(ROOT), flush=True)
