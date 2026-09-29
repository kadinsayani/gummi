import { expect, test, type GummiServer } from '../fixtures/test';
import type { Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { shot } from '../fixtures/shots';

// A card whose base was rewritten under it (an amend on main after the
// card was cut): every stage session is refused on the drift, so the page
// has to offer the one answer that clears it. It used to offer "try again"
// and "change profile" — both refused the same way — and a sentence
// pointing at a board key the page does not have.

const isPhone = (info: { project: { name: string } }) => info.project.name === 'phone';

async function open(page: Page, server: GummiServer, id: string) {
  await page.goto(`${server.url}/#${id}`);
  await expect(page.getByTestId('card-id')).toHaveText(id);
  await expect(page.getByTestId('conn')).toHaveAttribute('data-state', 'live');
}

async function option(page: Page, phone: boolean, id: string) {
  if (phone) {
    await page.getByTestId('mnav-panel').click();
    const toggle = page.getByTestId('mdec-toggle');
    if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
    return page.getByTestId(`mdec-option-${id}`);
  }
  return page.getByTestId(`decision-option-${id}`);
}

test.describe('a card whose base was rewritten under it', () => {
  let id: string;
  test.use({
    seed: {
      run: async (ws) => {
        id = await ws.seedVerifyFailed('Add a shout helper');
        // amend main's tip: the commit the card forked from is gone from it
        fs.writeFileSync(path.join(ws.repo, 'AMENDED.md'), 'amended\n');
        await ws.git('add', 'AMENDED.md');
        await ws.git('commit', '-q', '--amend', '--no-edit');
      },
    },
  });

  test('is answered from its own page', async ({ pairedPage: page, server, api, workspace }, info) => {
    const phone = isPhone(info);
    await open(page, server, id);

    // the stop it was already at leads with the rebase
    const card = (await api('GET', `/api/cards/${id}`)).json;
    expect(card.decision.options[0].id, JSON.stringify(card.decision.options)).toBe('rebase');
    await expect(await option(page, phone, 'rebase')).toContainText('rebase onto main');

    // sending it back runs implement, which is refused on the drift
    await (await option(page, phone, 'bounce')).click();
    await expect.poll(async () => {
      const c = (await api('GET', `/api/cards/${id}`)).json;
      return c.decision?.options?.map((o: any) => o.id).join(',');
    }, { timeout: 30_000 }).toBe('rebase,settle');
    const failed = (await api('GET', `/api/cards/${id}`)).json;
    expect(failed.decision.question).toContain('implement cannot run: main no longer carries the commit this card forked from');
    // the decision box stays short enough that its answers are in view
    await expect(await option(page, phone, 'rebase')).toBeInViewport();
    await shot(page, info, 'drift-failure');

    // the rebase clears it, and the retry that now works comes back (a
    // press within the page's settle window of a new decision is held)
    await page.waitForTimeout(1000);
    await (await option(page, phone, 'rebase')).click();
    await expect.poll(async () => {
      const c = (await api('GET', `/api/cards/${id}`)).json;
      return c.decision?.options?.[0]?.id;
    }, { timeout: 30_000 }).toBe('run');
    const cleared = (await api('GET', `/api/cards/${id}`)).json;
    expect(cleared.decision.question).toContain('rebased onto main');
    expect(cleared.decision.question).not.toContain('fork drift —');
    // only the card's own commits sit on main's new tip
    const log = await workspace.git('log', '--format=%s', `main..${cleared.branch ?? `gummi/${id}`}`);
    expect(log).not.toContain('init: a tiny module');
    await shot(page, info, 'drift-cleared');

    // and the stage runs again
    await page.waitForTimeout(1000);
    await (await option(page, phone, 'run')).click();
    // past the refusal to a finished run: its critique has spoken (the seed's
    // check still fails, which is the scripted agent's, not the drift's)
    await expect.poll(async () => (await api('GET', `/api/cards/${id}`)).json.decision?.question ?? '', { timeout: 30_000 })
      .toContain('critique');
    const after = (await api('GET', `/api/cards/${id}`)).json;
    expect(after.stage).toBe('implement');
    expect(after.decision.question).not.toContain('fork drift');
  });
});
