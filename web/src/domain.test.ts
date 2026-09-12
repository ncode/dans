import assert from "node:assert/strict";
import test from "node:test";
import { spawnSync } from "node:child_process";
import {
  canChange,
  sameRRset,
  validateRRset,
  type Delegation,
  type RRset,
} from "./domain.ts";
const zone = { id: "example.test.", name: "example.test.", kind: "Native" };
const rrset: RRset = {
  name: "www.example.test.",
  type: "A",
  ttl: 300,
  records: [{ content: "192.0.2.1" }, { content: "192.0.2.2", disabled: true }],
  comments: [{ content: "Keep this", account: "test", modified_at: 1 }],
};
function grant(overrides: Partial<Delegation> = {}): Delegation {
  return {
    id: "grant",
    zone_binding_id: "binding",
    zone_id: zone.id,
    zone_name: zone.name,
    grantee_kind: "identity",
    grantee_id: "user",
    selectors: [{ kind: "glob", value: "*.example.test." }],
    record_types: ["A"],
    change_kinds: ["REPLACE"],
    created_at: "2026-01-01T00:00:00Z",
    revoked_at: null,
    ...overrides,
  };
}
test("semantic comparison ignores value order and default disabled state but preserves content and comments", () => {
  assert.equal(
    sameRRset(rrset, {
      ...rrset,
      records: [
        { content: "192.0.2.2", disabled: true },
        { content: "192.0.2.1", disabled: false },
      ],
    }),
    true,
  );
  assert.equal(sameRRset(rrset, { ...rrset, ttl: 301 }), false);
  assert.equal(sameRRset(rrset, { ...rrset, comments: [] }), false);
  assert.equal(
    sameRRset(rrset, { ...rrset, records: [{ content: "192.0.2.1" }] }),
    false,
  );
  assert.equal(sameRRset(null, null), true);
  assert.equal(sameRRset(null, rrset), false);
});
test("complete grants match owner, zone, type, and action together", () => {
  assert.equal(
    canChange(false, [grant()], zone, "www.example.test.", "A", "REPLACE"),
    true,
  );
  assert.equal(
    canChange(false, [grant()], zone, "www.example.test.", "AAAA", "REPLACE"),
    false,
  );
  assert.equal(
    canChange(false, [grant()], zone, "www.example.test.", "A", "DELETE"),
    false,
  );
  assert.equal(
    canChange(
      false,
      [
        grant({ selectors: [{ kind: "exact", value: "other.example.test." }] }),
        grant({ record_types: ["AAAA"] }),
      ],
      zone,
      "www.example.test.",
      "A",
      "REPLACE",
    ),
    false,
  );
  assert.equal(
    canChange(
      false,
      [grant({ zone_id: "other.test." })],
      zone,
      "www.example.test.",
      "A",
      "REPLACE",
    ),
    false,
  );
});
test("apex and literal wildcard require exact selectors; glob can span labels", () => {
  assert.equal(
    canChange(false, [grant()], zone, zone.name, "A", "REPLACE"),
    false,
  );
  assert.equal(
    canChange(false, [grant()], zone, "*.example.test.", "A", "REPLACE"),
    false,
  );
  assert.equal(
    canChange(false, [grant()], zone, "a.b.example.test.", "A", "REPLACE"),
    true,
  );
  assert.equal(
    canChange(
      false,
      [grant({ selectors: [{ kind: "exact", value: "*.example.test." }] })],
      zone,
      "*.example.test.",
      "A",
      "REPLACE",
    ),
    true,
  );
  assert.equal(
    canChange(
      false,
      [grant({ selectors: [{ kind: "exact", value: zone.name }] })],
      zone,
      zone.name,
      "A",
      "REPLACE",
    ),
    true,
  );
});
test("revoked grants deny while current operator can write", () => {
  assert.equal(
    canChange(
      false,
      [grant({ revoked_at: "2026-01-02T00:00:00Z" })],
      zone,
      "www.example.test.",
      "A",
      "REPLACE",
    ),
    false,
  );
  assert.equal(canChange(true, [], zone, zone.name, "TXT", "DELETE"), true);
});
test("empty replacement cannot bypass destructive confirmation; TTL and owner are validated", () => {
  assert.match(validateRRset({ ...rrset, records: [] }), /value/i);
  assert.match(validateRRset({ ...rrset, ttl: -1 }), /TTL/);
  assert.match(validateRRset({ ...rrset, name: "relative" }), /absolute/);
  assert.equal(validateRRset(rrset), "");
});

test("root-zone descendants and hyphenated RR types retain backend semantics", () => {
  const root = { id: ".", name: ".", kind: "Native" };
  const rootGrant = grant({
    zone_id: ".",
    zone_name: ".",
    selectors: [{ kind: "glob", value: "*." }],
  });
  assert.equal(
    canChange(false, [rootGrant], root, "www.example.test.", "A", "REPLACE"),
    true,
  );
  assert.equal(canChange(false, [rootGrant], root, ".", "A", "REPLACE"), false);
  assert.equal(validateRRset({ ...rrset, type: "NSAP-PTR" }), "");
});

test("glob punctuation is literal and reverse-zone owners remain editable", () => {
  assert.equal(
    canChange(
      false,
      [grant({ selectors: [{ kind: "glob", value: "a?.example.test." }] })],
      zone,
      "a1.example.test.",
      "A",
      "REPLACE",
    ),
    true,
  );
  assert.equal(
    canChange(
      false,
      [grant({ selectors: [{ kind: "glob", value: "a?.example.test." }] })],
      zone,
      "a1xexample.test.",
      "A",
      "REPLACE",
    ),
    false,
  );
  const reverse = {
    id: "2.0.192.in-addr.arpa.",
    name: "2.0.192.in-addr.arpa.",
    kind: "Native",
  };
  assert.equal(
    canChange(
      false,
      [
        grant({
          zone_id: reverse.id,
          selectors: [{ kind: "exact", value: "1." + reverse.name }],
          record_types: ["PTR"],
        }),
      ],
      reverse,
      "1." + reverse.name,
      "PTR",
      "REPLACE",
    ),
    true,
  );
});

test("valid wildcard grants cannot stall permission rendering", () => {
  const source = `import {canChange} from ${JSON.stringify(new URL("./domain.ts", import.meta.url).href)};
 const zone={id:"example.test.",name:"example.test.",kind:"Native"};
 const grants=[{zone_id:zone.id,revoked_at:null,record_types:null,change_kinds:null,selectors:[{kind:"glob",value:"a*".repeat(18)+"b.example.test."}]}];
 if(canChange(false,grants,zone,"a".repeat(54)+".example.test.","A","REPLACE")) process.exit(1);`;
  const result = spawnSync(
    process.execPath,
    ["--input-type=module", "--eval", source],
    { timeout: 3000 },
  );
  assert.equal(
    result.error,
    undefined,
    "permission matching must finish within a bounded render budget",
  );
  assert.equal(result.status, 0);
});
