#!/bin/sh

# isns build command changes slightly based on architecture, so this script helps run it
arch=$(uname -m)

case ${arch} in
	x86_64|amd64)
		;;
	aarch64|arm64)
		cp $(ls -1tRd /usr/share/automake-* | head -1)/config.* aclocal/
		;;
	*)
		echo "Unknown arch ${arch}"
		exit 1
esac
