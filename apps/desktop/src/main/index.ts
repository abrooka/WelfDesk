import { app, BrowserWindow, clipboard, dialog, ipcMain, shell } from 'electron'
import { spawn, type ChildProcess } from 'node:child_process'
import { existsSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'

const PORT = Number(process.env.WEFT_PORT || 47123)
const SERVER_URL = process.env.WEFT_SERVER_URL || `http://127.0.0.1:${PORT}`

let win: BrowserWindow | null = null
let server: ChildProcess | null = null

function serverBinary(): string | null {
  const exe = process.platform === 'win32' ? 'weft.exe' : 'weft'
  const candidates = app.isPackaged
    ? [join(process.resourcesPath, 'bin', exe)]
    : [join(app.getAppPath(), '..', '..', 'bin', exe), join(process.cwd(), 'bin', exe)]
  return candidates.find((path) => existsSync(path)) ?? null
}

async function healthy(): Promise<boolean> {
  try {
    const res = await fetch(`${SERVER_URL}/api/health`, { signal: AbortSignal.timeout(800) })
    return res.ok
  } catch {
    return false
  }
}

async function ensureServer(): Promise<void> {
  if (await healthy()) return
  const bin = serverBinary()
  if (!bin) {
    console.warn('weft server binary not found; run "pnpm build:server" first')
    return
  }
  const args = ['server', '--port', String(PORT)]
  if (process.env.WEFT_DATA_DIR) args.push('--data-dir', process.env.WEFT_DATA_DIR)
  server = spawn(bin, args, {
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  })
  server.stdout?.on('data', (data) => console.log('[weft]', String(data).trim()))
  server.stderr?.on('data', (data) => console.error('[weft]', String(data).trim()))
  server.on('exit', (code) => {
    console.warn('[weft] server exited with code', code)
    server = null
  })
  for (let i = 0; i < 50; i++) {
    if (await healthy()) return
    await new Promise((resolve) => setTimeout(resolve, 200))
  }
  console.error('[weft] server did not become healthy')
}

function createWindow(): void {
  process.env.WEFT_SERVER_URL = SERVER_URL
  win = new BrowserWindow({
    width: 1440,
    height: 920,
    minWidth: 1080,
    minHeight: 680,
    show: false,
    autoHideMenuBar: true,
    backgroundColor: '#0d0f10',
    title: 'WeftDesk',
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  })
  win.once('ready-to-show', () => win?.show())
  win.on('closed', () => {
    win = null
  })
  if (process.env.ELECTRON_RENDERER_URL) {
    void win.loadURL(process.env.ELECTRON_RENDERER_URL)
  } else {
    void win.loadFile(join(__dirname, '../renderer/index.html'))
  }
}

function requirePath(path: unknown): string {
  if (typeof path !== 'string' || path.trim() === '') throw new Error('path is required')
  if (!existsSync(path)) throw new Error(`path does not exist: ${path}`)
  return path
}

function openInTerminal(path: string): void {
  const dir = statSync(path).isDirectory() ? path : dirname(path)
  if (process.platform === 'win32') {
    const child = spawn(process.env.COMSPEC || 'cmd.exe', ['/c', 'start', '""', 'cmd', '/k'], {
      cwd: dir,
      detached: true,
      stdio: 'ignore',
    })
    child.unref()
    return
  }
  if (process.platform === 'darwin') {
    spawn('open', ['-a', 'Terminal', dir], { detached: true, stdio: 'ignore' }).unref()
    return
  }
  spawn('x-terminal-emulator', ['--working-directory', dir], { detached: true, stdio: 'ignore' }).unref()
}

function fileURL(path: string): string {
  return encodeURI(path.replace(/\\/g, '/'))
}

ipcMain.handle('weft:info', () => ({
  version: app.getVersion(),
  serverURL: SERVER_URL,
  packaged: app.isPackaged,
  platform: process.platform,
}))

ipcMain.handle('weft:pick-folders', async () => {
  if (!win) return []
  const result = await dialog.showOpenDialog(win, {
    title: 'Add workspace folders',
    properties: ['openDirectory', 'multiSelections'],
  })
  if (result.canceled) return []
  return result.filePaths.map((path) => ({
    path,
    name: path.split(/[\\/]/).filter(Boolean).pop() ?? path,
  }))
})

ipcMain.handle('weft:reveal', (_event, path: unknown) => {
  shell.showItemInFolder(requirePath(path))
})

ipcMain.handle('weft:copy', (_event, text: unknown) => {
  clipboard.writeText(String(text ?? ''))
})

ipcMain.handle('weft:open-external', async (_event, url: unknown) => {
  if (typeof url !== 'string' || !/^https?:\/\//i.test(url)) throw new Error('unsupported URL')
  await shell.openExternal(url)
})

ipcMain.handle('weft:open-with', async (_event, target: unknown, path: unknown) => {
  const file = requirePath(path)
  switch (target) {
    case 'explorer':
    case 'finder':
      shell.showItemInFolder(file)
      return
    case 'terminal':
      openInTerminal(file)
      return
    case 'vscode':
      await shell.openExternal(`vscode://file/${fileURL(file)}`)
      return
    case 'idea':
      await shell.openExternal(`idea://open?file=${fileURL(file)}`)
      return
    case 'default':
      await shell.openPath(file)
      return
    default:
      throw new Error(`unknown target: ${String(target)}`)
  }
})

const locked = app.requestSingleInstanceLock()
if (!locked) {
  app.quit()
} else {
  app.on('second-instance', () => {
    if (!win) return
    if (win.isMinimized()) win.restore()
    win.focus()
  })

  app.whenReady().then(async () => {
    await ensureServer()
    createWindow()
    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow()
    })
  })

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit()
  })

  app.on('will-quit', () => {
    if (server && !server.killed) server.kill()
  })
}
