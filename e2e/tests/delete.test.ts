import { describe, expect, it } from 'vitest'
import { openStrata, ordersRepo } from '../testUtils'

describe('the d key', () => {
  it('shows what the delete loses, and y deletes the branch and names its tip', async () => {
    const repo = ordersRepo()
    const tip = repo.git('rev-parse', 'billing').slice(0, 7)
    const strata = await openStrata(repo.dir)
    await strata.waitForText('Branch · orders-handler')
    await strata.press('k')
    await strata.waitForText('Branch · orders-events')
    await strata.press('k')
    await strata.waitForText('Branch · billing')

    await strata.press('d')
    const confirm = await strata.waitForText('y delete 1 branch')
    expect(confirm).toMatch(/├─ billing\s+loses 1 commit/)
    await strata.press('y')

    const screen = await strata.waitForText(`Deleted billing (was ${tip}).`)
    expect(screen).not.toContain('├─ billing')
    expect(repo.git('branch', '--list', 'billing')).toBe('')
  })

  it('refuses a branch that another branch sits on', async () => {
    const strata = await openStrata(ordersRepo().dir)
    await strata.waitForText('Branch · orders-handler')
    await strata.press('k')
    await strata.waitForText('Branch · orders-events')

    await strata.press('d')

    await strata.waitForText('orders-handler sits on orders-events: mark orders-handler too')
  })
})
