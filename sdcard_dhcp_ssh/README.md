# SD Card DHCP + SSH

Fresh Raspberry Pi Alpine SD-card writer.

Goal:

- format the SD card
- extract the Alpine Raspberry Pi tarball
- boot on wired DHCP
- also expose a rescue IP alias at `192.168.0.250/24`
- allow SSH login through the headless Alpine bootstrap

It does not install LocalBox or run first-boot provisioning.

## Device Identity First

Before writing a new SD card or assigning domains/tunnels, create a device identity in the development-machine inventory:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
./localbox-device-create.sh
```

This allocates:

```text
device_name
asset_tag / barcode label
tailscale_name
expected Cloudflare tunnel name
expected Worker name
```

Read [DEVICE_MODEL.md](DEVICE_MODEL.md) before adding Headscale or Cloudflare automation. The key rule is: every Pi gets its own Cloudflare tunnel, and routes are only published when that tunnel is recorded against the same `DEVICE_NAME`.

## Clean Setup Sequence

For a fresh Pi/card, use this order:

```text
0. Create or choose the device identity and attach the barcode label.
1. Write the SD card and boot on wired DHCP.
2. Persist SSH.
3. Install package trust, time sync, and base tools.
4. Format/mount the USB data stick.
5. Create the LocalBox base layout.
6. Install LocalBox binaries and OpenRC services.
7. Install Dovecot/Postfix.
8. Create the first domain and admin mailbox.
9a. Create/connect the Pi's dedicated Cloudflare tunnel, then record it.
9. Publish the public hostname only after validate-publish passes.
9e. Create/update Cloudflare Email Routing catch-all to the per-device Worker.
9f. Add the sender domain to Brevo and publish Brevo DNS authentication records.
10. Join Headscale/Tailscale and advertise explicit routes when needed.
11. Run the reboot audit, reboot the Pi, then run the reboot audit again.
```

The public Cloudflare phase is intentionally late. A domain can exist before the tunnel is ready, but it must not be pointed at another Pi's tunnel.

A Pi is not considered installed until Phase 11 passes both before and after a real power reboot.

For fresh-card testing, use the reboot harness to prove persistence after each phase:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 \
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
LOCALBOX_ADMIN_RECOVERY_EMAIL=peter@example.com \
DATA_DEVICE=/dev/sda \
CONFIRM_DATA_DEVICE=/dev/sda \
./phase-reboot-test.sh phase2 phase3 phase4:format phase5 phase6 phase7 phase8 phase9a
```

The harness runs one phase, reboots the Pi, waits for SSH to return, and then runs a phase-specific persistence check before continuing. It intentionally does not run `phase9-cloudflare-route.sh`, because public Cloudflare route publishing happens on the development machine rather than on the Pi. Publish DNS/tunnel routes only after the Pi-side reboot checks pass.

## Local Inventory

The installer keeps a development-machine inventory in:

```text
sdcard_dhcp_ssh/state/localbox-inventory.sqlite
```

This file is local to the development machine. It is not copied to the Pi.

