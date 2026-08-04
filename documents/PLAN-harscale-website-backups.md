# Plan: Harscale Website Backups

## Purpose

Keep an off-site copy of each LocalBox website on the Harscale server so a
failed, lost, or damaged Pi does not also destroy the hosted websites.

The backup must also preserve contact-form records. These records can contain
names, email addresses, phone numbers, enquiry text, and attachments if the
contact form is extended later, so they must be treated as private customer
data rather than ordinary website files.

This is a planning document only. It does not install software, create a
Harscale account, or change the current backup job.

## Current LocalBox data

The existing LocalBox backup script can collect:

- Website files at `/mnt/data/localbox/domains/<domain>/site/`.
- Contact-service configuration and binaries.
- The service log at `/var/localbox/contact.log`.
- Contact-form messages delivered to LocalBox maildirs when
  `INCLUDE_MAIL_DATA=1` is selected.
- Nginx, mail, SSH, LocalBox, and Alpine configuration needed for recovery.

The website files and contact-form messages should be backed up as separate
logical datasets even if they are stored in one encrypted repository. This
makes it possible to restore a website without automatically restoring all
mail data.

## Recommended design

The Pi remains the source of truth. A scheduled job on the Pi creates a
consistent backup snapshot and sends it to a Harscale server over the private
network or SSH. Harscale stores the backup repository; it does not serve the
backup files publicly.

Recommended properties:

- Use an encrypted repository, with encryption performed before data leaves
  the Pi.
- Use a dedicated Harscale account and directory for LocalBox backups.
- Give the backup account no shell access and no access to unrelated data.
- Use a separate repository or repository namespace for each LocalBox host.
- Keep a local short-term copy on the Pi only as a transfer buffer, not as the
  disaster-recovery copy.
- Generate a manifest containing hostname, timestamp, domains, included data,
  archive size, and a checksum or repository snapshot ID.
- Send success and failure status to the existing administrative notification
  path once that path is selected.

The first implementation should use a mature backup tool with deduplication,
encryption, retention policies, and integrity checks, such as Restic over SFTP
or SSH. Plain `scp` of unencrypted tar files should not be the long-term
design because the current archive includes credentials, password hashes, SSH
keys, and contact-form data.

## Backup contents

### Website dataset

Include, per domain:

- `site/index.html` and all other HTML pages.
- CSS, JavaScript, images, fonts, downloads, and generated static assets.
- `contact.html`, `thankyou.html`, and any domain-specific contact-form
  assets.
- File ownership, permissions, timestamps, and symlinks where applicable.

Do not include generated caches, temporary upload files, or the entire mail
tree in this dataset.

### Contact-form dataset

Include:

- Contact-form messages stored in the domain maildirs.
- `/var/localbox/contact.log`.
- Any future contact-service queue or attachment directory.
- The domain and recipient mapping needed to identify each submission.

The implementation must decide whether contact-form messages are backed up as
raw Maildir messages, exported `.eml` files, or both. Raw Maildir preservation
is the safest initial restore format because it retains headers, dates, and
message bodies without introducing an export/import conversion step.

### Recovery dataset

The off-site plan should also retain the existing system backup archive or an
equivalent recovery snapshot containing:

- LocalBox and Nginx configuration.
- Contact-service and mail-service configuration.
- Alpine package and service state.
- Domain and mailbox configuration.
- Required secrets, stored only in the encrypted repository.

This dataset is needed to make the website and contact form usable after a
complete Pi replacement, not merely to recover static files.

## Retention proposal

Start with a modest policy and adjust after measuring Harscale storage:

- Keep the last 14 daily snapshots.
- Keep 8 weekly snapshots.
- Keep 12 monthly snapshots.
- Keep one manually marked release snapshot before major website changes.

Retention must be enforced on Harscale and verified periodically. Deleting a
local copy must never delete the off-site repository without an explicit
administrative action.

## Transfer and scheduling

