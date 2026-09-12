import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { chmodSync, copyFileSync, readFileSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import type { AddressInfo } from 'node:net'
import { join } from 'node:path'
import { describe, expect, it, onTestFinished } from 'vitest'
import { buildTag, getExecutablePath, runStrata, scratchDir } from '../testUtils'

function platform(): string {
  const arch = process.arch === 'x64' ? 'amd64' : process.arch
  return `${process.platform}-${arch}`
}

// fakeRelease serves a GitHub API whose latest release is tag, with an archive
// for this platform that holds newBinary as strata.
async function fakeRelease(tag: string, newBinary: string): Promise<string> {
  const dir = scratchDir()
  writeFileSync(join(dir, 'strata'), newBinary, { mode: 0o755 })
  const archive = `strata-${platform()}.tar.gz`
  execFileSync('tar', ['-czf', join(dir, archive), '-C', dir, 'strata'])
  const data = readFileSync(join(dir, archive))
  const assets: Record<string, Buffer> = {
    [archive]: data,
    'checksums.txt': Buffer.from(`${createHash('sha256').update(data).digest('hex')}  ${archive}\n`),
  }

  const server = createServer((request, response) => {
    const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
    if (request.url === '/repos/hpcsc/strata/releases/latest') {
      response.setHeader('content-type', 'application/json')
      response.end(JSON.stringify({
        tag_name: tag,
        assets: Object.keys(assets).map((name) => ({ name, url: `${base}/assets/${name}` })),
      }))
      return
    }
    const name = (request.url ?? '').replace('/assets/', '')
    if (request.headers.accept === 'application/octet-stream' && assets[name]) {
      response.end(assets[name])
      return
    }
    response.statusCode = 404
    response.end()
  })
  await new Promise<void>((done) => server.listen(0, '127.0.0.1', done))
  onTestFinished(() => new Promise<void>((done) => server.close(() => done())))
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`
}

function againstApi(api: string): Record<string, string> {
  return { GITHUB_API_URL: api, GITHUB_TOKEN: '', GH_TOKEN: '' }
}

describe('strata update', () => {
  it('--check reports a newer release and changes nothing', async () => {
    const api = await fakeRelease('v9.0.0', '#!/bin/sh\necho "the new strata"\n')

    const result = await runStrata(scratchDir(), ['update', '--check'], againstApi(api))

    expect(result.status).toBe(0)
    expect(result.stdout).toContain(`strata v9.0.0 is available. This is ${buildTag()}.`)
  })

  it('replaces the binary with the newer release', async () => {
    const api = await fakeRelease('v9.0.0', '#!/bin/sh\necho "the new strata"\n')
    const copy = join(scratchDir(), 'strata')
    copyFileSync(getExecutablePath(), copy)
    chmodSync(copy, 0o755)

    const update = await runStrata(scratchDir(), ['update'], againstApi(api), copy)

    expect(update.status).toBe(0)
    expect(update.stdout).toContain(`Updated strata from ${buildTag()} to v9.0.0`)
    const replaced = await runStrata(scratchDir(), [], {}, copy)
    expect(replaced.stdout).toContain('the new strata')
  })

  it('names each step on stderr while it updates, so that it does not look stuck', async () => {
    const api = await fakeRelease('v9.0.0', '#!/bin/sh\necho "the new strata"\n')
    const copy = join(scratchDir(), 'strata')
    copyFileSync(getExecutablePath(), copy)
    chmodSync(copy, 0o755)

    const update = await runStrata(scratchDir(), ['update'], againstApi(api), copy)

    expect(update.status).toBe(0)
    expect(update.stderr).toBe(`Finding the latest release of hpcsc/strata…\nDownloading strata v9.0.0 for ${platform()}…\n`)
  })

  it('says so when the binary is the latest release', async () => {
    const api = await fakeRelease(buildTag(), '#!/bin/sh\necho "never installed"\n')

    const result = await runStrata(scratchDir(), ['update'], againstApi(api))

    expect(result.status).toBe(0)
    expect(result.stdout).toContain(`strata ${buildTag()} is the latest release.`)
  })
})
