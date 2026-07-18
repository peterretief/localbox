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

## Single Entry Dashboard SSO

The product entry point should be:

```text
https://localboxmail.co.za/login
        |
        v
https://localboxmail.co.za/dashboard
        |
        +-- Mail
        +-- Files
        +-- Website
        +-- Admin
        +-- Password
```

Users should not choose between separate mail, files, and admin login pages. They log into LocalBox once, then the dashboard launches the correct app for that same mailbox.

### Session Ownership

LocalBox owns the browser session:

```text
localbox_session = signed/random LocalBox session token
```

The dashboard, password page, built-in inbox, and built-in files page already use this session.

External apps should not ask the user to log in again. They should receive identity from LocalBox through a trusted local handoff.

### Reverse Proxy Handoff

nginx should route app URLs through LocalBox auth:

```text
/apps/mail/   -> LocalBox auth check -> ALPS
/apps/files/  -> LocalBox auth check -> Filebrowser
```

After LocalBox validates `localbox_session`, nginx forwards trusted headers only on localhost/internal proxy paths:

```text
X-LocalBox-User: info
X-LocalBox-Domain: localboxzen.co.za
X-LocalBox-Mailbox: info@localboxzen.co.za
X-LocalBox-Auth: signed short-lived assertion
```

The upstream app must reject these headers from public traffic. Only nginx on the Pi may set them.

## App Strategy

### Filebrowser

Filebrowser already supports proxy auth. This is the easier integration.

Target:

```text
LocalBox session -> nginx auth_request -> Filebrowser proxy auth
```

Filebrowser receives one trusted username:

```text
info@localboxzen.co.za
```

and scopes the user to:

```text
/mnt/data/localbox/files/info@localboxzen.co.za
```

No Filebrowser password prompt is needed.

### ALPS

ALPS is harder because it normally creates its own session by logging into IMAP with the user password.

LocalBox cannot recover a password from `/etc/localbox/users.conf`, because it stores password hashes. That is correct and should not be changed.

There are two sane ALPS SSO options:

1. Add trusted proxy auth to ALPS for LocalBox.
2. Use a Dovecot master-user credential so ALPS can open IMAP as the mailbox user without knowing the user's password.

Preferred short-term design:

```text
LocalBox session
  -> signed SSO assertion
  -> ALPS accepts assertion from localhost/nginx only
  -> ALPS opens mailbox through Dovecot master-user auth
```

This keeps user passwords one-way hashed, avoids storing plaintext user passwords, and still lets ALPS create a real mail session.

Alternative:

```text
LocalBox session
  -> ALPS local Maildir provider
  -> no IMAP login
```

This may be cleaner later, but it is a larger ALPS change if contacts/calendar still depend on DAV/IMAP-related behavior.

## SSO Implementation Phases

### Phase 1: Make LocalBox The Only Public Login

- keep `/login` and `/dashboard` as the public entry;
- dashboard Mail links to an internal app path such as `/apps/mail/`;
- dashboard Files links to `/apps/files/`;
- direct app login pages should redirect back to `/login` where practical.

### Phase 2: Filebrowser Proxy Auth

- configure Filebrowser `auth.method=proxy`;
- configure nginx to call LocalBox session validation before proxying;
- pass `X-Remote-User: user@domain`;
- set Filebrowser logout page to `/logout`;
- disable independent password changes in Filebrowser.

### Phase 3: ALPS LocalBox SSO

- add an ALPS LocalBox SSO entrypoint;
- validate a short-lived signed assertion from LocalBox;
- create an ALPS session for the mailbox;
- connect to Dovecot with master-user auth or a local trusted provider;
- set normal ALPS session cookies so ALPS behaves normally after handoff.

### Phase 4: Auth Drift Health

- report LocalBox users missing from Dovecot;
- report Dovecot users missing from LocalBox;
- report external apps not configured for SSO/proxy auth;
- avoid printing password hashes.

## Next Code Step

Add a small `localbox-auth-check` command or mode to the Go binary:

```sh
localbox-auth-check info@localboxzen.co.za
```

It should read the password from stdin and exit:

- `0` when credentials are valid;
- non-zero when invalid.

Then Filebrowser, Radicale, and eventually ALPS can delegate directly to LocalBox authentication instead of maintaining parallel password stores.
