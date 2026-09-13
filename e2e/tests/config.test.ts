import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { openStrata, ordersRepo, runStrata, scratchDir } from '../testUtils'

function configHome(text: string): string {
  const home = scratchDir()
  mkdirSync(join(home, 'strata'))
  writeFileSync(join(home, 'strata', 'config.toml'), text)
  return home
}

describe('the config file', () => {
  it('strata config prints the defaults as a file that strata reads', async () => {
    const printed = await runStrata(scratchDir(), ['config'])

    expect(printed.status).toBe(0)
    expect(printed.stdout).toContain('next_hunk = "n"')
    const strata = await openStrata(ordersRepo().dir, [], { XDG_CONFIG_HOME: configHome(printed.stdout) })
    await strata.waitForText('Branch · orders-handler')
  })

  it('a key from the config file runs its action, and the footer and the keys screen show it', async () => {
    const strata = await openStrata(ordersRepo().dir, [], { XDG_CONFIG_HOME: configHome('[keys.diff]\nnext_file = "L"\n') })
    await strata.waitForText('Branch · orders-handler')
    await strata.press('enter')
    await strata.press('enter')
    await strata.waitForText('orders/handler.go · 1/2')

    await strata.type('L')

    const screen = await strata.waitForText('orders/handler_test.go · 2/2')
    expect(screen).toContain('L/K file')
    await strata.press('?')
    expect(await strata.waitForText('Keys · any key closes')).toMatch(/L +next file, past folded folders/)
  })

  it('split = false starts with the unified diff', async () => {
    const strata = await openStrata(ordersRepo().dir, [], { XDG_CONFIG_HOME: configHome('split = false\n') })
    await strata.waitForText('Branch · orders-handler')

    await strata.press('enter')
    await strata.waitForText('orders/handler.go · 1/2')

    await strata.press(']')

    const screen = await strata.waitForText('order.Reason = "requested"')
    expect(screen).toContain('│+ ')
  })

  it('an unknown action in the config file stops strata with the name of the action', async () => {
    const result = await runStrata(ordersRepo().dir, [], { XDG_CONFIG_HOME: configHome('[keys.diff]\nnxt_hunk = "n"\n') })

    expect(result.status).toBe(1)
    expect(result.stderr).toContain('config.toml: unknown action keys.diff.nxt_hunk')
  })

  it('--config reads a different file', async () => {
    const path = join(configHome('split = "no"\n'), 'strata', 'config.toml')

    const result = await runStrata(ordersRepo().dir, ['--config', path])

    expect(result.status).toBe(1)
    expect(result.stderr).toContain(`${path}: toml: line 1 (last key "split")`)
  })
})
