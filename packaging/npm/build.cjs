#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");

const here = __dirname;
const root = path.resolve(here, "../..");
const out = path.join(here, "dist");
const versionFile = path.join(root, "VERSION");
const templateFile = path.join(here, "package.template.json");
const launcherFile = path.join(root, "scripts", "npx-launch.cjs");
const readmeFile = path.join(here, "README.md");

const version = fs.readFileSync(versionFile, "utf8").trim();
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error(`VERSION is not valid npm semver: ${version}`);
}

const manifest = JSON.parse(fs.readFileSync(templateFile, "utf8"));
manifest.version = version;

fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
fs.writeFileSync(path.join(out, "package.json"), `${JSON.stringify(manifest, null, 2)}\n`);
fs.copyFileSync(launcherFile, path.join(out, "launcher.cjs"));
fs.copyFileSync(readmeFile, path.join(out, "README.md"));

const wrapper = `#!/usr/bin/env node\n"use strict";\n\n// Pin this npm package to the matching TL Studio GitHub Release.\n// Users can still override it explicitly with TL_STUDIO_VERSION.\nif (!process.env.TL_STUDIO_VERSION) process.env.TL_STUDIO_VERSION = "v${version}";\nrequire("./launcher.cjs");\n`;
fs.writeFileSync(path.join(out, "tl-studio.cjs"), wrapper, { mode: 0o755 });

console.log(`Prepared ${manifest.name}@${manifest.version} -> v${version} in ${out}`);
