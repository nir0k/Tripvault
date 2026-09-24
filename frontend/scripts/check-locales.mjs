// Fails when the locale dictionaries do not hold the same set of keys.
//
// Every dictionary in src/i18n is compared with every other, so a string added
// in one language and forgotten in another breaks `make lint` instead of
// showing up as a raw key in the interface.

import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

const dir = new URL('../src/i18n/', import.meta.url).pathname

// flatten returns the dotted paths of every leaf in a dictionary.
function flatten(value, prefix = '') {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return [prefix]
  }
  return Object.entries(value).flatMap(([key, child]) => flatten(child, prefix ? `${prefix}.${key}` : key))
}

const files = readdirSync(dir).filter((name) => name.endsWith('.json')).sort()
if (files.length === 0) {
  console.error(`no locale dictionaries found in ${dir}`)
  process.exit(1)
}

const keys = new Map(
  files.map((name) => [name, new Set(flatten(JSON.parse(readFileSync(join(dir, name), 'utf8'))))]),
)
const all = new Set([...keys.values()].flatMap((set) => [...set]))

let failed = false
for (const [name, set] of keys) {
  const missing = [...all].filter((key) => !set.has(key)).sort()
  if (missing.length > 0) {
    failed = true
    console.error(`${name} is missing ${missing.length} key(s):\n  ${missing.join('\n  ')}`)
  }
}

if (failed) {
  process.exit(1)
}
console.log(`locale dictionaries agree: ${files.join(', ')} (${all.size} keys)`)
