import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const baseURL = process.env.BASE_URL ?? 'http://localhost'
const chrome = process.env.CHROME_BIN ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const profile = await mkdtemp(join(tmpdir(), 'xiuxian-chrome-'))
const child = spawn(chrome, [
  '--headless=new',
  '--disable-gpu',
  '--no-first-run',
  '--no-default-browser-check',
  '--ignore-certificate-errors',
  '--remote-debugging-port=0',
  `--user-data-dir=${profile}`,
  'about:blank',
], { stdio: ['ignore', 'ignore', 'ignore'] })

const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds))

async function stopChrome() {
  if (child.exitCode === null) {
    child.kill('SIGTERM')
    await Promise.race([
      new Promise((resolve) => child.once('exit', resolve)),
      delay(2_000),
    ])
  }
  if (child.exitCode === null) child.kill('SIGKILL')
  for (let attempt = 0; attempt < 20; attempt += 1) {
    try {
      await rm(profile, { recursive: true, force: true })
      return
    } catch (error) {
      if (attempt === 19) {
        process.stderr.write(`warning: failed to remove Chrome profile: ${error.message}\n`)
        return
      }
      await delay(100)
    }
  }
}

async function devtoolsPort() {
  const file = join(profile, 'DevToolsActivePort')
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      const [port] = (await readFile(file, 'utf8')).trim().split('\n')
      return Number(port)
    } catch {
      await delay(100)
    }
  }
  throw new Error('Chrome DevTools port was not created')
}

class CDP {
  constructor(url) {
    this.socket = new WebSocket(url)
    this.nextID = 1
    this.pending = new Map()
  }

  async open() {
    await new Promise((resolve, reject) => {
      this.socket.addEventListener('open', resolve, { once: true })
      this.socket.addEventListener('error', reject, { once: true })
    })
    this.socket.addEventListener('message', (event) => {
      const message = JSON.parse(String(event.data))
      if (!message.id) return
      const pending = this.pending.get(message.id)
      if (!pending) return
      this.pending.delete(message.id)
      if (message.error) pending.reject(new Error(message.error.message))
      else pending.resolve(message.result)
    })
  }

  send(method, params = {}) {
    const id = this.nextID
    this.nextID += 1
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
      this.socket.send(JSON.stringify({ id, method, params }))
    })
  }

  async evaluate(expression) {
    const result = await this.send('Runtime.evaluate', {
      expression,
      awaitPromise: true,
      returnByValue: true,
      userGesture: true,
    })
    if (result.exceptionDetails) {
      throw new Error(result.exceptionDetails.text)
    }
    return result.result.value
  }

  close() {
    this.socket.close()
  }
}

async function waitFor(cdp, expression, description, timeout = 15_000) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    if (await cdp.evaluate(expression)) return
    await delay(100)
  }
  throw new Error(`Timed out waiting for ${description}`)
}

const quote = (value) => JSON.stringify(value)

async function clickText(cdp, text) {
  const clicked = await cdp.evaluate(`(() => {
    const element = [...document.querySelectorAll('button')].find((item) => item.textContent.trim() === ${quote(text)})
    if (!element) return false
    element.click()
    return true
  })()`)
  if (!clicked) throw new Error(`button not found: ${text}`)
}

async function fillLabel(cdp, label, value) {
  const changed = await cdp.evaluate(`(() => {
    const labelElement = [...document.querySelectorAll('label')].find((item) => item.textContent.trim().startsWith(${quote(label)}))
    const input = labelElement?.querySelector('input')
    if (!input) return false
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
    setter.call(input, ${quote(value)})
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
    return true
  })()`)
  if (!changed) throw new Error(`input not found: ${label}`)
}

async function fillFormInput(cdp, buttonText, label, value) {
  const changed = await cdp.evaluate(`(() => {
    const button = [...document.querySelectorAll('button')].find((item) => item.textContent.trim() === ${quote(buttonText)})
    const form = button?.closest('form')
    const labelElement = [...(form?.querySelectorAll('label') ?? [])].find((item) => item.textContent.trim().startsWith(${quote(label)}))
    const input = labelElement?.querySelector('input')
    if (!input) return false
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
    setter.call(input, ${quote(value)})
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
    return true
  })()`)
  if (!changed) throw new Error(`input not found for ${buttonText}: ${label}`)
}

class RoleClient {
  constructor(base) {
    this.base = base
    this.cookie = ''
    this.state = null
  }

