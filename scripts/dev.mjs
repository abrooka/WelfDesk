import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const bin = join(root, 'bin', process.platform === 'win32' ? 'weft.exe' : 'weft')

const build = spawn('go', ['build', '-o', bin, './cmd/weft'], {
  cwd: root,
  stdio: 'inherit',
})
build.on('exit', (code) => {
  if (code !== 0) process.exit(code ?? 1)
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const dev = spawn('pnpm', ['--filter', '@weft/desktop', 'dev'], {
    cwd: root,
    stdio: 'inherit',
    shell: process.platform === 'win32',
    env,
  })
  dev.on('exit', (devCode) => process.exit(devCode ?? 0))
})
