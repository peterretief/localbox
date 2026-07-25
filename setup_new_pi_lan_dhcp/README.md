# Raspberry Pi LAN DHCP Boot Card

This directory is only for creating a headless Alpine Raspberry Pi SD card that boots with wired LAN DHCP.

It uses the known-working `headless.apkovl.tar.gz` base overlay, patches wired DHCP into it, and does not install LocalBox, first-boot provisioning, Wi-Fi, or rescue services.

Usage:

```sh
cd /home/peter/projects/LocalBox/localbox-bootstrap/setup_new_pi_lan_dhcp
./write-lan-dhcp-boot-card.sh ../setup_new_pi/alpine-rpi-3.24.1-armv7.tar.gz /dev/sdX
```

The first run prints the exact confirmation command with the detected device serial. Re-run that command to erase and write the card.

Defaults to DHCP on `eth0` and `end0`. To change the wired interfaces:

```sh
IFACES="eth0" ./write-lan-dhcp-boot-card.sh ../setup_new_pi/alpine-rpi-3.24.1-armv7.tar.gz /dev/sdX
```
