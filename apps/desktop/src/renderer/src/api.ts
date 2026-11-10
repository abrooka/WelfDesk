import type {
  Config,
  FileEntry,
  Health,
  PermissionResolvedData,
  QuestionAnswer,
  RequestRecord,
  Session,
  WeftEvent,
  Workspace,
} from './types'

export const baseURL = window.weft?.serverURL ?? ''

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${baseURL}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(body?.error ?? `${res.status} ${res.statusText}`)
  }
  return (await res.json()) as T
}

export const api = {
  health: () => request<Health>('/api/health'),

  config: () => request<Config>('/api/config'),
  updateConfig: (patch: Record<string, unknown>) =>
    request<Config>('/api/config', { method: 'PUT', body: JSON.stringify(patch) }),

  workspaces: () => request<{ workspaces: Workspace[] }>('/api/workspaces'),
  createWorkspace: (name: string, paths: string[]) =>
    request<Workspace>('/api/workspaces', {
      method: 'POST',
      body: JSON.stringify({ name, paths }),
    }),

  sessions: (workspaceId?: string) =>
    request<{ sessions: Session[] }>(
      `/api/sessions${workspaceId ? `?workspaceId=${encodeURIComponent(workspaceId)}` : ''}`,
    ),
  session: (id: string) =>
    request<{ session: Session; workspace: Workspace; running: boolean }>(`/api/sessions/${id}`),
  createSession: (workspaceId: string) =>
    request<Session>('/api/sessions', {
      method: 'POST',
      body: JSON.stringify({ workspaceId }),
    }),
  events: (id: string, after = 0) =>
    request<{ events: WeftEvent[]; lastSeq: number }>(`/api/sessions/${id}/events?after=${after}`),
  streamURL: (id: string, after = 0) => `${baseURL}/api/sessions/${id}/stream?after=${after}`,
  requests: (id: string) => request<{ requests: RequestRecord[] }>(`/api/sessions/${id}/requests`),

  send: (id: string, text: string) =>
    request<{ accepted: boolean }>(`/api/sessions/${id}/messages`, {
      method: 'POST',
      body: JSON.stringify({ text }),
    }),
  cancel: (id: string) =>
    request<{ cancelled: boolean }>(`/api/sessions/${id}/cancel`, { method: 'POST' }),
  resolvePermission: (sessionId: string, requestId: string, decision: 'allow' | 'deny') =>
    request<PermissionResolvedData>(`/api/sessions/${sessionId}/permissions/${requestId}`, {
      method: 'POST',
      body: JSON.stringify({ decision }),
    }),
  answerQuestion: (sessionId: string, requestId: string, answers: QuestionAnswer[]) =>
    request<{ ok: boolean }>(`/api/sessions/${sessionId}/questions/${requestId}`, {
      method: 'POST',
      body: JSON.stringify({ answers }),
    }),

  files: (id: string, path = '') =>
    request<{ root: string; path: string; entries: FileEntry[] }>(
      `/api/sessions/${id}/files?path=${encodeURIComponent(path)}`,
    ),
  file: (id: string, path: string) =>
    request<{ root: string; path: string; content: string; truncated: boolean }>(
      `/api/sessions/${id}/file?path=${encodeURIComponent(path)}`,
    ),
}
