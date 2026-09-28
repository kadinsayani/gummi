import { expect, test, pair, type GummiServer } from '../fixtures/test';
import type { Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { shot } from '../fixtures/shots';

// Answering a card's pinned decision against a real `gummi web` and the
// scripted agent: the design gate, reworking a plan with a note, sending a
// failed verify back with a diff comment, an agent's question (an option,
// and "chat about this"), the composer's enter line, two people answering
// at once, and an answer given against a card that has since moved. The
// phone answers from the docked decision bar.

const isPhone = (info: { project: { name: string } }) => info.project.name === 'phone';

async function open(page: Page, server: GummiServer, id: string) {
  await page.goto(`${server.url}/#${id}`);
  await expect(page.getByTestId('card-id')).toHaveText(id);
  await expect(page.getByTestId('conn')).toHaveAttribute('data-state', 'live');
}

// answerOption answers the pinned decision with one option: on a phone
// from the docked bar (the documents view shows it), elsewhere from the
// pinned block. The first press only chooses an answer — even the one
// highlighted by default, which nobody chose — and the second gives it.
async function answerOption(page: Page, phone: boolean, option: string) {
  if (phone) {
    await page.getByTestId('mnav-panel').click();
    await page.getByTestId('mdec-toggle').click();
    const opt = page.getByTestId(`mdec-option-${option}`);
    await opt.click();
    await page.waitForTimeout(150);
    if (await page.getByTestId('mdec-note').filter({ hasText: 'again' }).count()) await opt.click();
    return;
  }
  const opt = page.getByTestId(`decision-option-${option}`);
  await opt.click();
  await page.waitForTimeout(150);
  // the press only chose it (or, for an answer that takes words, put the
  // reader in the composer): the second gives it
  const chose = (await page.getByTestId('decision-arm').count()) > 0 ||
    (await page.getByTestId('composer-input').evaluate((e) => e === document.activeElement));
  if (chose) await opt.click();
}

async function thread(page: Page, phone: boolean) {
  if (phone) await page.getByTestId('mnav-thread').click();
  return page.getByTestId('thread-items');
}

test.describe('a design gate', () => {
  let id: string;
  test.use({ seed: { run: async (ws) => { id = await ws.seedDesignGate('Add a wave helper'); } } });

  test('approving it moves the card to implement and the thread names who', async ({ pairedPage: page, server }, info) => {
    await open(page, server, id);
    await expect(page.getByTestId('decision')).toHaveAttribute('data-kind', 'gate');
    await expect(page.getByTestId('decision-against')).toContainText('spec ');
    await shot(page, info, 'gate-open');
    if (isPhone(info)) {
      await answerOption(page, true, 'advance');
    } else {
      // the keyboard: a digit picks, enter gives it
      // keys go to the page, not to whatever control the pointer last
      // touched: drop focus rather than click into the thread, whose
      // middle may be a control (enter on one activates it, by design)
      await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
      await page.keyboard.press('3');
      await expect(page.getByTestId('decision-option-pause')).toHaveAttribute('aria-pressed', 'true');
      await page.keyboard.press('ArrowUp');
      await page.keyboard.press('1');
      await expect(page.getByTestId('composer-says')).toHaveText('approve');
      await page.keyboard.press('Enter');
    }
    await expect(page.getByTestId('stage-implement')).toHaveAttribute('aria-current', 'step');
    const items = await thread(page, isPhone(info));
    await expect(items.getByTestId('receipt').last()).toContainText('Tester');
    await shot(page, info, 'gate-approved');
  });

  test('reworking the plan carries the note to the architect', async ({ pairedPage: page, server, workspace }, info) => {
    await open(page, server, id);
    if (isPhone(info)) await page.getByTestId('mnav-thread').click();
    await page.getByTestId('composer-input').fill('Cover an empty name too');
    await expect(page.getByTestId('decision-option-run')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.getByTestId('composer-says')).toHaveText('start the architect with your words');
    await page.getByTestId('composer-send').click();
    await expect(page.getByTestId('composer-input')).toHaveValue('');
    // a line at a gate is read first, and the reading is put to you
    const chip = page.getByTestId('decision');
    await expect(chip).toHaveAttribute('data-kind', 'confirm');
    await expect(page.getByTestId('decision-option-keep')).toBeVisible();
    await shot(page, info, 'gate-chip');
    // going on runs the architect again, which spends: as the terminal's
    // chip wants y rather than enter, the page asks before it goes
    await expect(page.getByTestId('decision-option-go')).toHaveClass(/\bdanger\b/);
    await answerOption(page, false, 'go');
    await expect(page.getByTestId('decision-confirm')).toBeVisible();
    await page.getByTestId('decision-confirm-yes').click();
    // the architect heard it, and the plan comes back to the gate
    await expect.poll(() => workspace.agentLog().includes('Cover an empty name too')).toBe(true);
    await expect(page.getByTestId('decision')).toHaveAttribute('data-kind', 'gate', { timeout: 30_000 });
    await shot(page, info, 'gate-rework');
  });

  test('a refused approval is said once, in the decision, and nothing covers the composer', async ({ pairedPage: page, server, workspace }, info) => {
    // a plan whose Chosen approach was emptied keeps the gate shut
    const dir = path.join(workspace.repo, '.gummi', 'specs');
    const file = path.join(dir, fs.readdirSync(dir).find((f) => f.startsWith(id))!);
    const body = fs.readFileSync(file, 'utf8');
    fs.writeFileSync(file, body.replace(/(## Chosen approach\n)[\s\S]*?(\n## )/, '$1$2'));
    expect(fs.readFileSync(file, 'utf8')).not.toBe(body);
    await open(page, server, id);
    if (isPhone(info)) await page.getByTestId('mnav-thread').click();
    await answerOption(page, false, 'advance');
    const note = page.getByTestId('decision-error');
    await expect(note).toContainText('gate stays shut');
    // the board says the same line to every viewer; this one has it in the
    // note already, so no toast repeats it over the page
    await page.waitForTimeout(1500);
    // (counted now, not polled: a toast goes by itself after a few seconds)
    expect(await page.getByTestId('toast').filter({ hasText: 'gate stays shut' }).count()).toBe(0);
    // and a toast, when there is one, stands clear of the composer
    await page.evaluate(async () => { (await import('/assets/toast.js')).toast('A notice\nof two lines', { ms: 8000 }); });
    const t = await page.getByTestId('toast').last().boundingBox();
    const c = await page.getByTestId('composer').boundingBox();
    expect(t && c && (t.y + t.height <= c.y || t.y >= c.y + c.height), 'the toast does not overlap the composer').toBe(true);
    await shot(page, info, 'gate-refused');
  });

  test('the enter line says what a line would do as it is typed', async ({ pairedPage: page, server }, info) => {
    await open(page, server, id);
    if (isPhone(info)) await page.getByTestId('mnav-thread').click();
    const says = page.getByTestId('composer-says');
    await expect(says).toHaveText('approve');
    await page.getByTestId('composer-input').fill('the plan misses the empty name');
    await expect(says).toHaveText('start the architect with your words');
    // a command that is in the card's menu, not one of the answers
    await page.getByTestId('composer-input').fill('/rebase');
    await expect(says).toContainText('menu');
    await expect(page.getByTestId('composer-send')).toHaveText('Send');
    await page.getByTestId('composer-send').click();
    await expect(page.getByTestId('card-actions-menu')).toBeVisible();
    await expect(page.getByTestId('card-actions-menu').locator('button').first()).toHaveAttribute('data-testid', 'action-rebase');
    await shot(page, info, 'composer-menu');
  });

  test('two people answer at once: one wins, the other is told who', async ({ pairedPage: page, server, browser }, info) => {
    const u = info.project.use as any;
    const other = await browser.newContext({ viewport: u.viewport, isMobile: u.isMobile, hasTouch: u.hasTouch, deviceScaleFactor: u.deviceScaleFactor, userAgent: u.userAgent });
    const page2 = await other.newPage();
    try {
      await pair(page2, server, 'Yuki');
      await open(page, server, id);
      await open(page2, server, id);
      const phone = isPhone(info);
      const go = async (p: Page) => {
        if (phone) {
          await p.getByTestId('mnav-panel').click();
          await p.getByTestId('mdec-toggle').click();
          return p.getByTestId('mdec-option-advance');
        }
        return p.getByTestId('decision-option-advance');
      };
      const [a, b] = [await go(page), await go(page2)];
      // each chooses the answer, then both give it at once
      await a.click();
      await b.click();
      await Promise.all([a.click(), b.click()]);
      // the loser is told: who answered, when its answer reached the board
      // second; or that the card moved, when the winner's answer reached
      // its page before its own second press did
      const told = (p: Page) => p.getByTestId(phone ? 'mdec-note' : 'decision-answered').or(p.getByTestId('decision-moved'));
      await expect.poll(async () => (await told(page).count()) + (await told(page2).count())).toBe(1);
      const [loser, winner] = (await told(page).count()) ? [page, 'Yuki'] : [page2, 'Tester'];
      await expect(told(loser)).toContainText(new RegExp(`Answered by ${winner}|moved`));
      await shot(loser, info, 'answered-first');
    } finally {
      await other.close();
    }
  });
});

test.describe('a failed verify', () => {
  let id: string;
  test.use({ seed: { run: async (ws) => { id = await ws.seedVerifyFailed('Add a regressing helper'); } } });

  test('a diff comment goes back with the failure', async ({ pairedPage: page, server, api }, info) => {
    const phone = isPhone(info);
    await open(page, server, id);
    if (phone) await page.getByTestId('mnav-panel').click();
    await expect(page.getByTestId('tab-diff')).toHaveAttribute('aria-selected', 'true');
    await page.locator('[data-testid^="diff-line-"]').nth(3).locator('.n').click();
    await page.getByTestId('annotation-input').fill('Return early on an empty name');
    await page.getByTestId('annotation-save').click();
    await expect(page.getByTestId('diff-pending')).toContainText('1 comment');
    if (phone) {
      await page.getByTestId('mdec-toggle').click();
      await expect(page.getByTestId('mdec-option-bounce')).toContainText('+ 1 diff comment');
      await shot(page, info, 'verify-carry');
      // a send-back takes words, so the bar sends the reader to the composer
      await page.getByTestId('mdec-option-bounce').click();
      await expect(page.getByTestId('composer-input')).toBeFocused();
      await page.getByTestId('composer-send').click();
    } else {
      await expect(page.getByTestId('decision-option-bounce')).toContainText('+ 1 diff comment');
      await shot(page, info, 'verify-carry');
      await answerOption(page, false, 'bounce');
    }
    // the card is back in implement, crossed by the person who answered
    await expect(page.getByTestId('stage-implement')).toHaveAttribute('aria-current', 'step');
    await expect.poll(async () => (await api('GET', `/api/cards/${id}/thread`)).json.items
      .some((it: any) => it.t === 'receipt' && /Tester/.test(`${it.receipt?.by} ${it.receipt?.text}`))).toBe(true);
    await shot(page, info, 'verify-sent-back');
    // and the comment goes to the implementer with its next run
    if (phone) await page.getByTestId('mnav-thread').click();
    await answerOption(page, false, 'run');
    await expect.poll(() => server.ws.agentLog().includes('Return early on an empty name'), { timeout: 20_000 }).toBe(true);
  });

  test('an answer given against a card that moved is refused and re-read', async ({ pairedPage: page, server, workspace }, info) => {
    await open(page, server, id);
    const against = await page.getByTestId('decision-against').textContent();
    // someone commits to the branch under the page
    const wt = workspace.worktree(id);
    await workspace.exec('sh', ['-c', 'echo "// late" >> README.md && git add README.md && git commit -qm late'], { cwd: wt });
    await answerOption(page, isPhone(info), 'pause');
    const note = page.getByTestId(isPhone(info) ? 'mdec-note' : 'decision-moved');
    await expect(note).toContainText('moved since you read it');
    await shot(page, info, 'moved');
    if (!isPhone(info)) {
      await expect(page.getByTestId('decision-against')).not.toHaveText(against || '');
      // read again, the same answer goes through
      await answerOption(page, false, 'pause');
      await expect(page.getByTestId('rail-row-' + id)).toHaveAttribute('data-status', /paused|idle|needs/);
      await expect(page.getByTestId('decision-moved')).toHaveCount(0);
    }
  });
});

test.describe('an agent’s question', () => {
  // A question's options live only in the process that asked it, so the
  // card is started from this board rather than seeded by `gummi run`.
  async function ask(api: any): Promise<string> {
    const c = (await api('POST', '/api/cards', { kind: 'feature', title: '[ask] Add a choosy helper' })).json;
    let card = (await api('POST', `/api/cards/${c.id}/answer`, { ref: c.decision.ref, option: 'advance', against: c.decision.against.token })).json;
    card = (await api('POST', `/api/cards/${c.id}/answer`, { ref: card.decision.ref, option: 'run', against: card.decision.against.token })).json;
    await expect.poll(async () => (await api('GET', `/api/cards/${c.id}`)).json.decision?.kind).toBe('ask');
    return c.id;
  }

  test('an option answers it', async ({ pairedPage: page, server, api }, info) => {
    const id = await ask(api);
    await open(page, server, id);
    await expect(page.getByTestId('decision-question')).toContainText('Where should');
    await expect(page.getByTestId('decision-option-chat')).toBeVisible();
    await shot(page, info, 'ask');
    await answerOption(page, isPhone(info), '1');
    const items = await thread(page, isPhone(info));
    await expect(items).toContainText('Extend the existing file');
    await expect.poll(async () => (await api('GET', `/api/cards/${id}`)).json.decision?.kind).not.toBe('ask');
  });

  test('chat about this answers in your words', async ({ pairedPage: page, server, api }, info) => {
    const id = await ask(api);
    await open(page, server, id);
    if (isPhone(info)) await page.getByTestId('mnav-thread').click();
    // the chat row with nothing typed asks for the words
    await page.getByTestId('decision-option-chat').click();
    await page.getByTestId('decision-option-chat').click();
    await expect(page.getByTestId('decision-needs')).toContainText('Type your answer');
    await page.getByTestId('composer-input').fill('Put it beside Greet, in greet.go');
    await expect(page.getByTestId('composer-says')).toHaveText('Chat about this');
    await page.keyboard.press('Enter');
    await expect(page.getByTestId('thread-items')).toContainText('Put it beside Greet, in greet.go');
    await expect.poll(async () => (await api('GET', `/api/cards/${id}`)).json.decision?.kind).not.toBe('ask');
  });
});

test.describe('a verified card', () => {
  let id: string;
  test.use({ seed: { run: async (ws) => { id = await ws.seedVerified('Add a parting helper'); } } });

  // A confirm-gated answer stops on the board's own question (the TUI's
  // y/n). The question and its buttons are drawn beside the decision, not
  // inside its capped box: inside it they were clipped under the composer
  // on a phone, and nobody could hand a card off by touch.
  test('handing it off asks first, and the question can be answered by touch', async ({ pairedPage: page, server, api }, info) => {
    await open(page, server, id);
    if (isPhone(info)) await page.getByTestId('mnav-thread').click();
    const opt = page.getByTestId('decision-option-handoff');
    await expect(opt).toBeVisible();
    await opt.click();
    await opt.click();
    const confirm = page.getByTestId('decision-confirm');
    await expect(confirm).toBeVisible();
    // the board's question, with its lines kept as the board wrote them
    await expect(page.getByTestId('decision-confirm-question')).toContainText(`Hand off ${id}`);
    await expect(page.getByTestId('decision-confirm-question')).toHaveCSS('white-space', 'pre-line');
    await expect(confirm).toBeFocused();
    await shot(page, info, 'handoff-confirm');
    // both buttons are on screen and nothing stands over them
    for (const tid of ['decision-confirm-no', 'decision-confirm-yes']) {
      const b = page.getByTestId(tid);
      await expect(b).toBeInViewport({ ratio: 1 });
      const hit = await b.evaluate((el) => {
        const r = el.getBoundingClientRect();
        const at = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return !!at && (at === el || el.contains(at));
      });
      expect(hit, `${tid} is the element under its own centre`).toBe(true);
    }
    if (isPhone(info)) await page.getByTestId('decision-confirm-yes').tap();
    else await page.getByTestId('decision-confirm-yes').click();
    await expect.poll(async () => (await api('GET', `/api/cards/${id}`)).json.stage).toBe('done');
    await expect(page.getByTestId('decision-confirm')).toHaveCount(0);
  });
});
