import { describe, expect, it } from 'vitest'
import { openStrata, ordersRepo } from '../testUtils'

describe('the strata screen', () => {
  it('opens on the checked-out branch with the stack as a tree', async () => {
    const strata = await openStrata(ordersRepo().dir)

    const screen = await strata.waitForText('Branch · orders-handler')

    expect(screen).toContain('├─ billing')
    expect(screen).toMatch(/└─ orders-handler.*1 behind parent/)
    expect(screen).toContain('orders-events has 1 commit that orders-handler lacks: restack it')
  })

  it('j moves to the next file and shows its diff', async () => {
    const strata = await openStrata(ordersRepo().dir)
    await strata.waitForText('Branch · orders-handler')

    await strata.press('enter')
    await strata.press('j')

    const screen = await strata.waitForText('orders/handler_test.go · 2/2')
    expect(screen).toContain('func TestHandler')
  })

  it('] keeps the same file selected on the next branch', async () => {
    const strata = await openStrata(ordersRepo().dir)
    await strata.waitForText('Branch · orders-handler')
    await strata.press('enter')
    await strata.waitForText('orders/handler.go · 1/2')

    await strata.press(']')

    const screen = await strata.waitForText('order.Reason = "requested"')
    expect(screen).toContain('Files · orders-api')
    expect(screen).toContain('orders/handler.go · 2/2')
  })

  it('s switches the diff from side by side to unified', async () => {
    const strata = await openStrata(ordersRepo().dir)
    await strata.waitForText('Branch · orders-handler')
    await strata.press('enter')
    await strata.press(']')
    const sideBySide = await strata.waitForText('order.Reason = "requested"')
    expect(sideBySide).not.toContain('│+ ')

    await strata.press('s')

    const unified = await strata.waitForText('│+ ')
    expect(unified).toContain('order.Reason = "requested"')
  })

  it('o folds the folder that holds the file', async () => {
    const strata = await openStrata(ordersRepo().dir)
    await strata.waitForText('Branch · orders-handler')
    await strata.press('enter')

    await strata.press('o')

    const screen = await strata.waitForText('▸ orders/  2 files')
    expect(screen).toContain('Folder · orders/')
  })

  it('folds a folder once every file in it is viewed, and keeps the marks after a restart', async () => {
    const repo = ordersRepo()
    const first = await openStrata(repo.dir)
    await first.waitForText('Branch · orders-handler')
    await first.press('enter')

    await first.press('v')
    await first.press('v')

    await first.waitForText('▸ orders/  2 files')
    await first.press('q')
    await first.waitForText('EXIT:0')

    const second = await openStrata(repo.dir)
    const screen = await second.waitForText('Branch · orders-handler')
    expect(screen).toContain('✓ 2/2 viewed')
  })
})
