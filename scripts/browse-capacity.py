#!/usr/bin/env python3
"""Validate real PowerDNS API ingestion and two application instances.

Use only a disposable environment. Credentials are read from files named by
DANS_CAPACITY_TOKEN_FILE and DANS_CAPACITY_UPSTREAM_KEY_FILE. A cold measurement
requires a fresh application index; --skip-seed reuses DNS
fixtures and may measure a warm index. --lifecycle-only checks delete/recreate
and old-grant denial in a separate disposable zone. The primary and
secondary application origins and PowerDNS origin come from the corresponding
DANS_CAPACITY_PRIMARY_URL, DANS_CAPACITY_SECONDARY_URL, and
DANS_CAPACITY_UPSTREAM_URL environment variables. No database seeding is used.
"""

import argparse
import json
import os
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


def request(origin, path, secret, method="GET", body=None, statuses=(200,)):
    data = None if body is None else json.dumps(body).encode()
    headers = {"X-API-Key": secret, "Content-Type": "application/json"}
    req = urllib.request.Request(origin + path, data, headers, method=method)
    try:
        response = urllib.request.urlopen(req, timeout=120)
    except urllib.error.HTTPError as error:
        response = error
    except (urllib.error.URLError, TimeoutError):
        raise RuntimeError("HTTP transport failed during capacity validation") from None
    with response:
        if response.status not in statuses:
            raise RuntimeError(f"Unexpected HTTP {response.status} during {method}")
        raw = response.read(2 * 1024 * 1024 + 1)
        if len(raw) > 2 * 1024 * 1024:
            raise RuntimeError("Response exceeded the bounded browse check")
        is_json = response.headers.get("Content-Type", "").split(";", 1)[0] == "application/json"
        return (json.loads(raw) if raw and is_json else None), len(raw)


def record(zone, number, content="192.0.2.1", change="REPLACE"):
    result = {"name": f"r{number:06d}.{zone}", "type": "A", "changetype": change}
    if change != "DELETE":
        result.update(ttl=60, records=[{"content": content, "disabled": False},
                                      {"content": "192.0.2.2", "disabled": True}])
    return result


