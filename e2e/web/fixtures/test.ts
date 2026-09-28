import { test as base, expect, type Page } from '@playwright/test';
import fs from 'node:fs';
import { api, pair, type ApiResponse, GummiServer } from './server';
import { Workspace } from './workspace';

export { expect };
export { GummiServer, Workspace };
export { api, pair, seedFreeform, freePort } from './server';

/**
 * What to put on the board before the server starts. An object rather than
 * a bare function because Playwright reads a function passed to test.use()
 * as a fixture implementation, not as the option's value.
 */
export interface Seed {
  run(ws: Workspace): Promise<void>;
}

/** `api` bound to the paired page's cookie and the server's origin. */
export type BoundApi = <T = any>(
  method: string,
  pathname: string,
  body?: unknown,
  opts?: { origin?: string | null; headers?: Record<string, string> },
) => Promise<ApiResponse<T>>;

interface Fixtures {
  /**
   * Option: runs against the fresh workspace BEFORE the server starts, so a
   * test's cards exist when the board first loads:
   *
   *   test.use({ seed: { run: async (ws) => { await ws.seedDesignGate('Add a wave helper'); } } });
   */
  seed: Seed | null;
  /** Option: extra environment for every gummi process (agent pacing etc.). */
  workspaceEnv: NodeJS.ProcessEnv;
  /** Option: extra `gummi web` flags. */
  serverArgs: string[];
  /** Option: the name pairedPage pairs as. */
  person: string;

  /** A fresh, seeded workspace; removed after the test. */
  workspace: Workspace;
  /** `gummi web` on a free loopback port for that workspace; stopped after. */
  server: GummiServer;
  /** A page whose context is paired (cookie set) and has the board open. */
  pairedPage: Page;
  /** JSON API calls as the paired page's device, same-origin by default. */
  api: BoundApi;
}

export const test = base.extend<Fixtures>({
  seed: [null, { option: true }],
  workspaceEnv: [{}, { option: true }],
  serverArgs: [[], { option: true }],
  person: ['Tester', { option: true }],

  workspace: async ({ seed, workspaceEnv }, use, testInfo) => {
    const ws = await Workspace.create({ env: workspaceEnv, name: testInfo.project.name });
    if (seed) await seed.run(ws);
    await use(ws);
    if (testInfo.status !== testInfo.expectedStatus) {
      for (const name of ['agent.log', 'gh.log']) {
        const p = `${ws.logs}/${name}`;
        if (fs.existsSync(p)) await testInfo.attach(name, { path: p, contentType: 'text/plain' });
      }
    }
    await ws.dispose();
  },

  server: async ({ workspace, serverArgs }, use, testInfo) => {
    const server = await GummiServer.start(workspace, { args: serverArgs });
    await use(server);
    await server.stop();
    if (testInfo.status !== testInfo.expectedStatus && fs.existsSync(server.logFile)) {
      await testInfo.attach('server.log', { path: server.logFile, contentType: 'text/plain' });
    }
  },

  pairedPage: async ({ page, server, person }, use) => {
    await pair(page, server, person);
    await use(page);
  },

  api: async ({ pairedPage, server }, use) => {
    const request = pairedPage.context().request;
    await use((method, pathname, body, opts) => api(request, server, method, pathname, body, opts));
  },
});
