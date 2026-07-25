# LAN DHCP Boot Patch

This patches an already-mounted Alpine Raspberry Pi boot partition so the Pi boots with LAN DHCP.

It does not format, partition, erase, or image the SD card.

Mount the boot partition read-write, then run:

```sh
BOOT_MOUNT=/mnt/piboot ./patch-boot-dhcp.sh
```

Defaults to LAN DHCP on `eth0` and `end0`.

To use different interfaces:

```sh
IFACES="eth0" BOOT_MOUNT=/mnt/piboot ./patch-boot-dhcp.sh
```
