#!/usr/bin/env node
'use strict'

/**
 * Finds the binary for this machine and runs it.
 *
 * The binaries are not in this package. Each platform has its own package,
 * listed under optionalDependencies, and npm installs only the one whose `os`
 * and `cpu` match — so `npm install refigure-cli` downloads one binary, not
 * six. Nothing is fetched after install, which is what makes this work behind
 * a proxy, in an offline cache, and with --ignore-scripts.
 */

const { spawnSync } = require('node:child_process')

// `${process.platform}-${process.arch}` on the left, the package that holds
// that binary on the right. They match except for one: npm's spam filter
// refuses the name `refigure-cli-win32-x64` — consistently, on different days
// and different networks, while every other name in this set was accepted. The
// package is called `-windows-x64` instead. Nobody types these names; only this
// map reads them.
const PLATFORMS = {
  'darwin-arm64': 'refigure-cli-darwin-arm64',
  'darwin-x64': 'refigure-cli-darwin-x64',
  'linux-arm64': 'refigure-cli-linux-arm64',
  'linux-x64': 'refigure-cli-linux-x64',
  'win32-arm64': 'refigure-cli-win32-arm64',
  'win32-x64': 'refigure-cli-windows-x64'
}

function fail(message) {
  process.stderr.write(`refigure: ${message}\n`)
  process.exit(1)
}

const key = `${process.platform}-${process.arch}`
const pkg = PLATFORMS[key]
if (!pkg) {
  fail(
    `there is no refigure build for ${key}.\n` +
      'Supported: ' +
      Object.keys(PLATFORMS).join(', ') +
      '\nBuild from source instead: go install github.com/oduvan/refigure-cli/cmd/refigure@latest'
  )
}

const executable = process.platform === 'win32' ? 'refigure.exe' : 'refigure'

let binary
try {
  binary = require.resolve(`${pkg}/${executable}`)
} catch {
  fail(
    `the ${pkg} package is missing.\n` +
      'It is an optional dependency, so this usually means the install skipped it —\n' +
      'try `npm install refigure-cli --force`, or install with optional dependencies enabled.'
  )
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' })

if (result.error) {
  fail(`could not run ${binary}: ${result.error.message}`)
}
// Pass the exporter's own exit code through: 2 means the project file could not
// be read, and a caller distinguishes that from an ordinary failure.
process.exit(result.status === null ? 1 : result.status)
