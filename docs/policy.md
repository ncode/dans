# DANS policy and compatibility model

DANS is an allow-only policy boundary in front of one PowerDNS Authoritative API. Authenticated identities share DNS reads. Operators manage identities, groups, bindings, delegations, zone lifecycles, and every sensitive PowerDNS operation; a non-operator may only patch RRsets that a current delegation completely authorizes.

## Route classes

| Class | Routes | Authority |
| --- | --- | --- |
| Anonymous | `GET /livez`, `GET /readyz` | No credential; response contains only status. |
| Authenticated DNS read | zone list/get/export and search-data | Any enabled identity with an active token; DNS reads are not filtered. |
| Delegated write | zone `PATCH` | Operators, or a non-operator whose current grants authorize every RRset tuple in the batch. |
| Operator-only PowerDNS | Every other pinned PowerDNS operation | Enabled operator only, including server/config/statistics, metadata, keys, DNSSEC, zone create/delete, and all other mutations. |
| Self service | `/api/v1/dans/me` profile, groups, effective delegations, and tokens | The enabled identity may read its own state and create/revoke only its own tokens. |
| Operator management | Other `/api/v1/dans` resources and audit history | Enabled operator only. |

Every operation in the combined contract has exactly one checked access class. Unknown routes, methods, and future PowerDNS operations are not forwarded.

## Selector rules

At delegation creation, DANS converts owner names to lowercase canonical ASCII A-label form and stores absolute names ending in a dot. Selectors must remain inside the binding's zone and have an explicit kind:

- `exact` treats every character literally.
- `glob` uses only `*` for zero or more canonical-name characters and `?` for exactly one character. Either wildcard can cross dots; the match is anchored to the entire absolute name.

This is Route 53-style glob matching, not a regular expression. For example, `*.apps.example.com.` matches both `one.apps.example.com.` and `one.two.apps.example.com.`, while `api.?.example.com.` matches exactly one canonical character between the dots.

A delegation optionally restricts RR types and change kinds. Omitting `record_types` means every PowerDNS-supported type, including `PTR`. Omitting `change_kinds` means `REPLACE`, `DELETE`, `EXTEND`, and `PRUNE`. An empty array is invalid.

One grant must match a tuple's binding, complete owner selector, type, and change kind together. Restrictions from separate grants cannot be combined to authorize one tuple. Different grants may authorize different tuples in the same batch, but if any tuple is denied DANS rejects the entire batch without forwarding any part.

## CLI policy workflow

Create an identity, a binding for the current zone lifetime, and a delegation through strict JSON bodies. IDs below are placeholders returned by earlier commands.

```sh
printf '%s\n' '{"handle":"app-team","kind":"service"}' |
  dans --output json identities create --data -

printf '%s\n' '{"zone_id":"example.com."}' |
  dans --output json bindings create --data -

cat >delegation.json <<'JSON'
{
  "zone_binding_id": "11111111-1111-4111-8111-111111111111",
  "identity_id": "22222222-2222-4222-8222-222222222222",
  "selectors": [{"kind": "glob", "value": "*.apps.example.com."}],
  "record_types": ["A", "AAAA"],
  "change_kinds": ["REPLACE", "DELETE"]
}
JSON
dans delegations create --data delegation.json
```

For group authority, create a group, add direct members, and use `group_id` instead of `identity_id`:

```sh
printf '%s\n' '{"handle":"application-teams"}' |
  dans --output json groups create --data -
dans groups members add GROUP_UUID IDENTITY_UUID
```

Groups are not nested. Disabling a group suspends its retained memberships and delegations; re-enabling it restores their contribution to later decisions.

## Forward-record examples

The glob delegation above permits an `A` or `AAAA` replacement below `apps.example.com.` but does not permit another type or action:

```sh
dans rrsets replace example.com. api.apps.example.com. A \
  --ttl 300 --value 192.0.2.10
```

`EXTEND` and `PRUNE` still require authority over the whole owner-and-type RRset; DANS does not assign ownership to individual values.

## PTR example

PTR is authorized by its reverse owner name, never by its target RDATA. A reverse-zone delegation can be:

```json
{
  "zone_binding_id": "33333333-3333-4333-8333-333333333333",
  "group_id": "44444444-4444-4444-8444-444444444444",
  "selectors": [{"kind": "glob", "value": "*.2.0.192.in-addr.arpa."}],
  "record_types": ["PTR"],
  "change_kinds": ["REPLACE", "DELETE"]
}
```

An authorized member can then replace the complete PTR RRset:

```sh
dans rrsets replace 2.0.192.in-addr.arpa. \
  10.2.0.192.in-addr.arpa. PTR --ttl 300 --value host.example.com.
```

## Apex and literal wildcard protection

Glob selectors never authorize the zone apex or an owner containing a literal DNS wildcard label, even when their characters match. Each needs an exact selector:

```json
{
  "zone_binding_id": "11111111-1111-4111-8111-111111111111",
  "identity_id": "22222222-2222-4222-8222-222222222222",
  "selectors": [
    {"kind": "exact", "value": "example.com."},
    {"kind": "exact", "value": "*.example.com."}
  ],
  "record_types": ["A", "TXT"],
  "change_kinds": ["REPLACE"]
}
```

In the second exact selector, `*` is the literal wildcard owner label, not a pattern operator.

## Explicit v1 limitations

- One configured PowerDNS Authoritative upstream in `>=5.1.3,<5.2`; there is no multi-upstream routing, write failover, queue, replay, or automatic mutation retry.
- PostgreSQL 16–18 current minors on one writable primary; no alternate state engine, authorization replica, or policy cache.
- Exact and `*`/`?` glob selectors only; no regular expressions, explicit deny rules, precedence rules, nested groups, per-value ownership, or field-level RRset ownership.
- Shared authenticated DNS reads; no per-identity read filtering.
- No inheritance across a retired/recreated zone lifetime. Out-of-band PowerDNS lifecycle changes are unsupported and are not represented as DANS-audited mutations.
- Coordinated same-version upgrades only. Mixed-version rolling operation and in-place downgrade after migration are unsupported.
- No PowerDNS metrics/web UI/undocumented-route proxy, generic CLI raw-request escape hatch, web UI, Helm chart/operator, application metrics, or tracing.
- Audit history is append-only and retained indefinitely; v1 has no edit, delete, or prune operation.

These limits are security boundaries, not implicit fallbacks. DANS fails closed when current PostgreSQL authorization state, audit persistence, schema compatibility, or the supported upstream is unavailable.
