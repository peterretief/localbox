#!/bin/sh
set -eu

echo "== lsblk =="
lsblk -o NAME,PATH,SIZE,MODEL,TRAN,RM,RO,FSTYPE,LABEL,UUID,MOUNTPOINTS

echo
echo "== removable candidates =="
lsblk -dn -o NAME,PATH,SIZE,MODEL,TRAN,RM,RO | awk '
NR == 1 { next }
($5 == "usb" || $6 == "1") { print }
'

echo
echo "== mounted media =="
findmnt -rn -o SOURCE,TARGET,FSTYPE,OPTIONS | grep -E '/media|/mnt' || true

echo
echo "== whole disks =="
lsblk -dn -o PATH,SIZE,MODEL,TRAN,RM,RO
