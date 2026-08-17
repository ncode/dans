#!/bin/sh
set -eu

test "$#" -eq 3 || {
	echo "usage: $0 binary-bytes|image-rootfs-bytes|rss-kib VALUE LABEL" >&2
	exit 2
}

metric=$1
value=$2
label=$3
case "$value" in
'' | *[!0-9]*)
	echo "$label has invalid footprint value: $value" >&2
	exit 2
	;;
esac

case "$metric" in
binary-bytes)
	maximum=25165824
	description='byte release size'
	unit=bytes
	;;
image-rootfs-bytes)
	maximum=27262976
	description='byte OCI root filesystem'
	unit=bytes
	;;
rss-kib)
	maximum=65536
	description='KiB ready-idle VmRSS'
	unit=KiB
	;;
*)
	echo "unsupported footprint metric: $metric" >&2
	exit 2
	;;
esac

test "$value" -le "$maximum" || {
	echo "$label exceeds $maximum-$description budget ($value $unit)" >&2
	exit 1
}
