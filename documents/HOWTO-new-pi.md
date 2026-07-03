# LocalBox New Pi HOWTO

This is the clean path for building another LocalBox Pi from an existing setup.

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

## 4. Provision Domains And Mailboxes

Copy the example template:

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
```

Run provisioning:

```sh
sudo localbox-provision-template /root/newbox.conf
```

This creates domain config, mailbox directories, default site files, users, admin roles, and persists with `lbu`.

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
