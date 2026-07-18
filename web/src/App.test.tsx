import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import axe from 'axe-core'
import App from './App'

const task = {
  task_id: 'task-1',
  session_id: 'session-1',
  objective: '检查受控 Web 服务',
  intent_type: 'PORT_RECOVERY',
  state: 'COMPLETED',
  plan: [{ step_id: 'step-1', description: '读取服务状态', tool: 'service.get_status', state: 'COMPLETED' }],
  current_step: 1,
  findings: ['端口 18080 由受控进程占用'],
  evidence_refs: ['trace:1'],
  updated_at: '2026-07-16T01:02:03Z',
}

const session = {
  session_id: 'session-1',
  name: '测试会话',
  archived: false,
  updated_at: '2026-07-16T01:02:03Z',
  messages: [{
    message_id: 'message-1',
    role: 'assistant',
    content: '<img src=x onerror=alert(1)>\n- 已安全转义',
    task_id: 'task-1',
    created_at: '2026-07-16T01:02:03Z',
  }],
}

const trace = {
  integrity: 'VALID',
  events: [
    { sequence: 1, timestamp: '2026-07-16T01:02:03Z', type: 'RCA_RESULT', event_hash: 'a'.repeat(64), data: { confidence: 0.9 } },
    { sequence: 2, timestamp: '2026-07-16T01:02:04Z', type: 'VERIFICATION', event_hash: 'b'.repeat(64), prev_hash: 'a'.repeat(64), data: { service: 'active' } },
  ],
}

const overview = {
  mcp: { total: 8, healthy: 8, tools: 39 },
  sessions: { active: 1, archived: 0 },
  tasks: { COMPLETED: 1, FAILED: 0, CANCELLED: 0, WAITING_APPROVAL: 0 },
  approvals: { PENDING: 0 },
  generated_at: '2026-07-16T01:02:05Z',
}

const servers = {
  servers: [{
    manifest: { id: 'system', display_name: '系统感知', version: '0.1.0', description: '真实系统状态', enabled: true, capabilities: ['system'] },
    status: 'HEALTHY',
    tools: [{ name: 'system.get_overview', description: '读取系统概览', schema_hash: 'c'.repeat(64) }],
    tool_set_hash: 'd'.repeat(64),
    tools_changed: false,
    last_checked: '2026-07-16T01:02:05Z',
  }],
}

const allowlistConfig = {
  config_path: '/etc/safeops/executor.yaml',
  read_only_roots: ['/'],
  managed_roots: ['/home'],
  candidate_roots: ['/home', '/home/config', '/home/logs'],
  allowed_file_roots: ['/home', '/var/lib/safeops/quarantine'],
  quarantine_root: '/var/lib/safeops/quarantine',
  missing_roots: [],
  requires_executor_restart: true,
  write_actions_enabled: true,
  updated_at: '2026-07-17T01:02:03Z',
}

type APIOptions = {
  session?: Record<string, unknown>
  approvals?: unknown[] | null
  tasks?: unknown[] | null
  servers?: unknown[] | null
  traceEvents?: unknown[] | null
  llmConfig?: Record<string, unknown>
  allowlistConfig?: Record<string, unknown>
}

class MockEventSource {
  onerror: ((event: Event) => void) | null = null
  constructor(public readonly url: string) {}
  addEventListener() {}
  close() {}
}

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response
}

