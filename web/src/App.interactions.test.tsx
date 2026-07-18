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
    actual_server_name: 'safeops-mcp-system',
    actual_server_version: '0.1.0',
    tools: [{ name: 'system.get_cpu_metrics', description: '读取 CPU', schema_hash: 'c'.repeat(64) }],
    tool_set_hash: 'd'.repeat(64),
    tools_changed: false,
    dependencies_checked: true,
    dependencies_healthy: true,
    dependency_checks: [{ name: '/proc', kind: 'path', available: true, resolved: '/proc', checked_at: '2026-07-16T01:02:05Z' }],
    health_history: [{ checked_at: '2026-07-16T01:02:05Z', status: 'HEALTHY', dependencies_healthy: true, duration_millis: 1 }],
    discovery_history: [{ discovered_at: '2026-07-16T01:02:05Z', server_name: 'safeops-mcp-system', server_version: '0.1.0', tool_set_hash: 'd'.repeat(64), tool_count: 1, tools_changed: false }],
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
  static onEmit: ((payload: unknown) => void) | null = null
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
    MockEventSource.onEmit?.(payload)
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

function setupAPI(options: { waitingApproval?: boolean; completedReply?: string; messageFailure?: boolean; sessionName?: string } = {}) {
  MockEventSource.instances = []
  MockEventSource.onEmit = null
  let task = options.waitingApproval ? waitingTask : completedTask
  let approval = baseApproval
  const session = {
    session_id: 'session-1',
    name: options.sessionName || '测试会话',
    archived: false,
    updated_at: '2026-07-16T01:02:03Z',
    messages: options.waitingApproval ? [{ message_id: 'message-1', role: 'assistant', content: '需要审批', task_id: 'task-1', created_at: '2026-07-16T01:02:03Z' }] : [],
  }
  MockEventSource.onEmit = payload => {
    const event = payload as { task_id?: string; state?: string }
    if (event.task_id !== 'task-new' || event.state !== 'COMPLETED') return
    task = { ...completedTask, task_id: 'task-new', state: 'COMPLETED' }
    session.messages.push({ message_id: 'message-reply', role: 'assistant', content: options.completedReply || '任务已经完成。', task_id: 'task-new', created_at: '2026-07-16T01:03:00Z' })
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/v1/sessions?')) return jsonResponse({ sessions: [session] })
    if (url === '/api/v1/sessions/session-1') return jsonResponse(session)
    if (url === '/api/v1/tasks/task-1') return jsonResponse(task)
    if (url === '/api/v1/tasks/task-new') return jsonResponse(task.task_id === 'task-new' ? task : { ...completedTask, task_id: 'task-new', state: 'INVESTIGATING' })
    if (url === '/api/v1/tasks/task-1/trace' || url === '/api/v1/tasks/task-new/trace') return jsonResponse(baseTrace)
    if (url === '/api/v1/approvals/approval-1') return jsonResponse(approval)
    if (url === '/api/v1/overview') return jsonResponse(baseOverview)
    if (url === '/api/v1/mcp/servers') return jsonResponse(baseServers)
    if (url === '/api/v1/approvals') return jsonResponse({ approvals: options.waitingApproval ? [approval] : [] })
    if (url.startsWith('/api/v1/tasks?')) return jsonResponse({ tasks: [task] })
    if (url === '/api/v1/sessions/session-1/messages' && init?.method === 'POST') {
      if (options.messageFailure) return jsonResponse({ error: '消息未被接受' }, 500)
      const content = JSON.parse(String(init.body)).content
      task = { ...completedTask, task_id: 'task-new', state: 'INVESTIGATING', objective: content }
      session.messages.push({ message_id: 'message-request', role: 'user', content, task_id: 'task-new', created_at: '2026-07-16T01:02:30Z' })
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
    const { container } = render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    const input = screen.getByLabelText('描述希望调查的系统问题') as HTMLTextAreaElement
    expect(input.value).toBe('')
    await user.clear(input)
    await user.type(input, '查看服务状态')
    await user.click(screen.getByRole('button', { name: '发送' }))

    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1))
    expect(MockEventSource.instances[0].url).toBe('/api/v1/tasks/task-new/events')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/sessions/session-1/messages', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ content: '查看服务状态' }),
    }))
    expect(input.value).toBe('')
    expect(container.querySelectorAll('.messages .message.user')).toHaveLength(1)
    expect(container.querySelector('.messages .message.user .markdown')?.textContent).toBe('查看服务状态')
    expect(screen.getByText('思考中')).toBeTruthy()

    MockEventSource.instances[0].emit('task.progress', { sequence: 1, type: 'task.progress', task_id: 'task-new', state: 'COMPLETED', message: '任务完成', timestamp: '2026-07-16T01:03:00Z' })
    await screen.findByText('任务完成')
    await waitFor(() => expect(screen.queryByText('思考中')).toBeNull())
  })

  it('reveals a newly completed LLM reply with a typewriter stream', async () => {
    const reply = '这是后端已经持久化的完整回答，现在通过前端逐字显示。'
    setupAPI({ completedReply: reply })
    const user = userEvent.setup()
    const { container } = render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.type(screen.getByLabelText('描述希望调查的系统问题'), '解释当前状态')
    await user.click(screen.getByRole('button', { name: '发送' }))
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1))
    expect(container.querySelectorAll('.messages .message.user')).toHaveLength(1)
    expect(container.querySelector('.messages .message.user .markdown')?.textContent).toBe('解释当前状态')
    expect(screen.getByText('思考中')).toBeTruthy()

    MockEventSource.instances[0].emit('task.progress', { sequence: 1, type: 'task.progress', task_id: 'task-new', state: 'COMPLETED', message: '任务完成', timestamp: '2026-07-16T01:03:00Z' })

    await waitFor(() => expect(container.querySelector('.typewriter-cursor')).not.toBeNull())
    expect(screen.queryByText('思考中')).toBeNull()
    const partial = container.querySelector('.streaming-markdown')?.textContent || ''
    expect(partial.length).toBeLessThan(reply.length)
    await waitFor(() => expect(container.querySelector('.typewriter-cursor')).toBeNull(), { timeout: 3000 })
    expect(container.querySelector('.message.assistant .markdown')?.textContent).toBe(reply)
  })

  it('rolls back the optimistic question when message creation fails', async () => {
    setupAPI({ messageFailure: true })
    const user = userEvent.setup()
    const { container } = render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    const input = screen.getByLabelText('描述希望调查的系统问题') as HTMLTextAreaElement
    await user.type(input, '这条消息应该回滚')
    await user.click(screen.getByRole('button', { name: '发送' }))

    await screen.findByText('消息未被接受')
    expect(input.value).toBe('这条消息应该回滚')
    expect(container.querySelector('.messages .message.user')).toBeNull()
    expect(screen.queryByText('思考中')).toBeNull()
    expect(MockEventSource.instances).toHaveLength(0)
  })

  it('uses the first question as the title for an unnamed session', async () => {
    setupAPI({ sessionName: '新会话' })
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '新会话' })
    await user.type(screen.getByLabelText('描述希望调查的系统问题'), '查看服务状态')
    await user.click(screen.getByRole('button', { name: '发送' }))

    await screen.findByRole('heading', { name: '查看服务状态' })
  })

  it('resolves a pending approval through the exact approval API', async () => {
    const fetchMock = setupAPI({ waitingApproval: true })
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: 'file.quarantine' })
    await user.click(screen.getByRole('button', { name: '批准精确动作' }))
    await screen.findByRole('dialog', { name: '批准精确绑定操作' })
    expect(screen.getByText('确认批准这个精确绑定的操作？审批不能授权其他目标。')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: '批准' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/approvals/approval-1/resolve', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ decision: 'APPROVE', reason: 'Web 控制台人工批准' }),
    })))
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
