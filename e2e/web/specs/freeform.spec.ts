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

// A freeform card walks no stages, so once it lands it has none to have
// passed: the head and the rail must not tick a plan, an implement and a
// verify it never had.
test('a landed freeform card claims no stages', async ({ pairedPage: page, server, api }, info) => {
  test.setTimeout(90_000);
  const id = String((await api('POST', '/api/cards', { kind: 'freeform', title: 'Poke at the padding' })).json?.id);
  await expect.poll(async () => (await api('GET', `/api/cards/${id}`)).json.decision?.options?.some((o: any) => o.id === 'merge'), { timeout: 30_000 }).toBe(true);
  let d = (await api('GET', `/api/cards/${id}`)).json.decision;
  let r = await api('POST', `/api/cards/${id}/answer`, { ref: d.ref, option: 'merge', against: d.against.token, words: 'chore: pad the padding' });
  for (let i = 0; i < 3 && r.json?.confirm; i++) {
    r = await api('POST', `/api/cards/${id}/answer`, { ref: d.ref, option: 'merge', against: d.against.token, words: 'chore: pad the padding', confirm: r.json.confirm });
  }
  await expect.poll(async () => (await api('GET', `/api/cards/${id}`)).json.stage, { timeout: 30_000 }).toBe('done');
  await page.goto(`${server.url}/#${id}`);
  await expect(page.getByTestId('card-id')).toHaveText(id);
  await expect(page.getByTestId('card-stages')).toContainText('freeform');
  await expect(page.getByTestId('card-stages')).not.toContainText('verify');
  await expect(page.getByTestId('stage-verify')).toHaveCount(0);
  if (info.project.name === 'phone') await page.getByTestId('mnav-cards').click();
  await expect(page.getByTestId(`rail-row-${id}`)).toContainText('freeform');
});
