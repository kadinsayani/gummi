import type { Page } from '@playwright/test';
import { expect, test } from '../fixtures/test';
import { seedGoalRunning } from '../fixtures/goals';
import { shot } from '../fixtures/shots';

// The goals view against a real `gummi web`: the list, one goal's page
// (its state, the budget ledger, done-when, its cards, the lead's log) and
// the goal's own verbs, each confirmed on the page before it runs. The goal
// is seeded through the CLI and left at implement for this board to
// conduct; its cards are [slow] so it stays there while the test works.

async function openGoals(page: Page, mobile: boolean) {
  if (mobile) await page.getByTestId('mnav-cards').click();
  await page.getByTestId('rail-more').click();
  await page.getByTestId('menu-goals').click();
  await expect(page.getByTestId('view-goals')).toBeVisible();
}

test.describe('a running goal', () => {
  let goal: string;
  test.use({
    workspaceEnv: { GUMMI_E2E_SLOW_SECONDS: '900' },
    seed: {
      run: async (ws) => {
        goal = await seedGoalRunning(ws, 'Greet in two languages', {
          cards: ['[slow] Add a hola helper', '[slow] Add a bonjour helper'],
        });
      },
    },
  });

  test('the list, the page, the ledger, a raise, a note and a stop', async ({ pairedPage: page }, info) => {
    test.setTimeout(120_000);
    const mobile = info.project.name === 'phone';
    await openGoals(page, mobile);
    const row = page.getByTestId(`goal-row-${goal}`);
    await expect(row).toContainText('Greet in two languages');
    await expect(row).toHaveAttribute('data-state', 'running');
    await expect(row.getByTestId('goal-row-met')).toContainText('0/2');
    await shot(page, info, 'goals-list');

    await row.click();
    const view = page.getByTestId('view-goal');
    await expect(view.getByTestId('goal-title')).toHaveText('Greet in two languages');
    await expect(view.getByTestId('goal-state')).toHaveText('running');
    // the ledger is the report's budget tree, laid out as it adds up
    await expect(view.getByTestId('ledger-envelope')).toHaveText('3,000');
    await expect(view.getByTestId('ledger-held')).toContainText(/\d/);
    await expect(view.getByTestId('ledger-reserve')).toContainText('450');
    await expect(view.getByTestId('ledger-seg-held')).toBeVisible();
    await expect(view.getByTestId('done-when-DW-1')).toContainText('the module builds');
    await expect(view.getByTestId('done-when-DW-2')).toHaveAttribute('data-status', 'not checked');
    const cards = view.getByTestId('goal-cards');
    await expect(cards.locator('[data-testid^="goal-card-FD-"]')).toHaveCount(2);
    await expect(view.getByTestId('goal-log')).toContainText('minted');
    await shot(page, info, 'goal-page');

    // raise the budget: the panel asks, the ledger moves
    await view.getByTestId('goal-action-budget').click();
    const budget = view.getByTestId('goal-panel-budget');
    await expect(budget).toBeVisible();
    await budget.getByTestId('goal-action-input').fill('2000');
    await budget.getByTestId('goal-action-confirm').click();
    await expect(budget.getByTestId('goal-action-error')).toContainText('only raised');
    await budget.getByTestId('goal-action-input').fill('3600');
    await shot(page, info, 'goal-raise');
    await budget.getByTestId('goal-action-confirm').click();
    await expect(page.getByTestId('toast').last()).toContainText('raised to 3600');
    await expect(view.getByTestId('ledger-envelope')).toHaveText('3,600');
    await expect(view.getByTestId('ledger-reserve')).toContainText('540');

    // a note to the lead lands in its log
    await view.getByTestId('goal-action-note').click();
    const note = view.getByTestId('goal-panel-note');
    await note.getByTestId('goal-action-input').fill('prefer short helper names');
    await note.getByTestId('goal-action-confirm').click();
    await expect(page.getByTestId('toast').last()).toContainText('note added');
    await expect(view.getByTestId('goal-log').locator('[data-action="note"]')).toContainText('prefer short helper names');

    // stop: confirmed on the page, never by a browser dialog
    page.on('dialog', (d) => { throw new Error(`unexpected browser dialog: ${d.message()}`); });
    await view.getByTestId('goal-action-stop').click();
    const stop = view.getByTestId('goal-panel-stop');
    await expect(stop).toContainText('Stop');
    await stop.getByTestId('goal-action-cancel').click();
    await expect(stop).toHaveCount(0);
    await expect(view.getByTestId('goal-state')).toHaveText('running');
    await view.getByTestId('goal-action-stop').click();
    await view.getByTestId('goal-panel-stop').getByTestId('goal-action-confirm').click();
    await expect(view.getByTestId('goal-state')).toHaveText(/wrapping up|ready for you/, { timeout: 30_000 });
    await expect(view.getByTestId('goal-partial')).toContainText('you stopped the goal', { timeout: 30_000 });
    await expect(view.getByTestId('goal-action-land')).toBeVisible();
    await shot(page, info, 'goal-stopped');

    // the land panel carries the goal's drafted merge message
    await view.getByTestId('goal-action-land').click();
    await expect(view.getByTestId('goal-panel-land').getByTestId('goal-action-input')).toHaveValue(/Merge .*Greet in two languages/);
    await view.getByTestId('goal-panel-land').getByTestId('goal-action-cancel').click();
  });

  test('a goal card opens on the board, and its head leads back to the goal', async ({ pairedPage: page }, info) => {
    const mobile = info.project.name === 'phone';
    await openGoals(page, mobile);
    await page.getByTestId(`goal-row-${goal}`).click();
    const card = page.getByTestId('view-goal').locator('[data-testid^="goal-card-FD-"]').first();
    const id = (await card.getAttribute('data-testid'))!.replace('goal-card-', '');
    await card.click();
    await expect(page.getByTestId('view-goal')).toHaveCount(0);
    await expect(page.getByTestId('card-id')).toHaveText(id);
    await page.getByTestId('card-goal').click();
    await expect(page.getByTestId('view-goal').getByTestId('goal-id')).toHaveText(goal);
  });
});

test('a goal is created from the form', async ({ pairedPage: page }, info) => {
  const mobile = info.project.name === 'phone';
  await openGoals(page, mobile);
  await expect(page.getByTestId('goals-empty')).toBeVisible();
  await page.getByTestId('goals-new').click();
  const form = page.getByTestId('goal-form');
  await form.getByTestId('goal-form-submit').click();
  await expect(form.getByTestId('goal-form-error')).toContainText('Describe the objective');
  await form.getByTestId('goal-form-desc').fill('Say goodbye in three languages');
  await form.getByTestId('goal-form-budget').fill('2500');
  await form.getByTestId('goal-form-refs').fill('README.md');
  await shot(page, info, 'goal-form');
  await form.getByTestId('goal-form-submit').click();

  const view = page.getByTestId('view-goal');
  await expect(view.getByTestId('goal-title')).toHaveText('Say goodbye in three languages');
  await expect(view.getByTestId('goal-state')).toHaveText('todo');
  await expect(view.getByTestId('ledger-envelope')).toHaveText('2,500');
  await expect(view.getByTestId('goal-notebook').getByTestId('goal-reference')).toContainText('README.md');
  await expect(view.getByTestId('goal-done-when')).toContainText('Nothing agreed yet');
  await shot(page, info, 'goal-new');

  await view.getByTestId('goal-back').click();
  await expect(page.getByTestId('view-goals').locator('[data-testid^="goal-row-GL-"]')).toHaveCount(1);
});
