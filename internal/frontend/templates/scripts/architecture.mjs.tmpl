import ts from "typescript";
import { readFile, readdir } from "node:fs/promises";
import { resolve, relative, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../src/", import.meta.url));
export function checkSource(filename, source) {
  const issues = [];
  const file = ts.createSourceFile(
    filename,
    source,
    ts.ScriptTarget.Latest,
    true,
    filename.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const feature = filename.match(/^features\/([^/]+)\//)?.[1];
  const presentation =
    /\/components\/|\/forms\/|\/pages\//.test(filename) ||
    filename.startsWith("shared/ui/");
  function dependency(specifier) {
    if (!specifier.startsWith("@/") && !specifier.startsWith(".")) return;
    const target = specifier.startsWith("@/")
      ? specifier.slice(2)
      : relative(root, resolve(root, dirname(filename), specifier)).replaceAll(
          "\\",
          "/",
        );
    if (filename.startsWith("shared/") && !target.startsWith("shared/"))
      issues.push("Shared code cannot depend on features/app/API");
    if (filename.startsWith("api/") && !target.startsWith("api/"))
      issues.push("Transport cannot depend on features/app/shared");
    if (feature && target.startsWith("app/"))
      issues.push("Features cannot depend on application composition");
    const importedFeature = target.match(/^features\/([^/]+)\/(.*)$/);
    if (
      importedFeature &&
      importedFeature[1] !== feature &&
      !/^public(?:\.tsx?)?$/.test(importedFeature[2])
    )
      issues.push("Cross-feature imports must use public.ts");
    if (presentation && /^api\/(client|generated)/.test(target))
      issues.push(
        "Presentational code must use feature hooks/types, not transport/generated bindings",
      );
    if (target === "api/client" && !/(?:queries|mutations)\.ts$/.test(filename))
      issues.push(
        "Only feature query/mutation modules may import the HTTP client",
      );
  }
  function visit(node) {
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
      if (node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier))
        dependency(node.moduleSpecifier.text);
    }
    if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
      if (node.expression.kind === ts.SyntaxKind.ImportKeyword) {
        const arg = node.arguments?.[0];
        if (!arg || !ts.isStringLiteral(arg))
          issues.push("Dynamic imports must use a literal path");
        else dependency(arg.text);
      }
      const callee = node.expression.getText(file);
      if (
        /(^|\.)(fetch|XMLHttpRequest|WebSocket|EventSource|sendBeacon)$/.test(
          callee,
        ) &&
        filename !== "api/client.ts"
      )
        issues.push("Network access belongs in api/client.ts");
      if (callee === "require") issues.push("Use static ES module imports");
      if (presentation && /(^|\.)useEffect$/.test(callee))
        issues.push(
          "UI effects require an explicitly reviewed integration hook; data belongs in feature queries",
        );
      if (/(^|\.)(localStorage|sessionStorage)\./.test(callee))
        issues.push(
          "Do not persist session or tenant state in browser storage",
        );
    }
    if (
      ts.isImportDeclaration(node) &&
      ts.isStringLiteral(node.moduleSpecifier)
    ) {
      const module = node.moduleSpecifier.text;
      if (
        ["axios", "openapi-fetch", "graphql-request", "ky"].includes(module) &&
        filename !== "api/client.ts"
      )
        issues.push("Transport dependencies belong in api/client.ts");
      if (presentation && module === "@tanstack/react-query")
        issues.push(
          "UI components use feature hooks, not query-cache internals",
        );
    }
    if (
      ts.isIdentifier(node) &&
      ["localStorage", "sessionStorage"].includes(node.text)
    )
      issues.push(
        "Browser storage is not an authority for session or tenant state",
      );
    ts.forEachChild(node, visit);
  }
  visit(file);
  if (source.split("\n").length > 1000)
    issues.push("File exceeds the 1000-line source ceiling");
  return [...new Set(issues)];
}

async function files(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  return (
    await Promise.all(
      entries.map(async (entry) =>
        entry.isDirectory()
          ? files(resolve(directory, entry.name))
          : [resolve(directory, entry.name)],
      ),
    )
  ).flat();
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const failures = [];
  const sources = new Map();
  for (const path of await files(root)) {
    const name = relative(root, path).replaceAll("\\", "/");
    if (!/\.tsx?$/.test(name)) continue;
    const source = await readFile(path, "utf8");
    const issues = checkSource(name, source);
    if (!name.includes(".test.") && !name.startsWith("test/"))
      sources.set(name, source);
    // Behavior tests may fake transport directly. Production modules remain checked.
    if (!name.includes(".test.") && !name.startsWith("test/"))
      failures.push(...issues.map((issue) => `${name}: ${issue}`));
  }
  failures.push(...checkGraph(sources));
  if (failures.length) throw new Error(failures.join("\n"));
  console.log("Frontend dependency boundaries and source limits verified");
}

// Leaf UI cannot hide network access behind another local module. Type-only
// dependencies are erased; pages are allowed to compose feature query hooks.
export function checkGraph(sources) {
  const graph = new Map();
  for (const [filename, source] of sources) {
    const file = ts.createSourceFile(
      filename,
      source,
      ts.ScriptTarget.Latest,
      true,
    );
    const dependencies = [];
    for (const statement of file.statements) {
      if (
        !ts.isImportDeclaration(statement) &&
        !ts.isExportDeclaration(statement)
      )
        continue;
      if (
        !statement.moduleSpecifier ||
        !ts.isStringLiteral(statement.moduleSpecifier)
      )
        continue;
      if (statement.isTypeOnly || statement.importClause?.isTypeOnly) continue;
      const specifier = statement.moduleSpecifier.text;
      if (!specifier.startsWith(".") && !specifier.startsWith("@/")) continue;
      const target = specifier.startsWith("@/")
        ? specifier.slice(2)
        : relative(
            root,
            resolve(root, dirname(filename), specifier),
          ).replaceAll("\\", "/");
      const found = [
        target,
        `${target}.ts`,
        `${target}.tsx`,
        `${target}/index.ts`,
      ].find((name) => sources.has(name));
      if (found) dependencies.push(found);
    }
    graph.set(filename, dependencies);
  }
  const failures = [];
  for (const filename of sources.keys()) {
    if (
      !/\/components\/|\/forms\//.test(filename) &&
      !filename.startsWith("shared/ui/")
    )
      continue;
    const seen = new Set();
    function visit(current) {
      if (seen.has(current)) return;
      seen.add(current);
      if (
        current === "api/client.ts" ||
        /\/(queries|mutations|provider)\.tsx?$/.test(current)
      )
        failures.push(
          `${filename}: leaf UI reaches data/provider module ${current}; receive props/callbacks instead`,
        );
      for (const next of graph.get(current) ?? []) visit(next);
    }
    visit(filename);
  }
  return failures;
}