  async request(method, path, body) {
    const headers = {}
    if (this.cookie) headers.Cookie = this.cookie
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    const response = await fetch(this.base + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const setCookie = response.headers.getSetCookie?.()[0] ?? response.headers.get('set-cookie')
    if (setCookie) this.cookie = setCookie.split(';', 1)[0]
    const text = await response.text()
    const payload = text ? JSON.parse(text) : {}
    if (!response.ok) throw new Error(`${method} ${path} failed: ${response.status} ${text}`)
    return payload
  }

  async register(suffix) {
    const stamp = Date.now()
    const response = await this.request('POST', '/registrations', {
      account: `browser-helper-${suffix}-${stamp}`,
      password: `browser helper password ${suffix}`,
      roleName: `协作${suffix}${stamp}`,
    })
    this.state = response.state
    if (!this.state?.id) throw new Error(`helper registration response missing role ID: ${JSON.stringify(response)}`)
  }

  async refresh() {
    this.state = await this.request('GET', '/state')
    return this.state
  }

  async waitConversation(requesterID) {
    for (let attempt = 0; attempt < 100; attempt += 1) {
      const response = await this.request('GET', '/conversations')
      const conversation = (response.conversations ?? []).find((item) => item.requesterId === requesterID && item.status === 'requested')
      if (conversation) return conversation
      await delay(100)
    }
    throw new Error('helper did not receive conversation request')
  }

  async accept(conversationID) {
    const state = await this.refresh()
    await this.request('POST', '/conversation-responses', {
      conversationId: conversationID,
      action: 'accept',
      idempotencyKey: `browser-accept-${conversationID}`,
      expectedLifeNumber: state.lifeNumber,
      expectedStateVersion: state.stateVersion,
    })
  }

  async waitMessage(conversationID, content) {
    for (let attempt = 0; attempt < 100; attempt += 1) {
      const response = await this.request('GET', '/conversations')
      const conversation = (response.conversations ?? []).find((item) => item.id === conversationID)
      if ((conversation?.messages ?? []).some((message) => message.content === content)) return
      await delay(100)
    }
    throw new Error('helper did not receive conversation message')
  }

  async reincarnateIfNeeded() {
    const state = await this.refresh()
    if (state.status !== 'pending_reincarnation') return
    this.state = await this.request('POST', '/reincarnations', {
      random: true,
      idempotencyKey: `browser-helper-rebirth-${state.lifeNumber}`,
      expectedLifeNumber: state.lifeNumber,
      expectedStateVersion: state.stateVersion,
    })
  }
}

async function runJourney(cdp, viewport, suffix, helper) {
  await cdp.send('Emulation.setDeviceMetricsOverride', {
    width: viewport.width,
    height: viewport.height,
    deviceScaleFactor: 1,
    mobile: viewport.mobile,
  })
  await cdp.send('Network.clearBrowserCookies')
  await cdp.send('Page.navigate', { url: baseURL })
  await waitFor(cdp, `document.body?.innerText.includes('创建角色')`, 'auth screen')

  const account = `browser-${suffix}-${Date.now()}`
  const password = `browser smoke password ${suffix}`
  const roleName = `验收${suffix}${Date.now()}`
  await clickText(cdp, '创建角色')
  await fillLabel(cdp, '账号', account)
  await fillLabel(cdp, '密码', password)
  await fillLabel(cdp, '永久角色名', roleName)
  await clickText(cdp, '创建并进入')
  await waitFor(cdp, `document.querySelector('h1')?.textContent === ${quote(roleName)}`, 'registered role')

  await clickText(cdp, '退出登录')
  await waitFor(cdp, `document.body.innerText.includes('进入世界')`, 'login screen')
  await fillLabel(cdp, '账号', account)
  await fillLabel(cdp, '密码', password)
  await clickText(cdp, '进入世界')
  await waitFor(cdp, `document.querySelector('h1')?.textContent === ${quote(roleName)}`, 'logged-in role')

  const accessible = await cdp.evaluate(`(() => ({
    noOverflow: document.documentElement.scrollWidth <= window.innerWidth + 1,
    inputsLabeled: [...document.querySelectorAll('input')].every((input) => input.labels?.length || input.getAttribute('aria-label')),
    buttonsNamed: [...document.querySelectorAll('button')].every((button) => button.textContent.trim() || button.getAttribute('aria-label')),
  }))()`)
  if (!accessible.noOverflow || !accessible.inputsLabeled || !accessible.buttonsNamed) {
    throw new Error(`accessibility/layout assertion failed: ${JSON.stringify(accessible)}`)
  }

  await fillFormInput(cdp, '移动', 'X', '0.001')
  await fillFormInput(cdp, '移动', 'Y', '0')
  await clickText(cdp, '移动')
  await waitFor(cdp, `document.body.innerText.includes('轨迹已更新')`, 'movement accepted')
  const arrived = await cdp.evaluate(`fetch('/test/clock/advance?milliseconds=2', { method: 'POST' }).then((response) => response.status)`)
  if (arrived !== 200) throw new Error(`movement clock status = ${arrived}`)
  await clickText(cdp, '刷新')
  await waitFor(cdp, `document.body.innerText.includes('(0.001, 0)')`, 'movement arrival')

  await clickText(cdp, '主动扫描')
  await waitFor(cdp, `document.body.innerText.includes('神识所见')`, 'scan result')

  const browserState = await cdp.evaluate(`fetch('/state').then((response) => response.json())`)
  await fillFormInput(cdp, '请求交谈', '目标 ID', helper.state.id)
  await clickText(cdp, '请求交谈')
  await waitFor(cdp, `document.body.innerText.includes('请求已送达') || Boolean(document.querySelector('.notice.error'))`, 'conversation request result')
  const requestError = await cdp.evaluate(`document.querySelector('.notice.error')?.textContent ?? ''`)
  if (requestError) throw new Error(`conversation request failed: ${requestError}`)
  const conversation = await helper.waitConversation(browserState.id)
  await helper.accept(conversation.id)
  await clickText(cdp, '刷新')
  await waitFor(cdp, `document.body.innerText.includes('accepted')`, 'accepted conversation')
  await delay(2_200)
  const message = `自动化问候-${suffix}`
  await fillFormInput(cdp, '发送', '玩家消息（不可信内容）', message)
  await clickText(cdp, '发送')
  await delay(300)
  const messageError = await cdp.evaluate(`document.querySelector('.notice.error')?.textContent ?? ''`)
  if (messageError) throw new Error(`conversation message failed: ${messageError}`)
  await helper.waitMessage(conversation.id, message)

  await delay(2_200)
  await clickText(cdp, '轮换 Key')
  await waitFor(cdp, `document.querySelector('output.secret')?.textContent.startsWith('xiu_') || Boolean(document.querySelector('.notice.error'))`, 'MCP key result')
  const keyError = await cdp.evaluate(`document.querySelector('.notice.error')?.textContent ?? ''`)
  if (keyError) throw new Error(`MCP key rotation failed: ${keyError}`)
  await delay(1_100)
  await clickText(cdp, '撤销 Key')
  await waitFor(cdp, `!document.querySelector('output.secret') && document.body.innerText.includes('Key 已撤销')`, 'MCP key revocation')

  const advanced = await cdp.evaluate(`fetch('/test/clock/advance?milliseconds=28800000', { method: 'POST' }).then((response) => response.status)`)
  if (advanced !== 200) throw new Error(`test clock status = ${advanced}`)
  await clickText(cdp, '刷新')
  await waitFor(cdp, `document.body.innerText.includes('本世已终，等待转世') && document.body.innerText.includes('转世')`, 'pending reincarnation')
  await clickText(cdp, '随机转世')
  await waitFor(cdp, `document.body.innerText.includes('第 2 世')`, 'second life')
}

let cdp
try {
  const port = await devtoolsPort()
  const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((response) => response.json())
  const page = targets.find((target) => target.type === 'page')
  if (!page) throw new Error('Chrome page target not found')
  cdp = new CDP(page.webSocketDebuggerUrl)
  await cdp.open()
  await cdp.send('Page.enable')
  await cdp.send('Runtime.enable')
  await cdp.send('Network.enable')
  const helper = new RoleClient(baseURL)
  await helper.register('目标')
  await runJourney(cdp, { width: 1280, height: 900, mobile: false }, '桌面', helper)
  await helper.reincarnateIfNeeded()
  await runJourney(cdp, { width: 390, height: 844, mobile: true }, '移动', helper)
  process.stdout.write('browser smoke passed at desktop and mobile widths\n')
} finally {
  cdp?.close()
  await stopChrome()
}
