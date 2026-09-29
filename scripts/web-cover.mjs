#!/usr/bin/env node
// Reads the byte coverage the web e2e suite recorded when it ran with
// GUMMI_E2E_COVERAGE=<dir> and reports, per page script, how much of it
// ran. A line counts as covered when any of its non-blank bytes did, so
// the figure is lines of code the browser executed, merged across every
// test and viewport.
//
//   scripts/web-cover.mjs <dir> [assets-dir]
//   scripts/web-cover.mjs <dir> --missed views/goals.js   # list a file's missed lines
import fs from 'node:fs';
import path from 'node:path';

const dir = process.argv[2];
const mi = process.argv.indexOf('--missed');
const missedFile = mi > 0 ? process.argv[mi + 1] : null;
const assets = (process.argv[3] && process.argv[3] !== '--missed' ? process.argv[3] : null) ?? path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'internal', 'web', 'assets');
if (!dir) {
  console.error('usage: web-cover.mjs <coverage-dir> [assets-dir]');
  process.exit(2);
}

const merged = new Map();
for (const f of fs.readdirSync(dir).filter((f) => f.endsWith('.json'))) {
  for (const s of JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8'))) {
    const hit = merged.get(key(s.url)) ?? new Uint8Array(s.length);
    for (const [a, b] of s.covered) hit.fill(1, a, Math.min(b, hit.length));
    merged.set(key(s.url), hit);
  }
}

const rows = [];
let total = 0;
let ran = 0;
for (const file of walk(assets).filter((f) => f.endsWith('.js')).sort()) {
  const url = '/' + path.relative(assets, file).split(path.sep).join('/');
  const src = fs.readFileSync(file, 'utf8');
  const hit = merged.get(url);
  let lines = 0;
  let covered = 0;
  let off = 0;
  for (const line of src.split('\n')) {
    const code = line.trim();
    if (code && !code.startsWith('//') && !code.startsWith('*') && !code.startsWith('/*') && code !== '}') {
      lines++;
      if (hit) {
        for (let i = off; i < off + line.length; i++) {
          if (hit[i] && /\S/.test(src[i])) {
            covered++;
            break;
          }
        }
      }
    }
    off += line.length + 1;
  }
  if (missedFile && url === '/' + missedFile.replace(/^\//, '')) {
    let o = 0;
    src.split('\n').forEach((line, n) => {
      const code = line.trim();
      if (code && !code.startsWith('//') && !code.startsWith('*') && !code.startsWith('/*') && code !== '}') {
        let c = false;
        for (let i = o; hit && i < o + line.length; i++) if (hit[i] && /\S/.test(src[i])) { c = true; break; }
        if (!c) console.log(`${String(n + 1).padStart(4)}  ${line}`);
      }
      o += line.length + 1;
    });
  }
  total += lines;
  ran += covered;
  rows.push({ url, lines, covered, loaded: !!hit });
}

rows.sort((a, b) => b.lines - b.covered - (a.lines - a.covered));
console.log('missed  cover  lines  file');
for (const r of rows) {
  const pct = r.lines ? ((100 * r.covered) / r.lines).toFixed(0) : '100';
  console.log(`${String(r.lines - r.covered).padStart(6)}  ${(pct + '%').padStart(5)}  ${String(r.lines).padStart(5)}  ${r.url}${r.loaded ? '' : '  (never loaded)'}`);
}
console.log(`\ntotal  ${((100 * ran) / Math.max(total, 1)).toFixed(1)}%  (${ran}/${total} lines)`);

function walk(d) {
  return fs.readdirSync(d, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? walk(path.join(d, e.name)) : [path.join(d, e.name)]));
}

// the server serves the assets directory under /assets/
function key(url) {
  return url.replace(/^\/assets(?=\/)/, '');
}
