#!/bin/sh
set -eu

src="/mnt/data/localbox/domains/mywagtall.co.za"
dst="/mnt/data/localbox/.archived-domains/mywagtall.co.za-20260630T132650Z"

mkdir -p /mnt/data/localbox/.archived-domains

if [ ! -d "$src" ]; then
    echo "Not found: $src"
    exit 0
fi

mv "$src" "$dst"
echo "Moved $src to $dst"
