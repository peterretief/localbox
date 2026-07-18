# LocalBox Single Password Source Design

LocalBox should have one canonical mailbox password source. Every app should either authenticate against it directly or receive generated credentials from it.

## Current State

LocalBox stores mailbox credentials in:

```text
/etc/localbox/users.conf
```

Each line is:

```text
username:domain:localbox-password-hash[:recovery-email]
```

Dovecot stores IMAP/SMTP credentials separately in:

```text
/etc/dovecot/localbox/users
```

ALPS authenticates through IMAP/Dovecot. Filebrowser authenticates through a hook that calls:

```sh
doveadm auth test "$USERNAME" "$PASSWORD"
```

Radicale either uses its own htpasswd-style file or delegates to Dovecot, depending on configuration.

## Problem

The system currently has one intended source of truth, but more than one effective password store:

- LocalBox dashboard uses `/etc/localbox/users.conf`;
- ALPS uses Dovecot;
- Filebrowser uses Dovecot;
- Radicale may use Dovecot or its own file.

When the stores drift, a password can work in one app and fail in another. Browser sessions also hide the problem because ALPS and Filebrowser keep their own sessions outside the LocalBox dashboard.

## Target Design

Use this model:

```text
LocalBox users.conf
        |
        | password set/reset/change
        v
generated service auth files
        |
        +-- Dovecot IMAP/SMTP
        +-- ALPS via Dovecot
        +-- Filebrowser via Dovecot or LocalBox auth hook
        +-- Radicale via Dovecot
```

Only LocalBox writes human mailbox passwords. Other apps must not offer independent password changes.

## Rules

1. `/etc/localbox/users.conf` is canonical.
2. Password changes happen only through LocalBox.
3. LocalBox regenerates Dovecot credentials whenever a password changes.
4. ALPS password-change endpoints stay blocked by nginx.
5. Filebrowser must not keep an independent password database.
6. Radicale should delegate to Dovecot where possible.
7. A health check should detect auth drift.

## Practical Implementation

### Phase 1: Repair Drift

Add a LocalBox admin operation or script that resets a selected mailbox password and immediately syncs:

- `/etc/localbox/users.conf`;
- `/etc/dovecot/localbox/users`;
- `/etc/radicale/users` only if Radicale is not using Dovecot.

This already exists in code paths such as `resetMailboxPassword` and `changePassword`; the operational need is to use those paths instead of manual hash edits.

### Phase 2: Auth Drift Check

Add a health check that reports:

- mailbox exists in LocalBox but not Dovecot;
- mailbox exists in Dovecot but not LocalBox;
- duplicate or malformed auth entries;
- Radicale not configured to use Dovecot.

The check must not print password hashes.

### Phase 3: Filebrowser Auth

Preferred: keep Filebrowser hook authentication, but point the hook at a LocalBox auth command instead of `doveadm`.

Example:

```sh
localbox-auth-check "$USERNAME" "$PASSWORD"
```

That command should read `/etc/localbox/users.conf` and verify the password with the same code as the dashboard.

If that is not available yet, keep Dovecot as the generated service auth layer.

### Phase 4: ALPS Integration

Short term: ALPS continues to authenticate through IMAP/Dovecot.

Long term: patch ALPS to support an external auth command or LocalBox auth endpoint. Until then, Dovecot remains the bridge.

## User Experience

Dashboard login, ALPS login, Filebrowser login, IMAP, SMTP, and calendar should all accept the same mailbox address and password:

```text
info@localboxzen.co.za
same-password
```

Dashboard links can open separate apps, but those apps should use the same credential source. If they keep separate browser sessions, users may still need to sign out in that app until SSO is implemented.

## Next Code Step

Add a small `localbox-auth-check` command or mode to the Go binary:

```sh
localbox-auth-check info@localboxzen.co.za
```

It should read the password from stdin and exit:

- `0` when credentials are valid;
- non-zero when invalid.

Then Filebrowser, Radicale, and eventually ALPS can delegate directly to LocalBox authentication instead of maintaining parallel password stores.
