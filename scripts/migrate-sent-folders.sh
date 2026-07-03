#!/bin/sh
set -e

base=${LOCALBOX_BASE:-/mnt/data/localbox}

for mailbox in "$base"/domains/*/mail/*; do
    [ -d "$mailbox/Sent" ] || continue
    [ -d "$mailbox/Maildir" ] || continue

    mkdir -p "$mailbox/Maildir/.Sent/new" "$mailbox/Maildir/.Sent/cur" "$mailbox/Maildir/.Sent/tmp"
    for dir in new cur tmp; do
        [ -d "$mailbox/Sent/$dir" ] || continue
        for file in "$mailbox/Sent/$dir"/*; do
            [ -f "$file" ] || continue
            mv "$file" "$mailbox/Maildir/.Sent/$dir/"
        done
    done
    rmdir "$mailbox/Sent/new" "$mailbox/Sent/cur" "$mailbox/Sent/tmp" "$mailbox/Sent" 2>/dev/null || true
done

chown -R www:mail "$base" 2>/dev/null || true
chmod -R g+rwX "$base" 2>/dev/null || true
