// Command-path preservation property test (Task 2 of web-stale-derived-state).
//
// Req 3.1 (Regression Prevention): every command (move, moveDirection, stop,
// transfer, seize, reincarnate, scan, and the conversation actions) MUST send
// the authoritative `state_version` / `life_number` captured from the last
// server-delivered state (`rememberState` in api.ts). Locally interpolated
// values must NEVER be used as command input.
//
// This tests the REAL api.ts (the source of truth for command gating) with the
// generated transport services mocked, so it is independent of the not-yet-
// existing interpolation. It passes on the current unfixed code and must keep
// passing after the display-only fix, since the fix never touches api.ts's
// command expectation.
import fc from 'fast-check'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// Mock the generated transport layer. Each command captures the request body
// it was called with so we can assert the authoritative expectation is sent.
const world = vi.hoisted(() => ({
  worldServiceGetState: vi.fn(),
  worldServiceMove: vi.fn(async () => ({})),
  worldServiceMoveDirection: vi.fn(async () => ({})),
  worldServiceStop: vi.fn(async () => ({})),
  worldServiceScan: vi.fn(async () => ({})),
  worldServiceTransferCultivation: vi.fn(async () => ({})),
  worldServiceSeizeCultivation: vi.fn(async () => ({})),
  worldServiceReincarnate: vi.fn(async () => ({})),
  worldServiceRequestConversation: vi.fn(async () => ({})),
  worldServiceRespondConversation: vi.fn(async () => ({})),
  worldServiceSendConversationMessage: vi.fn(async () => ({})),
  worldServiceCloseConversation: vi.fn(async () => ({})),
}))

vi.mock('./generated/services/WorldServiceService', () => ({ WorldServiceService: world }))
vi.mock('./generated/services/AuthServiceService', () => ({ AuthServiceService: {} }))

import { api } from './api'

// Minimal authoritative RoleState (v1RoleState shape) carrying the version and
// life number that commands must echo back. Other fields fall back to defaults
// in contractState.
function authoritativeState(stateVersion: number, lifeNumber: number) {
  return {
    id: 'role-1',
    name: '测试道友',
    lifeNumber: String(lifeNumber),
    status: 'alive',
    cultivationMillis: '720000',
    realmLevel: 1,
    realmName: '炼气',
    ageMillis: '3600000',
    lifespanMillis: '36000000',
    speed: 10,
    senseRadius: 50,
    position: { xMilliunits: '100000', yMilliunits: '200000' },
    movementState: 'idle',
    movementMode: 'idle',
    movementSpeedSetting: 0,
    actualMovementSpeed: 0,
    stateVersion: String(stateVersion),
    ruleVersion: 1,
  }
}

beforeAll(() => {
  // crypto.randomUUID is used by idempotent commands; ensure it exists.
  if (!globalThis.crypto?.randomUUID) {
    ;(globalThis as unknown as { crypto: Crypto }).crypto = {
      ...(globalThis.crypto ?? ({} as Crypto)),
      randomUUID: () => '00000000-0000-4000-8000-000000000000',
    } as Crypto
  }
})

beforeEach(() => {
  vi.clearAllMocks()
  world.worldServiceMove.mockResolvedValue({})
  world.worldServiceMoveDirection.mockResolvedValue({})
  world.worldServiceStop.mockResolvedValue({})
  world.worldServiceScan.mockResolvedValue({})
  world.worldServiceTransferCultivation.mockResolvedValue({})
  world.worldServiceSeizeCultivation.mockResolvedValue({})
  world.worldServiceReincarnate.mockResolvedValue({})
  world.worldServiceRequestConversation.mockResolvedValue({})
  world.worldServiceRespondConversation.mockResolvedValue({})
  world.worldServiceSendConversationMessage.mockResolvedValue({})
  world.worldServiceCloseConversation.mockResolvedValue({})
})

// The commands under test and how to invoke each after state is remembered.
const commands: Array<{
  name: string
  invoke: () => Promise<unknown>
  fn: () => ReturnType<typeof vi.fn>
  idempotent: boolean
}> = [
  { name: 'move', invoke: () => api.move(10, 20), fn: () => world.worldServiceMove, idempotent: true },
  { name: 'moveDirection', invoke: () => api.moveDirection('up', 5), fn: () => world.worldServiceMoveDirection, idempotent: true },
  { name: 'stop', invoke: () => api.stop(), fn: () => world.worldServiceStop, idempotent: true },
  { name: 'scan', invoke: () => api.scan(), fn: () => world.worldServiceScan, idempotent: false },
  { name: 'transfer', invoke: () => api.transfer('other', 3), fn: () => world.worldServiceTransferCultivation, idempotent: true },
  { name: 'seize', invoke: () => api.seize('other'), fn: () => world.worldServiceSeizeCultivation, idempotent: true },
  { name: 'reincarnate', invoke: () => api.reincarnate(), fn: () => world.worldServiceReincarnate, idempotent: true },
  { name: 'requestConversation', invoke: () => api.requestConversation('other'), fn: () => world.worldServiceRequestConversation, idempotent: true },
  { name: 'respondConversation', invoke: () => api.respondConversation('conv-1', 'accept'), fn: () => world.worldServiceRespondConversation, idempotent: true },
  { name: 'sendMessage', invoke: () => api.sendMessage('conv-1', '你好'), fn: () => world.worldServiceSendConversationMessage, idempotent: true },
  { name: 'closeConversation', invoke: () => api.closeConversation('conv-1'), fn: () => world.worldServiceCloseConversation, idempotent: true },
]

describe('preservation: commands send the authoritative state_version / life_number (Req 3.1)', () => {
  it('every command echoes the last authoritative version + life number', async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.integer({ min: 0, max: 1_000_000 }),
        fc.integer({ min: 1, max: 10_000 }),
        async (stateVersion, lifeNumber) => {
          for (const command of commands) {
            vi.clearAllMocks()
            world.worldServiceGetState.mockResolvedValue(authoritativeState(stateVersion, lifeNumber))
            command.fn().mockResolvedValue({})

            // Authoritative push captured by rememberState.
            await api.state()
            await command.invoke()

            const body = command.fn().mock.calls.at(-1)?.[0] as Record<string, string> | undefined
            expect(body, `${command.name} was not called with a body`).toBeDefined()
            expect(body!.expectedStateVersion, `${command.name} state_version`).toBe(String(stateVersion))
            expect(body!.expectedLifeNumber, `${command.name} life_number`).toBe(String(lifeNumber))
            if (command.idempotent) {
              expect(body!.idempotencyKey, `${command.name} idempotencyKey`).toBeTruthy()
            }
          }
        },
      ),
      { numRuns: 25 },
    )
  })

  it('throws before issuing any command when no authoritative state was remembered', async () => {
    // A fresh module state has no remembered expectation; api.ts guards this so
    // an interpolated/absent value can never leak into a command.
    vi.resetModules()
    vi.doMock('./generated/services/WorldServiceService', () => ({ WorldServiceService: world }))
    vi.doMock('./generated/services/AuthServiceService', () => ({ AuthServiceService: {} }))
    const fresh = await import('./api')
    await expect(fresh.api.move(1, 2)).rejects.toThrow('请先刷新角色状态')
    expect(world.worldServiceMove).not.toHaveBeenCalled()
  })
})
