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
AllowUsers $user
AllowTcpForwarding yes
SetEnv XDG_CACHE_HOME=$root/remote-cache
EOF
cat > "$root/ssh_config" <<EOF
Host native
  HostName 127.0.0.1
  Port 2222
  User $user
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
  docker exec slush-e2e bash -ec 'apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq openssh-server python3 mosh; mkdir -p /run/sshd; passwd -d root'
  docker exec slush-e2e /usr/sbin/sshd -f "$root/sshd_config" -E "$root/sshd.log"
else
  sudo /usr/sbin/sshd -f "$root/sshd_config" -E "$root/sshd.log"
fi
for i in {1..30}; do
  if /usr/bin/ssh -F "$root/ssh_config" native true; then exit 0; fi
  sleep 2
done
cat "$root/sshd.log"
exit 1
