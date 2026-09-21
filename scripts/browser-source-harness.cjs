"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { transformSync } = require("esbuild");

const repoRoot = path.resolve(__dirname, "..");

function readBrowserTypeScript(name) {
  return fs.readFileSync(path.join(repoRoot, "cmd", "launcher", "ui", name), "utf8");
}

function stripKernelImport(source) {
  const marker = 'import { K } from "./kernel";';
  if (!source.includes(marker)) {
    throw new Error("Browser regression source is missing the module-kernel import");
  }
  return source.replace(marker, "");
}

function transpileBrowserTypeScript(source, sourcefile) {
  return transformSync(source, {
    loader: "ts",
    target: "es2020",
    format: "iife",
    sourcefile,
    sourcemap: false,
  }).code;
}

function loadBrowserModule(name) {
  const source = stripKernelImport(readBrowserTypeScript(name));
  return transpileBrowserTypeScript(source, name);
}

module.exports = {
  loadBrowserModule,
  readBrowserTypeScript,
  stripKernelImport,
  transpileBrowserTypeScript,
};
