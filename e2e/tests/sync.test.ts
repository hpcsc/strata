import { describe, expect, it } from 'vitest'
import { ordersRepo, pathWithOldGit, runStrata } from '../testUtils'

describe('strata sync on git older than 2.44', () => {
  it('is not in the help', async () => {
    const result = await runStrata(ordersRepo().dir, ['--help'], { PATH: pathWithOldGit() })

    expect(result.status).toBe(0)
    expect(result.stdout).toMatch(/^\s+update\s/m)
    expect(result.stdout).not.toMatch(/^\s+sync\s/m)
  })

  it('prints the git version error and exits 1', async () => {
    const result = await runStrata(ordersRepo().dir, ['sync', '--dry-run'], { PATH: pathWithOldGit() })

    expect(result.status).toBe(1)
    expect(result.stderr.trim()).toBe('strata: sync needs git 2.44 or later, and this is git 2.43.0')
  })
})

describe('strata sync --dry-run', () => {
  it('is in the help', async () => {
    const result = await runStrata(ordersRepo().dir, ['--help'])

    expect(result.status).toBe(0)
    expect(result.stdout).toMatch(/^\s+sync\s/m)
  })

  it('prints the plan with an outcome for each branch after the remote trunk gets a commit', async () => {
    const repo = ordersRepo()
    repo.commitOnOrigin('README.md', '# shop\n\nOpen all day\n', 'Open all day')

    const result = await runStrata(repo.dir, ['sync', '--dry-run'])

    expect(result.status).toBe(0)
    const lines = result.stdout.trimEnd().split('\n').map((line) => line.trimEnd())
    expect(lines[0]).toBe('origin/main  1 new commit')
    expect(lines[1]).toMatch(/^├─ billing\s+moves onto origin\/main$/)
    expect(lines[2]).toMatch(/^└─ orders-events\s+moves onto origin\/main$/)
    expect(lines[3]).toMatch(/^ {3}└─ orders-handler\s+moves onto orders-events$/)
    expect(lines[4]).toMatch(/^ {6}└─ orders-api\s+moves onto orders-handler$/)
    expect(lines).toHaveLength(5)
  })

  it('keeps a stack with a conflict where it is, moves the other stacks, and exits 1', async () => {
    const repo = ordersRepo()
    repo.commitOnOrigin('orders/events.go', 'package orders\n\ntype OrderShipped struct{}\n', 'Ship orders')

    const result = await runStrata(repo.dir, ['sync', '--dry-run'])

    expect(result.status).toBe(1)
    expect(result.stderr.trimEnd().split('\n').at(-1)).toBe('strata: 1 stack stays because of a conflict')
    const lines = result.stdout.trimEnd().split('\n').map((line) => line.trimEnd())
    expect(lines[1]).toMatch(/^├─ billing\s+moves onto origin\/main$/)
    expect(lines[2]).toMatch(/^└─ orders-events\s+stays: conflict in orders\/events.go$/)
    expect(lines[3]).toMatch(/^ {3}└─ orders-handler\s+stays: orders-events cannot move$/)
    expect(lines[4]).toMatch(/^ {6}└─ orders-api\s+stays: orders-events cannot move$/)
  })
})
