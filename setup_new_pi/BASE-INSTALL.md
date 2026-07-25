# LocalBox Base Pi Install Procedure

Use this procedure to get a fresh Raspberry Pi to a booted, reachable LocalBox base system. Domain-specific mail routing, Brevo, and customer mailbox provisioning come after this base install.

## 0. Prepare Local Config Files

On the development machine:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/setup_new_pi
cp localbox-firstboot.conf.example localbox-firstboot.conf
vi localbox-firstboot.conf
```

Minimum values to set:

```ini
hostname=localbox-a
ssh_authorized_key=ssh-ed25519 AAAA... peter@nix64bit
headscale_login_server=https://your-headscale.example.com
tailscale_authkey=tskey-auth-example
mesh_box_name=pi-a
```

Leave `domain=`, `mailbox=`, `admin=`, and `box_admin=` as example values only if you do not want first boot to provision a real domain yet.

Cloudflare API credentials live one directory up:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
vi cloudflare.conf
```

For `localboxmail.co.za`, the token only needs:

```text
Zone Read
DNS Write
Scope: localboxmail.co.za
```

## 1. Identify Devices

Plug in only the SD card first, then run:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/setup_new_pi
./setup.sh inventory
```

Record the whole-disk path, for example `/dev/sdf`. Then plug in the LocalBox data USB and run inventory again.

Fill this in before writing or formatting anything:

```text
New SD card whole disk:   /dev/____
New SD boot partition:    /dev/____1
Data USB whole disk:      /dev/____
Data USB partition:       /dev/____1
Pi hostname:              __________________
Pi LAN IP after boot:     __________________
Pi Tailscale IP:          __________________
```

Use whole disk devices for destructive operations, for example `/dev/sdf`, not `/dev/sdf1`.

## 2. Build The Alpine SD Boot Card

For a clean rebuild from the Alpine Raspberry Pi tarball, use this path. It erases the SD card, creates a fresh FAT32 boot partition, extracts Alpine, seeds the LocalBox headless overlay, and injects DHCP, SSH, and first boot in one run.

Wired networking is DHCP by default on both `eth0` and `end0`, covering old and new Raspberry Pi interface naming. If Wi-Fi credentials are set in `localbox-firstboot.conf`, `wlan0` is also configured for DHCP.

```sh
./setup.sh inventory
sudo CONFIRM_BOOT_DEVICE=/dev/sdX \
  ./setup.sh fresh-boot-card ./alpine-rpi-3.24.1-armv7.tar.gz /dev/sdX ./localbox-firstboot.conf
```

Use the whole SD card device, for example `/dev/sdf`, not `/dev/sdf1`.

To SSH with the exact matching private key after boot:

```sh
ssh -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes root@PI_LAN_IP
```

The public key line in `localbox-firstboot.conf` must match that private key:

```ini
ssh_authorized_key=ssh-ed25519 AAAA... peter@devmachine
```

## 3. Write A Saved Alpine SD Image

If a known-good LocalBox Alpine image exists:

```sh
sudo umount /dev/sdX* 2>/dev/null || true
sudo CONFIRM_WRITE_DEVICE=/dev/sdX \
  ./setup.sh write-image ../images/localbox-alpine-rpi-YYYYMMDD-HHMMSS.img.gz /dev/sdX
```

Unplug and reinsert the SD card after writing.

If no saved image exists yet, flash the standard Alpine Raspberry Pi image manually, boot it once if needed to create the Alpine overlay, then continue with the boot-card preparation step.

## 4. Mount And Prepare An Existing Boot Partition

Mount the SD card boot partition read-write:

```sh
sudo mkdir -p /mnt/piboot
sudo fsck.vfat -a /dev/sdX1
sudo mount -o rw,uid=$(id -u),gid=$(id -g) /dev/sdX1 /mnt/piboot
touch /mnt/piboot/write-test && rm /mnt/piboot/write-test
```

Inject persistent networking, SSH, and LocalBox first boot:

```sh
BOOT_MOUNT=/mnt/piboot \
FIRSTBOOT_CONFIG=./localbox-firstboot.conf \
./setup.sh prepare-boot
sync
sudo umount /mnt/piboot
```

## 5. Format The Data USB

This erases the whole target USB disk:

```sh
./setup.sh inventory
sudo CONFIRM_FORMAT_DEVICE=/dev/sdY ./setup.sh format-usb /dev/sdY
```

Copy the printed `UUID=... /mnt/data ext4 defaults 0 2` line into your install notes. The first-boot/provisioning path can use that UUID when adding a persistent `/mnt/data` mount.

## 6. First Boot

Insert the prepared SD card and data USB into the Pi. Connect Ethernet, then power on.

Find the Pi:

```sh
nmap -sn 192.168.0.0/24
```

SSH in:

```sh
ssh root@PI_LAN_IP
```

Or force the specific private key:

```sh
ssh -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes root@PI_LAN_IP
```

Check first-boot results:

```sh
hostname
rc-status
ip addr
tailscale status 2>/dev/null || true
tailscale ip 2>/dev/null || true
ls -la /etc/localbox 2>/dev/null || true
mount | grep /mnt/data || true
```

If Tailscale joined successfully, prefer the Tailscale IP for later deployment:

```sh
tailscale ip
```

## 7. Deploy Current LocalBox Code

From the development machine:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
PI_HOST=PI_LAN_OR_TAILSCALE_IP ./scripts/deploy.sh
```

Verify on the Pi:

```sh
sudo rc-service mail-ingest status
sudo rc-service contact-service status
sudo rc-service nginx status
sudo rc-service sshd status
```

Persist any manual changes:

```sh
sudo lbu add /etc/localbox
sudo lbu add /var/lib/tailscale 2>/dev/null || true
sudo lbu commit -d
```

## 8. Base Install Done

At this point the Pi should have:

- persistent networking;
- SSH access;
- optional Headscale/Tailscale access;
- `/mnt/data` available for LocalBox data;
- LocalBox scripts, services, templates, and examples deployed;
- first-boot disabled after successful run.

Next steps are domain-specific:

```sh
cp domain.conf.example domain.conf
vi domain.conf
WORKER_DEPLOY_METHOD=wrangler ./setup.sh cloudflare ./domain.conf
PI_HOST=PI_LAN_OR_TAILSCALE_IP ./setup.sh deploy-domain ./domain.conf
```
