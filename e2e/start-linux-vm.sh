#!/usr/bin/env bash
# Real Linux peer on macOS, using software emulation (no nested KVM/HVF).
set -euo pipefail
root=$1
image=noble-server-cloudimg-amd64.img
url=https://cloud-images.ubuntu.com/noble/current
curl --fail --location --retry 3 "$url/$image" -o "$root/$image"
curl --fail --location --retry 3 "$url/SHA256SUMS" -o "$root/SHA256SUMS"
(cd "$root"; grep " [*]$image\$" SHA256SUMS | shasum -a 256 -c -)
qemu-img resize "$root/$image" 8G
mkdir -p "$root/seed"
cat > "$root/seed/meta-data" <<EOF
instance-id: slush-e2e
local-hostname: slush-linux
EOF
cat > "$root/seed/user-data" <<EOF
#cloud-config
users:
  - name: e2e
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - $(cat "$root/key.pub")
packages: [mosh, python3]
write_files:
  - path: /etc/ssh/sshd_config.d/slush-e2e.conf
    content: |
      SetEnv XDG_CACHE_HOME=/tmp/slush-remote-cache
EOF
hdiutil makehybrid -iso -joliet -default-volume-name cidata \
  -o "$root/seed.iso" "$root/seed"
net="user,id=net,hostfwd=tcp:127.0.0.1:2223-:22"
for port in {60000..60010}; do net="$net,hostfwd=udp:127.0.0.1:$port-:$port"; done
qemu-system-x86_64 -accel tcg -machine q35 -cpu max -smp 2 -m 2048 \
  -drive "file=$root/$image,format=qcow2,if=virtio" \
  -drive "file=$root/seed.iso,format=raw,media=cdrom" \
  -netdev "$net" -device virtio-net-pci,netdev=net \
  -display none -serial "file:$root/vm-console.log" \
  -daemonize -pidfile "$root/qemu.pid"
cat >> "$root/ssh_config" <<EOF
Host linux
  HostName 127.0.0.1
  Port 2223
  User e2e
EOF
# Host * appears earlier; it sets only common auth options, not the port/user.
for i in {1..180}; do
  if /usr/bin/ssh -F "$root/ssh_config" linux true; then
    /usr/bin/ssh -F "$root/ssh_config" linux 'sudo cloud-init status --wait; command -v mosh-server; command -v python3'
    exit 0
  fi
  sleep 5
done
cat "$root/vm-console.log"
exit 1
