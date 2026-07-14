# LocalBox Domain Mirroring HOWTO

## Goal

LocalBox can use more than one Pi without making every Pi a copy of every other Pi.

Use this simple rule:

```text
Each domain has one home box and one backup box.
```

The home box is where the domain normally lives. The backup box is where incoming mail can go if the home box is offline.

## Domain Placement Table

Keep one small table as the source of truth:

```text
Domain          Home box   Backup box
diode.co.za     pi-a       pi-b
opamp.co.za     pi-a       pi-c
example.co.za   pi-b       pi-a
club.org.za     pi-c       pi-b
```

A LocalBox can be home for many domains and backup for many other domains.

Example:

```text
pi-a
  home for:
    diode.co.za
    opamp.co.za

  backup for:
    example.co.za
```

## Core Rules

1. Every domain has exactly one home box.
2. Every domain has exactly one backup box.
3. The home box and backup box must be different.
4. Incoming mail can be delivered to either box.
5. Website edits happen on the home box only.
6. Website sync is one-way: home to backup.
7. Mail sync is additive both ways: home to backup and backup to home.
8. The Cloudflare Email Worker routes mail to the home box unless it is unhealthy.
9. If the home box is unhealthy, the Worker routes mail to the backup box.
10. When the home box recovers, sync mail back before normal routing resumes.

## Example Machine Config

Use a config shape like this for automation later:

```yaml
boxes:
  pi-a:
    ingest_url: https://a.diode.co.za/ingest
    health_url: https://a.diode.co.za/health
    vpn_host: pi-a

  pi-b:
    ingest_url: https://b.diode.co.za/ingest
    health_url: https://b.diode.co.za/health
    vpn_host: pi-b

  pi-c:
    ingest_url: https://c.diode.co.za/ingest
    health_url: https://c.diode.co.za/health
    vpn_host: pi-c

domains:
  diode.co.za:
    home_box: pi-a
    backup_box: pi-b

  opamp.co.za:
    home_box: pi-a
    backup_box: pi-c

  example.co.za:
    home_box: pi-b
    backup_box: pi-a
```

This file can later drive:

- Worker route generation
- sync jobs
- health checks
- admin status pages

## Worker Mail Routing

Cloudflare receives incoming email for the domain. The Email Worker chooses where to post the message.

For each incoming message:

```text
read recipient domain
look up home box and backup box
if home box is healthy:
  post to home ingest URL
else:
  post to backup ingest URL
if first delivery attempt fails:
  try the other box
if both fail:
  reject the message
```

The Worker should keep failover state per domain, not just one global state.

Example state keys:

```text
active_target:diode.co.za=home
fail_count:diode.co.za=0
active_target:example.co.za=backup
```

## Data To Sync

Sync only domain data:

```text
/mnt/data/localbox/domains/<domain>/mail/
/mnt/data/localbox/domains/<domain>/site/
```

Do not sync the whole box.

Avoid syncing:

```text
/etc/
/var/
/usr/
/run/
/tmp/
/var/lib/tailscale/
/etc/cloudflared/
/etc/ssh/
```

Those paths contain machine identity, service state, tunnel credentials, SSH host keys, package state, and runtime files.

## Mail Sync

Mail sync must be additive. Do not delete mail during normal sync.

Home to backup:

```sh
rsync -aH --numeric-ids --ignore-existing \
  /mnt/data/localbox/domains/diode.co.za/mail/ \
  pi-b:/mnt/data/localbox/domains/diode.co.za/mail/
```

Backup to home:

```sh
rsync -aH --numeric-ids --ignore-existing \
  pi-b:/mnt/data/localbox/domains/diode.co.za/mail/ \
  /mnt/data/localbox/domains/diode.co.za/mail/
```

Important:

```text
Use --ignore-existing for mail.
Do not use --delete for mail while both boxes can receive messages.
```

## Website Sync

Website sync can be authoritative because website edits should happen only on the home box.

Home to backup:

```sh
rsync -aH --numeric-ids --delete \
  /mnt/data/localbox/domains/diode.co.za/site/ \
  pi-b:/mnt/data/localbox/domains/diode.co.za/site/
```

Important:

```text
Use --delete only for website files when the home box is the clear source of truth.
Do not edit the same website on both boxes.
```

## Per-Domain Sync Script Shape

A sync job for one domain needs:

```text
domain
home box
backup box
direction
```

Example normal sync on the home box:

```sh
DOMAIN=diode.co.za
BACKUP=pi-b

rsync -aH --numeric-ids --ignore-existing \
  "/mnt/data/localbox/domains/$DOMAIN/mail/" \
  "$BACKUP:/mnt/data/localbox/domains/$DOMAIN/mail/"

rsync -aH --numeric-ids --delete \
  "/mnt/data/localbox/domains/$DOMAIN/site/" \
  "$BACKUP:/mnt/data/localbox/domains/$DOMAIN/site/"
```

Example sync-back after failover:

```sh
DOMAIN=diode.co.za
BACKUP=pi-b

rsync -aH --numeric-ids --ignore-existing \
  "$BACKUP:/mnt/data/localbox/domains/$DOMAIN/mail/" \
  "/mnt/data/localbox/domains/$DOMAIN/mail/"
```

## Health Checks

Each box should expose:

```text
/health
```

The endpoint should return `200 OK` only when the box can accept mail ingest.

Minimum checks:

- web server is running
- ingest endpoint is running
- data disk is mounted
- enough free disk space remains

Optional checks:

- mail UI service is running
- sync state is recent
- Tailscale or Headscale is connected

## Failover Flow

Normal:

```text
Cloudflare Email Worker -> home box
home box syncs mail and site to backup box
```

Home offline:

```text
Cloudflare Email Worker -> backup box
backup box stores new incoming mail
```

Home returns:

```text
backup box syncs new mail back to home box
Cloudflare Email Worker routes domain back to home box
```

## Failback Rule

Do not immediately fail back just because `/health` returns OK once.

Recommended rule:

```text
home must be healthy for several checks
then sync-back must run successfully
then route mail back to home
```

This avoids losing messages that arrived on the backup while the home box was down.

## Config Sync

The backup box must know about the mirrored domain and mailboxes.

Copy only selected LocalBox config when provisioning changes:

```text
/etc/localbox/domains.conf
/etc/localbox/users.conf
/etc/localbox/admins.conf
/etc/localbox/box-admins.conf
```

Do not blindly sync all of `/etc`.

## Practical Starting Point

Start with one domain and two boxes:

```text
diode.co.za
  home:   pi-a
  backup: pi-b
```

Get these working first:

1. Both boxes have the same domain and mailbox layout.
2. Worker can deliver to home.
3. Worker can deliver to backup if home is down.
4. Mail sync from home to backup works.
5. Mail sync-back from backup to home works.
6. Website sync from home to backup works.

Only after that, add more domains and more box pairs.
