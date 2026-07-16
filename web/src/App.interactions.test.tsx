import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import App from './App'

type Listener = (event: MessageEvent) => void

const baseTrace = {
  integrity: 'VALID',
  events: [{ sequence: 1, timestamp: '2026-07-16T01:02:03Z', type: 'TASK_CREATED', event_hash: 'a'.repeat(64) }],
}

const baseOverview = {
  mcp: { total: 8, healthy: 8, tools: 39 },
  sessions: { active: 1, archived: 0 },
  tasks: { COMPLETED: 0, FAILED: 0, CANCELLED: 0, WAITING_APPROVAL: 0 },
  approvals: { PENDING: 0 },
  generated_at: '2026-07-16T01:02:05Z',
}

const baseServers = {
  servers: [{
    manifest: { id: 'system', display_name: '系统感知', version: '0.1.0', description: '真实系统状态', enabled: true, capabilities: ['system', 'read_only'] },
    status: 'HEALTHY',
    tools: [{ name: 'system.get_cpu_metrics', description: '读取 CPU', schema_hash: 'c'.repeat(64) }],
    tool_set_hash: 'd'.repeat(64),
    tools_changed: false,
    last_checked: '2026-07-16T01:02:05Z',
  }],
}

const completedTask = {
  task_id: 'task-1',
  session_id: 'session-1',
  objective: '查看 CPU',
  intent_type: 'READ_SYSTEM',
  state: 'COMPLETED',
  plan: [{ step_id: 'step-1', description: '读取 CPU', tool: 'system.get_cpu_metrics', state: 'COMPLETED' }],
  current_step: 1,
  findings: ['CPU 使用率正常'],
  evidence_refs: ['trace:1'],
  updated_at: '2026-07-16T01:02:03Z',
}

const waitingTask = {
  ...completedTask,
  state: 'WAITING_APPROVAL',
  pending_approval_id: 'approval-1',
  pending_action: {
    proposal: { tool: 'file.quarantine', target: { type: 'file', id: '/var/lib/safeops/lab/a.log' }, batch_size: 1, reversible: true, rollback_strategy: 'restore quarantine' },
    target_snapshot: { type: 'file', id: '/var/lib/safeops/lab/a.log', canonical_path: '/var/lib/safeops/lab/a.log' },
    risk: { risk_level: 'L1', risk_score: 40, risk_factors: ['LAB_ALLOWLIST'], risk_reason: '受控 Lab 文件隔离' },
    expires_at: '2026-07-16T02:02:03Z',
  },
}

const baseApproval = {
  approval_id: 'approval-1',
  status: 'PENDING',
  expires_at: '2026-07-16T02:02:03Z',
  binding: { task_id: 'task-1', tool: 'file.quarantine', risk_level: 'L1', policy_version: 'policy-v1', target_snapshot_digest: 'f'.repeat(64) },
}

class MockEventSource {
  static instances: MockEventSource[] = []
  listeners: Record<string, Listener[]> = {}
  onerror: ((event: Event) => void) | null = null
  closed = false
  readonly url: string

  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    const fn = typeof listener === 'function' ? listener as Listener : (event: MessageEvent) => listener.handleEvent(event)
    this.listeners[type] = [...(this.listeners[type] || []), fn]
  }

  close() {
    this.closed = true
  }

  emit(type: string, payload: unknown) {
    for (const listener of this.listeners[type] || []) {
      listener(new MessageEvent(type, { data: JSON.stringify(payload) }))
    }
  }
}

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response
}

