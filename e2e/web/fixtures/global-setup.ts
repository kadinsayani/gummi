import { execFileSync } from 'node:child_process';
import { gummiBin, repoRoot } from './paths';

// Build bin/gummi once for the whole run. `go build` is incremental, so a
// second run with no Go changes costs a couple of seconds. GUMMI_E2E_BIN
// (a prebuilt binary) skips the build entirely.
export default function globalSetup(): void {
  if (process.env.GUMMI_E2E_BIN) return;
  const started = Date.now();
  execFileSync('go', ['build', '-o', gummiBin, './cmd/gummi'], {
    cwd: repoRoot,
    stdio: 'inherit',
  });
  console.log(`[e2e] built ${gummiBin} in ${((Date.now() - started) / 1000).toFixed(1)}s`);
}
