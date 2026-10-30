import assert from "node:assert/strict";
import { test } from "node:test";
import path from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import ts from "typescript";
import { ESLint } from "eslint";
import { formatSource } from "./format.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const file = path.join(root, "src/readability-fixture.ts");

test("dense control flow becomes readable without changing its result", async () => {
  const dense =
    'export function decide(ok:boolean){const value=1;if(ok)return {kind:"done",value};return {kind:"idle",value:0};}';
  const expanded = await formatSource(dense, file);
  assert.match(expanded, /if \(ok\) \{\n/);
  assert.match(expanded, /\n\s*kind: "done",\n\s*value,/);
  assert.match(expanded, /\n\n\s*return/);
  assert.equal(await formatSource(expanded, file), expanded);
  const execute = (source) => {
    const output = ts.transpileModule(source, {
      compilerOptions: { module: ts.ModuleKind.CommonJS },
    }).outputText;
    const context = { exports: {} };
    vm.runInNewContext(output, context);
    return [context.exports.decide(true), context.exports.decide(false)];
  };
  assert.equal(
    JSON.stringify(execute(dense)),
    JSON.stringify(execute(expanded)),
  );
});

test("typed records and callback bodies expand consistently", async () => {
  const dense =
    'export type State={kind:"pending";requestId:string};export const run=()=>{return {kind:"pending",requestId:"id"};};';
  const expanded = await formatSource(dense, file);
  assert.match(expanded, /kind: "pending";\n\s*requestId: string;/);
  assert.equal(await formatSource(expanded, file), expanded);
  const eslint = new ESLint({ cwd: root });
  const [result] = await eslint.lintText(expanded, { filePath: file });
  assert.equal(result.errorCount, 0, JSON.stringify(result.messages));
});

test("nested ternaries produce actionable diagnostics rather than silent rewrites", async () => {
  const eslint = new ESLint({ cwd: root });
  const [result] = await eslint.lintText(
    'export const label=(a:boolean,b:boolean)=>a?"A":b?"B":"C";',
    { filePath: file },
  );
  assert(
    result.messages.some((message) => message.ruleId === "no-nested-ternary"),
  );
});
