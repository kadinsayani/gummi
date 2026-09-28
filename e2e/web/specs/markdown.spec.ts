import { expect, test } from '../fixtures/test';

// The page's markdown renderer (assets/markdown.js) on its own, in the
// served page: what it builds for a source, and that code keeps its spaces
// on screen. A bug report's repro is often an indented block whose double
// space is the bug itself; drawn as a paragraph it collapses to one.

const REPRO = [
  'Words miscounts whitespace. Run:',
  '',
  '    printf "a  b\\n c\\t d\\n" | go run ./cmd/textstat',
  '',
  'and it prints:',
  '',
  '    words 5',
  '    lines 3',
  '',
  'Expected `words  4` from `a  b`.',
].join('\n');

async function render(page: import('@playwright/test').Page, src: string) {
  return page.evaluate(async (s) => {
    const { markdown } = await import('/assets/markdown.js');
    const el = markdown(s);
    el.dataset.testid = 'md-probe';
    document.getElementById('md-probe')?.remove();
    el.id = 'md-probe';
    document.body.append(el);
    const kids = [...el.children].map((c) => ({ tag: c.tagName.toLowerCase(), text: c.textContent }));
    return kids;
  }, src);
}

test.beforeEach(({}, info) => {
  test.skip(info.project.name !== 'desktop', 'the renderer is the same on every viewport');
});

test('an indented block is code, with its spaces and lines as written', async ({ pairedPage: page }) => {
  const kids = await render(page, REPRO);
  expect(kids.map((k) => k.tag)).toEqual(['p', 'pre', 'p', 'pre', 'p']);
  expect(kids[1].text).toBe('printf "a  b\\n c\\t d\\n" | go run ./cmd/textstat');
  expect(kids[3].text).toBe('words 5\nlines 3');
  const probe = page.locator('#md-probe');
  await expect(probe.locator('pre').first()).toHaveCSS('white-space', 'pre');
  // a code span keeps its double space on screen too
  const span = probe.locator('p code').first();
  await expect(span).toHaveText('words  4');
  await expect(span).toHaveCSS('white-space', 'pre-wrap');
  // what is on screen is what was written: two lines, the double space
  expect(await probe.locator('pre').nth(1).innerText()).toBe('words 5\nlines 3');
  expect(await probe.locator('pre').first().innerText()).toContain('"a  b');
});

test('an indented block follows CommonMark’s edges', async ({ pairedPage: page }) => {
  // an indented line under a paragraph's text continues the paragraph
  let kids = await render(page, 'Some text\n    still the same paragraph');
  expect(kids.map((k) => k.tag)).toEqual(['p']);
  // a tab is four columns; blank lines inside the block are kept, the
  // blank tail is not; the block ends at the first line less indented
  kids = await render(page, '\tone\n\n\t  two\n\n\nafter');
  expect(kids.map((k) => k.tag)).toEqual(['pre', 'p']);
  expect(kids[0].text).toBe('one\n\n  two');
  // a list item's continuation is the item's, not code
  kids = await render(page, '- an item\n\n    more of the item\n- another');
  expect(kids.map((k) => k.tag)).toEqual(['ul']);
  // fenced code is unchanged, and an indented fence is code, not a fence
  kids = await render(page, '```\n  x  y\n```\n\n    ```\n    z');
  expect(kids.map((k) => k.tag)).toEqual(['pre', 'pre']);
  expect(kids[0].text).toBe('  x  y');
  expect(kids[1].text).toBe('```\nz');
});
