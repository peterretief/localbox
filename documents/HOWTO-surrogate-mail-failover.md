# LocalBox Surrogate Mail Failover

This is the first practical two-Pi failover flow.

```text
Home Pi online:
  Cloudflare Email Worker -> home Pi /ingest

Home Pi offline:
  Worker tries home Pi, then backup/surrogate Pi
  Surrogate stores mail in the same domain/mailbox layout

Home Pi returns:
  Home Pi pulls new mail from surrogate with rsync --ignore-existing
```

## Requirements

- Both Pis have the same domain and mailbox layout.
- Both Pis have `/ingest` reachable through their own Cloudflare Tunnel host.
- Both Pis have an ingest secret in `/etc/localbox/secret`.
- SSH from home to surrogate works over LAN or Headscale/Tailscale.
- The surrogate is provisioned for the domain before failover is enabled.

## Domain Config

In `setup_new_pi/domain.conf`, set the home Pi as the normal ingest target and the backup Pi as the surrogate:

```ini
domain=localboxmail.co.za
email_worker=alpine

ingest_host=a.diode.co.za
ingest_tunnel_id=home-tunnel-uuid
email_ingest_secret=home-ingest-secret

backup_ingest_url=https://b.diode.co.za/ingest
backup_ingest_secret=backup-ingest-secret

email_route=info@localboxmail.co.za
```

Deploy/update the Worker:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/setup_new_pi
WORKER_DEPLOY_METHOD=wrangler ./setup.sh cloudflare ./domain.conf
```

The Worker remains backwards compatible. Without `backup_ingest_url`, it behaves like the old single-Pi Worker.

## Recovery Sync

When the home Pi comes back, pull mail from the surrogate:

```sh
sudo localbox-sync-surrogate-mail pull localboxmail.co.za pi-b
```

The sync is additive:

```text
rsync -aH --numeric-ids --ignore-existing
```

It does not delete mail from either Pi.

## Normal Mirroring

While both Pis are healthy, push home mail to the surrogate periodically:

```sh
sudo localbox-sync-surrogate-mail push localboxmail.co.za pi-b
```

For the first rollout, run this manually after tests. Later it can be moved into a supervised cron/OpenRC timer once the route table is stable.

## Test

1. Confirm both health endpoints return OK:

   ```sh
   curl -i https://a.diode.co.za/health
   curl -i https://b.diode.co.za/health
   ```

2. Send a test message while the home Pi is online.
3. Stop the home Pi tunnel or power off the home Pi.
4. Send another test message.
5. Confirm the second message appears on the surrogate.
6. Power the home Pi back on.
7. Pull from the surrogate:

   ```sh
   sudo localbox-sync-surrogate-mail pull localboxmail.co.za pi-b
   ```

8. Confirm the message appears on the home Pi.

## Important Limits

This first version does not use Cloudflare KV state. It simply tries home first and backup second for each incoming message.

That is acceptable for initial failover because Cloudflare only stores the message for the duration of the Worker invocation. Long-term buffering is done by the surrogate Pi, not by Cloudflare.