Use:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
./localbox-inventory.sh list devices
./localbox-inventory.sh list domains
./localbox-inventory.sh list tunnels
./localbox-inventory.sh list routes
./localbox-inventory.sh show-device localbox-001
```

Each physical Pi should get a stable asset tag and barcode label:

```sh
./localbox-inventory.sh upsert-device localbox-001 asset_tag=LBX-0001
./localbox-label.sh localbox-001
./localbox-inventory.sh lookup-tag LBX-0001
```

Reserve and print a batch of future labels:

```sh
./localbox-label-batch.sh 10
```

This creates placeholder devices like `localbox-002` and labels like `LBX-0002`.

Labels are generated locally as SVG files:

```text
sdcard_dhcp_ssh/state/labels/
```

Allocate sequential names before creating new resources:

```sh
./localbox-inventory.sh next-name device localbox
./localbox-inventory.sh next-name tunnel localbox-tunnel
./localbox-inventory.sh next-name worker localbox-worker
```

Record manually when needed:

```sh
./localbox-inventory.sh upsert-device localbox-001 hostname=localbox-pi pi_host=192.168.0.7
./localbox-inventory.sh upsert-domain localboxtest.co.za device_name=localbox-001 admin_email=admin@localboxtest.co.za
./localbox-inventory.sh upsert-tunnel localbox-001-tunnel tunnel_id=... device_name=localbox-001
./localbox-inventory.sh upsert-route mail.localboxtest.co.za domain=localboxtest.co.za tunnel_name=localbox-001-tunnel service=http://localhost:8080
```

Before a risky/public phase, validate the ownership:

```sh
./localbox-inventory.sh validate-device localbox-001
./localbox-inventory.sh validate-publish localbox-001 localboxtest.co.za localbox-001-tunnel
```

## Port Convention

Use the same local services on every Pi:

```text
80    nginx local front door
8080  LocalBox dashboard/app/ingest
1323  ALPS webmail
8082  Filebrowser/files
```

Cloudflare tunnel routes should normally be:

```text
localboxtest.co.za       -> http://localhost:80
www.localboxtest.co.za   -> http://localhost:80
mail.localboxtest.co.za  -> http://localhost:1323
files.localboxtest.co.za -> http://localhost:8082
```

## Headscale API

The development machine can query Headscale through the REST API using:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
cp .env.example .env
```

Set the real API key in `.env`:

```sh
HEADSCALE_URL=https://headscale.withcare.co.za
HEADSCALE_API_KEY=hskey-api-...
```

Use:

```sh
./headscale-api.sh list-users
./headscale-api.sh list-nodes
./headscale-api.sh routes
./headscale-api.sh get-node localmail
./headscale-api.sh list-preauthkeys
./headscale-api.sh list-apikeys
```

Create a preauth key for a new Pi:

```sh
./headscale-api.sh create-preauthkey user=2 expiration=24h reusable=false
```

Use the returned `hskey-auth-...` once on the Pi with `tailscale up --login-server https://headscale.withcare.co.za --authkey ...`.

## Phase 10: Headscale Routes

Join a Pi/client to Headscale and advertise explicit subnet routes:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.3 \
DEVICE_NAME=localbox-001 \
TAILSCALE_HOSTNAME=localbox-001 \
HEADSCALE_CREATE_AUTHKEY=true \
HEADSCALE_APPROVE_ROUTES=false \
./phase-reboot-test.sh phase10
```

If `TAILSCALE_ADVERTISE_ROUTES` is omitted, Phase 10 advertises the current `PI_HOST` as a single-host route, for example `192.168.0.3/32`.

By default Phase 10 creates the one-use auth key by SSHing to the Headscale server alias `headscale` and running the Headscale CLI with sudo. Override if needed:

```sh
HEADSCALE_SSH_HOST=headscale
HEADSCALE_PREAUTHKEY_METHOD=ssh
```

Use `HEADSCALE_PREAUTHKEY_METHOD=api` only when the local `.env` API token has permission to create preauth keys.

For a LAN subnet-router test, use:

```sh
TAILSCALE_ADVERTISE_ROUTES=192.168.0.0/24
```

Approve routes only after checking that the advertised prefix is correct:

```sh
./headscale-api.sh routes
./headscale-api.sh approve-routes node=NODE_ID routes=192.168.0.0/24
```

To test HA failover, advertise the same prefix from a second suitable node and approve that route too. Headscale/Tailscale can then use another approved subnet router when one goes offline.

## Write The Card

Insert the SD card and identify the whole disk:

```sh
lsblk -o NAME,SIZE,MODEL,SERIAL,RM,RO,MOUNTPOINTS
```

Run the writer once. It will refuse to erase and print the exact confirmation command:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
./write-sdcard.sh ../setup_new_pi/alpine-rpi-3.24.1-armv7.tar.gz /dev/sdX
```

