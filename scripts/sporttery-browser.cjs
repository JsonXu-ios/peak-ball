// One-shot browser collection, no project server or persistent browser daemon.
// Single: node scripts/sporttery-browser.cjs --match-id 498235457
// Batch:  newline-separated match IDs on stdin with --batch; NDJSON on stdout.
const { spawn } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')
const readline = require('node:readline')

const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const origin = 'https://www.vipc.cn'
function matchID(raw) {
  if (!/^\d{1,20}$/.test(raw || '')) throw new Error('Invalid match ID')
  return raw
}

async function collectSporttery(ids, consume) {
  ids = [...new Set(ids.map(matchID))]
  if (!ids.length) return
  const candidates = [process.env.SPORTTERY_BROWSER_CHROME,
    path.join(process.env.PROGRAMFILES || 'C:/Program Files', 'Google/Chrome/Application/chrome.exe'),
    path.join(process.env['PROGRAMFILES(X86)'] || 'C:/Program Files (x86)', 'Microsoft/Edge/Application/msedge.exe'),
    '/usr/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser']
  const executable = candidates.find((candidate) => candidate && fs.existsSync(candidate))
  if (!executable) throw new Error('Chrome/Edge was not found; configure SPORTTERY_BROWSER_CHROME')
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'peak-ball-sporttery-'))
  const browser = spawn(executable, ['--headless=new', '--disable-gpu', '--disable-extensions', '--no-first-run',
    '--no-default-browser-check', '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=0',
    '--user-data-dir=' + profile, 'about:blank'], { windowsHide: true, stdio: 'ignore' })
  let socket, call, launchError
  browser.once('error', (error) => { launchError = error })
  const pending = new Map()
  try {
    const portFile = path.join(profile, 'DevToolsActivePort')
    for (let attempt = 0; attempt < 150 && !fs.existsSync(portFile); attempt++) {
      if (launchError) throw launchError
      if (browser.exitCode !== null) throw new Error('Browser exited before it was ready')
      await delay(100)
    }
    if (!fs.existsSync(portFile)) throw new Error('Browser startup timed out')
    const port = fs.readFileSync(portFile, 'utf8').split('\n')[0].trim()
    if (!/^\d+$/.test(port)) throw new Error('Invalid local browser port')
    const tabs = await fetch('http://127.0.0.1:' + port + '/json', { signal: AbortSignal.timeout(5000) }).then((r) => r.json())
    const tab = tabs.find((entry) => entry.type === 'page')
    if (!tab) throw new Error('Browser page is unavailable')
    socket = new WebSocket(tab.webSocketDebuggerUrl)
    await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
    let sequence = 0
    socket.onmessage = (event) => {
      const message = JSON.parse(event.data)
      const request = pending.get(message.id)
      if (!request) return
      pending.delete(message.id)
      clearTimeout(request.timer)
      message.error ? request.reject(new Error(message.error.message)) : request.resolve(message.result)
    }
    call = (method, params = {}) => new Promise((resolve, reject) => {
      const id = ++sequence
      const timer = setTimeout(() => { pending.delete(id); reject(new Error(method + ' timed out')) }, 30000)
      pending.set(id, { resolve, reject, timer })
      socket.send(JSON.stringify({ id, method, params }))
    })
    await call('Page.enable')
    await call('Runtime.enable')
    const firstURL = origin + '/i/match/jczq/sporttery/' + ids[0]
    const navigation = await call('Page.navigate', { url: firstURL })
    if (navigation.errorText) throw new Error('Source navigation failed: ' + navigation.errorText)
    let firstBody
    for (let attempt = 0; attempt < 200; attempt++) {
      const result = await call('Runtime.evaluate', { expression: 'JSON.stringify({url:location.href,ready:document.readyState,body:document.body?.innerText})', returnByValue: true })
      const page = JSON.parse(result.result.value)
      if (page.ready === 'complete' && page.url === firstURL && page.body) { firstBody = page.body; break }
      await delay(100)
    }
    if (!firstBody) throw new Error('Source page timed out')
    for (let index = 0; index < ids.length; index++) {
      let result
      if (index === 0) {
        let valid = false
        try { JSON.parse(firstBody); valid = true } catch {}
        result = { matchId: ids[index], status: valid ? 200 : 0, body: valid ? firstBody : '', error: valid ? undefined : 'Source did not return JSON' }
      } else {
        // Share only this isolated browser's ordinary same-origin network
        // session. No user cookies or browser profile are accessed.
        await delay(1000)
        const endpoint = '/i/match/jczq/sporttery/' + ids[index]
        try {
          const response = await call('Runtime.evaluate', {
            expression: `(async()=>{const r=await fetch(${JSON.stringify(endpoint)},{signal:AbortSignal.timeout(20000)});return {status:r.status,body:await r.text()}})()`,
            awaitPromise: true, returnByValue: true,
          })
          if (response.exceptionDetails || !response.result.value) throw new Error('Source browser request failed')
          result = { matchId: ids[index], ...response.result.value }
        } catch (error) { result = { matchId: ids[index], status: 0, body: '', error: error.message } }
      }
      if (result.body.length > 1024 * 1024) result = { matchId: ids[index], status: 0, body: '', error: 'Source response is too large' }
      await consume(result)
    }
  } finally {
    if (call && socket?.readyState === WebSocket.OPEN) { try { await call('Browser.close') } catch {} }
    for (const request of pending.values()) { clearTimeout(request.timer); request.reject(new Error('Browser closed')) }
    pending.clear()
    socket?.close()
    if (browser.exitCode === null) browser.kill()
    await delay(800)
    const resolved = path.resolve(profile)
    if (path.dirname(resolved) === path.resolve(os.tmpdir()) && path.basename(resolved).startsWith('peak-ball-sporttery-')) {
      fs.rmSync(resolved, { recursive: true, force: true, maxRetries: 8, retryDelay: 200 })
    }
  }
}

module.exports = { collectSporttery, matchID }
if (require.main === module) {
  ;(async () => {
    const batch = process.argv.includes('--batch')
    const ids = []
    if (batch) {
      for await (const line of readline.createInterface({ input: process.stdin })) if (line.trim()) ids.push(matchID(line.trim()))
    } else {
      const flag = process.argv.indexOf('--match-id')
      if (flag < 0) throw new Error('Use --match-id ID or --batch')
      ids.push(matchID(process.argv[flag + 1]))
    }
    await collectSporttery(ids, (result) => {
      if (batch) process.stdout.write(JSON.stringify(result) + '\n')
      else {
        if (result.status !== 200) throw new Error(result.error || 'Source HTTP ' + result.status)
        JSON.parse(result.body)
        process.stdout.write(result.body)
      }
    })
  })().catch((error) => { process.stderr.write(error.message + '\n'); process.exitCode = 1 })
}
