import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { openStrata, ordersRepo, pathWithOldGit, runStrata, scratchDir } from '../testUtils'

describe('the S key', () => {
  it('fetches the trunk and shows the plan in the Stack panel', async () => {
    const repo = ordersRepo()
    repo.commitOnOrigin('README.md', '# shop\n\nOpen all day\n', 'Open all day')
    const strata = await openStrata(repo.dir)
    await strata.waitForText('S sync')

    await strata.type('S')

    const screen = await strata.waitForText('Stack · sync plan')
    expect(screen).toContain('origin/main  1 new commit')
    expect(screen).toMatch(/├─ billing\s+moves onto origin\/main/)
    expect(screen).toMatch(/└─ orders-api\s+moves onto orders-handler/)
    expect(screen).toContain('esc close')
  })

  it('then enter moves the stacks, and the Stack panel shows the new tree', async () => {
    const repo = ordersRepo()
    repo.commitOnOrigin('README.md', '# shop\n\nOpen all day\n', 'Open all day')
    const strata = await openStrata(repo.dir)
    await strata.waitForText('S sync')
    await strata.type('S')
    await strata.waitForText('enter move 2 stacks')

    await strata.press('enter')

    const screen = await strata.waitForText('Moved 2 stacks.')
    expect(screen).not.toContain('sync plan')
    expect(screen).toMatch(/└─ orders-handler\s+2 commits/)
    expect(screen).not.toContain('behind parent')
    expect(repo.git('status', '--porcelain')).toBe('')
  })

  it('shows the error of a fetch that needs a passphrase, and nothing asks for it on the screen', async () => {
    const repo = ordersRepo()
    const askPassphrase = join(scratchDir(), 'ask-passphrase')
    writeFileSync(askPassphrase, '#!/bin/sh\nexec </dev/tty >/dev/tty\nprintf "Enter passphrase: "\nread answer\n', {
      mode: 0o755,
    })
    repo.git('remote', 'set-url', 'origin', 'ssh://example.invalid/shop.git')
    repo.git('config', 'core.sshCommand', askPassphrase)
    const strata = await openStrata(repo.dir)
    await strata.waitForText('S sync')

    await strata.type('S')

    const screen = await strata.waitForText('git fetch --prune origin')
    expect(screen).not.toContain('Enter passphrase')
    expect(screen).toContain('Branch · orders-handler')
  })

  it('on git older than 2.44 is not in the footer, and shows the git version error', async () => {
    const strata = await openStrata(ordersRepo().dir, [], { PATH: pathWithOldGit() })
    const before = await strata.waitForText('Branch · orders-handler')
    expect(before).toContain('r refresh')
    expect(before).not.toContain('S sync')

    await strata.type('S')

    await strata.waitForText('sync needs git 2.44 or later, and this is git 2.43.0')
  })
})

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

describe('the c key', () => {
  it('opens a shell in the sync worktree, and enter then moves the resolved stack', async () => {
    const repo = ordersRepo()
    repo.commitOnOrigin('orders/events.go', 'package orders\n\ntype OrderShipped struct{}\n', 'Ship orders')
    const strata = await openStrata(repo.dir, [], { SHELL: '/bin/sh' })
    await strata.waitForText('S sync')
    await strata.type('S')
    await strata.waitForText('c resolve the conflict')

    await strata.press('c')
    await strata.waitForText('strata: resolve the conflict of the stack of orders-events')
    await strata.type("printf 'package orders\\n\\ntype OrderShipped struct{}\\n\\ntype OrderPlaced struct{}\\n' > orders/events.go")
    await strata.press('enter')
    await strata.type('git add orders/events.go && git -c core.editor=true rebase --continue && exit')
    await strata.press('enter')
    await strata.waitForText('enter move the resolved stack')
    await strata.press('enter')

    const screen = await strata.waitForText('Moved 1 stack.')
    expect(screen).not.toContain('behind parent')
    expect(repo.git('show', 'orders-events:orders/events.go')).toContain('OrderShipped')
    expect(repo.git('status', '--porcelain')).toBe('')
  })
})

describe('strata sync', () => {
  it('moves each stack onto the new trunk, with the checked-out branch and its files', async () => {
    const repo = ordersRepo()
    repo.commitOnOrigin('README.md', '# shop\n\nOpen all day\n', 'Open all day')

    const result = await runStrata(repo.dir, ['sync'])

    expect(result.status).toBe(0)
    expect(result.stdout).toContain('Moved 2 stacks.')
    const list = await runStrata(repo.dir, ['--list'])
    expect(list.stdout).toMatch(/└─ orders-handler\s+2 commits/)
    expect(list.stdout).not.toContain('behind parent')
    expect(repo.git('status', '--porcelain')).toBe('')
    expect(repo.git('show', 'HEAD:README.md')).toBe('# shop\n\nOpen all day\n')
  })
})

describe('strata sync --resolve', () => {
  it('stops at the conflict, and strata sync moves the stack after git rebase --continue', async () => {
    const repo = ordersRepo()
    repo.commitOnOrigin('orders/events.go', 'package orders\n\ntype OrderShipped struct{}\n', 'Ship orders')

    const started = await runStrata(repo.dir, ['sync', '--resolve', 'orders-events'])

    expect(started.status).toBe(1)
    const worktree = started.stdout.match(/stopped at the conflict, in (.+)\.\n/)?.[1]
    expect(worktree).toBeDefined()
    const waiting = await runStrata(repo.dir, ['sync'])
    expect(waiting.status).toBe(1)
    expect(waiting.stderr).toContain(`waits in ${worktree}`)

    const sync = repo.worktree(worktree as string)
    sync.write('orders/events.go', 'package orders\n\ntype OrderShipped struct{}\n\ntype OrderPlaced struct{}\n')
    sync.git('add', 'orders/events.go')
    sync.git('-c', 'core.editor=true', 'rebase', '--continue')
    const finished = await runStrata(repo.dir, ['sync'])

    expect(finished.status).toBe(0)
    expect(finished.stdout).toContain('The sync rebase for the stack of orders-events is done.')
    const list = await runStrata(repo.dir, ['--list'])
    expect(list.stdout).not.toContain('behind parent')
    expect(repo.git('show', 'orders-events:orders/events.go')).toContain('OrderShipped')
    expect(repo.git('status', '--porcelain')).toBe('')
  })
})

describe('strata sync --dry-run', () => {
  it('is in the help', async () => {
    const result = await runStrata(ordersRepo().dir, ['--help'])

    expect(result.status).toBe(0)
    expect(result.stdout).toMatch(/^\s+sync\s/m)
  })

  it('gives git the terminal, so that git can ask for a passphrase', async () => {
    const repo = ordersRepo()
    const askPassphrase = join(scratchDir(), 'ask-passphrase')
    writeFileSync(askPassphrase, '#!/bin/sh\nexec </dev/tty >/dev/tty\nprintf "Enter passphrase: "\nread answer\n', {
      mode: 0o755,
    })
    repo.git('remote', 'set-url', 'origin', 'ssh://example.invalid/shop.git')
    repo.git('config', 'core.sshCommand', askPassphrase)
    const strata = await openStrata(repo.dir, ['sync', '--dry-run'])

    await strata.waitForText('Enter passphrase:')
    await strata.press('enter')

    await strata.waitForText('EXIT:1')
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
    expect(lines[3]).toMatch(/^ {3}└─ orders-handler\s+moves onto orders-events, checked out in \/.+\/repo$/)
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
