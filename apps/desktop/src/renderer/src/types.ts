export interface Folder {
  id: string
  name: string
  path: string
}

export interface Workspace {
  id: string
  name: string
  folders: Folder[]
  createdAt: string
}

export type SessionStatus = 'idle' | 'running' | 'error'

export interface Session {
  id: string
  workspaceId: string
  title: string
  status: SessionStatus
  createdAt: string
  updatedAt: string
  lastSeq: number
}

export interface WeftEvent<T = unknown> {
  id: string
  sessionId: string
  seq: number
  type: string
  runId?: string
  at: string
  data?: T
}

export interface UserMessageData {
  text: string
}

export interface ToolCallData {
  id: string
  name: string
  arguments: string
}

export interface AssistantMessageData {
  text: string
  toolCalls?: ToolCallData[]
}

export interface ToolResultData {
  toolCallId: string
  name: string
  output: string
  diff?: string
  path?: string
  isError: boolean
  durationMs: number
}

export interface PermissionRequestData {
  requestId: string
  tool: string
  input: string
  reason: string
}

export interface PermissionResolvedData {
  requestId: string
  decision: 'allow' | 'deny'
}

export interface ErrorData {
  message: string
}

export interface ProviderConfig {
  kind: string
  baseUrl: string
  model: string
  maxTokens: number
  keySet: boolean
}

export interface Config {
  dataDir: string
  listen: string
  permissionMode: 'ask' | 'acceptEdits' | 'yolo'
  maxTurns: number
  provider: ProviderConfig
}

export interface Health {
  ok: boolean
  name: string
  version: string
  provider: string
  model: string
}

export interface RequestRecord {
  at: string
  started: {
    requestId: string
    provider: string
    model: string
    system: string
    messages: unknown[]
    tools: unknown[]
    startedAt: string
  }
  result?: {
    requestId: string
    durationMs: number
    usage: { inputTokens: number; outputTokens: number }
    text: string
    toolCalls?: ToolCallData[]
    error?: string
  }
}

export interface FileEntry {
  name: string
  path: string
  isDir: boolean
}

export interface MainInfo {
  version: string
  serverURL: string
  packaged: boolean
  platform: string
}

export interface WeftBridge {
  serverURL: string
  platform: string
  info(): Promise<MainInfo>
  pickFolders(): Promise<Array<{ name: string; path: string }>>
  reveal(path: string): Promise<void>
  copyText(text: string): Promise<void>
  openExternal(url: string): Promise<void>
  openWith(target: string, path: string): Promise<void>
}

declare global {
  interface Window {
    weft?: WeftBridge
  }
}
