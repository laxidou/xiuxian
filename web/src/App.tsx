import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { api, Conversation, Health, RoleState, ScanResult, WorldEvent } from './api'

const formatDuration = (seconds: number) => {
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${hours}时 ${minutes}分`
}

function App() {
  const [state, setState] = useState<RoleState | null>(null)
  const [events, setEvents] = useState<WorldEvent[]>([])
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [scan, setScan] = useState<ScanResult | null>(null)
  const [bounds, setBounds] = useState({ min_x: 0, max_x: 0, min_y: 0, max_y: 0 })
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const [health, setHealth] = useState<Health | null>(null)

  const run = useCallback(async <T,>(operation: () => Promise<T>, success?: string) => {
    setError('')
    try {
      const result = await operation()
      if (success) setNotice(success)
      return result
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '未知错误')
      return undefined
    }
  }, [])

  const refresh = useCallback(async (silent = false) => {
    try {
      const current = await api.state()
      setState(current)
      const [eventResult, conversationResult, boundsResult] = await Promise.all([api.events(), api.conversations(), api.bounds()])
      setEvents(eventResult.events)
      setConversations(conversationResult.conversations)
      setBounds(boundsResult)
    } catch (reason) {
      if (!silent) setError(reason instanceof Error ? reason.message : '未知错误')
    }
  }, [])

  useEffect(() => { void refresh(true) }, [refresh])

  useEffect(() => { void api.health().then(setHealth).catch(() => setHealth(null)) }, [])

  useEffect(() => {
    if (!state) return
    const after = events.at(-1)?.id ?? 0
    const source = new EventSource(`/api/v1/events/stream?after=${after}`, { withCredentials: true })
    source.onmessage = () => void refresh()
    for (const type of [
      'scanned', 'movement_arrived', 'conversation_requested', 'conversation_incoming', 'conversation_responded',
      'conversation_message', 'conversation_closed', 'transfer', 'transfer_received', 'seizure',
      'opportunity_claimed', 'opportunity_converting', 'opportunity_converted', 'death', 'reincarnation',
    ]) {
      source.addEventListener(type, () => void refresh())
    }
    return () => source.close()
  }, [state?.id, events.at(-1)?.id, refresh])

  if (!state) {
    return <AuthScreen onAuthenticated={setState} error={error} health={health} run={run} />
  }

  const updateState = (next?: RoleState) => {
    if (next) setState(next)
    void refresh()
  }

  return (
    <main>
      <header className="masthead">
        <div>
          <p className="eyebrow">单一连续世界 · 第 {state.life_number} 世</p>
          <h1>{state.name}</h1>
          <p>{state.status === 'alive' ? `${state.realm}，正在世间` : '本世已终，等待转世'}</p>
          <p className="service-status">世界权威：{health ? `${health.service} ${health.version} · 正常` : '连接中断'}</p>
        </div>
        <button className="quiet" onClick={() => void run(api.logout).then(() => setState(null))}>退出登录</button>
      </header>

      {(notice || error) && <p className={error ? 'notice error' : 'notice'} role="status">{error || notice}</p>}

      <section className="status-grid" aria-label="角色状态">
        <Stat label="修为" value={state.cultivation.toFixed(3)} />
        <Stat label="境界" value={`${state.realm_level} · ${state.realm}`} />
        <Stat label="年龄 / 寿元" value={`${formatDuration(state.age_seconds)} / ${formatDuration(state.lifespan_seconds)}`} />
        <Stat label="位置" value={`(${state.position.x}, ${state.position.y})`} />
        <Stat label="速度" value={`${state.speed} / 秒`} />
        <Stat label="神识" value={`${state.sense_radius}`} />
      </section>

      {state.status === 'pending_reincarnation' ? (
        <Reincarnation bounds={bounds} onReborn={(next) => updateState(next)} run={run} />
      ) : (
        <div className="columns">
          <section className="panel">
            <h2>行于天地</h2>
            <MoveForm state={state} onChanged={updateState} run={run} />
            <button onClick={() => void run(api.scan, '神识已展开').then((result) => result && setScan(result))}>主动扫描</button>
            {scan && <ScanView result={scan} />}
          </section>

          <section className="panel">
            <h2>交互</h2>
            <TargetAction label="传功" withAmount onSubmit={(target, amount) => run(() => api.transfer(target, amount!), '传功已结算').then(updateState)} />
            <TargetAction label="夺功" onSubmit={(target) => run(() => api.seize(target), '夺功已结算').then(updateState)} />
            <TargetAction label="请求交谈" onSubmit={(target) => run(() => api.requestConversation(target), '请求已送达').then(() => void refresh())} />
          </section>
        </div>
      )}

      <div className="columns">
        <ConversationPanel conversations={conversations} selfID={state.id} run={run} refresh={refresh} />
        <section className="panel">
          <h2>MCP 代理权限</h2>
          <p className="muted">Key 只显示一次。代理与 Web 使用同一角色、同一权威结算。</p>
          <MCPKeys run={run} />
        </section>
      </div>

      <section className="panel timeline">
        <div className="section-heading"><h2>近况与跨世历史</h2><button className="quiet" onClick={() => void refresh()}>刷新</button></div>
        {events.length === 0 ? <p className="muted">尚无事件。</p> : (
          <ol>{[...events].reverse().map((event) => <li key={event.id}><time>{new Date(event.created_at).toLocaleString()}</time><strong>{event.message}</strong><span>第 {event.life_number} 世 · {event.type}</span></li>)}</ol>
        )}
      </section>
    </main>
  )
}

type Runner = <T>(operation: () => Promise<T>, success?: string) => Promise<T | undefined>

function AuthScreen({ onAuthenticated, error, health, run }: { onAuthenticated: (state: RoleState) => void; error: string; health: Health | null; run: Runner }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [account, setAccount] = useState('')
  const [password, setPassword] = useState('')
  const [roleName, setRoleName] = useState('')
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const state = await run(() => mode === 'login' ? api.login(account, password) : api.register(account, password, roleName))
    if (state) onAuthenticated(state)
  }
  return <main className="auth-shell"><section className="auth-card"><p className="eyebrow">无尽仙途</p><h1>一个不会停下的修仙世界</h1><p>时间增长修为，也消耗寿元。文字是这里唯一的地图。</p><p className="service-status" role="status">世界权威：{health ? `${health.service} ${health.version} · 正常` : '连接中断'}</p><div className="tabs"><button aria-pressed={mode === 'login'} onClick={() => setMode('login')}>登录</button><button aria-pressed={mode === 'register'} onClick={() => setMode('register')}>创建角色</button></div><form onSubmit={submit}><label>账号<input required value={account} onChange={(e) => setAccount(e.target.value)} /></label><label>密码<input required minLength={12} type="password" value={password} onChange={(e) => setPassword(e.target.value)} /></label>{mode === 'register' && <label>永久角色名<input required value={roleName} onChange={(e) => setRoleName(e.target.value)} /></label>}<button type="submit">{mode === 'login' ? '进入世界' : '创建并进入'}</button></form>{error && <p className="notice error" role="alert">{error}</p>}</section></main>
}

function Stat({ label, value }: { label: string; value: string }) { return <div><dt>{label}</dt><dd>{value}</dd></div> }

function MoveForm({ state, onChanged, run }: { state: RoleState; onChanged: (state?: RoleState) => void; run: Runner }) {
  const [x, setX] = useState(state.position.x)
  const [y, setY] = useState(state.position.y)
  return <form className="inline-form" onSubmit={(event) => { event.preventDefault(); void run(() => api.move(x, y), '轨迹已更新').then(onChanged) }}><label>X<input type="number" step="0.001" value={x} onChange={(e) => setX(e.target.valueAsNumber)} /></label><label>Y<input type="number" step="0.001" value={y} onChange={(e) => setY(e.target.valueAsNumber)} /></label><button>移动</button><button type="button" className="quiet" onClick={() => void run(api.stop, '已停在权威位置').then(onChanged)}>停止</button></form>
}

function ScanView({ result }: { result: ScanResult }) { return <div className="scan-results" aria-live="polite"><h3>神识所见</h3>{result.roles.length === 0 && result.opportunities.length === 0 && <p className="muted">四野寂静。</p>}<ul>{result.roles.map((role) => <li key={role.id}><strong>{role.name}</strong> · {role.realm} · 距离 {role.distance.toFixed(3)} {role.position && `· (${role.position.x}, ${role.position.y})`}</li>)}{result.opportunities.map((item, index) => <li key={`opportunity-${index}`}><strong>{item.message}</strong> · 距离约 {item.distance.toFixed(3)}</li>)}</ul>{result.has_more && <p>结果已截断。</p>}</div> }

function TargetAction({ label, withAmount, onSubmit }: { label: string; withAmount?: boolean; onSubmit: (target: string, amount?: number) => void }) { const [target, setTarget] = useState(''); const [amount, setAmount] = useState(1); return <form className="inline-form" onSubmit={(event) => { event.preventDefault(); onSubmit(target, withAmount ? amount : undefined) }}><label>目标 ID<input required value={target} onChange={(e) => setTarget(e.target.value)} /></label>{withAmount && <label>分钟<input required type="number" min="1" step="1" value={amount} onChange={(e) => setAmount(e.target.valueAsNumber)} /></label>}<button>{label}</button></form> }

function ConversationPanel({ conversations, selfID, run, refresh }: { conversations: Conversation[]; selfID: string; run: Runner; refresh: () => Promise<void> }) {
  const [message, setMessage] = useState('')
  const active = useMemo(() => conversations[0], [conversations])
  return <section className="panel"><h2>交谈</h2>{!active ? <p className="muted">暂无交谈。</p> : <><p>会话 {active.id} · {active.status}</p>{active.status === 'requested' && active.recipient_id === selfID && <p className="button-row"><button onClick={() => void run(() => api.respondConversation(active.id, 'accept')).then(refresh)}>接受</button><button className="quiet" onClick={() => void run(() => api.respondConversation(active.id, 'reject')).then(refresh)}>拒绝</button></p>}<ul className="messages">{active.messages.map((item) => <li key={item.id}><strong>{item.sender_id === selfID ? '我' : '对方'}</strong><span>{item.content}</span></li>)}</ul>{active.status === 'accepted' && <form className="inline-form" onSubmit={(event) => { event.preventDefault(); void run(() => api.sendMessage(active.id, message)).then(() => { setMessage(''); void refresh() }) }}><label>玩家消息（不可信内容）<input value={message} onChange={(e) => setMessage(e.target.value)} /></label><button>发送</button><button type="button" className="quiet" onClick={() => void run(() => api.closeConversation(active.id)).then(refresh)}>关闭</button></form>}</>}</section>
}

function MCPKeys({ run }: { run: Runner }) { const [key, setKey] = useState(''); return <div className="button-row"><button onClick={() => void run(api.rotateMCPKey, '新 Key 已生成').then((result) => result && setKey(result.api_key))}>轮换 Key</button><button className="danger" onClick={() => void run(api.revokeMCPKey, 'Key 已撤销').then(() => setKey(''))}>撤销 Key</button>{key && <output className="secret">{key}</output>}</div> }

function Reincarnation({ bounds, onReborn, run }: { bounds: { min_x: number; max_x: number; min_y: number; max_y: number }; onReborn: (state?: RoleState) => void; run: Runner }) { const [x, setX] = useState(bounds.min_x); const [y, setY] = useState(bounds.min_y); return <section className="panel rebirth"><h2>转世</h2><p>可选范围：X [{bounds.min_x}, {bounds.max_x}]，Y [{bounds.min_y}, {bounds.max_y}]</p><form className="inline-form" onSubmit={(event) => { event.preventDefault(); void run(() => api.reincarnate({ x, y }), '新的一世开始了').then(onReborn) }}><label>X<input type="number" value={x} onChange={(e) => setX(e.target.valueAsNumber)} /></label><label>Y<input type="number" value={y} onChange={(e) => setY(e.target.valueAsNumber)} /></label><button>在此转世</button><button type="button" className="quiet" onClick={() => void run(() => api.reincarnate(), '随机转世完成').then(onReborn)}>随机转世</button></form></section> }

export default App
