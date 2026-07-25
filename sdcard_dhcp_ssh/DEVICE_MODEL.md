# LocalBox Device Model

This directory uses a development-machine inventory as the source of truth.
Do not infer device ownership from old tunnel names, DNS records, IP addresses, or memory.

## Naming

For device number `001`:

```text
device_name:      localbox-001
asset_tag:        LBX-0001
tailscale_name:   localbox-001
tunnel_name:      localbox-001-tunnel
worker_name:      localbox-001-mail-worker
```

Domain names are assigned to a device:

```text
domain:           localboxtest.co.za
mail hostname:    mail.localboxtest.co.za
files hostname:   files.localboxtest.co.za
```

## Rules

Every physical Pi must have:

```text
1. a device record
2. an asset tag / barcode label
3. a Headscale node name matching device_name
4. one dedicated Cloudflare tunnel
5. domains and routes assigned to that same device
```

Cloudflare publishing must not proceed unless:

```text
DEVICE_NAME exists
asset_tag exists
tunnel exists
tunnel.device_name == DEVICE_NAME
domain.device_name == DEVICE_NAME
```

## First Step For Any New Pi

Create or reserve identity before writing/booting/provisioning:

```sh
./localbox-device-create.sh
```

Or for a known device:

```sh
DEVICE_NAME=localbox-001 ./localbox-device-create.sh
```

Print and attach the generated label before the card is deployed.

## Lookup

```sh
./localbox-inventory.sh show-device localbox-001
./localbox-inventory.sh lookup-tag LBX-0001
```

## Current Known Issue

`mail.localboxtest.co.za` was previously pointed at the existing `rpimail3`
tunnel. That is not the correct tunnel for `localbox-001`.

Do not publish more routes for `localbox-001` until `localbox-001-tunnel`
exists, is installed on the Pi, and is recorded in inventory.
