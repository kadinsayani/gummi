import { expect, test } from '../fixtures/test';
import { shot } from '../fixtures/shots';

// The event stream only says what changed; the page reads the card itself.
// A read that fails on the way (a dropped connection while the stream
// stays up, a server that answered 5xx) must be asked again, or the page
// goes on drawing a card that has moved: here a stage the page shows
// running long after it parked at its gate.

test('a card read that failed is read again', async ({ pairedPage: page, server, api }, info) => {
  const c = (await api('POST', '/api/cards', { kind: 'feature', title: 'Add a missed helper' })).json;
  const card = (await api('POST', `/api/cards/${c.id}/answer`, { ref: c.decision.ref, option: 'advance', against: c.decision.against.token })).json;
  await page.goto(`${server.url}/#${c.id}`);
  await expect(page.getByTestId('decision')).toHaveAttribute('data-ref', card.decision.ref);

  // every read fails while the stage runs and parks; the stream stays up
  let failing = true;
  await page.route((u) => u.pathname.startsWith('/api/') && u.pathname !== '/api/events', (r) => (failing && r.request().method() === 'GET' ? r.abort('internetdisconnected') : r.fallback()));
  await api('POST', `/api/cards/${c.id}/answer`, { ref: card.decision.ref, option: 'run', against: card.decision.against.token });
  await expect.poll(async () => (await api('GET', `/api/cards/${c.id}`)).json.decision?.kind, { timeout: 30_000 }).toBe('gate');
  // let what trails the gate (its notices, the spend) go by unread too
  await page.waitForTimeout(8000);
  await expect(page.getByTestId('decision')).not.toHaveAttribute('data-kind', 'gate');

  // the network comes back; nothing on the card moves again
  failing = false;
  await expect(page.getByTestId('decision')).toHaveAttribute('data-kind', 'gate', { timeout: 20_000 });
  await shot(page, info, 'refetched-gate');
});
