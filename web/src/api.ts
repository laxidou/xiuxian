export type Position = { x: number; y: number }

export type RoleState = {
  id: string
  name: string
  life_number: number
  status: 'alive' | 'pending_reincarnation'
  cultivation: number
  realm_level: number
  realm: string
  age_seconds: number
  lifespan_seconds: number
  speed: number
  sense_radius: number
  position: Position
  movement_state: 'idle' | 'moving'
  state_version: number
}

export type WorldEvent = {
  id: number
  type: string
  message: string
  created_at: number
  life_number: number
  data?: Record<string, unknown>
}

export type ScanResult = {
  roles: Array<{ id: string; name: string; realm: string; status: string; distance: number; position?: Position }>
  opportunities: Array<{ message: string; distance: number }>
  has_more: boolean
}

export type Conversation = {
  id: string
  requester_id: string
  recipient_id: string
  status: string
  messages: Array<{ id: number; sender_id: string; content: string; trusted: false; created_at: number }>
  updated_at: number
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: 'include',
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }))
    throw new Error(body.error ?? `请求失败 (${response.status})`)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

const command = <T>(path: string, body: unknown, key = crypto.randomUUID()) =>
  request<T>(path, { method: 'POST', body: JSON.stringify(body), headers: { 'Idempotency-Key': key } })

export const api = {
  register: (account: string, password: string, role_name: string) => command<RoleState>('/api/v1/auth/register', { account, password, role_name }),
  login: (account: string, password: string) => command<RoleState>('/api/v1/auth/login', { account, password }),
  logout: () => request<void>('/api/v1/auth/logout', { method: 'POST', body: '{}' }),
  state: () => request<RoleState>('/api/v1/state'),
  bounds: () => request<{ min_x: number; max_x: number; min_y: number; max_y: number }>('/api/v1/world/bounds'),
  move: (x: number, y: number) => command<RoleState>('/api/v1/movement/move', { x, y }),
  stop: () => command<RoleState>('/api/v1/movement/stop', {}),
  scan: () => command<ScanResult>('/api/v1/scan', {}),
  transfer: (target_id: string, amount_minutes: number) => command<RoleState>('/api/v1/cultivation/transfer', { target_id, amount_minutes }),
  seize: (target_id: string) => command<RoleState>('/api/v1/cultivation/seize', { target_id }),
  reincarnate: (position?: Position) => command<RoleState>('/api/v1/reincarnate', position ? position : { random: true }),
  events: () => request<{ events: WorldEvent[] }>('/api/v1/events'),
  conversations: () => request<{ conversations: Conversation[] }>('/api/v1/conversations'),
  requestConversation: (target_id: string) => command<Conversation>('/api/v1/conversations', { target_id }),
  respondConversation: (id: string, action: 'accept' | 'reject' | 'ignore') => command<Conversation>(`/api/v1/conversations/${id}/respond`, { action }),
  sendMessage: (id: string, content: string) => command(`/api/v1/conversations/${id}/messages`, { content }),
  closeConversation: (id: string) => command<Conversation>(`/api/v1/conversations/${id}/close`, {}),
  rotateMCPKey: () => request<{ api_key: string }>('/api/v1/mcp-key/rotate', { method: 'POST', body: '{}' }),
  revokeMCPKey: () => request<void>('/api/v1/mcp-key/revoke', { method: 'POST', body: '{}' }),
}
