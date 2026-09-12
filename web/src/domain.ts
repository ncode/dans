export interface DNSRecord {
  content: string;
  disabled?: boolean;
}
export interface DNSComment {
  content?: string;
  account?: string;
  modified_at?: number;
}
export interface RRset {
  name: string;
  type: string;
  ttl: number;
  records: DNSRecord[];
  comments?: DNSComment[];
}
export interface Zone {
  id: string;
  name: string;
  kind: string;
  serial?: number;
  dnssec?: boolean;
  account?: string;
  masters?: string[];
  nameservers?: string[];
  soa_edit_api?: string;
  api_rectify?: boolean;
  rrsets?: RRset[];
}
export interface Identity {
  id: string;
  handle: string;
  display_name: string | null;
  operator: boolean;
  enabled: boolean;
}
export interface Delegation {
  id: string;
  zone_binding_id: string;
  zone_id?: string;
  zone_name?: string;
  grantee_kind: string;
  grantee_id: string;
  selectors: { kind: "exact" | "glob"; value: string }[];
  record_types: string[] | null;
  change_kinds: string[] | null;
  created_at: string;
  revoked_at: string | null;
}
export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}
export interface BrowsePage extends Page<RRset> {
  state: "indexing" | "ready" | "refreshing" | "stale";
  last_refreshed_at: string | null;
  error: string | null;
}
export function sameRRset(a: RRset | null, b: RRset | null): boolean {
  const semantic = (item: RRset | null) =>
    item === null
      ? null
      : {
          name: item.name.toLowerCase(),
          type: item.type.toUpperCase(),
          ttl: item.ttl,
          records: item.records
            .map((value) =>
              JSON.stringify([value.content, value.disabled ?? false]),
            )
            .sort(),
          comments: (item.comments ?? [])
            .map((value) =>
              JSON.stringify([value.content ?? "", value.account ?? ""]),
            )
            .sort(),
        };
  return JSON.stringify(semantic(a)) === JSON.stringify(semantic(b));
}
export function canChange(
  operator: boolean,
  grants: Delegation[],
  zone: Zone,
  owner: string,
  type: string,
  action: string,
): boolean {
  if (operator) return true;
  const name = owner.toLowerCase();
  const apex = zone.name.toLowerCase();
  if (
    name !== apex &&
    !(apex === "." ? name.endsWith(".") : name.endsWith("." + apex))
  )
    return false;
  return grants.some(
    (grant) =>
      !grant.revoked_at &&
      grant.zone_id === zone.id &&
      (!grant.record_types?.length ||
        grant.record_types.includes(type.toUpperCase())) &&
      (!grant.change_kinds?.length || grant.change_kinds.includes(action)) &&
      grant.selectors.some((selector) => {
        if (selector.kind === "exact") return selector.value === name;
        if (name === apex || name.split(".").includes("*")) return false;
        return matchesGlob(selector.value, name);
      }),
  );
}
// Match canonical patterns without regex backtracking. The last wildcard can
// consume additional characters, so work is bounded by pattern and owner length.
function matchesGlob(pattern: string, value: string): boolean {
  let p = 0,
    v = 0,
    star = -1,
    retry = 0;
  while (v < value.length) {
    if (p < pattern.length && (pattern[p] === "?" || pattern[p] === value[v])) {
      p++;
      v++;
    } else if (pattern[p] === "*") {
      star = p++;
      retry = v;
    } else if (star >= 0) {
      p = star + 1;
      v = ++retry;
    } else return false;
  }
  while (pattern[p] === "*") p++;
  return p === pattern.length;
}
export function validateRRset(rrset: RRset): string {
  if (!rrset.name.endsWith(".") || /\s/.test(rrset.name))
    return "Use an absolute owner name ending in a dot.";
  if (!/^[A-Z][A-Z0-9-]*$/.test(rrset.type))
    return "Enter a valid record type.";
  if (!Number.isInteger(rrset.ttl) || rrset.ttl < 0 || rrset.ttl > 2147483647)
    return "TTL must be a whole number from 0 to 2147483647 seconds.";
  if (
    !rrset.records.length ||
    rrset.records.some((value) => !value.content.trim())
  )
    return "Add at least one nonempty record value. Use Delete to remove an RRset.";
  return "";
}
