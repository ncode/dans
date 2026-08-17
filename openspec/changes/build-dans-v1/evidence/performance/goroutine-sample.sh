#!/bin/sh
set -eu

container=dans-baseline-final-20260817-dans-a-1
ready_url=http://127.0.0.1:29271/readyz

rtk docker stop --timeout 3 "$container" >/dev/null
iteration=1
while [ "$iteration" -le 5 ]; do
	rtk docker start "$container" >/dev/null
	rtk curl --silent --show-error --fail \
		--retry 100 --retry-delay 0 --retry-all-errors --retry-max-time 15 \
		--connect-timeout 1 --max-time 2 "$ready_url" >/dev/null
	rtk sleep 3
	rtk docker kill --signal QUIT "$container" >/dev/null
	rtk docker wait "$container" >/dev/null
	printf '%s\n' "$iteration"
	iteration=$((iteration + 1))
done
