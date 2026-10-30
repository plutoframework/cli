import { readdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { ESLint } from "eslint";
import * as prettier from "prettier";

const root = fileURLToPath(new URL("../", import.meta.url));
const check = process.argv.includes("--check");
const eslint = new ESLint({ cwd: root, fix: true });
const ignored = new Set([
  "node_modules",
  ".git",
  "dist",
  ".cert",
  "coverage",
  "test-results",
  "playwright-report",
  "package-lock.json",
]);

export async function formatSource(source, file, linter = eslint) {
  const options = await prettier.resolveConfig(file);
  let current = source;
  // Compute one canonical result, including ESLint's structural fixes. This
  // avoids Prettier's compact type printing fighting expanded TypeScript rules.
  for (let pass = 0; pass < 5; pass++) {
    const printed = await prettier.format(current, {
      ...options,
      filepath: file,
    });
    let next = printed;
    if (/\.[cm]?tsx?$/.test(file)) {
      const [result] = await linter.lintText(printed, { filePath: file });
      if (result.fatalErrorCount) throw new Error(`Cannot parse ${file}`);
      next = await prettier.format(result.output ?? printed, {
        ...options,
        filepath: file,
      });
      const [expanded] = await linter.lintText(next, { filePath: file });
      next = expanded.output ?? next;
    }
    if (next === current) return next;
    current = next;
  }
  throw new Error(`Formatting did not converge: ${file}`);
}

async function walk(directory) {
  let changed = 0;
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if (ignored.has(entry.name)) continue;
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      changed += await walk(file);
    } else if (entry.isFile()) {
      const info = await prettier.getFileInfo(file, {
        ignorePath: path.join(root, ".prettierignore"),
      });
      if (info.ignored || !info.inferredParser) continue;
      const before = await readFile(file, "utf8");
      const after = await formatSource(before, file);
      if (before !== after) {
        changed++;
        console.log(path.relative(root, file));
        if (!check) await writeFile(file, after);
      }
    }
  }
  return changed;
}

// Importable for formatter regression tests without executing a repository walk.
if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const changed = await walk(root);
  if (check && changed) {
    console.error("Run npm run fmt to apply the expanded frontend style.");
    process.exitCode = 1;
  } else {
    console.log(
      check
        ? "Canonical frontend formatting verified"
        : "Frontend formatting applied",
    );
  }
}
