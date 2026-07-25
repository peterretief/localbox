#!/bin/sh
set -eu

PI_HOST=${PI_HOST:-${1:-192.168.0.7}}
PI_USER=${PI_USER:-root}
PI_HOSTNAME=${PI_HOSTNAME:-localbox-pi}
AUTHORIZED_KEYS=${AUTHORIZED_KEYS:-}
IFACES=${IFACES:-eth0 end0}
RESCUE_IP=${RESCUE_IP:-192.168.0.250/24}

if [ -z "$AUTHORIZED_KEYS" ]; then
    if [ -f "$HOME/.ssh/id_ed25519.pub" ]; then
        AUTHORIZED_KEYS=$HOME/.ssh/id_ed25519.pub
    elif [ -f "$HOME/.ssh/id_rsa.pub" ]; then
        AUTHORIZED_KEYS=$HOME/.ssh/id_rsa.pub
    fi
fi

SSH_OPTS=${SSH_OPTS:-"-o PreferredAuthentications=publickey,none -o PubkeyAuthentication=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase2_known_hosts -o ConnectTimeout=10"}
VERIFY_SSH_OPTS=${VERIFY_SSH_OPTS:-"-o BatchMode=yes -o PreferredAuthentications=publickey -o PubkeyAuthentication=yes -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase2_verify_known_hosts -o ConnectTimeout=10"}

quote_sh() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

PUBKEY=
if [ -n "$AUTHORIZED_KEYS" ]; then
    [ -f "$AUTHORIZED_KEYS" ] || {
        echo "ERROR: AUTHORIZED_KEYS file not found: $AUTHORIZED_KEYS" >&2
        exit 1
    }
    PUBKEY=$(sed -n '1p' "$AUTHORIZED_KEYS")
fi

echo "Phase 2 target: $PI_USER@$PI_HOST"
echo "Hostname:       $PI_HOSTNAME"
echo "DHCP ifaces:    $IFACES"
echo "Rescue IP:      ${RESCUE_IP:-disabled}"
if [ -n "$PUBKEY" ]; then
    echo "SSH key:        $AUTHORIZED_KEYS"
else
    echo "SSH key:        none; keeping no-auth root SSH"
fi

ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "PI_HOSTNAME=$(quote_sh "$PI_HOSTNAME") PUBKEY=$(quote_sh "$PUBKEY") IFACES=$(quote_sh "$IFACES") RESCUE_IP=$(quote_sh "$RESCUE_IP") sh -s" <<'REMOTE'
set -eu

write_interfaces() {
    output=$1
    {
        printf '%s\n' 'auto lo'
        printf '%s\n\n' 'iface lo inet loopback'
        for iface in $IFACES; do
            [ -n "$iface" ] || continue
            printf 'auto %s\n' "$iface"
            printf 'iface %s inet dhcp\n\n' "$iface"
            if [ -n "$RESCUE_IP" ]; then
                printf 'iface %s inet static\n' "$iface"
                printf '    address %s\n\n' "$RESCUE_IP"
            fi
        done
    } > "$output"
}

