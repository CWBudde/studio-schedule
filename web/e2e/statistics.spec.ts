import { expect, gotoDemo, hashSearch, type Page, signIn, test } from './fixtures';

// The demo has the lessons since its term started, three weeks ago, with
// their members' bookings, whose attendance the instructors took (but
// Classical Ballet's), with no-shows and late cancellations among them,
// guests and trials the instructors added, and the pass sales of the past
// weeks. The statistics count them in the school's time zone.

/** Opens the statistics from the account menu and the administration's navigation. */
async function openStatistics(page: Page, name: string) {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Statistics' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Statistics' })).toBeVisible();
}

/** The chart library's chunk, which only the statistics page loads. */
const chartsChunk = /\/assets\/charts-[\w-]+\.js$/;

test('the office reads the statistics of the last weeks', async ({ page }) => {
  const scripts: string[] = [];
  page.on('request', (r) => {
    if (r.resourceType() === 'script') scripts.push(r.url());
  });

  // The app starts without the chart library.
  await gotoDemo(page, '/');
  expect(scripts.filter((url) => chartsChunk.test(url))).toEqual([]);

  const office = await signIn(page, 'Office');
  await openStatistics(page, office);

  // The last four weeks: totals, charts with their tables, by week.
  await expect(page.getByRole('radio', { name: 'Last 4 weeks' })).toBeChecked();
  const totals = page.getByRole('region', { name: 'Totals of the range' });
  await expect(totals.getByText('Bookings', { exact: true })).toBeVisible();
  await expect(totals.getByText(/^\d+%$/).first()).toBeVisible();
  await expect(totals.getByText(/ with attendance taken$/)).toBeVisible();
  await expect(totals.getByText(/^[1-9]\d* trials · [1-9]\d* guests$/)).toBeVisible();
  await expect(page.getByText(/ · by week$/)).toBeVisible();
  await expect(page.getByRole('img', { name: /^Bookings by week\. Column chart; / })).toBeVisible();
  const weeks = page.getByRole('table', { name: 'By week' });
  await expect(weeks.getByRole('row')).toHaveCount(5);
  await expect(page.getByRole('table', { name: 'By class' }).getByRole('row').nth(1)).toBeVisible();
  expect(scripts.some((url) => chartsChunk.test(url))).toBe(true);

  // Nothing is wider than the page, on a phone too.
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true);

  // Twelve weeks, kept in the URL.
  await page.getByRole('radio', { name: 'Last 12 weeks' }).click();
  await expect(weeks.getByRole('row')).toHaveCount(13);
  expect(hashSearch(page).get('range')).toBe('last12Weeks');

  // A custom range of more than a year is refused before it is asked for.
  await page.getByRole('radio', { name: 'Custom' }).click();
  await page.getByLabel('From', { exact: true }).fill('2020-01-01');
  await page.getByLabel('To', { exact: true }).fill('2021-06-30');
  await page.getByRole('button', { name: 'Show' }).click();
  await expect(page.getByRole('alert')).toHaveText('Choose at most 366 days.');
});

test('the current term counts from its start', async ({ page }) => {
  await gotoDemo(page, '/');
  const office = await signIn(page, 'Office');
  await openStatistics(page, office);

  await page.getByRole('radio', { name: 'Current term' }).click();
  await expect(page.getByText(/ · Term: School Year /)).toBeVisible();
  expect(hashSearch(page).get('range')).toBe('term');
  // The demo's term began three weeks ago: four weeks or so.
  await expect(page.getByRole('table', { name: 'By week' }).getByRole('row')).not.toHaveCount(1);
});
