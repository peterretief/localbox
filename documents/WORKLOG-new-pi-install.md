# LocalBox New Pi Install Worklog

Date: 2026-07-11

This is the working log for preparing a new Raspberry Pi LocalBox install. Keep each command, device name, and failure here first; once the flow is proven, move the stable steps into `HOWTO-new-pi.md` or scripts.

## Safety Rules

- Never format or write an image to a device until `lsblk` output has been recorded.
- Use whole disk devices for imaging, for example `/dev/sdf`, not `/dev/sdf1`.
- Use partition devices for formatting/mounting individual filesystems, for example `/dev/sdf1`.
- Before destructive writes, unmount all partitions on the target device.
- For destructive scripts, require an explicit confirmation variable containing the target device path.

## Current Device Inventory

Command:

```sh
lsblk -o NAME,PATH,SIZE,MODEL,TRAN,RM,RO,FSTYPE,LABEL,UUID,MOUNTPOINTS
```

Observed:

```text
/dev/sda  238.5G internal SATA system disk
/dev/sdb  931.5G internal SATA storage disk, mounted at /media/peter/storage 1
/dev/sdc  931.5G internal SATA storage disk, mounted at /media/peter/storage 2
/dev/sdd  238.5G USB disk, partitions:
  /dev/sdd1 1M
  /dev/sdd2 513M vfat
  /dev/sdd3 238G ext4 mounted at /media/peter/a70b4dc5-6faf-46c1-aa28-36897b768c5e
```

Problem found:

```text
The new SD card and USB flash drive are not clearly visible yet.
The visible USB disk /dev/sdd looks like a 238.5G Ubuntu install, not a small removable Pi boot card or USB flash drive.
Several media mounts are read-only with errors=remount-ro.
```

Action:

```text
Do not format anything yet. Reinsert the new SD card and USB flash drive one at a time, then rerun lsblk after each insertion.
```

## Target Device Table

Fill this before formatting or imaging:

```text
Alpine OS source image:   ______________________________
New SD card whole disk:   /dev/____
New SD boot partition:    /dev/____1
USB flash whole disk:     /dev/____
USB flash partition:      /dev/____1
Pi hostname:              ______________________________
Pi LAN IP after boot:     ______________________________
Pi Tailscale IP:          ______________________________
```

## Planned Flow

1. Identify devices with `lsblk`.
2. Save or select a known-good Alpine OS image.
3. Write Alpine OS image to the new SD card.
4. Mount the new SD card boot partition.
5. Prepare the Alpine overlay with `prepare-pi-boot-card.sh`.
6. Format the USB flash drive for `/mnt/data`.
7. Put SD card and USB flash drive into the Pi.
8. Boot Pi on Ethernet.
9. Find Pi on LAN with `nmap -sn 192.168.0.0/24`.
10. SSH in and verify `rc-status`, Tailscale, LocalBox services, and persistence.
11. Deploy a domain config with `deploy-domain-from-config.sh`.

## Commands To Capture Next

Run after inserting the new SD card:

```sh
lsblk -o NAME,PATH,SIZE,MODEL,TRAN,RM,RO,FSTYPE,LABEL,UUID,MOUNTPOINTS
```

## Device Inventory After Plugging In SD Card And USB Flash

Command:

```sh
lsblk -o NAME,PATH,SIZE,MODEL,TRAN,RM,RO,FSTYPE,LABEL,UUID,MOUNTPOINTS
lsblk -S -o NAME,HCTL,TYPE,VENDOR,MODEL,REV,SERIAL,TRAN
```

Observed new devices:

```text
/dev/sde  0B     Generic MassStorageClass USB reader slot, no media
/dev/sdf  29.8G  Generic MassStorageClass USB reader slot, removable
  /dev/sdf1   32M vfat     hassos-boot
  /dev/sdf2   24M squashfs mounted at /media/peter/disk
  /dev/sdf3  256M erofs    mounted at /media/peter/fbc8ac0a-f0b9-45da-95a5-fd9d7ba840d6
  /dev/sdf4   24M
  /dev/sdf5  256M
  /dev/sdf6    8M
  /dev/sdf7   96M ext4     hassos-overlay mounted at /media/peter/hassos-overlay
  /dev/sdf8 29.1G ext4     hassos-data mounted at /media/peter/hassos-data

/dev/sdg  57.3G SanDisk 3.2Gen1 USB flash drive, removable
  /dev/sdg1  3.2G iso9660  Ubuntu-Server 24.04.4 LTS amd64 mounted at /media/peter/Ubuntu-Server 24.04.4 LTS amd64
  /dev/sdg2    5M vfat     ESP
  /dev/sdg3  300K
  /dev/sdg4 54.1G ext4     writable mounted at /media/peter/writable
```

Interpretation:

```text
/dev/sde is the empty logical slot in the multi-card reader, not a target.
/dev/sdf is the likely SD card. It currently contains Home Assistant OS data and will be overwritten if used for LocalBox Alpine.
/dev/sdg is the likely USB flash drive. It currently contains an Ubuntu Server installer and writable partition and will be erased when formatted for LocalBox data.
```

Problem found:

```text
Both target devices contain existing OS/data layouts. Confirm they are disposable before any formatting or image write.
The desktop auto-mounted several target partitions read-only. Unmount them before destructive operations.
No saved LocalBox Alpine `.img.gz` exists in `localbox-bootstrap/images` yet.
```

Run after inserting the USB flash drive:

```sh
lsblk -o NAME,PATH,SIZE,MODEL,TRAN,RM,RO,FSTYPE,LABEL,UUID,MOUNTPOINTS
```