function mockAPI(options: APIOptions = {}) {
  let sessionValue = options.session || session
  let llmConfig = options.llmConfig || { configured: false, api_key_configured: false, last_configuration: 'not configured' }
  let allowlist = options.allowlistConfig || allowlistConfig
  const currentManagedRoots = () => ((allowlist as { managed_roots?: string[] }).managed_roots || [])
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/v1/sessions?')) return jsonResponse({ sessions: [sessionValue] })
    if (url === '/api/v1/sessions/session-1' && init?.method === 'PATCH') {
      const body = JSON.parse(String(init.body))
      sessionValue = { ...sessionValue, ...body, updated_at: '2026-07-17T01:03:00Z' }
      return jsonResponse(sessionValue)
    }
    if (url === '/api/v1/sessions/session-1') return jsonResponse(sessionValue)
    if (url === '/api/v1/tasks/task-1') return jsonResponse(task)
    if (url === '/api/v1/tasks/task-1/trace') return jsonResponse({ ...trace, events: options.traceEvents === undefined ? trace.events : options.traceEvents })
    if (url === '/api/v1/overview') return jsonResponse(overview)
    if (url === '/api/v1/llm/config' && (!init?.method || init.method === 'GET')) return jsonResponse(llmConfig)
    if (url === '/api/v1/llm/config' && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as { base_url: string; api_key: string; model: string }
      llmConfig = { configured: true, base_url: body.base_url, model: body.model, api_key_configured: true, source: 'web', updated_at: '2026-07-17T01:02:03Z' }
      return jsonResponse(llmConfig)
    }
    if (url === '/api/v1/llm/config' && init?.method === 'DELETE') {
      llmConfig = { configured: false, api_key_configured: false, last_configuration: 'not configured' }
      return jsonResponse(llmConfig)
    }
    if (url === '/api/v1/executor/allowlist' && (!init?.method || init.method === 'GET')) return jsonResponse(allowlist)
    if (url === '/api/v1/executor/allowlist' && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as { managed_roots: string[] }
      allowlist = { ...allowlistConfig, managed_roots: body.managed_roots, allowed_file_roots: [...body.managed_roots, allowlistConfig.quarantine_root] }
      return jsonResponse(allowlist)
    }
    if (url.startsWith('/api/v1/executor/path-browser?')) {
      const parsed = new URL(url, 'http://safeops.test')
      const mode = parsed.searchParams.get('mode') || 'read'
      const path = parsed.searchParams.get('path') || (mode === 'write' ? '/home' : '/')
      return jsonResponse({
        path,
        parent: path === '/' ? '' : path.split('/').slice(0, -1).join('/') || '/',
        mode,
        read_only_roots: ['/'],
        managed_roots: currentManagedRoots(),
        candidate_roots: allowlistConfig.candidate_roots,
        entries: mode === 'write' ? [
          { name: 'config', path: '/home/config', is_dir: true, size_bytes: 0, mode: 'drwxr-x---', modified: '2026-07-17T01:02:03Z', selectable_read: true, selectable_write: true },
          { name: 'logs', path: '/home/logs', is_dir: true, size_bytes: 0, mode: 'drwxr-x---', modified: '2026-07-17T01:02:03Z', selectable_read: true, selectable_write: true },
        ] : [
          { name: 'var', path: '/var', is_dir: true, size_bytes: 0, mode: 'drwxr-xr-x', modified: '2026-07-17T01:02:03Z', selectable_read: true, selectable_write: false },
        ],
        truncated: false,
        can_select_read: true,
        can_select_write: mode === 'write',
        can_create_child: mode === 'write',
        write_root_missing: false,
      })
    }
    if (url === '/api/v1/executor/path-browser/directories' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as { parent: string; name: string }
      const created = `${body.parent}/${body.name}`.replace(/\/+/g, '/')
      return jsonResponse({
        path: created,
        parent: body.parent,
        mode: 'write',
        read_only_roots: ['/'],
        managed_roots: currentManagedRoots(),
        candidate_roots: [...allowlistConfig.candidate_roots, created],
        entries: [],
        truncated: false,
        can_select_read: true,
        can_select_write: true,
        can_create_child: true,
        write_root_missing: false,
      }, 201)
    }
    if (url === '/api/v1/mcp/servers') return jsonResponse({ servers: options.servers === undefined ? servers.servers : options.servers })
    if (url === '/api/v1/approvals') return jsonResponse({ approvals: options.approvals === undefined ? [] : options.approvals })
    if (url.startsWith('/api/v1/tasks?')) return jsonResponse({ tasks: options.tasks === undefined ? [task] : options.tasks })
    return jsonResponse({ error: `unhandled test route: ${url}` }, 404)
  }))
  vi.stubGlobal('EventSource', MockEventSource)
}

