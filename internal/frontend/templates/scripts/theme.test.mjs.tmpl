import { test } from "node:test";
import assert from "node:assert/strict";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  readFileSync,
  symlinkSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import vm from "node:vm";
import { renderTheme, syncTheme } from "./theme.mjs";

const frontend = (ui = "heroui-v3", theme = {}) => ({
  ui,
  directory: "web",
  theme,
});
test("tokens map to each profile; absent tokens preserve defaults", () => {
  const hero = renderTheme(
    frontend("heroui-v3", { dark: { accent: "#338EF7" } }),
  );
  assert.match(
    hero["src/generated/theme.css"],
    /:root\[data-theme="dark"\] \{\n  --accent: #338EF7;/,
  );
  assert.doesNotMatch(hero["src/generated/theme.css"], /--background/);
  const shadcn = renderTheme(
    frontend("shadcn", { light: { accent: "#123456", surface: "#EEEEEE" } }),
  );
  assert.match(shadcn["src/generated/theme.css"], /--primary: #123456/);
  assert.match(shadcn["src/generated/theme.css"], /--card: #EEEEEE/);
  assert.match(
    renderTheme(frontend("native", { radius: "0.75rem" }))[
      "src/generated/theme.css"
    ],
    /--radius: 0.75rem/,
  );
});

test("unknown settings, CSS injection and unsafe paths are rejected", () => {
  for (const theme of [
    { mode: "automatic" },
    { mode: 3 },
    { radiuz: "1rem" },
    { radius: "1rem; color:red" },
    { font_family: "url(https://bad.test)" },
    { dark: { unknown: "#123456" } },
    { light: { accent: "red; } body {}" } },
    { dark: [] },
    { overrides: "web/../outside.css" },
    { overrides: "web/src/generated/theme.css" },
    { overrides: 'web/src/styles/evil".css' },
  ])
    assert.throws(() => renderTheme(frontend("heroui-v3", theme)));
});

test("bootstrap handles explicit modes and changing system preference without browser storage", () => {
  for (const mode of ["system", "dark", "light"]) {
    let listener;
    const classes = new Set();
    const root = {
      dataset: {},
      style: {},
      classList: {
        toggle: (key, enabled) =>
          enabled ? classes.add(key) : classes.delete(key),
      },
    };
    const media = {
      matches: true,
      addEventListener: (_, callback) => {
        listener = callback;
      },
    };
    const script = renderTheme(frontend("heroui-v3", { mode }))[
      "public/pluto-theme.js"
    ];
    vm.runInNewContext(script, {
      document: { documentElement: root },
      window: { matchMedia: () => media },
    });
    assert.equal(root.dataset.theme, mode === "light" ? "light" : "dark");
    assert.equal(classes.has("dark"), mode !== "light");
    if (mode === "system") {
      media.matches = false;
      listener();
      assert.equal(root.dataset.theme, "light");
    } else assert.equal(listener, undefined);
    assert.doesNotMatch(script, /localStorage|fetch|secret/);
  }
});

test("generation preserves authored CSS, checks drift without writes and refuses symlinks", () => {
  const root = mkdtempSync(path.join(tmpdir(), "pluto-theme-"));
  const web = path.join(root, "web");
  try {
    mkdirSync(path.join(web, "src/styles"), { recursive: true });
    const override = path.join(web, "src/styles/overrides.css");
    writeFileSync(override, ".owned { color: red; }\n");
    writeFileSync(
      path.join(root, "pluto.toml"),
      '[frontend]\nui="heroui-v3"\ndirectory="web"\n[frontend.theme]\nmode="dark"\n',
    );
    syncTheme({ directory: web });
    syncTheme({ directory: web, check: true });
    assert.equal(readFileSync(override, "utf8"), ".owned { color: red; }\n");
    const generated = path.join(web, "src/generated/theme.css");
    writeFileSync(generated, "modified");
    assert.throws(() => syncTheme({ directory: web, check: true }), /stale/);
    assert.equal(readFileSync(generated, "utf8"), "modified");
    syncTheme({ directory: web });
    rmSync(generated);
    symlinkSync(override, generated);
    assert.throws(() => syncTheme({ directory: web }), /symlink/);
    assert.equal(readFileSync(override, "utf8"), ".owned { color: red; }\n");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
