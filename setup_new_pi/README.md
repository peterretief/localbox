# LocalBox New Pi Setup Bundle

This directory is the entry point for preparing a new Raspberry Pi LocalBox install from the development machine.

The implementation still lives in the main project scripts so fixes do not drift. Use `./setup.sh` here as the guided wrapper.

## Files

- `setup.sh` - wrapper for the new-Pi workflow.
- `localbox-firstboot.conf.example` - example boot-partition first-boot config.
- `domain.conf.example` - example domain/mailbox/Cloudflare Email Routing config.

## Basic Flow

From this directory:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/setup_new_pi
cp localbox-firstboot.conf.example localbox-firstboot.conf
cp domain.conf.example domain.conf
vi localbox-firstboot.conf
vi domain.conf
```

For a fresh Raspberry Pi base install, follow:

```text
BASE-INSTALL.md
```

Check attached disks:

```sh
./setup.sh inventory
```

Prepare an Alpine boot card after mounting the boot partition at `/mnt/piboot`:

```sh
BOOT_MOUNT=/mnt/piboot FIRSTBOOT_CONFIG=./localbox-firstboot.conf ./setup.sh prepare-boot
```

Create or update the Cloudflare Worker and Email Routing rules from the dev machine:

```sh
WORKER_DEPLOY_METHOD=wrangler ./setup.sh cloudflare ./domain.conf
```

For a new Pi, give it a unique ingest host in `domain.conf`:

```ini
email_worker=alpine-n
ingest_host=n.diode.co.za
ingest_tunnel_id=your-cloudflare-tunnel-uuid
email_ingest_secret=change-this-secret
```

The setup creates or updates the proxied Cloudflare CNAME for `ingest_host`, then uses Wrangler on the dev machine to deploy that Worker and point it at `https://n.diode.co.za/ingest`. Do not reuse one Worker name across multiple Pis unless the Worker code is changed to route by recipient domain.

Wrangler runs on the development machine. It does not need to be installed on the Pi.

Provision the domain on the Pi after it is reachable:

```sh
PI_HOST=192.168.0.6 ./setup.sh deploy-domain ./domain.conf
```

## Destructive Steps

These commands intentionally require explicit device confirmation:

```sh
sudo CONFIRM_WRITE_DEVICE=/dev/sdX ./setup.sh write-image ./images/localbox-alpine-rpi.img.gz /dev/sdX
sudo CONFIRM_FORMAT_DEVICE=/dev/sdY ./setup.sh format-usb /dev/sdY
```

Use whole disk devices such as `/dev/sdf`, not partitions such as `/dev/sdf1`.
