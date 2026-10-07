#!/usr/bin/env bash
# CI-only isolated sshd. All keys/config/cache reside in the supplied /tmp dir.
set -euo pipefail
root=$1
mkdir -p "$root"
ssh-keygen -q -t ed25519 -f "$root/key" -N ''
ssh-keygen -q -t ed25519 -f "$root/hostkey" -N ''
user=$USER
listen=127.0.0.1
if [[ $(uname -s) == Linux ]]; then user=root; listen=0.0.0.0; fi
cat > "$root/sshd_config" <<EOF
Port 2222
ListenAddress $listen
HostKey $root/hostkey
PidFile $root/sshd.pid
AuthorizedKeysFile $root/key.pub
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
UsePAM no
AllowUsers $user slush-zsh
AllowTcpForwarding yes
Match User $user
  SetEnv XDG_CACHE_HOME=$root/remote-cache PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin
Match User slush-zsh
  SetEnv XDG_CACHE_HOME=$root/zsh-cache PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin
EOF
cat > "$root/ssh_config" <<EOF
Host native
  HostName 127.0.0.1
  Port 2222
  User $user
Host native-zsh
  HostName 127.0.0.1
  Port 2222
  User slush-zsh
Host *
  IdentityFile $root/key
  IdentitiesOnly yes
  StrictHostKeyChecking no
  UserKnownHostsFile $root/known_hosts
  BatchMode yes
  ConnectTimeout 5
EOF
if [[ $(uname -s) == Linux ]]; then
  docker run -d --name slush-e2e --publish 127.0.0.1:2222:2222 \
    --publish 127.0.0.1:60000-60010:60000-60010/udp \
    --volume "$root:$root" ubuntu:24.04 sleep infinity
  docker exec slush-e2e bash -ec 'apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq openssh-server python3 mosh zsh; mkdir -p /run/sshd; passwd -d root; useradd -m -d /tmp/slush-zsh-home -s /bin/zsh slush-zsh; passwd -d slush-zsh'
  docker exec slush-e2e bash -ec "mkdir -p '$root/zsh-cache'; chown slush-zsh:slush-zsh '$root/zsh-cache'"
  docker exec slush-e2e /usr/sbin/sshd -f "$root/sshd_config" -E "$root/sshd.log"
else
  sudo dscl . -create /Users/slush-zsh
  sudo dscl . -create /Users/slush-zsh UniqueID 599
  sudo dscl . -create /Users/slush-zsh PrimaryGroupID 20
  sudo dscl . -create /Users/slush-zsh NFSHomeDirectory "$root/zsh-home"
  sudo dscl . -create /Users/slush-zsh UserShell /bin/zsh
  sudo dscl . -passwd /Users/slush-zsh "$(openssl rand -hex 24)"
  mkdir -p "$root/zsh-home" "$root/zsh-cache"
  sudo chown slush-zsh:staff "$root/zsh-home" "$root/zsh-cache"
  sudo /usr/sbin/sshd -f "$root/sshd_config" -E "$root/sshd.log"
fi
for i in {1..30}; do
  if /usr/bin/ssh -F "$root/ssh_config" native true; then exit 0; fi
  sleep 2
done
cat "$root/sshd.log"
exit 1