describe('SafeOps Chinese operational UI', () => {
  beforeEach(() => mockAPI())
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('restores durable state and exposes all required source-backed pages', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeTruthy()
    expect(screen.getByText('已安全转义')).toBeTruthy()
    expect(screen.getByText('VALID')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: '系统概览' }))
    await screen.findByRole('heading', { name: '系统运行全景' })
    expect(screen.getByText('39 个已发现 Tool')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: '工具中心' }))
    await screen.findByRole('heading', { name: 'MCP 插件与工具' })
    expect(screen.getByText('system.get_overview')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: '安全中心' }))
    await screen.findByRole('heading', { name: '本地安全决策面' })
    expect(screen.getByText('未知写能力 fail closed')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: '根因分析' }))
    await screen.findByRole('heading', { name: '当前任务根因视图' })
    expect(screen.getAllByText('端口 18080 由受控进程占用')).toHaveLength(2)

    await user.click(screen.getByRole('button', { name: '审计追踪' }))
    await screen.findByRole('heading', { name: '可审计推理轨迹' })
    expect(screen.getAllByText('RCA_RESULT')).toHaveLength(2)
    expect(screen.getAllByText('VERIFICATION')).toHaveLength(2)

    await user.click(screen.getByRole('button', { name: '管控路径' }))
    await screen.findByRole('heading', { name: 'Agent 管控路径' })
    expect(screen.getAllByText('/home').length).toBeGreaterThan(0)
  })

  it('treats null session messages as an empty durable session', async () => {
    mockAPI({ session: { ...session, messages: null } })
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    expect(screen.getByRole('heading', { name: '从真实系统证据开始' })).toBeTruthy()
  })

  it('treats null workspace collections as empty lists', async () => {
    mockAPI({
      approvals: null,
      tasks: null,
      traceEvents: null,
      servers: [{
        ...servers.servers[0],
        manifest: { ...servers.servers[0].manifest, capabilities: null },
        tools: null,
      }],
    })
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByRole('button', { name: '安全中心' }))
    await screen.findByRole('heading', { name: '本地安全决策面' })
    expect(screen.getByText('暂无审批记录')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: '工具中心' }))
    await screen.findByRole('heading', { name: 'MCP 插件与工具' })
    expect(screen.getByText('0 tools ·')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: '审计追踪' }))
    await screen.findByRole('heading', { name: '可审计推理轨迹' })
    expect(screen.getByText('从会话或系统概览选择任务后显示完整 Trace。')).toBeTruthy()
  })

  it('saves LLM provider settings without rendering the API key', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByRole('button', { name: 'LLM 配置' }))
    await screen.findByRole('heading', { name: 'LLM Provider 配置' })
    await user.type(screen.getByLabelText('接口地址'), 'https://llm.example/v1')
    await user.type(screen.getByLabelText('API Key'), 'secret-key')
    await user.type(screen.getByLabelText('模型'), 'ops-model')
    await user.click(screen.getByRole('button', { name: '保存并启用' }))

    await screen.findByText('已启用')
    expect(screen.getByText('ops-model')).toBeTruthy()
    expect(screen.queryByText('secret-key')).toBeNull()
    expect(fetch).toHaveBeenCalledWith('/api/v1/llm/config', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ base_url: 'https://llm.example/v1', api_key: 'secret-key', model: 'ops-model' }),
    }))
  })

  it('saves executor allowlist paths from the settings page', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByRole('button', { name: '管控路径' }))
    await screen.findByRole('heading', { name: 'Agent 管控路径' })
    const textarea = screen.getByLabelText('管控路径')
    await user.clear(textarea)
    await user.type(textarea, '/home/config')
    await user.click(screen.getByRole('button', { name: '保存路径' }))

    await waitFor(() => expect(screen.getAllByText('/home/config').length).toBeGreaterThan(0))
    expect(fetch).toHaveBeenCalledWith('/api/v1/executor/allowlist', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ managed_roots: ['/home/config'] }),
    }))
  })

  it('previews and saves executor allowlist paths through graphical selection', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByRole('button', { name: '管控路径' }))
    await screen.findByRole('heading', { name: 'Agent 管控路径' })
    await user.click(screen.getByRole('checkbox', { name: '选择 /home' }))
    await user.click(screen.getByRole('checkbox', { name: '选择 /home/config' }))

    expect((screen.getByLabelText('管控路径') as HTMLTextAreaElement).value).toBe('/home/config')
    expect(screen.getByLabelText('管控路径预览').textContent).toContain('将保存 1 个管控根')
    await user.click(screen.getByRole('button', { name: '保存路径' }))

    await waitFor(() => expect(screen.getAllByText('/home/config').length).toBeGreaterThan(0))
    expect(fetch).toHaveBeenCalledWith('/api/v1/executor/allowlist', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ managed_roots: ['/home/config'] }),
    }))
  })

  it('selects and creates writable folders from the resource manager view', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByRole('button', { name: '管控路径' }))
    await screen.findByRole('heading', { name: 'Agent 管控路径' })
    await user.click(screen.getByRole('button', { name: '可写选择' }))
    await screen.findByLabelText('资源管理器视图')
    await user.click(screen.getAllByRole('button', { name: '选择' })[0])
    expect((screen.getByLabelText('管控路径') as HTMLTextAreaElement).value).toContain('/home/config')

    await user.type(screen.getByLabelText('新建文件夹名称'), 'reports')
    await user.click(screen.getByRole('button', { name: '新建文件夹' }))
    await waitFor(() => expect((screen.getByLabelText('管控路径') as HTMLTextAreaElement).value).toContain('/home/reports'))
    expect(fetch).toHaveBeenCalledWith('/api/v1/executor/path-browser/directories', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ parent: '/home', name: 'reports' }),
    }))
  })

  it('renames a session through the inline dialog instead of browser prompt', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByTitle('重命名'))
    await screen.findByRole('dialog', { name: '修改会话名称' })
    const nameInput = screen.getByLabelText('会话名称')
    await user.clear(nameInput)
    await user.type(nameInput, '新的会话名')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await screen.findByRole('heading', { name: '新的会话名' })
    expect(fetch).toHaveBeenCalledWith('/api/v1/sessions/session-1', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ name: '新的会话名' }),
    }))
  })

  it('clears LLM provider settings through the inline confirmation dialog', async () => {
    mockAPI({ llmConfig: { configured: true, base_url: 'https://llm.example/v1', model: 'ops-model', api_key_configured: true, source: 'web' } })
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole('heading', { name: '测试会话' })
    await user.click(screen.getByRole('button', { name: 'LLM 配置' }))
    await screen.findByRole('heading', { name: 'LLM Provider 配置' })
    await user.click(screen.getByRole('button', { name: '清除配置' }))
    await screen.findByRole('dialog', { name: '清除 LLM 配置' })
    expect(screen.getByText('确认清除 Web 保存的 LLM 配置？API key 会从 SafeOps 数据目录删除。')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: '确认清除' }))

    await screen.findByText('未配置')
    expect(fetch).toHaveBeenCalledWith('/api/v1/llm/config', expect.objectContaining({
      method: 'DELETE',
    }))
  })

  it('has no serious or critical automated accessibility violations', async () => {
    const { container } = render(<App />)
    await screen.findByRole('heading', { name: '测试会话' })
    await waitFor(() => expect(screen.getByText('VALID')).toBeTruthy())
    const results = await axe.run(container, {
      rules: {
        'color-contrast': { enabled: false },
      },
    })
    const severe = results.violations.filter(item => item.impact === 'serious' || item.impact === 'critical')
    expect(severe.map(item => ({ id: item.id, nodes: item.nodes.map(node => node.target) }))).toEqual([])
  })
})
