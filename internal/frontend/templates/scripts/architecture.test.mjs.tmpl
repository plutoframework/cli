import { test } from "node:test";
import assert from "node:assert/strict";
import { checkSource, checkGraph } from "./architecture.mjs";
const forbidden = [
  [
    "features/purchases/components/Table.tsx",
    'import {api} from "@/api/client";',
  ],
  [
    "features/purchases/components/Table.tsx",
    'import {api} from "../../../api/client";',
  ],
  [
    "features/purchases/queries.ts",
    'import {secret} from "@/features/session/queries";',
  ],
  [
    "shared/ui/Box.tsx",
    'import {useSession} from "@/features/session/public";',
  ],
  ["api/helper.ts", 'export {useSession} from "@/features/session/public";'],
  ["features/purchases/helper.ts", 'import("@/features/session/queries");'],
  [
    "features/purchases/components/Table.tsx",
    'window.fetch("/api/purchases");',
  ],
  ["features/purchases/components/Table.tsx", "new XMLHttpRequest();"],
  ["features/purchases/components/Table.tsx", "useEffect(()=>{},[]);"],
  [
    "features/session/helper.ts",
    'window.localStorage.setItem("session","unsafe");',
  ],
];
for (const [name, source] of forbidden)
  test(`rejects ${name}: ${source}`, () =>
    assert.ok(checkSource(name, source).length));
test("allows public feature contracts and feature-owned query modules", () => {
  assert.deepEqual(
    checkSource(
      "features/purchases/queries.ts",
      'import {api} from "@/api/client"; import {scopeKey} from "@/features/workspace/public";',
    ),
    [],
  );
  assert.deepEqual(
    checkSource(
      "features/purchases/pages/List.tsx",
      'import {usePurchases} from "../queries";',
    ),
    [],
  );
});

test("leaf components cannot reach transport through a helper", () => {
  const sources = new Map([
    [
      "features/purchases/components/Table.tsx",
      'import {load} from "../helper";',
    ],
    ["features/purchases/helper.ts", 'import {load} from "./queries";'],
    ["features/purchases/queries.ts", 'import {api} from "@/api/client";'],
    ["api/client.ts", "export const api = {};"],
  ]);
  assert.ok(
    checkGraph(sources).some((message) => message.includes("leaf UI reaches")),
  );
  sources.set(
    "features/purchases/components/Table.tsx",
    'import type {Purchase} from "../queries";',
  );
  assert.deepEqual(checkGraph(sources), []);
});
