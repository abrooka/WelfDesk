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

export interface QuestionOption {
  label: string
  description?: string
}

export interface Question {
  id: string
  header?: string
  question: string
  multiSelect?: boolean
  options: QuestionOption[]
}

export interface QuestionRequestData {
  requestId: string
  toolCallId: string
  questions: Question[]
}

export interface QuestionAnswer {
  questionId: string
  selected?: string[]
  custom?: string
}

export interface QuestionResolvedData {
  requestId: string
  answers?: QuestionAnswer[]
  reason?: string
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

export interface MCPServerConfig {
  command: string
  args: string[]
  envKeys: string[]
}

export interface MCPServerStatus {
  name: string
  tools: number
  error?: string
}

export interface Config {
  dataDir: string
  listen: string
  permissionMode: 'ask' | 'acceptEdits' | 'yolo'
  maxTurns: number
  compactTokens: number
  provider: ProviderConfig
  mcpServers: Record<string, MCPServerConfig>
}

export interface Health {
  ok: boolean
  name: string
  version: string
  provider: string
  model: string
  mcp: MCPServerStatus[]
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

export type TodoStatus = 'pending' | 'in_progress' | 'completed'

export interface TodoItem {
  id: string
  content: string
  status: TodoStatus
}

export interface TodosData {
  todos: TodoItem[]
}

export interface CompactedData {
  summary: string
  upToSeq: number
  messages: number
}

export interface Checkpoint {
  id: string
  sessionId: string
  runId: string
  toolCallId: string
  tool: string
  path: string
  existed: boolean
  diff?: string
  createdAt: string
  revertedAt?: string
}

export interface MainInfo {
  version: string
  serverURL: string
  packaged: boolean
  platform: string
}

export interface WindowState {
  maximized: boolean
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
  windowControl(action: 'minimize' | 'toggle-maximize' | 'close'): Promise<WindowState>
  windowState(): Promise<WindowState>
  onWindowState(callback: (state: WindowState) => void): () => void
}

declare global {
  interface Window {
    weft?: WeftBridge
  }
}
