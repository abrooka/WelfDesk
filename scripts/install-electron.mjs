import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'

const require = createRequire(new URL('../apps/desktop/package.json', import.meta.url))

function installed() {
  try {
    const executable = require('electron')
    return typeof executable === 'string' && existsSync(executable)
  } catch {
    return false
  }
}

if (installed()) {
  console.log('[electron] binary already present')
  process.exit(0)
}

const packageDir = dirname(require.resolve('electron/package.json'))
const installScript = join(packageDir, 'install.js')

function run(env) {
  return spawnSync(process.execPath, [installScript], {
    cwd: packageDir,
    stdio: 'inherit',
    env: { ...process.env, ...env },
  })
}

console.log('[electron] downloading binary')
let result = run({})
if (result.status !== 0) {
  console.log('[electron] default download failed, retrying through the npmmirror mirror')
  result = run({ ELECTRON_MIRROR: process.env.ELECTRON_MIRROR || 'https://npmmirror.com/mirrors/electron/' })
}

if (result.status !== 0 || !installed()) {
  console.error('[electron] binary download failed; set ELECTRON_MIRROR to a reachable mirror and run "pnpm install" again')
  process.exit(1)
}

console.log('[electron] binary ready')
