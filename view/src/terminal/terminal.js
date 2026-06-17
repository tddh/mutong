import { Terminal } from 'https://cdn.jsdelivr.net/npm/@xterm/xterm@5.5.0/+esm'
import { createApp } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import '../styles/common.css'

// 独立挂载 NavBar 到 #vue-nav
createApp({ components: { NavBar } }).mount('#vue-nav')

let term = null
let ws = null

async function init() {
  const namespaceSel = document.getElementById('namespaceSelect')
  const podSel = document.getElementById('podSelect')
  const containerSel = document.getElementById('containerSelect')
  const shellSel = document.getElementById('shellSelect')
  const status = document.getElementById('status')

  const resizeSender = (w, h) => {
    try {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', width: w, height: h }))
      }
    } catch {
      // 发送 resize 消息失败时静默处理，不影响终端使用
    }
  }

  const urlParams = new URLSearchParams(location.search)
  const prefillNamespace = urlParams.get('namespace') || ''
  const prefillPod = urlParams.get('pod') || ''

  namespaceSel.innerHTML = ''
  let namespaces = ['default']
  try {
    const r = await fetch('/api/v1/terminal/namespaces')
    const d = await r.json()
    if (Array.isArray(d.namespaces) && d.namespaces.length > 0) {
      namespaces = d.namespaces
    }
  } catch (e) {
    console.error('Failed to fetch namespaces:', e)
  }

  namespaces.forEach((ns) => {
    const opt = document.createElement('option')
    opt.value = ns
    opt.text = ns
    namespaceSel.appendChild(opt)
  })

  namespaceSel.addEventListener('change', () => refreshPods())
  podSel.addEventListener('change', () => refreshContainers())

  if (prefillNamespace && namespaces.includes(prefillNamespace)) {
    namespaceSel.value = prefillNamespace
  } else {
    namespaceSel.value = 'default'
  }

  await refreshPods()

  if (prefillPod) {
    await new Promise((r) => setTimeout(r, 100))
    const podOptions = podSel.querySelectorAll('option')
    for (const opt of podOptions) {
      if (opt.value === prefillPod) {
        podSel.value = prefillPod
        await refreshContainers()
        break
      }
    }
  }

  term = new Terminal({ cols: 80, rows: 24, cursorBlink: true, theme: { background: '#1e1e1e' } })
  term.open(document.getElementById('terminal'))
  term.writeln('\x1b[32mWeb Terminal Ready\x1b[0m')
  term.writeln('Select namespace, pod, and container above to connect.')

  const connectBtn = document.getElementById('connectBtn')
  if (connectBtn) {
    connectBtn.addEventListener('click', () => connect())
  }

  term.onData((data) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(data)
    }
  })

  window.addEventListener('resize', () => {
    if (term) {
      const cols = Math.max(20, Math.floor(window.innerWidth / 9))
      const rows = Math.max(10, Math.floor(window.innerHeight / 18))
      term.resize(cols, rows)
      resizeSender(cols, rows)
    }
  })
}

async function refreshPods() {
  const namespace = document.getElementById('namespaceSelect').value || 'default'
  const podSel = document.getElementById('podSelect')
  const containerSel = document.getElementById('containerSelect')

  podSel.innerHTML = '<option value="">-- Select Pod --</option>'
  containerSel.innerHTML = '<option value="">-- Select Container --</option>'

  try {
    const r = await fetch(`/api/v1/terminal/pods?namespace=${encodeURIComponent(namespace)}`)
    const d = await r.json()
    ;(d.pods || []).forEach((p) => {
      const opt = document.createElement('option')
      opt.value = p
      opt.text = p
      podSel.appendChild(opt)
    })
  } catch (e) {
    console.error('Failed to fetch pods:', e)
  }
}

async function refreshContainers() {
  const namespace = document.getElementById('namespaceSelect').value || 'default'
  const pod = document.getElementById('podSelect').value
  const containerSel = document.getElementById('containerSelect')

  containerSel.innerHTML = '<option value="">-- Select Container --</option>'

  if (!pod) return

  try {
    const r = await fetch(
      `/api/v1/terminal/containers?namespace=${encodeURIComponent(namespace)}&pod=${encodeURIComponent(pod)}`,
    )
    const d = await r.json()
    ;(d.containers || []).forEach((c) => {
      const opt = document.createElement('option')
      opt.value = c
      opt.text = c
      containerSel.appendChild(opt)
    })
    ;(d.initContainers || []).forEach((c) => {
      const opt = document.createElement('option')
      opt.value = c
      opt.text = c + ' (init)'
      containerSel.appendChild(opt)
    })
    if (d.containers && d.containers.length > 0) {
      containerSel.value = d.containers[0]
    }
  } catch (e) {
    console.error('Failed to fetch containers:', e)
  }
}

function connect() {
  if (ws) {
    ws.close()
  }

  const namespace = document.getElementById('namespaceSelect').value || 'default'
  const pod = document.getElementById('podSelect').value
  const container = document.getElementById('containerSelect').value
  const shell = document.getElementById('shellSelect').value || '/bin/sh'
  const status = document.getElementById('status')

  if (!pod || !container) {
    term.writeln('\x1b[31mError: Please select pod and container first\x1b[0m')
    return
  }

  const cluster = 'default-cluster'
  const protocol = location.protocol === 'https:' ? 'wss' : 'ws'
  const url = `${protocol}://${location.host}/api/v1/terminal/ws?cluster=${encodeURIComponent(cluster)}&namespace=${encodeURIComponent(namespace)}&pod=${encodeURIComponent(pod)}&container=${encodeURIComponent(container)}&shell=${encodeURIComponent(shell)}`

  status.textContent = 'connecting...'
  term.writeln(`\x1b[33mConnecting to ${namespace}/${pod}/${container}...\x1b[0m`)

  ws = new WebSocket(url)
  ws.onopen = () => {
    status.textContent = 'connected'
    status.style.color = 'green'
    term.writeln('\x1b[32mConnected!\x1b[0m')
  }
  ws.onmessage = (ev) => {
    term.write(ev.data)
  }
  ws.onerror = (ev) => {
    status.textContent = 'error'
    status.style.color = 'red'
    term.writeln('\x1b[31mConnection error\x1b[0m')
  }
  ws.onclose = () => {
    status.textContent = 'disconnected'
    status.style.color = 'gray'
    term.writeln('\x1b[33mDisconnected\x1b[0m')
  }
}

init()
