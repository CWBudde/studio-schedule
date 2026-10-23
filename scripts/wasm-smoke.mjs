#!/usr/bin/env node
// Smoke test for the WebAssembly build: loads app.wasm in Node, waits for
// globalThis.StudioScheduleAPI and calls a few endpoints through the bridge.
//
// Usage: node scripts/wasm-smoke.mjs path/to/app.wasm path/to/wasm_exec.js
//   (wasm_exec.js lives in "$(go env GOROOT)/lib/wasm/")
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";

const [wasmPath, execPath] = process.argv.slice(2);
if (!wasmPath || !execPath) {
  console.error("usage: node scripts/wasm-smoke.mjs path/to/app.wasm path/to/wasm_exec.js");
  process.exit(2);
}

createRequire(import.meta.url)(path.resolve(execPath)); // defines globalThis.Go

const ready = new Promise((resolve) => {
  globalThis.__studioScheduleReady = resolve;
});
const timeout = (ms) =>
  new Promise((_, reject) => setTimeout(() => reject(new Error(`timed out after ${ms} ms`)), ms).unref());

const go = new globalThis.Go();
const { instance } = await WebAssembly.instantiate(await readFile(wasmPath), go.importObject);
go.run(instance); // never settles: the Go program blocks forever to serve calls
await Promise.race([ready, timeout(10_000)]);

const api = globalThis.StudioScheduleAPI;
const decoder = new TextDecoder();
let failures = 0;

// summary turns a successful response body into the logged text (default:
// the body itself).
async function check(name, req, assert, summary = (text) => text) {
  try {
    const res = await Promise.race([api.fetch(req), timeout(5_000)]);
    const text = decoder.decode(res.body).trim();
    const problem = assert(res, text);
    if (problem) throw new Error(`${problem} (status ${res.status}, body ${text.slice(0, 500)})`);
    console.log(`ok   ${name}: ${res.status} ${summary(text)}`);
  } catch (err) {
    failures++;
    console.error(`FAIL ${name}: ${err.message}`);
  }
}

const header = (res, name) =>
  res.headers.find(([n]) => n.toLowerCase() === name.toLowerCase())?.[1];

console.log(`StudioScheduleAPI.version = ${api.version}, ready = ${api.ready}`);
if (api.ready !== true || typeof api.version !== "string") {
  failures++;
  console.error("FAIL api object: ready/version missing");
}

await check("GET /api/v1/health", { method: "GET", url: "/api/v1/health", headers: [], body: null }, (res, text) => {
  if (res.status !== 200) return "unexpected status";
  if (!header(res, "content-type")?.includes("json")) return "missing JSON content type";
  const body = JSON.parse(text);
  if (body.status !== "ok" || body.version !== api.version) return "unexpected body";
});

await check(
  "GET /api/v1/settings/public",
  { method: "GET", url: "/api/v1/settings/public", headers: [["Accept", "application/json"]], body: null },
  (res, text) => {
    if (res.status !== 200) return "unexpected status";
    const body = JSON.parse(text);
    if (!body.schoolName || body.timezone !== "Europe/Berlin" || !body.defaultLocale) return "unexpected body";
  },
);

await check(
  "POST /api/v1/health passes cross-origin protection (405, not 403)",
  {
    method: "POST",
    url: "/api/v1/health",
    headers: [["Content-Type", "application/json"]],
    body: new TextEncoder().encode("{}"),
  },
  (res) => (res.status === 405 ? undefined : "unexpected status"),
);

// The schedule of the coming week, in the school time zone. The demo terms
// have fixed dates, so lessons are only required while today lies in one.
const isoDate = (d) => new Intl.DateTimeFormat("en-CA", { timeZone: "Europe/Berlin" }).format(d);
const today = isoDate(new Date());
const nextWeek = isoDate(new Date(Date.now() + 7 * 24 * 3600 * 1000));
const termsRes = await api.fetch({ method: "GET", url: "/api/v1/terms", headers: [], body: null });
const terms = termsRes.status === 200 ? JSON.parse(decoder.decode(termsRes.body)).items : [];
const inTerm = terms.some((t) => t.startsOn <= today && today <= t.endsOn);

await check(
  `GET /api/v1/schedule ${today}..${nextWeek} (lessons ${inTerm ? "required" : "optional"})`,
  { method: "GET", url: `/api/v1/schedule?from=${today}&to=${nextWeek}`, headers: [], body: null },
  (res, text) => {
    if (res.status !== 200) return "unexpected status";
    const body = JSON.parse(text);
    if (!Array.isArray(body.lessons) || !body.releasedUntil || !body.nextReleaseAt) return "unexpected body";
    if (inTerm && body.lessons.length === 0) return "no lessons generated";
    const releasedUntil = Date.parse(body.releasedUntil);
    if (body.lessons.some((l) => l.classId && l.kind === "regular" && Date.parse(l.startsAt) >= releasedUntil))
      return "unreleased lesson visible";
  },
  (text) => {
    const body = JSON.parse(text);
    return `${body.lessons.length} lessons, released until ${body.releasedUntil}`;
  },
);

await check("GET /api/v1/nope", { method: "GET", url: "/api/v1/nope", headers: [], body: null }, (res) => {
  if (res.status !== 404) return "unexpected status";
  if (header(res, "content-type") !== "application/problem+json") return "expected problem+json";
});

try {
  await api.fetch({ method: "GET", url: "no-leading-slash", headers: [], body: null });
  failures++;
  console.error("FAIL invalid url: promise resolved, expected rejection");
} catch (err) {
  console.log(`ok   invalid url rejected: ${err.message}`);
}

process.exit(failures === 0 ? 0 : 1);
