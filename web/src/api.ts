import { OpenAPI } from './generated/core/OpenAPI'
import type { v1AuthResponse } from './generated/models/v1AuthResponse'
import type { v1Conversation } from './generated/models/v1Conversation'
import type { v1ConversationMessage } from './generated/models/v1ConversationMessage'
import type { rpcStatus } from './generated/models/rpcStatus'
import type { v1RoleState } from './generated/models/v1RoleState'
import type { v1ScanResponse } from './generated/models/v1ScanResponse'
import type { v1WorldEvent } from './generated/models/v1WorldEvent'
import type { v1WorldBounds } from './generated/models/v1WorldBounds'
import { AuthServiceService } from './generated/services/AuthServiceService'
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
  rule_version: number
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
  truncated_roles: number
  truncated_opportunities: number
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

const expectation = () => {
  if (!commandExpectation) throw new Error('请先刷新角色状态')
  return {
    expectedLifeNumber: String(commandExpectation.lifeNumber),
    expectedStateVersion: String(commandExpectation.stateVersion),
  }
}

const idempotent = () => ({ idempotencyKey: crypto.randomUUID(), ...expectation() })

export const api = {
  health: () => request<Health>('/healthz'),
  register: async (account: string, password: string, roleName: string) => rememberState(contractAuthState(await AuthServiceService.authServiceRegister({ account, password, roleName }))),
  login: async (account: string, password: string) => rememberState(contractAuthState(await AuthServiceService.authServiceLogin({ account, password }))),
  logout: async () => { unwrap(await AuthServiceService.authServiceLogout()); commandExpectation = null },
  state: async () => rememberState(contractState(unwrap(await WorldServiceService.worldServiceGetState()))),
  bounds: async () => contractBounds(unwrap(await WorldServiceService.worldServiceGetWorldBounds())),
  move: async (x: number, y: number) => rememberState(contractState(unwrap(await WorldServiceService.worldServiceMove({
    ...idempotent(),
    target: { xMilliunits: String(Math.round(x * 1000)), yMilliunits: String(Math.round(y * 1000)) },
  })))),
  stop: async () => rememberState(contractState(unwrap(await WorldServiceService.worldServiceStop(idempotent())))),
  scan: async () => contractScan(unwrap(await WorldServiceService.worldServiceScan(expectation()))),
  transfer: async (targetId: string, amountMinutes: number) => rememberState(contractState(unwrap(await WorldServiceService.worldServiceTransferCultivation({
    ...idempotent(), targetId, amountMinutes: String(amountMinutes),
  })))),
  seize: async (targetId: string) => rememberState(contractState(unwrap(await WorldServiceService.worldServiceSeizeCultivation({ ...idempotent(), targetId })))),
  reincarnate: async (position?: Position) => rememberState(contractState(unwrap(await WorldServiceService.worldServiceReincarnate(position ? {
    ...idempotent(),
    position: { xMilliunits: String(Math.round(position.x * 1000)), yMilliunits: String(Math.round(position.y * 1000)) },
  } : { ...idempotent(), random: true })))),
  events: async () => ({ events: contractEvents(unwrap(await WorldServiceService.worldServiceListRecentEvents(undefined, 100)).events ?? []) }),
  conversations: async () => ({ conversations: (unwrap(await WorldServiceService.worldServiceListConversations()).conversations ?? []).map(contractConversation) }),
  requestConversation: async (targetId: string) => contractConversation(unwrap(await WorldServiceService.worldServiceRequestConversation({ ...idempotent(), targetId }))),
  respondConversation: async (conversationId: string, action: 'accept' | 'reject' | 'ignore') => contractConversation(unwrap(await WorldServiceService.worldServiceRespondConversation({ ...idempotent(), conversationId, action }))),
  sendMessage: async (conversationId: string, content: string) => contractConversationMessage(unwrap(await WorldServiceService.worldServiceSendConversationMessage({ ...idempotent(), conversationId, content }))),
  closeConversation: async (conversationId: string) => contractConversation(unwrap(await WorldServiceService.worldServiceCloseConversation({ ...idempotent(), conversationId }))),
  rotateMCPKey: async () => {
    const result = unwrap(await AuthServiceService.authServiceRotateMcpKey({}))
    await api.state()
    return { api_key: result.apiKey ?? '' }
  },
  revokeMCPKey: async () => { unwrap(await AuthServiceService.authServiceRevokeMcpKey()); await api.state() },
}

function isRPCStatus(response: object): response is rpcStatus {
  return 'code' in response || ('message' in response && !('id' in response))
}

function unwrap<T extends object>(response: T | rpcStatus): T {
  if (isRPCStatus(response)) throw new Error(response.message ?? '契约请求失败')
  return response
}

function contractAuthState(response: v1AuthResponse | rpcStatus): RoleState {
  const result = unwrap(response)
  if (!result.state) throw new Error('鉴权响应缺少角色状态')
  return contractState(result.state)
}

function contractState(response: v1RoleState): RoleState {
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
    rule_version: response.ruleVersion ?? 0,
  }
}

function contractBounds(response: v1WorldBounds) {
  return {
    min_x: Number(response.minXMilliunits ?? 0) / 1000,
    max_x: Number(response.maxXMilliunits ?? 0) / 1000,
    min_y: Number(response.minYMilliunits ?? 0) / 1000,
    max_y: Number(response.maxYMilliunits ?? 0) / 1000,
  }
}

function contractScan(response: v1ScanResponse): ScanResult {
  return {
    roles: (response.roles ?? []).map((role) => ({
      id: role.id ?? '',
      name: role.name ?? '',
      realm: role.realm ?? '',
      status: role.status ?? '',
      distance: role.distance ?? 0,
      position: role.position ? {
        x: Number(role.position.xMilliunits ?? 0) / 1000,
        y: Number(role.position.yMilliunits ?? 0) / 1000,
      } : undefined,
    })),
    opportunities: (response.opportunities ?? []).map((item) => ({ message: item.message ?? '', distance: item.distance ?? 0 })),
    has_more: response.hasMore ?? false,
    truncated_roles: response.truncatedRoles ?? 0,
    truncated_opportunities: response.truncatedOpportunities ?? 0,
  }
}

function contractConversation(value: v1Conversation): Conversation {
  return {
    id: value.id ?? '',
    requester_id: value.requesterId ?? '',
    recipient_id: value.recipientId ?? '',
    status: value.status ?? '',
    messages: (value.messages ?? []).map(contractConversationMessage),
    updated_at: Number(value.updatedAtUnixMillis ?? 0),
  }
}

function contractConversationMessage(value: v1ConversationMessage): Conversation['messages'][number] {
  return {
    id: Number(value.id ?? 0),
    sender_id: value.senderId ?? '',
    content: value.content ?? '',
    trusted: false,
    created_at: Number(value.createdAtUnixMillis ?? 0),
  }
}

function contractEvents(values: v1WorldEvent[]): WorldEvent[] {
  return values.map((event) => ({
    id: Number(event.id ?? 0),
    type: event.type ?? '',
    message: event.message ?? '',
    created_at: Number(event.createdAtUnixMillis ?? 0),
    life_number: Number(event.lifeNumber ?? 0),
    data: event.dataJson ? JSON.parse(event.dataJson) as Record<string, unknown> : undefined,
  }))
}
