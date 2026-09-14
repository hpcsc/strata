import { existsSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { openStrata, ordersRepo, scratchDir } from '../testUtils'

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

  it('removes the worktree of the branch with the files that it lists, and then the branch', async () => {
    const repo = ordersRepo()
    const worktree = join(scratchDir(), 'shop-api')
    repo.git('worktree', 'add', '-q', worktree, 'orders-api')
    writeFileSync(join(worktree, 'notes.txt'), 'call the shop\n')
    const strata = await openStrata(repo.dir)
    const before = await strata.waitForText('in shop-api')
    expect(before).toMatch(/└─ orders-api\s+1 commit .* in shop-api/)
    await strata.press('j')
    await strata.waitForText('Branch · orders-api')

    await strata.press('d')
    const confirm = await strata.waitForText('y delete 1 branch, remove 1 worktree and lose 1 changed file')
    expect(confirm).toContain('removes worktree shop-api and loses 1 changed file: notes.txt')
    await strata.press('y')

    await strata.waitForText('Removed worktree shop-api.')
    expect(existsSync(worktree)).toBe(false)
    expect(repo.git('branch', '--list', 'orders-api')).toBe('')
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
