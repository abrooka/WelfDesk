import { Icon } from './Icon'
import { Modal } from './Modal'
import { fileBaseName } from '../lib/format'

export function FilePreview({
  file,
  onClose,
  onNotify,
}: {
  file: { path: string; content: string; truncated: boolean }
  onClose: () => void
  onNotify: (text: string, tone?: 'info' | 'error') => void
}) {
  const bridge = window.weft
  const openWith = async (target: string) => {
    if (!bridge) return
    try {
      await bridge.openWith(target, file.path)
    } catch (cause) {
      onNotify(cause instanceof Error ? cause.message : 'Failed to open externally', 'error')
    }
  }

  return (
    <Modal title={fileBaseName(file.path)} subtitle={file.path} wide onClose={onClose}>
      <div className="file-viewer">
        <div className="file-actions">
          {bridge ? (
            <>
              <button className="link-btn" onClick={() => void openWith('vscode')}>VS Code</button>
              <button className="link-btn" onClick={() => void openWith('idea')}>IDEA</button>
              <button className="link-btn" onClick={() => void openWith('terminal')}>Terminal</button>
              <button className="link-btn" onClick={() => void openWith('explorer')}>Reveal</button>
              <button className="link-btn" onClick={() => void openWith('default')}>Open</button>
            </>
          ) : (
            <span className="file-note">External editors are available in the desktop app.</span>
          )}
          <button className="link-btn" onClick={() => void (bridge ? bridge.copyText(file.content) : navigator.clipboard.writeText(file.content))}>
            <Icon name="copy" size={13} /> Copy
          </button>
        </div>
        {file.truncated && <p className="panel-note">Preview truncated at 2 MB.</p>}
        <pre className="file-content selectable">{file.content}</pre>
      </div>
    </Modal>
  )
}
