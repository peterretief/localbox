# LocalBox Raspberry Pi Base System Design

The goal is not a boring Pi. The goal is a Pi that boots predictably, repairs cleanly, and can take on interesting LocalBox roles without turning every install into a rescue session.

## Design Principles

1. The boot card is a replaceable appliance part.
2. The box identity is small, explicit, and per-device.
3. User data never depends on the boot card.
4. First boot must make the Pi reachable before doing anything clever.
5. Mail and file services can fail without locking us out of SSH.
6. A second Pi can act as a surrogate when the first Pi is offline.

## Layers

### 1. Boot Card

The SD card contains:

- Alpine Raspberry Pi boot files;
- one `headless.apkovl.tar.gz`;
- `localbox-firstboot.conf`;
- optional APK cache.

The card is created by tooling, not by manual file-browser edits. The tooling must refuse large disks and non-removable drives.

### 2. Box Identity

Each Pi has a small identity file:

```ini
hostname=localbox-b
mesh_box_name=pi-b
ssh_authorized_key=ssh-ed25519 AAAA... peter@devmachine
```

Optional secrets can be added later:

```ini
tailscale_authkey=...
cloudflare_tunnel_token=...
ingest_secret=...
```

The same image can therefore become `pi-a`, `pi-b`, or a customer box without rebuilding the whole OS.

### 3. First Boot Phase 1: Reachability

Phase 1 must do only enough to make the Pi reachable and persistent:

- set hostname;
- configure DHCP for `eth0` and `end0`;
- install the SSH public key;
- start `sshd`;
- configure `lbu`;
- configure APK cache;
- persist the above with `lbu commit -d`.

If this phase succeeds, the Pi is recoverable over SSH.

### 4. First Boot Phase 2: LocalBox Base

Phase 2 installs the useful LocalBox base:

- `/mnt/data` mount point;
- required packages;
- nginx, postfix, dovecot, file service, webmail services;
- LocalBox binaries and init scripts;
- health endpoints;
- log files and permissions;
- Tailscale or Headscale if configured.

This phase should be rerunnable. A failure here should leave SSH working.

### 5. Provisioning Phase

Domain, mailbox, Brevo, Cloudflare Email Routing, and customer setup should run after the Pi is reachable.

This avoids a bad API key, expired tunnel token, or DNS issue breaking the base install.

### 6. Data Disk

`/mnt/data` owns the valuable state:

- mailboxes;
- uploaded files;
- site content;
- queue data;
- surrogate sync state;
- backups.

The boot card can be rebuilt without erasing `/mnt/data`.

### 7. Surrogate Mode

If `pi-a` is offline, a worker can store inbound mail for `pi-a` on `pi-b` or another surrogate. When `pi-a` returns, it fetches and acknowledges queued mail.

The surrogate should store:

- recipient domain;
- recipient mailbox;
- original message;
- received timestamp;
- source worker/event id;
- delivery status.

The returning Pi should pull messages idempotently so duplicate downloads do not duplicate mail delivery.

## Intended Lifecycle

1. Create SD card with `fresh-boot-card`.
2. Boot Pi with Ethernet.
3. Phase 1 makes SSH reliable.
4. Phase 2 installs the LocalBox base.
5. Save a golden image once the base is proven.
6. Provision domains and services from the dev machine.
7. Add surrogate sync between boxes.

## Non-Negotiable Safety Rules

- Never identify destructive targets by `/dev/sdX` alone.
- Refuse non-removable disks for SD-card creation.
- Refuse large disks for SD-card creation.
- Require model or serial confirmation for destructive writes.
- Keep customer data off the SD card.
- Keep base reachability separate from mail/domain provisioning.