echo ">> Finding boot APK cache..."
APK_CACHE=
for dir in /media/mmcblk0p1/apks /boot/apks /media/*/apks; do
    if [ -d "$dir/armv7" ]; then
        APK_CACHE=$dir
        break
    fi
done
[ -n "$APK_CACHE" ] || {
    echo "ERROR: could not find boot APK cache under /media or /boot" >&2
    exit 1
}
echo "   $APK_CACHE"

echo ">> Installing persistent SSH server from boot APK cache..."
apk add --force-non-repository \
    "$APK_CACHE"/armv7/openssh-keygen-*.apk \
    "$APK_CACHE"/armv7/openssh-server-common-*.apk \
    "$APK_CACHE"/armv7/openssh-server-common-openrc-*.apk \
    "$APK_CACHE"/armv7/openssh-server-*.apk
ssh-keygen -A

echo ">> Setting hostname..."
printf '%s\n' "$PI_HOSTNAME" > /etc/hostname
hostname "$PI_HOSTNAME" 2>/dev/null || true

echo ">> Writing wired DHCP networking..."
mkdir -p /etc/network
write_interfaces /etc/network/interfaces
rc-update add networking default 2>/dev/null || true

echo ">> Configuring SSH login..."
mkdir -p /etc/ssh/sshd_config.d
if [ -n "$PUBKEY" ]; then
    mkdir -p /root/.ssh
    chmod 700 /root/.ssh
    printf '%s\n' "$PUBKEY" > /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys
    cat > /etc/ssh/sshd_config.d/20-localbox-phase2.conf <<'EOF'
PermitRootLogin yes
PubkeyAuthentication yes
AuthorizedKeysFile .ssh/authorized_keys
PasswordAuthentication no
KbdInteractiveAuthentication no
AuthenticationMethods publickey
PermitEmptyPasswords no
EOF
else
    cat > /etc/ssh/sshd_config.d/20-localbox-phase2.conf <<'EOF'
PermitRootLogin yes
AuthenticationMethods none
PermitEmptyPasswords yes
EOF
fi
rc-update add sshd default 2>/dev/null || true
rc-service sshd restart

echo ">> Persisting diskless Alpine overlay..."
echo ">> Configuring lbu boot media..."
if grep -q '^# LBU_MEDIA=' /etc/lbu/lbu.conf; then
    sed -i 's/^# LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf
elif grep -q '^LBU_MEDIA=' /etc/lbu/lbu.conf; then
    sed -i 's/^LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf
else
    printf '%s\n' 'LBU_MEDIA=mmcblk0p1' >> /etc/lbu/lbu.conf
fi
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/hostname
lbu add /etc/network/interfaces
lbu add /etc/ssh
lbu add /etc/ssh/ssh_host_*_key /etc/ssh/ssh_host_*_key.pub 2>/dev/null || true
lbu add /root/.ssh 2>/dev/null || true
lbu add /etc/lbu/lbu.conf
lbu add /etc/apk/world 2>/dev/null || true
lbu add /etc/runlevels/default/networking 2>/dev/null || true
lbu add /etc/runlevels/default/sshd 2>/dev/null || true
lbu commit -d

echo ">> Verifying SSH was written to the Alpine overlay..."
APKOVL=$(ls -t /media/mmcblk0p1/*.apkovl.tar.gz 2>/dev/null | sed -n '1p')
[ -n "$APKOVL" ] || {
    echo "ERROR: no apkovl archive found on /media/mmcblk0p1 after lbu commit" >&2
    exit 1
}
tar -tzf "$APKOVL" | grep -Eq '(^|/)etc/ssh/ssh_host_.*_key$' || {
    echo "ERROR: SSH host keys are not present in $APKOVL" >&2
    exit 1
}
if [ -n "$PUBKEY" ]; then
    tar -tzf "$APKOVL" | grep -Eq '^(\./)?root/\.ssh/authorized_keys$' || {
        echo "ERROR: root authorized_keys is not present in $APKOVL" >&2
        exit 1
    }
fi
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo ">> Phase 2 complete."
hostname
ip -4 addr show eth0 2>/dev/null || true
rc-service sshd status || true
REMOTE

echo
if [ -n "$PUBKEY" ]; then
    echo ">> Verifying fresh key-only SSH login..."
    if ! ssh $VERIFY_SSH_OPTS "$PI_USER@$PI_HOST" 'hostname; whoami' >/tmp/localbox_phase2_verify.out 2>/tmp/localbox_phase2_verify.err; then
        echo "ERROR: key-only SSH verification failed after Phase 2." >&2
        echo "Phase 2 is not complete. Do not continue to Phase 3." >&2
        sed 's/^/  /' /tmp/localbox_phase2_verify.err >&2 || true
        exit 30
    fi
    sed 's/^/  /' /tmp/localbox_phase2_verify.out
fi

echo "Phase 2 finished."
if [ -n "$PUBKEY" ]; then
    echo "Next login:"
    echo "  ssh $PI_USER@$PI_HOST"
else
    echo "Next login:"
    echo "  ssh -o PreferredAuthentications=none -o PubkeyAuthentication=no $PI_USER@$PI_HOST"
fi
