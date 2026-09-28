import { expect, test } from '../fixtures/test';

// A freeform card is a conversation: one enter sends a line as a turn, the
// agent's reply lands in the thread, and when the turn ends the card stops
// offering to stop it — without a reload.
test('a freeform turn is sent by one enter and ends on the page when it ends', async ({ pairedPage: page, server, api }, info) => {
  test.setTimeout(90_000);
  const made = await api('POST', '/api/cards', { kind: 'freeform', title: 'Poke at the rounding' });
  const id = String(made.json?.id);
  await page.goto(`${server.url}/#${id}`);
  await expect(page.getByTestId('card-id')).toHaveText(id);
  if (info.project.name === 'phone') await page.getByTestId('mnav-thread').click();
  // the card's opening turn runs and ends
  await expect(page.getByTestId('composer-says')).not.toContainText('stop this turn', { timeout: 30_000 });

  const posts: string[] = [];
  page.on('request', (r) => { if (r.method() === 'POST' && /\/api\/cards\/[^/]+\/(send|answer)$/.test(r.url())) posts.push(r.url().split('/').pop()!) });
  const input = page.getByTestId('composer-input');
  await input.click();
  await input.fill('make the week view sum before rounding');
  await page.keyboard.press('Enter');
  // one enter was enough: the line went as a turn, not as an answer
  await expect(input).toHaveValue('');
  await expect.poll(() => posts).toEqual(['send']);

  // the reply arrives, and the card is no longer working
  await expect(page.getByTestId('thread')).toContainText('Done: noted it in NOTES.md.', { timeout: 30_000 });
  await expect(page.getByTestId('composer-says')).not.toContainText('stop this turn', { timeout: 10_000 });
  await expect(page.getByTestId('decision-question').filter({ hasText: 'working on a turn' })).toHaveCount(0);
  const card = (await api('GET', `/api/cards/${id}`)).json;
  expect(JSON.stringify(card.decision ?? {})).not.toContain('working on a turn');
});
