#!/bin/sh
set -eu

test "$#" -eq 5 || {
	echo "usage: $0 COMPOSE_FILE PROJECT SERVICE HTTP_ROOT OUTPUT_DIR" >&2
	exit 2
}
test "$(uname -s)" = Linux || {
	echo 'runtime measurement requires a native Linux Docker host' >&2
	exit 2
}

compose_file=$1
project=$2
service=$3
http_root=$4
output=$5
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
health_samples=$output/time-to-health.tsv
process_samples=$output/ready-idle-process.tsv
summary=$output/summary.txt

mkdir -p "$output"

compose() {
	docker compose --project-name "$project" --file "$compose_file" "$@"
}

monotonic_ns() {
	awk '{ printf "%.0f\n", $1 * 1000000000 }' /proc/uptime
}

wait_for_200() {
	url=$1
	deadline=$2
	while :; do
		if [ "$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 1 "$url" || true)" = 200 ]; then
			monotonic_ns
			return 0
		fi
		now=$(monotonic_ns)
		[ "$now" -le "$deadline" ] || {
			echo "timed out waiting for $url" >&2
			return 1
		}
	done
}

printf 'run\tlive_ns\tready_ns\n' >"$health_samples"
for run in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
	compose stop --timeout 5 "$service" >/dev/null
	started=$(monotonic_ns)
	deadline=$((started + 5000000000))
	compose start "$service" >/dev/null
	live=$(wait_for_200 "$http_root/livez" "$deadline")
	ready=$(wait_for_200 "$http_root/readyz" "$deadline")
	printf '%s\t%s\t%s\n' "$run" "$((live - started))" "$((ready - started))" >>"$health_samples"
done

container=$(compose ps --quiet "$service")
pid=$(docker inspect --format '{{.State.Pid}}' "$container")
[ -r "/proc/$pid/status" ] || {
	echo "local Docker PID $pid is not readable in host /proc" >&2
	exit 1
}

printf 'sample\tvmrss_kib\trssanon_kib\trssfile_kib\tthreads\tfds\n' >"$process_samples"
for sample in 1 2 3 4 5; do
	sleep 1
	vmrss=$(awk '/^VmRSS:/ { print $2 }' "/proc/$pid/status")
	rssanon=$(awk '/^RssAnon:/ { print $2 }' "/proc/$pid/status")
	rssfile=$(awk '/^RssFile:/ { print $2 }' "/proc/$pid/status")
	threads=$(awk '/^Threads:/ { print $2 }' "/proc/$pid/status")
	fds=$(ls -1 "/proc/$pid/fd" | awk 'END { print NR }')
	"$script_dir/footprint.sh" rss-kib "$vmrss" "$service"
	case "$rssanon:$rssfile:$threads:$fds" in
	*[!0-9:] | :* | *::* | *:) echo "could not read complete process metrics for $service" >&2; exit 1 ;;
	esac
	printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$sample" "$vmrss" "$rssanon" "$rssfile" "$threads" "$fds" >>"$process_samples"
done

summarize() {
	label=$1
	file=$2
	column=$3
	unit=$4
	values=$output/.$label.values
	awk -v column="$column" 'NR > 1 { print $column }' "$file" | sort -n >"$values"
	count=$(awk 'END { print NR }' "$values")
	minimum=$(sed -n '1p' "$values")
	maximum=$(sed -n "${count}p" "$values")
	if [ $((count % 2)) -eq 1 ]; then
		middle=$((count / 2 + 1))
		median=$(sed -n "${middle}p" "$values")
	else
		lower=$(sed -n "$((count / 2))p" "$values")
		upper=$(sed -n "$((count / 2 + 1))p" "$values")
		median=$(( (lower + upper) / 2 ))
	fi
	# Nearest-rank p95: ceil(0.95 * count).
	p95_rank=$(( (95 * count + 99) / 100 ))
	p95=$(sed -n "${p95_rank}p" "$values")
	rm -f "$values"
	printf '%s: n=%s median=%s%s range=%s..%s%s p95=%s%s\n' "$label" "$count" "$median" "$unit" "$minimum" "$maximum" "$unit" "$p95" "$unit"
}

{
	summarize live_ns "$health_samples" 2 ns
	summarize ready_ns "$health_samples" 3 ns
	summarize vmrss_kib "$process_samples" 2 KiB
	summarize threads "$process_samples" 5 ''
	summarize fds "$process_samples" 6 ''
} >"$summary"

sed 's/^/integration-performance: /' "$summary"