Then run the printed `CONFIRM_BOOT_DEVICE=... CONFIRM_BOOT_SERIAL=...` command.

## SSH

Optional, recommended:

```sh
AUTHORIZED_KEYS=~/.ssh/id_rsa.pub ./write-sdcard.sh ../setup_new_pi/alpine-rpi-3.24.1-armv7.tar.gz /dev/sdX
```

Without `AUTHORIZED_KEYS`, the headless bootstrap enables temporary root SSH with no authentication. Use:

```sh
ssh -o PreferredAuthentications=none root@IP_ADDRESS
```

With `AUTHORIZED_KEYS`, use:

```sh
ssh root@IP_ADDRESS
```

The Pi should request DHCP on `eth0` and `end0`.

## Phase 2: Persist SSH

After you can SSH into the Pi, run phase 2 from the development machine:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 PI_HOSTNAME=localbox-pi ./phase2-persist-ssh.sh
```

By default it uses `~/.ssh/id_ed25519.pub` or `~/.ssh/id_rsa.pub` if present. To choose a key:

```sh
PI_HOST=192.168.0.7 AUTHORIZED_KEYS=~/.ssh/id_rsa.pub ./phase2-persist-ssh.sh
```

Phase 2 installs persistent `sshd`, sets the hostname, keeps wired DHCP, adds the SSH key when supplied, and commits those files with `lbu`.

## Phase 3: Base System

After phase 2 completes, run:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 ./phase3-base-system.sh
```

Phase 3 fixes package trust/repositories, installs base utilities, enables `chronyd`, verifies `apk update`, and commits the overlay with `lbu`.

## Phase 4: Data USB

If the USB data stick is plugged into the Pi, inspect it first:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 ./phase4-data-usb.sh inventory
```

On the current Pi, the detected USB stick is `/dev/sda`:

```text
USB SanDisk 3.2Gen1, about 57.3 GiB
```

To erase it, create one ext4 partition, mount it at `/mnt/data`, and persist `/etc/fstab`:

```sh
PI_HOST=192.168.0.7 \
DATA_DEVICE=/dev/sda \
CONFIRM_DATA_DEVICE=/dev/sda \
./phase4-data-usb.sh format
```

Phase 4 creates:

```text
/mnt/data/localbox
/mnt/data/backups
```

## Phase 5: LocalBox Base Layout

After Phase 4 has `/mnt/data` mounted, run:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 ./phase5-localbox-base.sh
```

Phase 5 installs base packages, creates LocalBox users/groups/directories/logs, copies templates and example configs, and persists the base layout with `lbu`.

It does not create a domain, mailbox, tunnel, worker, or mail-ingest service yet.

## Phase 6: Install Binaries

Install the LocalBox Go binaries and OpenRC service definitions:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 ./phase6-install-binaries.sh
```

Phase 6 builds `mail-ingest` and `contact-service` for `linux/armv7`, copies them to `/usr/local/bin`, installs service definitions, and persists with `lbu`.

Phase 6 also installs `sudo` and a narrow sudoers rule so the web app can run `/usr/local/sbin/localbox-sync-mailmaps` without a password. Phase 7 should create the settings/domain/mailbox config before intentionally starting the app services.

## Phase 7: Mail Stack

Install and configure the base Dovecot/Postfix stack:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 ./phase7-mail-stack.sh
```

Phase 7 installs Dovecot/Postfix, writes base IMAP/submission config, enables services, and persists all mail-stack paths with `lbu`.

Phase 8 should create the first domain/mailbox and regenerate Dovecot/Postfix maps with real users.

## Phase 8: First Domain

