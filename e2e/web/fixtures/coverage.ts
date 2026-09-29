import type { Page } from '@playwright/test';

/** One script's covered byte ranges, [start, end) into its source. */
export interface ScriptCoverage {
  /** Path the page loaded it from, e.g. /app.js. */
  url: string;
  length: number;
  covered: [number, number][];
}

/**
 * Starts recording V8 coverage on page, before anything has loaded, and
 * returns what to call at the end. Only the page's own scripts are kept
 * (same-origin files), never an extension's or an injected one.
 */
export async function collectCoverage(page: Page): Promise<() => Promise<ScriptCoverage[]>> {
  await page.coverage.startJSCoverage({ resetOnNavigation: false });
  return async () => {
    const raw = await page.coverage.stopJSCoverage();
    const out: ScriptCoverage[] = [];
    for (const e of raw) {
      let u: URL;
      try {
        u = new URL(e.url);
      } catch {
        continue;
      }
      if (!/^https?:$/.test(u.protocol) || !u.pathname.endsWith('.js')) continue;
      const length = (e.source ?? '').length;
      // V8 reports nested ranges outermost first; a later, inner range
      // overrides the outer one's count, so paint in order.
      const hit = new Uint8Array(length);
      for (const fn of e.functions) {
        for (const r of fn.ranges) {
          hit.fill(r.count > 0 ? 1 : 0, r.startOffset, Math.min(r.endOffset, length));
        }
      }
      const covered: [number, number][] = [];
      for (let i = 0; i < length; ) {
        if (!hit[i]) {
          i++;
          continue;
        }
        let j = i;
        while (j < length && hit[j]) j++;
        covered.push([i, j]);
        i = j;
      }
      out.push({ url: u.pathname, length, covered });
    }
    return out;
  };
}
