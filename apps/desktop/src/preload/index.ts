import { contextBridge, ipcRenderer } from 'electron'

export interface PickedFolder {
  name: string
  path: string
}

const serverURL = process.env.WEFT_SERVER_URL || 'http://127.0.0.1:47123'

contextBridge.exposeInMainWorld('weft', {
  serverURL,
  platform: process.platform,
  info: () => ipcRenderer.invoke('weft:info'),
  pickFolders: (): Promise<PickedFolder[]> => ipcRenderer.invoke('weft:pick-folders'),
  reveal: (path: string): Promise<void> => ipcRenderer.invoke('weft:reveal', path),
  copyText: (text: string): Promise<void> => ipcRenderer.invoke('weft:copy', text),
  openExternal: (url: string): Promise<void> => ipcRenderer.invoke('weft:open-external', url),
  openWith: (target: string, path: string): Promise<void> => ipcRenderer.invoke('weft:open-with', target, path),
  windowControl: (action: 'minimize' | 'toggle-maximize' | 'close'): Promise<{ maximized: boolean }> =>
    ipcRenderer.invoke('weft:window', action),
  windowState: (): Promise<{ maximized: boolean }> => ipcRenderer.invoke('weft:window-state'),
  onWindowState: (callback: (state: { maximized: boolean }) => void): (() => void) => {
    const listener = (_event: Electron.IpcRendererEvent, state: { maximized: boolean }) => callback(state)
    ipcRenderer.on('weft:window-state', listener)
    return () => ipcRenderer.removeListener('weft:window-state', listener)
  },
})
