import type { BudgetCategoryRow, CostCategory } from '@/api/types'
import { amountCents, centsToAmount } from '@/utils/amount'

/** CategoryRow is a row of the budget by category; a part is shown beneath the row it belongs to. */
export interface CategoryRow extends BudgetCategoryRow {
  part: boolean
}

/**
 * CATEGORY_PARTS names the categories counted as a part of another in the
 * budget by category: tolls and vignettes are a cost of transport, and a
 * reader asking what transport costs expects them in its figure. They stay a
 * category of their own in the data, so they are still shown - beneath
 * transport - and still filtered on.
 */
const CATEGORY_PARTS: Partial<Record<CostCategory, CostCategory>> = { tolls: 'transport' }

// addAmounts adds two amounts of the API in whole hundredths.
function addAmounts(a: string, b: string): string {
  return centsToAmount((amountCents(a) ?? 0) + (amountCents(b) ?? 0))
}

// carries says whether a row has anything in it.
function carries(row: BudgetCategoryRow): boolean {
  return Number(row.planned) !== 0 || Number(row.actual) !== 0
}

/**
 * categoryRows lays out the budget by category: each part folded into the
 * category it belongs to and shown again beneath it, and the categories that
 * carry nothing left out, since they would be rows of zeroes.
 *
 * Arguments:
 *   - categories: every category with its figures, in the order the API gives.
 *
 * Returns:
 *   - the rows to show, parts marked as such.
 */
export function categoryRows(categories: BudgetCategoryRow[]): CategoryRow[] {
  const rows: CategoryRow[] = []
  for (const row of categories) {
    if (CATEGORY_PARTS[row.category]) {
      continue
    }
    const parts = categories.filter((part) => CATEGORY_PARTS[part.category] === row.category && carries(part))
    const whole = parts.reduce((sum, part) => ({
      ...sum,
      planned: addAmounts(sum.planned, part.planned),
      actual: addAmounts(sum.actual, part.actual),
    }), row)
    if (carries(whole)) {
      rows.push({ ...whole, part: false }, ...parts.map((part) => ({ ...part, part: true })))
    }
  }
  return rows
}
