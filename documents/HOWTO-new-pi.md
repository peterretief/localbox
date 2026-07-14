# LocalBox New Pi HOWTO

This is the clean path for building another LocalBox Pi from an existing setup.

## Optional: First-Boot SD Card Config

For repeated LocalBox installs, keep the SD card image generic and put per-box settings in one editable file on the boot partition.

Workflow:

```text
Dev PC
  flash SD card
  mount boot partition
  edit localbox-firstboot.conf
  unmount SD card

Pi first boot
  reads localbox-firstboot.conf
  sets hostname, VPN, tunnel, domain, users, and secrets
  provisions LocalBox
  persists config with lbu
  disables the first-boot service
```

Example boot partition file:

```ini
# /boot/localbox-firstboot.conf

hostname=localbox-a

# Optional root access setup.
# Password login for root is disabled by default over SSH; use an SSH key.
ssh_authorized_key=ssh-ed25519 AAAA... peter@devmachine
# root_password=temporary-console-password

headscale_login_server=https://headscale.example.com
tailscale_authkey=tskey-auth-example

domain=example.co.za
mailbox=info@example.co.za:ChangeMe123!
admin=info@example.co.za
box_admin=info@example.co.za

cloudflare_tunnel_token=your-cloudflare-tunnel-token
ingest_secret=change-this-secret

mesh_box_name=pi-a
```

The base image should include:

```text
/usr/local/sbin/localbox-firstboot
/etc/init.d/localbox-firstboot
```

The first-boot script should:

1. Read `/boot/localbox-firstboot.conf`.
2. Set the hostname.
3. Join Tailscale or Headscale if configured.
4. Write LocalBox config files under `/etc/localbox`.
5. Run `localbox-provision-template` or equivalent provisioning.
6. Install/start LocalBox services.
7. Persist required files with `lbu add` and `lbu commit -d`.
8. Disable itself so it does not rerun on every boot.

Do not edit the whole Linux filesystem from the dev PC. Keep the image generic and customize it through the boot partition config file.

## Automated Alpine Boot Card Prep

Mount the Alpine Raspberry Pi boot partition read-write on the dev machine. The boot partition is the FAT partition containing `*.apkovl.tar.gz`, `.alpine-release`, `config.txt`, and `cmdline.txt`.

Example:

```sh
sudo mkdir -p /mnt/piboot
sudo fsck.vfat -a /dev/sdX1
sudo mount -o rw,uid=$(id -u),gid=$(id -g) /dev/sdX1 /mnt/piboot
touch /mnt/piboot/write-test && rm /mnt/piboot/write-test
```

Prepare the boot card and inject the first-boot service:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
BOOT_MOUNT=/mnt/piboot \
FIRSTBOOT_CONFIG=/path/to/localbox-firstboot.conf \
./scripts/prepare-pi-boot-card.sh
sync
sudo umount /mnt/piboot
```

This script:

- backs up the existing `*.apkovl.tar.gz`;
- adds persistent DHCP for `eth0` and `end0`;
- enables `networking`, `sshd`, `chronyd`, and `localbox-firstboot`;
- installs `/usr/local/sbin/localbox-firstboot` into the Alpine overlay;
- copies `localbox-firstboot.conf` onto the boot partition when provided.

After boot, the Pi first-boot script reads `/media/mmcblk0p1/localbox-firstboot.conf`, repairs network persistence, starts core services, optionally joins Headscale/Tailscale, provisions domains, persists with `lbu commit -d`, and disables itself.

## Save And Restore A Base Alpine SD Image

After preparing a known-good Alpine Pi boot card, save a compressed full-card image:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
BOOT_MOUNT=/mnt/piboot OUT_DIR=./images ./scripts/save-alpine-os-image.sh
```

This writes:

```text
images/localbox-alpine-rpi-YYYYMMDD-HHMMSS.img.gz
images/localbox-alpine-rpi-YYYYMMDD-HHMMSS.img.gz.sha256
images/localbox-alpine-rpi-YYYYMMDD-HHMMSS.txt
```

To write that saved Alpine OS image to a new SD card, identify the whole target device with `lsblk`, unmount any partitions, and run:

```sh
sudo umount /dev/sdX1 2>/dev/null || true
sudo CONFIRM_WRITE_DEVICE=/dev/sdX \
  ./scripts/write-alpine-os-image.sh ./images/localbox-alpine-rpi-YYYYMMDD-HHMMSS.img.gz /dev/sdX
```

Use the whole disk device, for example `/dev/sdf` or `/dev/mmcblk0`, not a partition like `/dev/sdf1`.

## Format The LocalBox Data USB

LocalBox uses a separate data disk mounted at `/mnt/data`. To erase and format a USB flash drive as ext4:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
./scripts/device-inventory.sh
sudo CONFIRM_FORMAT_DEVICE=/dev/sdX \
  ./scripts/format-localbox-data-usb.sh /dev/sdX
