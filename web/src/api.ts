import { OpenAPI } from './generated/core/OpenAPI'
import type { rpcStatus } from './generated/models/rpcStatus'
import type { v1RoleState } from './generated/models/v1RoleState'
import type { v1WorldBounds } from './generated/models/v1WorldBounds'
import { WorldServiceService } from './generated/services/WorldServiceService'

OpenAPI.WITH_CREDENTIALS = true
OpenAPI.CREDENTIALS = 'include'

let commandExpectation: { lifeNumber: number; stateVersion: number } | null = null

export type Position = { x: number; y: number }

export type Health = { status: 'ok'; service: string; version: string }

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

const rememberState = (state: RoleState) => {
  commandExpectation = { lifeNumber: state.life_number, stateVersion: state.state_version }
  return state
}

const command = <T>(path: string, body: unknown, key = crypto.randomUUID()) => {
  if (!commandExpectation) throw new Error('请先刷新角色状态')
  return request<T>(path, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: {
      'Idempotency-Key': key,
      'X-Expected-Life-Number': String(commandExpectation.lifeNumber),
      'X-Expected-State-Version': String(commandExpectation.stateVersion),
    },
  })
}

const stateCommand = async (path: string, body: unknown) => rememberState(await command<RoleState>(path, body))

export const api = {
  health: () => request<Health>('/api/v1/healthz'),
  register: async (account: string, password: string, role_name: string) => rememberState(await request<RoleState>('/api/v1/auth/register', { method: 'POST', body: JSON.stringify({ account, password, role_name }) })),
  login: async (account: string, password: string) => rememberState(await request<RoleState>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ account, password }) })),
  logout: async () => { await request<void>('/api/v1/auth/logout', { method: 'POST', body: '{}' }); commandExpectation = null },
  state: async () => rememberState(contractState(await WorldServiceService.worldServiceGetState({}))),
  bounds: async () => contractBounds(await WorldServiceService.worldServiceGetWorldBounds({})),
  move: (x: number, y: number) => stateCommand('/api/v1/movement/move', { x, y }),
  stop: () => stateCommand('/api/v1/movement/stop', {}),
  scan: () => command<ScanResult>('/api/v1/scan', {}),
  transfer: (target_id: string, amount_minutes: number) => stateCommand('/api/v1/cultivation/transfer', { target_id, amount_minutes }),
  seize: (target_id: string) => stateCommand('/api/v1/cultivation/seize', { target_id }),
  reincarnate: (position?: Position) => stateCommand('/api/v1/reincarnate', position ? position : { random: true }),
  events: () => request<{ events: WorldEvent[] }>('/api/v1/events'),
  conversations: () => request<{ conversations: Conversation[] }>('/api/v1/conversations'),
  requestConversation: (target_id: string) => command<Conversation>('/api/v1/conversations', { target_id }),
  respondConversation: (id: string, action: 'accept' | 'reject' | 'ignore') => command<Conversation>(`/api/v1/conversations/${id}/respond`, { action }),
  sendMessage: (id: string, content: string) => command(`/api/v1/conversations/${id}/messages`, { content }),
  closeConversation: (id: string) => command<Conversation>(`/api/v1/conversations/${id}/close`, {}),
  rotateMCPKey: async () => { const result = await request<{ api_key: string }>('/api/v1/mcp-key/rotate', { method: 'POST', body: '{}' }); await api.state(); return result },
  revokeMCPKey: async () => { await request<void>('/api/v1/mcp-key/revoke', { method: 'POST', body: '{}' }); await api.state() },
}

function isRPCStatus(response: v1RoleState | v1WorldBounds | rpcStatus): response is rpcStatus {
  return 'code' in response || ('message' in response && !('id' in response))
}

function contractState(response: v1RoleState | rpcStatus): RoleState {
  if (isRPCStatus(response)) throw new Error(response.message ?? '契约请求失败')
  return {
    id: response.id ?? '',
    name: response.name ?? '',
    life_number: Number(response.lifeNumber ?? 0),
    status: response.status as RoleState['status'],
    cultivation: Number(response.cultivationMillis ?? 0) / 60000,
    realm_level: response.realmLevel ?? 0,
    realm: response.realmName ?? '',
    age_seconds: Number(response.ageMillis ?? 0) / 1000,
    lifespan_seconds: Number(response.lifespanMillis ?? 0) / 1000,
    speed: Number(response.speed ?? 0),
    sense_radius: Number(response.senseRadius ?? 0),
    position: { x: Number(response.position?.xMilliunits ?? 0) / 1000, y: Number(response.position?.yMilliunits ?? 0) / 1000 },
    movement_state: response.movementState as RoleState['movement_state'],
    state_version: Number(response.stateVersion ?? 0),
  }
}

function contractBounds(response: v1WorldBounds | rpcStatus) {
  if (isRPCStatus(response)) throw new Error(response.message ?? '契约请求失败')
  return {
    min_x: Number(response.minXMilliunits ?? 0) / 1000,
    max_x: Number(response.maxXMilliunits ?? 0) / 1000,
    min_y: Number(response.minYMilliunits ?? 0) / 1000,
    max_y: Number(response.maxYMilliunits ?? 0) / 1000,
  }
}
