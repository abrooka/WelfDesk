import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { mkdirSync } from 'node:fs'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const out = join(root, 'bin', process.platform === 'win32' ? 'weft.exe' : 'weft')
mkdirSync(join(root, 'bin'), { recursive: true })

const result = spawnSync('go', ['build', '-o', out, './cmd/weft'], {
  cwd: root,
  stdio: 'inherit',
})
process.exit(result.status ?? 1)
