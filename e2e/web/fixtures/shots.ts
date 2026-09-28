import fs from 'node:fs';
import path from 'node:path';
import type { Page, TestInfo } from '@playwright/test';

/**
 * Screenshot the page for a person to look at: attached to the HTML report
 * (report/, which the reporter rewrites on every run) and copied to
 * screens/<project>-<name>.png, which survives the run. Both are gitignored.
 */
export async function shot(page: Page, info: TestInfo, name: string): Promise<void> {
  const file = path.join(__dirname, '..', 'screens', `${info.project.name}-${name}.png`);
  await fs.promises.mkdir(path.dirname(file), { recursive: true });
  const body = await page.screenshot({ path: file });
  await info.attach(name, { body, contentType: 'image/png' });
}
