import { execFileSync } from "node:child_process";
import { access, readdir, rm, stat } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const webDir = path.join(root, "cmd", "launcher", "web");

process.chdir(root);

const tsc = require.resolve("typescript/bin/tsc");

// The product Browser build is an ES-module graph bundled by esbuild.
execFileSync(process.execPath, [tsc, "-p", "tsconfig.json"], { stdio: "inherit" });

for (const name of await readdir(webDir)) {
  if (
    name.endsWith(".js") ||
    name === "monaco-editor.css" ||
    /^monaco-.+\.ttf$/.test(name)
  ) {
    await rm(path.join(webDir, name), { force: true });
  }
}

await build({
  entryPoints: { browser: path.join(root, "cmd", "launcher", "ui", "browser.ts") },
  outdir: webDir,
  bundle: true,
  minify: false,
  sourcemap: false,
  format: "esm",
  platform: "browser",
  target: ["es2020"],
  logLevel: "warning",
});

const resolvedMonaco = require.resolve("monaco-editor");
const packageRoot = path.resolve(path.dirname(resolvedMonaco), "..", "..");
const vs = path.join(packageRoot, "esm", "vs");

await build({
  entryPoints: { "monaco-editor": path.join(root, "scripts", "monaco", "entry.ts") },
  outdir: webDir,
  bundle: true,
  minify: true,
  sourcemap: false,
  format: "esm",
  platform: "browser",
  target: ["es2020"],
  loader: { ".ttf": "file" },
  assetNames: "monaco-[name]-[hash]",
  logLevel: "warning",
});

await build({
  entryPoints: {
    "monaco-editor-worker": path.join(vs, "editor", "editor.worker.js"),
    "monaco-json-worker": path.join(vs, "language", "json", "json.worker.js"),
    "monaco-css-worker": path.join(vs, "language", "css", "css.worker.js"),
    "monaco-html-worker": path.join(vs, "language", "html", "html.worker.js"),
    "monaco-ts-worker": path.join(vs, "language", "typescript", "ts.worker.js"),
  },
  outdir: webDir,
  bundle: true,
  minify: true,
  sourcemap: false,
  format: "iife",
  platform: "browser",
  target: ["es2020"],
  logLevel: "warning",
});

const required = [
  "browser.js",
  "monaco-editor.js",
  "monaco-editor.css",
  "monaco-editor-worker.js",
  "monaco-json-worker.js",
  "monaco-css-worker.js",
  "monaco-html-worker.js",
  "monaco-ts-worker.js",
];
for (const name of required) await access(path.join(webDir, name));

const sizes = [];
for (const name of required) {
  const info = await stat(path.join(webDir, name));
  sizes.push(`${name}=${Math.ceil(info.size / 1024)}KB`);
}
console.log(`TL Studio Browser bundle ready: ${sizes.join(" · ")}`);