```

The script prints the `fstab` line to use on the Pi:

```text
UUID=... /mnt/data ext4 defaults 0 2
```

Use the whole disk device, for example `/dev/sdg`, not a partition like `/dev/sdg1`.

## 1. Back Up An Existing Pi

On the existing Pi:

```sh
sudo localbox-create-backup
```

Include domains, sites, and mail data:

```sh
sudo INCLUDE_MAIL_DATA=1 localbox-create-backup
```

Backups are written to:

```text
/mnt/data/localbox/backups/
```

Copy the backup to the dev machine:

```sh
mkdir -p ~/LocalBox-backups
scp peter@100.64.0.8:/mnt/data/localbox/backups/*.tar.gz ~/LocalBox-backups/
```

Treat these archives as secrets. They can contain SMTP credentials, Tailscale state, SSH keys, and password hashes.

## 2. Prepare A Fresh Pi

Install Alpine and get basic SSH access working.

From the dev machine, deploy LocalBox:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
./scripts/deploy.sh
```

This installs the LocalBox binaries, templates, and optional admin tools.

## 3. Join Headscale

On the Pi:

```sh
sudo HEADSCALE_LOGIN_SERVER=https://your-headscale.example.com localbox-enable-tailscale-persistence
```

With a preauth key:

```sh
sudo HEADSCALE_LOGIN_SERVER=https://your-headscale.example.com \
TAILSCALE_AUTHKEY=your-preauth-key \
localbox-enable-tailscale-persistence
```

Verify:

```sh
tailscale status
tailscale ip
```

Persist if needed:

```sh
sudo lbu add /var/lib/tailscale
sudo lbu commit -d
```

## 4. Provision Domains, Mailboxes, And Email Routing

Keep Cloudflare account changes on the dev machine. The Pi should not be the place where a new Worker, MX records, or Email Routing rules are created.

On the dev machine, create a private Cloudflare config:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
cp cloudflare.conf.example cloudflare.conf
chmod 600 cloudflare.conf
vi cloudflare.conf
```

Required values:

```ini
enabled=true
api_url=https://api.cloudflare.com/client/v4
account_id=your-cloudflare-account-id
api_token=your-cloudflare-api-token
```

Create a domain config on the dev machine:

```ini
domain=example.com
mailbox=info@example.com:ChangeMe123!
mailbox=peter@example.com:ChangeMe123!
admin=peter@example.com
box_admin=peter@example.com

# Cloudflare Email Routing sends matching mail to this Worker.
# Use a unique Worker name per Pi because each Worker has its own ingest URL.
# The existing/current Pi may use alpine; a new Pi can use alpine-n, alpine-b, etc.
email_worker=alpine-n

# Each Pi needs a stable ingest host.
# Examples: a.diode.co.za, b.diode.co.za, c.diode.co.za ... n.diode.co.za
ingest_host=n.diode.co.za
ingest_tunnel_id=your-cloudflare-tunnel-uuid

email_ingest_secret=change-this-secret
email_route=info@example.com
email_route=peter@example.com
```

Create or update the Cloudflare Worker and Email Routing rules from the dev machine:

```sh
WORKER_DEPLOY_METHOD=wrangler ./scripts/setup-cloudflare-email-worker.sh /path/to/domain.conf
```

This creates or updates the `ingest_host` Cloudflare CNAME, uses Wrangler on the dev machine to deploy `workers/localbox-email-worker.mjs`, sets the Worker ingest URL and secret, enables Cloudflare Email Routing DNS for the zone, and creates or updates the listed `email_route=` addresses. Wrangler does not need to be installed on the Pi.

Then provision the domain on the Pi from the same config:

```sh
PI_HOST=192.168.0.6 ./scripts/deploy-domain-from-config.sh /path/to/domain.conf
```

This creates domain config, mailbox directories, default site files, users, admin roles, and persists with `lbu`.

If working directly on the Pi, copy the example template:

```sh
sudo cp /etc/localbox/examples/localbox-template.conf.example /root/newbox.conf
sudo vi /root/newbox.conf
```

Template format:

```ini
domain=example.com
mailbox=info@example.com:ChangeMe123!
mailbox=peter@example.com:ChangeMe123!
admin=peter@example.com
box_admin=peter@example.com

# Optional Cloudflare Email Routing metadata:
email_worker=alpine-n
ingest_host=n.diode.co.za
ingest_tunnel_id=your-cloudflare-tunnel-uuid
email_ingest_secret=change-this-secret
email_route=info@example.com
email_route=peter@example.com
```

Run provisioning:

```sh
sudo localbox-provision-template /root/newbox.conf
```

Direct Pi provisioning does not create the Cloudflare Worker. Run `setup-cloudflare-email-worker.sh` from the dev machine for that.

For external forwarding instead of a Worker, use:

```ini
email_forward_to=owner@example.net
email_route=info@example.com
```

## 5. Optional Thunderbird/Outlook Access

Enable mail-client access:

```sh
sudo localbox-enable-mail-client-access
```

Use the VPN IP from `tailscale ip`.

Thunderbird settings:

```text
Incoming IMAP:
Server: 100.64.x.y
Port: 143
Security: None
Authentication: Normal password
Username: user@example.com

Outgoing SMTP:
Server: 100.64.x.y
Port: 587
Security: None
Authentication: Normal password
Username: user@example.com
```

Only use this over the Headscale VPN.

## 6. Verify Services

```sh
sudo rc-service mail-ingest status
sudo rc-service contact-service status
sudo rc-service nginx status
sudo rc-service sshd status
sudo rc-service tailscale status 2>/dev/null || sudo rc-service tailscaled status
```

If mail-client access is enabled:

```sh
sudo rc-service dovecot status
sudo rc-service postfix status
```

Test IMAP:

```sh
printf 'a login user@example.com password\r\na logout\r\n' | nc 127.0.0.1 143
```

Expected result includes:

```text
a OK
```

## 7. Persist Changes

After manual config changes:

```sh
sudo lbu add /etc/localbox
sudo lbu add /etc/dovecot 2>/dev/null || true
sudo lbu add /etc/postfix 2>/dev/null || true
sudo lbu add /var/lib/tailscale 2>/dev/null || true
sudo lbu commit -d
```

## Notes

- LocalBox remains the main mail UI.
- IMAP/SMTP is optional for Thunderbird/Outlook users.
- Local delivery stays on the Pi.
- External delivery uses the configured SMTP relay.
- Future private mail routes can be added between Headscale nodes.