Create the first LocalBox domain and admin mailbox:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 \
LOCALBOX_DOMAIN=localboxtest.co.za \
LOCALBOX_ADMIN_USER=admin \
LOCALBOX_ADMIN_RECOVERY_EMAIL=peter@example.com \
./phase8-first-domain.sh
```

If `LOCALBOX_ADMIN_PASSWORD` is not supplied, Phase 8 generates one and prints it once. `LOCALBOX_ADMIN_RECOVERY_EMAIL` is required and must be outside the LocalBox-hosted domains. Phase 8 creates the domain/user config, mailbox directories, Dovecot/Postfix maps, LocalBox secrets, starts the LocalBox services, verifies the admin login hash, and persists with `lbu`.

For the current test box, Cloudflare has:

```text
localboxtest.co.za status: active
mail.localboxtest.co.za -> old rpimail3 tunnel
```

That is the wrong tunnel for `localbox-001`. Do not treat this as a completed Phase 9 result. Create a dedicated `localbox-001-tunnel`, install/connect it on the Pi, record it in the local inventory, then publish the route.

## Phase 9a: Create Device Tunnel

Every Pi must have its own Cloudflare tunnel. Create it, install `cloudflared` on the Pi, start it under OpenRC, persist it with `lbu`, and record ownership in the local inventory:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 \
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
./phase9a-create-device-tunnel.sh
```

By default this uses the device inventory's `expected_tunnel`, for example `localbox-001-tunnel`.

The script reads `../cloudflare.conf` on the development machine. The Cloudflare API token needs account tunnel edit permissions. It does not copy Cloudflare API credentials to the Pi; only the connector token for this tunnel is installed on the Pi.

## Phase 9a Manual Fallback: Record Device Tunnel

Every Pi must have its own Cloudflare tunnel. Record tunnel ownership in the local inventory before any public hostname is pointed at it:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
DEVICE_NAME=localbox-001 \
LOCALBOX_TUNNEL=localbox-001-tunnel \
LOCALBOX_TUNNEL_ID=cloudflare-tunnel-uuid \
./phase9a-record-device-tunnel.sh
```

Phase 9 refuses to publish a route unless the selected tunnel is already recorded against the same `DEVICE_NAME`.

## Phase 9: Cloudflare Public Route

Publish the LocalBox web login hostname through the existing Pi tunnel:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
LOCALBOX_PUBLIC_HOST=www.localboxtest.co.za \
LOCALBOX_TUNNEL=<test-box-tunnel-name-or-id> \
./phase9-cloudflare-route.sh
```

Phase 9 runs on the development machine. It reads `../cloudflare.conf`, finds the Cloudflare zone and tunnel, verifies the tunnel belongs to `DEVICE_NAME` in the local inventory, creates/updates the proxied CNAME, and creates/updates the tunnel ingress route. It does not copy Cloudflare credentials to the Pi. If `LOCALBOX_TUNNEL_SERVICE` is omitted, Phase 9 uses the standard port convention: `mail` to ALPS on `1323`, `files` to Filebrowser on `8082`, and apex/`www` to nginx on `80`.

Publish the standard routes explicitly or one at a time:

```sh
DEVICE_NAME=localbox-001 LOCALBOX_DOMAIN=localboxtest.co.za LOCALBOX_PUBLIC_HOST=localboxtest.co.za LOCALBOX_TUNNEL=localbox-001-tunnel ./phase9-cloudflare-route.sh
DEVICE_NAME=localbox-001 LOCALBOX_DOMAIN=localboxtest.co.za LOCALBOX_PUBLIC_HOST=www.localboxtest.co.za LOCALBOX_TUNNEL=localbox-001-tunnel ./phase9-cloudflare-route.sh
DEVICE_NAME=localbox-001 LOCALBOX_DOMAIN=localboxtest.co.za LOCALBOX_PUBLIC_HOST=mail.localboxtest.co.za LOCALBOX_TUNNEL=localbox-001-tunnel ./phase9-cloudflare-route.sh
DEVICE_NAME=localbox-001 LOCALBOX_DOMAIN=localboxtest.co.za LOCALBOX_PUBLIC_HOST=files.localboxtest.co.za LOCALBOX_TUNNEL=localbox-001-tunnel ./phase9-cloudflare-route.sh
```

