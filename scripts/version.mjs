import {readFileSync, existsSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
import path from 'node:path'

export function normalizeVersion(value, increment = 0n) {
  const match = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.exec(value)
  if (!match || typeof increment !== 'bigint' || increment < 0n) throw new Error('Invalid version')
  let [major, middle, last] = match.slice(1).map(BigInt)
  last += increment
  middle += last / 10n
  last %= 10n
  major += middle / 100n
  middle %= 100n
  return `${major}.${middle}.${last}`
}

export function checkVersions(root, tag) {
  const read = name => readFileSync(path.join(root, name), 'utf8').replace(/\r\n/g, '\n')
  const version = read('VERSION').trim()
  if (normalizeVersion(version) !== version) throw new Error('VERSION must use base-10 last and base-100 middle digits')
  const pkg = JSON.parse(read('web/package.json'))
  const lock = JSON.parse(read('web/package-lock.json'))
  const go = read('internal/app/version.go').match(/^var Version = "([^"]+)"$/m)?.[1]
  const docker = [...read('Dockerfile').matchAll(/^ARG AETHER_VERSION=(.+)$/gm)].map(match => match[1].trim())
  const versions = [pkg.version, lock.version, lock.packages?.['']?.version, go, ...docker]
  if (docker.length !== 2 || versions.some(value => value !== version)) throw new Error('Version metadata is inconsistent')
  const readme = read('README.md')
  if (!readme.includes(`**v${version}**`) || !readme.includes(`(docs/releases/v${version}.md)`)
      || !existsSync(path.join(root, `docs/releases/v${version}.md`))) throw new Error('Current release documentation is inconsistent')
  if (tag && tag !== `v${version}`) throw new Error('Release tag must match VERSION')
  return version
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const root = fileURLToPath(new URL('../', import.meta.url))
    const [command, value] = process.argv.slice(2)
    if (command === '--normalize' && value) console.log(normalizeVersion(value))
    else if (command === '--next' && !value) console.log(normalizeVersion(readFileSync(path.join(root, 'VERSION'), 'utf8').trim(), 1n))
    else if (command === '--check' && !value) {
      const tag = process.env.GITHUB_REF_TYPE === 'tag' ? process.env.GITHUB_REF_NAME : undefined
      console.log(`Version verified: ${checkVersions(root, tag)}`)
    } else throw new Error('Usage: node scripts/version.mjs --check | --next | --normalize VERSION')
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}