def verify_lifecycle(primary, secondary, token):
    suffix = uuid.uuid4().hex[:8]
    zone = f"console-lifecycle-{suffix}.test."
    owner = "www." + zone
    base = "/api/v1/servers/localhost/zones"
    management = "/api/v1/dans"
    zone_body = {"name": zone, "kind": "Native", "nameservers": ["ns1.example.test."]}
    request(primary, base, token, "POST", zone_body, (201,))
    identity, _ = request(primary, management + "/identities", token, "POST",
                          {"kind": "service", "handle": "lifecycle-" + suffix}, (201,))
    credential, _ = request(primary, management + "/identities/" + identity["id"] + "/tokens",
                            token, "POST", {"label": "lifecycle-check"}, (201,))
    writer = credential["secret"]

    def binding():
        result, _ = request(secondary, management + "/zone-bindings?" +
                            urllib.parse.urlencode({"zone_name": zone, "status": "active"}), token)
        assert len(result["items"]) == 1, "zone has no unique active lifetime"
        return result["items"][0]["id"]

    old_binding = binding()
    grant, _ = request(primary, management + "/delegations", token, "POST",
                       {"zone_binding_id": old_binding, "identity_id": identity["id"],
                        "selectors": [{"kind": "exact", "value": owner}]}, (201,))
    change = {"rrsets": [{"name": owner, "type": "A", "changetype": "REPLACE", "ttl": 60,
                          "records": [{"content": "192.0.2.41", "disabled": False}]}]}
    request(primary, base + "/" + zone, writer, "PATCH", change, (204,))

    def observed(expected):
        deadline = time.monotonic() + 30
        path = management + "/servers/localhost/zones/" + zone + "/rrsets?" + \
            urllib.parse.urlencode({"name": owner, "match": "exact", "type": "A"})
        while time.monotonic() < deadline:
            page, _ = request(secondary, path, writer, statuses=(200, 202))
            if page["state"] == "ready" and len(page["items"]) == expected:
                return
            time.sleep(.1)
        raise RuntimeError("Zone lifetime browse did not reconcile")

    observed(1)
    request(primary, base + "/" + zone, token, "DELETE", statuses=(204,))
    retired, _ = request(secondary, management + "/zone-bindings/" + old_binding, token)
    revoked, _ = request(secondary, management + "/delegations/" + grant["id"], token)
    assert retired["status"] == "retired", "deleted lifetime stayed active"
    assert revoked["revoked_at"] is not None, "old delegation survived deletion"
    request(secondary, base + "/" + zone, token, statuses=(404,))
    request(secondary, base, token, "POST", zone_body, (201,))
    assert binding() != old_binding, "recreation reused retired lifetime"
    request(primary, base + "/" + zone, writer, "PATCH", change, (403,))
    observed(0)
    request(primary, base + "/" + zone, token, "DELETE", statuses=(204,))
    request(primary, management + "/identities/" + identity["id"] + "/tokens/" + credential["id"],
            token, "DELETE", statuses=(204,))
    request(primary, management + "/identities/" + identity["id"], token, "PATCH", {"enabled": False})
    print(json.dumps({"phase": "lifecycle_verified", "delegated_write": True,
                      "retired_binding": True, "revoked_grant": True,
                      "recreated_zone_denied_old_grant": True,
                      "recreated_index_excludes_old_records": True}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--zones", type=int, default=400)
    parser.add_argument("--rrsets", type=int, default=250000)
    parser.add_argument("--skip-seed", action="store_true")
    parser.add_argument("--lifecycle-only", action="store_true")
    args = parser.parse_args()
    if args.zones < 1 or args.rrsets < 101:
        parser.error("at least one zone and 101 RRsets are required")
    primary = os.environ["DANS_CAPACITY_PRIMARY_URL"].rstrip("/")
    secondary = os.environ["DANS_CAPACITY_SECONDARY_URL"].rstrip("/")
    upstream = os.environ["DANS_CAPACITY_UPSTREAM_URL"].rstrip("/")
    token = Path(os.environ["DANS_CAPACITY_TOKEN_FILE"]).read_text().strip()
    key = Path(os.environ["DANS_CAPACITY_UPSTREAM_KEY_FILE"]).read_text().strip()
    if args.lifecycle_only:
        verify_lifecycle(primary, secondary, token)
        return
    large = "console-capacity.test."
    zones = [f"console-capacity-{i:03d}.test." for i in range(1, args.zones)] + [large]
    upstream_base = "/api/v1/servers/localhost/zones"
    browse_base = "/api/v1/dans/servers/localhost/zones"
    seed_start = time.monotonic()
    if not args.skip_seed:
        for zone in zones:
            request(upstream, upstream_base, key, "POST",
                    {"name": zone, "kind": "Native", "nameservers": ["ns1.example.test."]},
                    statuses=(201, 409, 422))
        for offset in range(0, args.rrsets, 1000):
            batch = [record(large, i) for i in range(offset, min(offset + 1000, args.rrsets))]
            request(upstream, upstream_base + "/" + large, key, "PATCH", {"rrsets": batch}, (204,))
            if (offset + len(batch)) % 50000 == 0:
                print(json.dumps({"phase": "seeding", "rrsets_written": offset + len(batch)}), flush=True)
        print(json.dumps({"phase": "seed", "zones": args.zones, "synthetic_rrsets": args.rrsets,
                          "seconds": round(time.monotonic() - seed_start, 3)}), flush=True)

    def browse(zone=large, query="", origin=secondary):
        body, size = request(origin, browse_base + "/" + zone + "/rrsets" + query, token,
                             statuses=(200, 202))
        assert len(body["items"]) <= 100, "browse exceeded 100 RRsets"
        assert all(field in body for field in ("state", "error", "last_refreshed_at", "next_cursor"))
        return body, size

    cold_start = time.monotonic()
    for zone in zones:
        browse(zone, origin=primary)
    deadline = time.monotonic() + 300
    while True:
        first, largest_response = browse()
        if first["state"] == "ready":
            break
        if time.monotonic() > deadline:
            raise RuntimeError("Index did not become ready within five minutes")
        time.sleep(.1)
    cold = time.monotonic() - cold_start
    count, pages, synthetic, previous = 0, 0, 0, None
    page = first
    while True:
        for item in page["items"]:
            order = (item["name"], item["type"])
            assert previous is None or previous < order, "duplicate or unordered RRset"
            previous = order
            if item["name"].startswith("r") and item["type"] == "A":
                assert item["name"] == f"r{synthetic:06d}.{large}", "synthetic RRset omitted"
                synthetic += 1
                assert len(item["records"]) == 2, "incomplete RRset values"
                assert any(value["content"] == "192.0.2.2" and value["disabled"]
                           for value in item["records"]), "disabled record state was lost"
            count += 1
        pages += 1
        cursor = page["next_cursor"]
        if cursor is None:
            break
        page, size = browse(query="?" + urllib.parse.urlencode({"cursor": cursor}))
        largest_response = max(largest_response, size)
    assert synthetic == args.rrsets, "browse omitted synthetic RRsets"
    warm_start = time.monotonic()
    filtered, size = browse(query="?" + urllib.parse.urlencode({"name": "r249", "type": "A"}))
    warm_ms = (time.monotonic() - warm_start) * 1000
    assert all(item["name"].startswith("r249") and item["type"] == "A" for item in filtered["items"])
    largest_response = max(largest_response, size)
    for zone in zones:
        deadline = time.monotonic() + 300
        while browse(zone)[0]["state"] != "ready":
            if time.monotonic() > deadline:
                raise RuntimeError("A requested zone did not finish indexing")
            time.sleep(.1)

    def mutate(change, content="192.0.2.99"):
        request(primary, upstream_base + "/" + large, token, "PATCH",
                {"rrsets": [record(large, 0, content, change)]}, (204,))
        start = time.monotonic()
        query = "?" + urllib.parse.urlencode({"name": f"r000000.{large}", "match": "exact", "type": "A"})
        while time.monotonic() - start < 5:
            page, _ = browse(query=query)
            if change == "DELETE" and not page["items"]:
                return (time.monotonic() - start) * 1000
            if change != "DELETE" and page["items"] and any(record["content"] == content for record in page["items"][0]["records"]):
                return (time.monotonic() - start) * 1000
            time.sleep(.02)
        raise RuntimeError("Cross-instance mutation visibility exceeded five seconds")

    replace_ms = mutate("REPLACE")
    delete_ms = mutate("DELETE")
    restore_ms = mutate("REPLACE", "192.0.2.1")
    print(json.dumps({"phase": "verified", "zones": args.zones, "rrsets_traversed": count, "synthetic_rrsets_verified": synthetic,
                      "pages": pages, "cold_seconds": round(cold, 3),
                      "warm_filtered_ms": round(warm_ms, 3), "max_page_bytes": largest_response,
                      "replace_visibility_ms": round(replace_ms, 3),
                      "delete_visibility_ms": round(delete_ms, 3),
                      "restore_visibility_ms": round(restore_ms, 3),
                      "total_seconds": round(time.monotonic() - cold_start, 3)}), flush=True)


if __name__ == "__main__":
    main()