Do not reuse a tunnel from another device. `LOCALBOX_TUNNEL` must be the tunnel connected by the current test box.

The API token in `../cloudflare.conf` must be able to read/edit the target zone and edit Cloudflare Tunnels. For a newly added test zone, an older token may not include that zone. In that case create/update a token with access to `localboxtest.co.za`, or override at runtime:

```sh
CLOUDFLARE_API_TOKEN=... \
CLOUDFLARE_ZONE_ID=3041ba7d733be5aac66decc30e1e1f45 \
./phase9-cloudflare-route.sh
```

## Phase 9e: Inbound Email Routing

Cloudflare Email Routing should be configured at the domain level with a catch-all to the per-device Worker. That means new LocalBox mailboxes do not need individual Cloudflare routes.

```sh
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
./phase9e-email-worker.sh
```

## Phase 9f: Brevo Sender Domain

Brevo should also be configured at the domain level, not per mailbox. Phase 9f creates or reads the Brevo sender domain, publishes Brevo's TXT/CNAME authentication records into Cloudflare DNS, asks Brevo to authenticate the domain, and records the result in the local inventory.

If `../smtp.conf` exists and contains `from=admin@yourdomain`, Phase 9f also creates that one primary Brevo sender identity. It does not add every mailbox as a Brevo sender.

Create the local config once:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
cp brevo.conf.example brevo.conf
```

Set `enabled=true` and the real Brevo REST API key as `password=`, then run:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
./phase9f-brevo-domain.sh
```

If Brevo says authentication is not ready yet, wait for DNS propagation and rerun the same command. It is idempotent.

## Phase 9g: Outbound SMTP

Phase 9f authenticates the domain in Brevo and publishes DNS. Phase 9g puts the SMTP relay credentials on the Pi and configures both the LocalBox app and local Postfix/ALPS to send through that relay.

Create the local secret config once:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap
cp smtp.conf.example smtp.conf
```

Set the Brevo SMTP login/key, not just the Brevo REST API key:

```sh
enabled=true
host=smtp-relay.brevo.com
port=587
username=your-brevo-smtp-login
password=your-brevo-smtp-key
from_name=LocalBox
from=admin@localboxtest.co.za
```

Then deploy and reboot-test it:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.3 \
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
./phase-reboot-test.sh phase9g
```

If `to=` is set in `smtp.conf`, Phase 9g sends a contact-service SMTP test message. Without `to=`, it configures the relay and skips that external send test.

If no DHCP lease appears, test the rescue IP:

```sh
ping 192.168.0.250
ssh -o PreferredAuthentications=none -o PubkeyAuthentication=no root@192.168.0.250
```

To disable the rescue IP while writing:

```sh
RESCUE_IP= ./write-sdcard.sh ../setup_new_pi/alpine-rpi-3.24.1-armv7.tar.gz /dev/sdX
```

## Phase 11: Reboot Audit

Before calling an install complete, run the audit:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/sdcard_dhcp_ssh
PI_HOST=192.168.0.7 \
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
./phase11-reboot-audit.sh
```

Then power reboot the Pi and run the same command again. The audit checks SSH access, `/mnt/data`, `lbu`, OpenRC runlevels, service status, LocalBox secrets, recovery emails, Dovecot/Postfix maps, binary symlinks, and optional app services.

To repair known safe local drift after SSH works:

```sh
PI_HOST=192.168.0.7 \
DEVICE_NAME=localbox-001 \
LOCALBOX_DOMAIN=localboxtest.co.za \
./phase11-reboot-audit.sh repair
```

Repair mode does not change Cloudflare, delete tunnels, reset passwords, or weaken SSH. If SSH fails after reboot, use console access or rewrite the card. Do not publish new Cloudflare routes while the reboot audit fails.
