import { execFileSync, spawn } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { launchTerminal, type Session } from 'tuistory'
import { onTestFinished } from 'vitest'

export function getExecutablePath(): string {
  const executablePath = process.env.EXECUTABLE
  if (!executablePath) {
    throw new Error('EXECUTABLE environment variable is required. Set it to the path of the strata binary.')
  }
  return resolve(executablePath)
}

export function buildTag(): string {
  const tag = process.env.BUILD_TAG
  if (!tag) {
    throw new Error('BUILD_TAG environment variable is required. Set it to the tag the strata binary was built with.')
  }
  return tag
}

const gitEnv = {
  ...process.env,
  GIT_AUTHOR_NAME: 'strata',
  GIT_AUTHOR_EMAIL: 'strata@example.com',
  GIT_COMMITTER_NAME: 'strata',
  GIT_COMMITTER_EMAIL: 'strata@example.com',
  GIT_CONFIG_COUNT: '3',
  GIT_CONFIG_KEY_0: 'commit.gpgsign',
  GIT_CONFIG_VALUE_0: 'false',
  GIT_CONFIG_KEY_1: 'core.hooksPath',
  GIT_CONFIG_VALUE_1: '/dev/null',
  GIT_CONFIG_KEY_2: 'init.defaultBranch',
  GIT_CONFIG_VALUE_2: 'main',
}

function git(cwd: string, ...args: string[]): string {
  return execFileSync('git', args, { cwd, env: gitEnv, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })
}

export function scratchDir(): string {
  const dir = mkdtempSync(join(tmpdir(), 'strata-e2e-'))
  onTestFinished(() => rmSync(dir, { recursive: true, force: true }))
  return dir
}

export class Repo {
  private constructor(readonly dir: string) {}

  static create(): Repo {
    const root = scratchDir()
    git(root, 'init', '-q', '--bare', '-b', 'main', 'origin.git')
    git(root, 'clone', '-q', 'origin.git', 'repo')
    const repo = new Repo(join(root, 'repo'))
    repo.commit('README.md', '# shop\n', 'Start the shop')
    repo.git('push', '-q', 'origin', 'main')
    repo.git('remote', 'set-head', 'origin', '-a')
    return repo
  }

  git(...args: string[]): string {
    return git(this.dir, ...args)
  }

  write(path: string, content: string): void {
    mkdirSync(dirname(join(this.dir, path)), { recursive: true })
    writeFileSync(join(this.dir, path), content)
  }

  commit(path: string, content: string, message: string): void {
    this.write(path, content)
    this.git('add', '-A')
    this.git('commit', '-q', '-m', message)
  }

  switchNew(branch: string): void {
    this.git('switch', '-q', '-c', branch)
  }

  switch(branch: string): void {
    this.git('switch', '-q', branch)
  }
}

const handler = `package orders

type Handler struct {
	store Store
}

func (h *Handler) Cancel(id string) error {
	order, err := h.store.Load(id)
	if err != nil {
		return err
	}
	order.Status = "cancelled"
	return h.store.Save(order)
}
`

// ordersRepo holds one stack on the trunk and one branch beside it:
//
//	origin/main
//	├─ billing
//	└─ orders-events            got a fix after orders-handler started
//	   └─ orders-handler        checked out
//	      └─ orders-api
export function ordersRepo(): Repo {
  const repo = Repo.create()
  repo.switchNew('billing')
  repo.commit('billing/invoice.go', 'package billing\n\ntype Invoice struct{}\n', 'Add the invoice')
  repo.switch('main')

  repo.switchNew('orders-events')
  repo.commit('orders/events.go', 'package orders\n\ntype OrderPlaced struct{}\n', 'Name the events')
  repo.commit('orders/order.go', 'package orders\n\ntype Order struct {\n\tStatus string\n}\n', 'Add the order')

  repo.switchNew('orders-handler')
  repo.commit('orders/handler.go', handler, 'Cancel an order through the handler')
  repo.commit('orders/handler_test.go', 'package orders\n\nimport "testing"\n\nfunc TestHandler(t *testing.T) {}\n', 'Test the handler')

  repo.switchNew('orders-api')
  repo.write('orders/handler.go', handler.replace('order.Status = "cancelled"', 'order.Status = "cancelled"\n\torder.Reason = "requested"'))
  repo.commit('api/routes.go', 'package api\n\nfunc Routes() {}\n', 'Expose cancellation over HTTP')

  repo.switch('orders-events')
  repo.commit('orders/events.go', 'package orders\n\ntype OrderPlaced struct{}\n\ntype OrderCancelled struct{}\n', 'Add the cancelled event')
  repo.switch('orders-handler')
  return repo
}

export async function openStrata(cwd: string, args: string[] = []): Promise<Session> {
  const session = await launchTerminal({
    command: 'sh',
    // Bubble Tea v1 asks the terminal for its background colour at start and
    // waits 5 seconds for a reply that this emulator never sends. termenv does
    // not ask under a screen TERM, and tuistory sets its own TERM for the shell.
    args: ['-c', `TERM=screen-256color "${getExecutablePath()}" ${args.join(' ')}; echo "EXIT:$?"`],
    cwd,
    cols: 160,
    rows: 40,
  })
  onTestFinished(() => session.close())
  return session
}

export interface Result {
  stdout: string
  stderr: string
  status: number | null
}

// runStrata does not block, so a fake server in this process can still
// answer the requests strata makes.
export function runStrata(
  cwd: string,
  args: string[],
  env: Record<string, string> = {},
  executable = getExecutablePath(),
): Promise<Result> {
  return new Promise((done, fail) => {
    const child = spawn(executable, args, { cwd, env: { ...process.env, ...env } })
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (chunk) => (stdout += chunk))
    child.stderr.on('data', (chunk) => (stderr += chunk))
    child.on('error', fail)
    child.on('close', (status) => done({ stdout, stderr, status }))
  })
}