Proposed sequence:

1. Run after the website publishing window, or use a temporary staging copy so
   a file is not changed while it is being archived.
2. Capture the website dataset for all active domains.
3. Capture the contact-form dataset and the recovery dataset according to the
   selected switches.
4. Create or update the encrypted Harscale snapshot.
5. Verify the upload and record the snapshot ID and manifest.
6. Run a lightweight repository integrity check on a regular schedule.
7. Alert if the last successful backup is older than the agreed threshold.

The job must use a lock so two backups cannot run at the same time. It must
fail clearly on an incomplete upload and must not mark a snapshot successful
until the remote repository confirms it.

## Restore procedure to design

The implementation should support these independent recovery cases:

1. Restore one domain's website to a temporary directory for review.
2. Restore one domain's website directly to a replacement Pi.
3. Restore contact-form messages for one domain into its LocalBox mailbox.
4. Restore the full LocalBox service to a fresh Alpine Pi.
5. Restore the Harscale repository itself to a new storage location if the
   original Harscale disk fails.

A restore must not overwrite a live website without an explicit confirmation
and a pre-restore snapshot. Restored site files should be checked for correct
ownership and permissions before Nginx is reloaded.

## Security requirements

- Encrypt data before transfer and protect the repository password separately
  from the Harscale server.
- Store the repository password in a protected LocalBox secret or password
  manager, never in the website tree or a committed configuration file.
- Use a dedicated SSH key restricted to the backup account and Harscale
  backup directory.
- Disable public HTTP directory listing and public download access.
- Limit Harscale firewall access to the required SSH or private-network source.
- Log upload, prune, check, and restore operations without logging secrets or
  contact-form message bodies.
- Define who may request a restore and who may access restored contact data.

## Implementation phases

### Phase 1: Confirm the Harscale target

- Confirm Harscale hostname, operating system, storage capacity, filesystem,
  backup account, and private-network or SSH connectivity.
- Confirm expected number of domains, current website size, and monthly growth.
- Agree on retention, recovery time objective, and acceptable data loss window.

### Phase 2: Build a manual proof of concept

- Create a restricted backup account and directory on Harscale.
- Send one encrypted test snapshot from a non-production or test dataset.
- Confirm that Harscale cannot read the plaintext contents.
- Restore the test website and a sample contact-form message to a temporary
  location.

### Phase 3: Automate on the Pi

- Add a dedicated backup configuration file with no plaintext repository
  password in the script.
- Add a scheduled service and timer/cron equivalent appropriate for Alpine.
- Add locking, manifests, remote confirmation, retention, and failure alerts.
- Keep the current local archive backup available until remote backups have
  passed restore testing.

### Phase 4: Operate and test

- Perform a monthly sample restore.
- Perform a quarterly full replacement-Pi restore exercise.
- Review storage usage, failed jobs, and retention every quarter.
- Record the last successful restore and the person responsible for it.

## Decisions required before implementation

- Is Harscale reachable from the Pi over SSH, Tailscale/Headscale, or a
  provider API?
- Will Harscale provide a normal VPS filesystem, object storage, or both?
- What are the required recovery time and maximum acceptable data loss?
- Should contact-form submissions be restored into mailboxes, exported for
  admin review, or both?
- How long must contact-form data be retained, and who is permitted to access
  it?
- Where will the encryption/repository password be stored and how will it be
  recovered if the Pi is lost?
- Which administrative channel should report backup success and failure?

## Acceptance criteria

The work is ready for production when:

- A website snapshot completes automatically on schedule.
- A new website file is present in a later Harscale snapshot.
- Contact-form records are present and can be restored independently.
- The Harscale account cannot read backup contents without the repository
  password.
- A single-domain restore has been tested.
- A full fresh-Pi restore has been tested and documented.
- Retention and failure alerts have been observed in a test.
- The recovery instructions identify the required keys, passwords, domains,
  and restore commands without embedding secret values.
