import { describe, expect, it } from 'vitest'
import { buildTag, ordersRepo, runStrata, scratchDir } from '../testUtils'

describe('strata --list', () => {
  it('prints each branch under the branch it sits on', async () => {
    const result = await runStrata(ordersRepo().dir, ['--list'])

    expect(result.status).toBe(0)
    const lines = result.stdout.trimEnd().split('\n').map((line) => line.trimEnd())
    expect(lines[0]).toBe('origin/main')
    expect(lines[1]).toMatch(/^├─ billing\s+1 commit\s/)
    expect(lines[2]).toMatch(/^└─ orders-events\s+3 commits\s/)
    expect(lines[3]).toMatch(/^ {3}└─ orders-handler\s+2 commits\s.*1 behind parent$/)
    expect(lines[4]).toMatch(/^ {6}└─ orders-api\s+1 commit\s/)
  })

  it('fails outside a git repository', async () => {
    const result = await runStrata(scratchDir(), ['--list'])

    expect(result.status).toBe(1)
    expect(result.stderr).toContain('not a git repository')
  })
})

describe('strata version', () => {
  it('prints the tag the binary was built with', async () => {
    const result = await runStrata(scratchDir(), ['version'])

    expect(result.status).toBe(0)
    expect(result.stdout.trim()).toBe(buildTag())
  })
})