function setupAPI(options: { waitingApproval?: boolean } = {}) {
  MockEventSource.instances = []
  let task = options.waitingApproval ? waitingTask : completedTask
  let approval = baseApproval
  const session = {
    session_id: 'session-1',
    name: '测试会话',
    archived: false,
    updated_at: '2026-07-16T01:02:03Z',
    messages: options.waitingApproval ? [{ message_id: 'message-1', role: 'assistant', content: '需要审批', task_id: 'task-1', created_at: '2026-07-16T01:02:03Z' }] : [],
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/v1/sessions?')) return jsonResponse({ sessions: [session] })
    if (url === '/api/v1/sessions/session-1') return jsonResponse(session)
    if (url === '/api/v1/tasks/task-1') return jsonResponse(task)
    if (url === '/api/v1/tasks/task-new') return jsonResponse({ ...completedTask, task_id: 'task-new', state: 'INVESTIGATING' })
    if (url === '/api/v1/tasks/task-1/trace' || url === '/api/v1/tasks/task-new/trace') return jsonResponse(baseTrace)
    if (url === '/api/v1/approvals/approval-1') return jsonResponse(approval)
    if (url === '/api/v1/overview') return jsonResponse(baseOverview)
    if (url === '/api/v1/mcp/servers') return jsonResponse(baseServers)
    if (url === '/api/v1/approvals') return jsonResponse({ approvals: options.waitingApproval ? [approval] : [] })
    if (url.startsWith('/api/v1/tasks?')) return jsonResponse({ tasks: [task] })
    if (url === '/api/v1/sessions/session-1/messages' && init?.method === 'POST') {
      task = { ...completedTask, task_id: 'task-new', state: 'INVESTIGATING', objective: JSON.parse(String(init.body)).content }
      return jsonResponse({ task_id: 'task-new', session_id: 'session-1', state: 'NEW', events_url: '/api/v1/tasks/task-new/events' }, 202)
    }
    if (url === '/api/v1/approvals/approval-1/resolve' && init?.method === 'POST') {
      approval = { ...approval, status: 'APPROVED' }
      task = { ...completedTask, state: 'COMPLETED' }
      return jsonResponse({ approval, task })
    }
    if (url.startsWith('/api/v1/mcp/servers/system/') && init?.method === 'POST') {
      return jsonResponse(baseServers.servers[0])
    }
    return jsonResponse({ error: `unhandled test route: ${url}` }, 404)
  })
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('EventSource', MockEventSource)
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  vi.spyOn(window, 'prompt').mockReturnValue(null)
  return fetchMock
}

describe('SafeOps UI interactions', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('submits a user request and follows the returned task event stream', async () => {
    const fetchMock = setupAPI()
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    const input = screen.getByLabelText('描述希望调查的系统问题')
    await user.clear(input)
    await user.type(input, '查看服务状态')
    await user.click(screen.getByRole('button', { name: '发送' }))

    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1))
    expect(MockEventSource.instances[0].url).toBe('/api/v1/tasks/task-new/events')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/sessions/session-1/messages', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ content: '查看服务状态' }),
    }))

    MockEventSource.instances[0].emit('task.progress', { sequence: 1, type: 'task.progress', task_id: 'task-new', state: 'COMPLETED', message: '任务完成', timestamp: '2026-07-16T01:03:00Z' })
    await screen.findByText('任务完成')
  })

  it('resolves a pending approval through the exact approval API', async () => {
    const fetchMock = setupAPI({ waitingApproval: true })
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: 'file.quarantine' })
    await user.click(screen.getByRole('button', { name: '批准精确动作' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/approvals/approval-1/resolve', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ decision: 'APPROVE', reason: 'Web 控制台人工批准' }),
    })))
    expect(window.confirm).toHaveBeenCalledWith('确认批准这个精确绑定的操作？审批不能授权其他目标。')
    await screen.findByText('批准结果已持久化，任务自动恢复')
  })

  it('runs MCP server management actions from the tool center', async () => {
    const fetchMock = setupAPI()
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByRole('button', { name: '工具中心' }))
    await screen.findByRole('heading', { name: 'MCP 插件与工具' })
    await user.click(screen.getByRole('button', { name: '健康检查' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/mcp/servers/system/health', expect.objectContaining({
      method: 'POST',
      body: '{}',
    })))
  })
})
