import { expect, test, type GummiServer } from '../fixtures/test';
import type { Page } from '@playwright/test';
import { shot } from '../fixtures/shots';

// A card's menu against a real `gummi web`: every entry collects what it
// needs first (a number, cards, a message, a yes), prefilled with what the
// board suggests, and runs through the code the TUI's key runs.

async function open(page: Page, server: GummiServer, id: string, phone: boolean) {
  await page.goto(`${server.url}/#${id}`);
  await expect(page.getByTestId('card-id')).toHaveText(id);
  await expect(page.getByTestId('conn')).toHaveAttribute('data-state', 'live');
  if (phone) await page.getByTestId('mnav-thread').click();
}

async function menu(page: Page, action: string) {
  await page.getByTestId('card-actions').click();
  await expect(page.getByTestId('card-actions-menu')).toBeVisible();
  await page.getByTestId(`action-${action}`).click();
}

const phone = (info: { project: { name: string } }) => info.project.name === 'phone';

test.describe('a backlog', () => {
  let ids: string[];
  test.use({ seed: { run: async (ws) => { ids = await ws.seedBacklog(['Add a shrug helper', 'Add a nod helper', 'Add a bow helper']); } } });

  test('the budget is raised from the number it is now', async ({ pairedPage: page, server, api }, info) => {
    await open(page, server, ids[0], phone(info));
    await menu(page, 'envelope');
    const input = page.getByTestId('action-input');
    await expect(input).toHaveValue('2000');
    await input.fill('2500');
    await shot(page, info, 'action-budget');
    await page.getByTestId('action-confirm').click();
    await expect(page.getByTestId('action-dialog')).toHaveCount(0);
    await expect(page.getByTestId('card-spend')).toContainText('/ 2500 cr');
    expect((await api('GET', `/api/cards/${ids[0]}`)).json.envelope).toBe(2500);
  });

  test('a cycle is refused in the dialog, in the board’s words', async ({ pairedPage: page, server, api }, info) => {
    const [a, b] = ids;
    expect((await api('POST', `/api/cards/${b}/actions/deps`, { cards: [a] })).status).toBe(200);
    await open(page, server, a, phone(info));
    await menu(page, 'deps');
    await page.getByTestId(`action-card-${b}`).check();
    await page.getByTestId('action-confirm').click();
    await expect(page.getByTestId('action-error')).toBeVisible();
    await expect(page.getByTestId('action-dialog')).toBeVisible();
    await shot(page, info, 'action-refused');
  });

  test('delete asks first, then the card is gone', async ({ pairedPage: page, server, api }, info) => {
    const id = ids[1];
    await open(page, server, id, phone(info));
    await menu(page, 'delete');
    await expect(page.getByTestId('action-question')).toContainText(`Delete ${id}`);
    await shot(page, info, 'action-delete');
    await page.getByTestId('action-cancel').click();
    expect((await api('GET', `/api/cards/${id}`)).status).toBe(200);
    await menu(page, 'delete');
    await page.getByTestId('action-confirm').click();
    await expect.poll(async () => (await api('GET', `/api/cards/${id}`)).status).toBe(404);
    if (phone(info)) await page.getByTestId('mnav-cards').click();
    await expect(page.getByTestId(`rail-row-${id}`)).toHaveCount(0);
    await expect(page.getByTestId('card-id')).not.toHaveText(id);
  });
});

test.describe('a card in its design stage', () => {
  let ids: string[];
  test.use({ seed: { run: async (ws) => { ids = await ws.seedBacklog(['Add a shrug helper', 'Add a nod helper', 'Add a bow helper']); } } });

  test('dependencies are ticked from the board, and cleared the same way', async ({ pairedPage: page, server, api }, info) => {
    const [a, b, c] = ids;
    // into its design stage, where what it waits on starts to matter
    const card = (await api('GET', `/api/cards/${c}`)).json;
    expect((await api('POST', `/api/cards/${c}/answer`, { ref: card.decision.ref, option: 'advance', against: card.decision.against.token })).status).toBe(200);
    await open(page, server, c, phone(info));
    await expect(page.getByTestId('stage-plan')).toHaveAttribute('aria-current', 'step');
    await menu(page, 'deps');
    await page.getByTestId(`action-card-${a}`).check();
    await page.getByTestId(`action-card-${b}`).check();
    await shot(page, info, 'action-deps');
    await page.getByTestId('action-confirm').click();
    await expect(page.getByTestId('action-dialog')).toHaveCount(0);
    // it now waits on both before it may implement
    await expect(page.getByTestId('card-head')).toContainText(`waits on ${a}, ${b}`);
    // the picker opens with what is set ticked
    await menu(page, 'deps');
    await expect(page.getByTestId(`action-card-${a}`)).toBeChecked();
    await page.getByTestId(`action-card-${a}`).uncheck();
    await page.getByTestId('action-confirm').click();
    await expect.poll(async () => (await api('GET', `/api/cards/${c}`)).json.waits).toEqual([b]);
    await expect(page.getByTestId('card-head')).not.toContainText(a);
  });
});

test.describe('a verified card', () => {
  let id: string;
  test.use({ seed: { run: async (ws) => { id = await ws.seedVerified('Add a farewell helper'); } } });

  test('lands with the message drafted when verify passed', async ({ pairedPage: page, server, workspace }, info) => {
    await open(page, server, id, phone(info));
    await menu(page, 'merge');
    const msg = page.getByTestId('action-input');
    await expect(msg).toHaveValue(/^feat: land /);
    await expect(page.getByTestId('action-dialog')).toContainText('Drafted when verify passed');
    await shot(page, info, 'action-land');
    await page.getByTestId('action-confirm').click();
    await expect(page.getByTestId('action-dialog')).toHaveCount(0, { timeout: 30_000 });
    if (phone(info)) await page.getByTestId('mnav-cards').click();
    await expect(page.getByTestId('rail-group-done').getByTestId(`rail-row-${id}`)).toBeVisible();
    expect(await workspace.git('log', '-1', '--format=%s', 'main')).toMatch(/^feat: land /);
    await shot(page, info, 'landed');
  });
});

test.describe('a running card', () => {
  test.use({ workspaceEnv: { GUMMI_E2E_SLOW_SECONDS: '30' } });

  test('pauses from the head’s button', async ({ pairedPage: page, server, api }, info) => {
    const c = (await api('POST', '/api/cards', { kind: 'feature', title: '[slow] Add a lazy helper' })).json;
    let card = (await api('POST', `/api/cards/${c.id}/answer`, { ref: c.decision.ref, option: 'advance', against: c.decision.against.token })).json;
    card = (await api('POST', `/api/cards/${c.id}/answer`, { ref: card.decision.ref, option: 'run', against: card.decision.against.token })).json;
    await open(page, server, c.id, phone(info));
    const pause = page.getByTestId('action-btn-pause');
    await expect(pause).toBeVisible();
    await expect(page.getByTestId('live')).toBeVisible();
    await shot(page, info, 'running');
    await pause.click();
    await expect.poll(async () => (await api('GET', `/api/cards/${c.id}`)).json.status).toBe('paused');
    if (phone(info)) await page.getByTestId('mnav-cards').click();
    await expect(page.getByTestId(`rail-row-${c.id}`)).toHaveAttribute('data-status', 'paused');
  });
});
