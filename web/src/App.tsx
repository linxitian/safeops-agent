import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'

type View = 'console' | 'overview' | 'tools' | 'safety' | 'rca' | 'audit'
type Message = { message_id: string; role: 'user' | 'assistant' | 'system'; content: string; task_id?: string; created_at: string }
type Session = { session_id: string; name: string; archived: boolean; messages: Message[]; summary?: string; updated_at: string }
type TaskStep = { step_id: string; description: string; tool?: string; state: string }
type TargetRef = { type: string; id: string }
type TargetSnapshot = { type: string; id: string; pid?: number; start_ticks?: number; executable?: string; service_name?: string; canonical_path?: string }
type ActionEnvelope = { proposal: { tool: string; target: TargetRef; batch_size: number; reversible: boolean; rollback_strategy?: string }; target_snapshot: TargetSnapshot; risk: { risk_level: string; risk_score: number; risk_factors: string[]; risk_reason: string }; expires_at: string }
type Task = { task_id: string; session_id?: string; objective?: string; intent_type?: string; state: string; plan: TaskStep[]; current_step: number; findings: string[]; evidence_refs: string[]; pending_approval_id?: string; pending_action?: ActionEnvelope; failure_reason?: string; updated_at?: string }
type Approval = { approval_id: string; status: string; reason?: string; expires_at: string; binding: { task_id: string; tool: string; risk_level: string; policy_version: string; target_snapshot_digest: string } }
type TraceEvent = { sequence: number; timestamp: string; type: string; event_hash: string; prev_hash?: string; data?: unknown }
type RuntimeEvent = { sequence: number; type: string; task_id: string; state: string; message: string; timestamp: string }
type Overview = { mcp: Record<string, number>; sessions: Record<string, number>; tasks: Record<string, number>; approvals: Record<string, number>; generated_at: string }
type ToolRecord = { name: string; description: string; schema_hash: string }
type MCPServer = { manifest: { id: string; display_name: string; version: string; description: string; enabled: boolean; capabilities: string[] }; status: string; error?: string; tools: ToolRecord[]; tool_set_hash?: string; tools_changed: boolean; last_checked: string }

const api = async <T,>(path: string, init?: RequestInit): Promise<T> => {
  const response = await fetch(path, { ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } })
  const body = await response.json()
  if (!response.ok) throw new Error(body.error || `请求失败：${response.status}`)
  return body as T
}

const terminal = (state?: string) => ['COMPLETED', 'FAILED', 'CANCELLED'].includes(state || '')
const riskLabel = (level?: string) => ({ L0: 'L0 低风险 / 只读', L1: 'L1 中风险 / 可逆或受控', L2: 'L2 高风险 / 高影响', L3: 'L3 关键操作' }[level || ''] || level || '未评估')
const targetLabel = (snapshot?: TargetSnapshot) => snapshot ? snapshot.service_name || snapshot.canonical_path || (snapshot.pid ? `PID ${snapshot.pid} / start ${snapshot.start_ticks}` : snapshot.id) : '未知目标'
const viewTitle: Record<View, [string, string]> = {
  console: ['智能体控制台', '与真实系统证据对话'],
  overview: ['系统概览', '运行状态与耐久任务'],
  tools: ['工具中心', 'MCP Server 与已发现能力'],
  safety: ['安全中心', '风险、审批与执行边界'],
  rca: ['根因分析', '候选原因、证据与缺失项'],
  audit: ['审计追踪', 'Hash-Chained 决策与执行事件'],
}

function SafeMarkdown({ content }: { content: string }) {
  return <div className="markdown">{content.split(/\r?\n/).map((line, index) => {
    if (line.startsWith('### ')) return <h4 key={index}>{line.slice(4)}</h4>
    if (line.startsWith('## ')) return <h3 key={index}>{line.slice(3)}</h3>
    if (line.startsWith('- ')) return <div className="md-list" key={index}><i />{line.slice(2)}</div>
    if (line.startsWith('`') && line.endsWith('`')) return <code key={index}>{line.slice(1, -1)}</code>
    return line ? <p key={index}>{line}</p> : <br key={index} />
  })}</div>
}

export default function App() {
  const [view, setView] = useState<View>('console')
  const [sessions, setSessions] = useState<Session[]>([])
  const [active, setActive] = useState<Session | null>(null)
  const [task, setTask] = useState<Task | null>(null)
  const [approval, setApproval] = useState<Approval | null>(null)
  const [traceEvents, setTraceEvents] = useState<TraceEvent[]>([])
  const [traceIntegrity, setTraceIntegrity] = useState('')
  const [overview, setOverview] = useState<Overview | null>(null)
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([])
  const [approvals, setApprovals] = useState<Approval[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [input, setInput] = useState('查看 CPU 和内存。')
  const [search, setSearch] = useState('')
  const [toolSearch, setToolSearch] = useState('')
  const [showArchived, setShowArchived] = useState(false)
  const [progress, setProgress] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [resolving, setResolving] = useState(false)
  const [error, setError] = useState('')
  const streamRef = useRef<EventSource | null>(null)
  const initializedRef = useRef(false)
  const lastSequenceRef = useRef<Record<string, number>>({})

  const syncTask = useCallback(async (taskID: string) => {
    const value = await api<Task>(`/api/v1/tasks/${taskID}`)
    setTask(value)
    if (value.state === 'WAITING_APPROVAL' && value.pending_approval_id) {
      setApproval(await api<Approval>(`/api/v1/approvals/${value.pending_approval_id}`))
    } else setApproval(null)
    try {
      const audit = await api<{ integrity: string; events: TraceEvent[] }>(`/api/v1/tasks/${taskID}/trace`)
      setTraceIntegrity(audit.integrity)
      setTraceEvents(audit.events)
    } catch {
      setTraceIntegrity('')
      setTraceEvents([])
    }
    return value
  }, [])

  const refreshSessions = useCallback(async () => {
    const query = new URLSearchParams({ archived: showArchived ? 'true' : 'false' })
    if (search.trim()) query.set('q', search.trim())
    const result = await api<{ sessions: Session[] }>(`/api/v1/sessions?${query}`)
    setSessions(result.sessions)
    return result.sessions
  }, [search, showArchived])

  const openSession = useCallback(async (id: string) => {
    const value = await api<Session>(`/api/v1/sessions/${id}`)
    setActive(value)
    setView('console')
    const latestTask = [...value.messages].reverse().find(message => message.task_id)?.task_id
    if (latestTask) {
      try { await syncTask(latestTask) } catch { setTask(null); setApproval(null); setTraceEvents([]) }
    } else {
      setTask(null); setApproval(null); setTraceEvents([]); setTraceIntegrity('')
    }
  }, [syncTask])

  const createSession = useCallback(async () => {
    const value = await api<Session>('/api/v1/sessions', { method: 'POST', body: JSON.stringify({ name: '系统感知会话' }) })
    setShowArchived(false)
    setSearch('')
    setActive(value); setTask(null); setApproval(null); setTraceEvents([]); setProgress([]); setView('console')
    const result = await api<{ sessions: Session[] }>('/api/v1/sessions?archived=false')
    setSessions(result.sessions)
  }, [])

  const loadWorkspace = useCallback(async () => {
    const [overviewValue, serverValue, approvalValue, taskValue] = await Promise.all([
      api<Overview>('/api/v1/overview'),
      api<{ servers: MCPServer[] }>('/api/v1/mcp/servers'),
      api<{ approvals: Approval[] }>('/api/v1/approvals'),
      api<{ tasks: Task[] }>('/api/v1/tasks?limit=200'),
    ])
    setOverview(overviewValue)
    setMcpServers(serverValue.servers)
    setApprovals(approvalValue.approvals)
    setTasks(taskValue.tasks)
  }, [])

  useEffect(() => {
    if (initializedRef.current) return
    initializedRef.current = true
    refreshSessions().then(values => values[0] ? openSession(values[0].session_id) : createSession()).catch(err => setError(String(err)))
    return () => streamRef.current?.close()
  }, [createSession, openSession, refreshSessions])

  useEffect(() => {
    if (!initializedRef.current) return
    const timer = window.setTimeout(() => refreshSessions().catch(err => setError(String(err))), 180)
    return () => window.clearTimeout(timer)
  }, [refreshSessions])

  useEffect(() => {
    if (view !== 'console') loadWorkspace().catch(err => setError(err instanceof Error ? err.message : String(err)))
  }, [loadWorkspace, view])

  const followTask = (taskID: string, sessionID: string) => {
    streamRef.current?.close()
    lastSequenceRef.current[taskID] = 0
    const source = new EventSource(`/api/v1/tasks/${taskID}/events`)
    streamRef.current = source

    const consume = async (event: RuntimeEvent) => {
      if (event.sequence > 0 && event.sequence <= (lastSequenceRef.current[taskID] || 0)) return
      if (event.sequence > 0) lastSequenceRef.current[taskID] = event.sequence
      setProgress(items => items[items.length - 1] === event.message ? items : [...items, event.message])
      setError('')
      if (terminal(event.state)) {
        source.close(); setBusy(false)
        await Promise.all([openSession(sessionID), refreshSessions()])
      } else {
        try { await syncTask(taskID) } catch { /* projection can lag the first event by a few milliseconds */ }
      }
    }

    source.addEventListener('task.progress', raw => { void consume(JSON.parse((raw as MessageEvent).data) as RuntimeEvent) })
    source.addEventListener('task.gap', raw => {
      const event = JSON.parse((raw as MessageEvent).data) as RuntimeEvent
      setProgress(items => [...items, event.message])
      void syncTask(taskID)
    })
    source.addEventListener('task.snapshot', raw => { void consume(JSON.parse((raw as MessageEvent).data) as RuntimeEvent) })
    source.onerror = async () => {
      setError('实时事件暂时中断，正在按 Last-Event-ID 自动重连；页面同时回读持久 Task。')
      try {
        const value = await syncTask(taskID)
        if (terminal(value.state)) {
          source.close(); setBusy(false)
          await Promise.all([openSession(sessionID), refreshSessions()])
        }
      } catch { /* EventSource keeps its bounded reconnect loop */ }
    }
  }

  const resolveApproval = async (decision: 'APPROVE' | 'REJECT') => {
    if (!approval || !task || resolving) return
    const verb = decision === 'APPROVE' ? '批准' : '拒绝'
    if (!window.confirm(`确认${verb}这个精确绑定的操作？审批不能授权其他目标。`)) return
    setResolving(true); setError('')
    try {
      const result = await api<{ approval: Approval; task: Task }>(`/api/v1/approvals/${approval.approval_id}/resolve`, { method: 'POST', body: JSON.stringify({ decision, reason: `Web 控制台人工${verb}` }) })
      setTask(result.task); setProgress(items => [...items, `${verb}结果已持久化，任务自动恢复`])
      await syncTask(result.task.task_id)
      if (terminal(result.task.state) && active) {
        setBusy(false); await Promise.all([openSession(active.session_id), refreshSessions()])
      }
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
    finally { setResolving(false) }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!active || !input.trim() || busy) return
    setBusy(true); setError(''); setProgress([])
    try {
      const result = await api<{ task_id: string; session_id: string }>(`/api/v1/sessions/${active.session_id}/messages`, { method: 'POST', body: JSON.stringify({ content: input.trim() }) })
      setTask({ task_id: result.task_id, state: 'NEW', plan: [], current_step: 0, findings: [], evidence_refs: [] })
      followTask(result.task_id, result.session_id)
    } catch (err) { setBusy(false); setError(err instanceof Error ? err.message : String(err)) }
  }

  const renameSession = async (value: Session) => {
    const name = window.prompt('输入新的会话名称（1-128 字）', value.name)?.trim()
    if (!name || name === value.name) return
    try {
      const updated = await api<Session>(`/api/v1/sessions/${value.session_id}`, { method: 'PATCH', body: JSON.stringify({ name }) })
      if (active?.session_id === updated.session_id) setActive(updated)
      await refreshSessions()
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
  }

  const toggleArchive = async (value: Session) => {
    const action = value.archived ? '恢复' : '归档'
    if (!window.confirm(`确认${action}会话“${value.name}”？存在未终结任务时后端会拒绝归档。`)) return
    try {
      await api<Session>(`/api/v1/sessions/${value.session_id}`, { method: 'PATCH', body: JSON.stringify({ archived: !value.archived }) })
      if (active?.session_id === value.session_id) setActive(null)
      await refreshSessions()
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
  }

  const changeServer = async (server: MCPServer, action: 'enable' | 'disable' | 'rediscover' | 'health') => {
    if (action === 'disable' && !window.confirm(`确认停用 MCP Server “${server.manifest.display_name}”？只影响新的工具调用。`)) return
    try {
      await api(`/api/v1/mcp/servers/${server.manifest.id}/${action}`, { method: 'POST', body: '{}' })
      await loadWorkspace()
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
  }

  const renderConsole = () => <>
    <section className="messages">
      {!active?.messages.length && <div className="welcome"><div className="orb">S</div><h2>从真实系统证据开始</h2><p>可输入“查看 CPU 和内存”，或在受控 Demo Lab 中输入“为什么 Web 服务启动失败？帮我恢复。”每个写动作都会展示精确目标、风险、过期时间和独立审批。</p></div>}
      {active?.messages.map(message => <article key={message.message_id} className={`message ${message.role}`}><div className="avatar">{message.role === 'user' ? '你' : 'S'}</div><div><span className="role">{message.role === 'user' ? '运维人员' : 'SafeOps'}</span><SafeMarkdown content={message.content} /></div></article>)}
      {progress.length > 0 && <div className="progress-card"><strong>{busy ? '任务运行中' : '任务事件'}</strong>{progress.map((item, index) => <div key={`${index}-${item}`}><i className={index === progress.length - 1 && busy ? 'pulse' : ''} />{item}</div>)}</div>}
      {approval?.status === 'PENDING' && task?.pending_action && <section className="approval-card" aria-live="polite">
        <div className="approval-heading"><div><span>人工审批</span><h3>{task.pending_action.proposal.tool}</h3></div><b className={`risk ${task.pending_action.risk.risk_level}`}>{riskLabel(task.pending_action.risk.risk_level)}</b></div>
        <dl><div><dt>精确目标</dt><dd>{targetLabel(task.pending_action.target_snapshot)}</dd></div><div><dt>风险分数</dt><dd>{task.pending_action.risk.risk_score} / 100</dd></div><div><dt>批量范围</dt><dd>{task.pending_action.proposal.batch_size || 1} 个目标</dd></div><div><dt>可逆性</dt><dd>{task.pending_action.proposal.reversible ? `可逆：${task.pending_action.proposal.rollback_strategy || '已声明回滚'}` : '不可逆；执行器仅允许固定验证策略'}</dd></div></dl>
        <p>{task.pending_action.risk.risk_reason}</p>
        <div className="risk-factors">{task.pending_action.risk.risk_factors?.map(factor => <span key={factor}>{factor}</span>)}</div>
        <small>审批 ID：{approval.approval_id}<br />过期：{new Date(approval.expires_at).toLocaleString('zh-CN')}<br />目标快照摘要：{approval.binding.target_snapshot_digest.slice(0, 18)}…</small>
        <div className="approval-actions"><button className="reject" disabled={resolving} onClick={() => resolveApproval('REJECT')}>拒绝并安全结束</button><button className="approve" disabled={resolving} onClick={() => resolveApproval('APPROVE')}>{resolving ? '正在提交' : '批准精确动作'}</button></div>
      </section>}
      {task && terminal(task.state) && <section className={`result-card ${task.state.toLowerCase()}`}><span>执行结果</span><h3>{task.state === 'COMPLETED' ? '完成条件已满足' : '任务已安全结束'}</h3><p>{task.failure_reason || task.findings?.[task.findings.length - 1] || '权威结果已写入持久 Task 与审计链。'}</p><small>{task.evidence_refs?.length || 0} 条证据引用 · Trace {traceIntegrity || '待校验'}</small></section>}
    </section>
    <form onSubmit={submit}>
      <label className="sr-only" htmlFor="agent-input">描述希望调查的系统问题</label>
      <textarea id="agent-input" value={input} onChange={event => setInput(event.target.value)} placeholder="描述你希望调查的系统问题…" rows={2} />
      <button disabled={busy || !active}>{busy ? '处理中' : '发送'}</button>
      <small>所有操作先经过结构化工具与安全策略；当前不提供任意 Shell。</small>
    </form>
  </>

  const filteredServers = mcpServers.map(server => ({ ...server, tools: server.tools.filter(tool => !toolSearch.trim() || tool.name.toLowerCase().includes(toolSearch.trim().toLowerCase()) || tool.description.toLowerCase().includes(toolSearch.trim().toLowerCase())) })).filter(server => !toolSearch.trim() || server.tools.length > 0 || server.manifest.display_name.includes(toolSearch.trim()))
  const rcaEvents = traceEvents.filter(event => event.type === 'RCA_RESULT' || event.type === 'KNOWLEDGE_RETRIEVED')

  const renderWorkspace = () => <section className="workspace-page">
    {view === 'overview' && <><div className="page-lead"><h2>系统运行全景</h2><p>来自持久 Session/Task、审批 Store 与 MCP Registry 的实时汇总；不是静态演示数字。</p></div>
      <div className="metric-grid">
        <Metric label="MCP 健康" value={`${overview?.mcp.healthy || 0} / ${overview?.mcp.total || 0}`} detail={`${overview?.mcp.tools || 0} 个已发现 Tool`} />
        <Metric label="活动会话" value={String(overview?.sessions.active || 0)} detail={`${overview?.sessions.archived || 0} 个已归档`} />
        <Metric label="等待审批" value={String(overview?.tasks.WAITING_APPROVAL || 0)} detail={`${overview?.approvals.PENDING || 0} 个 Pending Approval`} />
        <Metric label="已完成任务" value={String(overview?.tasks.COMPLETED || 0)} detail={`${overview?.tasks.FAILED || 0} 失败 · ${overview?.tasks.CANCELLED || 0} 取消`} />
      </div>
      <div className="workspace-grid"><section className="workspace-card"><h3>耐久任务状态</h3>{Object.entries(overview?.tasks || {}).map(([state, count]) => <div className="bar-row" key={state}><span>{state}</span><b>{count}</b></div>)}</section><section className="workspace-card"><h3>最近任务</h3>{tasks.slice(0, 8).map(item => <button className="task-row" key={item.task_id} onClick={() => { setTask(item); if (item.task_id) void syncTask(item.task_id); setView('audit') }}><span>{item.objective || item.task_id}</span><b className={`state ${item.state}`}>{item.state}</b></button>)}</section></div>
    </>}
    {view === 'tools' && <><div className="page-lead"><h2>MCP 插件与工具</h2><p>全部来自官方协议 initialize/tools-list；生命周期操作不修改 Linux 业务状态。</p><input className="page-search" value={toolSearch} onChange={event => setToolSearch(event.target.value)} placeholder="搜索 Tool 名称或说明" /></div>
      <div className="server-grid">{filteredServers.map(server => <section className="server-card" key={server.manifest.id}><div><span>{server.manifest.display_name}</span><b className={`server-status ${server.status}`}>{server.status}</b></div><h3>{server.manifest.id} · v{server.manifest.version}</h3><p>{server.manifest.description}</p><small>{server.tools.length} tools · {server.manifest.capabilities.join(' / ')}</small><div className="tool-list">{server.tools.map(tool => <details key={tool.name}><summary>{tool.name}</summary><p>{tool.description}</p><code>{tool.schema_hash.slice(0, 16)}…</code></details>)}</div><div className="server-actions"><button onClick={() => changeServer(server, 'health')}>健康检查</button><button onClick={() => changeServer(server, 'rediscover')}>重新发现</button><button onClick={() => changeServer(server, server.manifest.enabled ? 'disable' : 'enable')}>{server.manifest.enabled ? '停用' : '启用'}</button></div></section>)}</div>
    </>}
    {view === 'safety' && <><div className="page-lead"><h2>本地安全决策面</h2><p>Tool 自报风险不可信；本地 Policy、Intent Guard、目标快照与执行器重验证才是授权依据。</p></div>
      <div className="boundary-grid"><Metric label="任意命令工具" value="0" detail="未知写能力 fail closed" /><Metric label="待审批" value={String(overview?.approvals.PENDING || 0)} detail="每个动作精确绑定" /><Metric label="执行边界" value="Unix Socket" detail="无公网特权 TCP" /><Metric label="永久清理" value="无 Handler" detail="L3 默认拒绝" /></div>
      <section className="workspace-card"><h3>审批记录</h3>{approvals.length ? approvals.slice(0, 20).map(item => <div className="approval-row" key={item.approval_id}><div><span>{item.binding.tool}</span><small>{item.approval_id} · policy {item.binding.policy_version}</small></div><b className={`risk ${item.binding.risk_level}`}>{item.binding.risk_level} · {item.status}</b></div>) : <p className="muted">暂无审批记录</p>}</section>
    </>}
    {view === 'rca' && <><div className="page-lead"><h2>当前任务根因视图</h2><p>展示候选原因、已用证据和缺失证据；D3 不会伪装成确定根因。</p></div>
      <div className="workspace-grid"><section className="workspace-card"><h3>任务发现</h3>{task?.findings?.length ? task.findings.map(item => <p className="evidence-item" key={item}>{item}</p>) : <p className="muted">选择含 RCA 的任务后显示发现。</p>}<div className="evidence-count">证据引用：{task?.evidence_refs?.length || 0}</div></section><section className="workspace-card"><h3>RCA / Knowledge 事件</h3>{rcaEvents.length ? rcaEvents.map(event => <details className="audit-detail" key={event.sequence}><summary>#{event.sequence} {event.type}</summary><pre>{JSON.stringify(event.data, null, 2)}</pre></details>) : <p className="muted">当前 Trace 尚无 RCA_RESULT。</p>}</section></div>
    </>}
    {view === 'audit' && <><div className="page-lead"><h2>可审计推理轨迹</h2><p>只展示结构化 DecisionRecord、Guard、工具、审批、执行与验证；不保存模型隐藏思维过程。</p><span className={`integrity ${traceIntegrity}`}>{traceIntegrity || '请选择任务'}</span></div>
      <section className="audit-timeline">{traceEvents.length ? traceEvents.map(event => <article key={event.sequence}><span>{event.sequence}</span><div><header><b>{event.type}</b><time>{new Date(event.timestamp).toLocaleString('zh-CN')}</time></header><code>{event.event_hash.slice(0, 20)}…</code>{event.data !== undefined && <details className="audit-detail"><summary>结构化事件数据</summary><pre>{JSON.stringify(event.data, null, 2)}</pre></details>}</div></article>) : <p className="muted">从会话或系统概览选择任务后显示完整 Trace。</p>}</section>
    </>}
  </section>

  return <div className="shell">
    <aside className="sidebar" aria-label="会话与导航">
      <div className="brand"><span className="brand-mark">S</span><div><strong>SafeOps</strong><small>安全自治运维智能体</small></div></div>
      <div className="page-nav" aria-label="主要页面">{(Object.keys(viewTitle) as View[]).map(item => <button key={item} className={view === item ? 'active' : ''} onClick={() => setView(item)}>{viewTitle[item][0]}</button>)}</div>
      <button className="new-session" onClick={createSession}>＋ 新建会话</button>
      <div className="session-tools"><input value={search} onChange={event => setSearch(event.target.value)} placeholder="搜索历史会话" aria-label="搜索历史会话" /><button onClick={() => setShowArchived(value => !value)}>{showArchived ? '查看活动' : '查看归档'}</button></div>
      <div className="section-label">{showArchived ? '已归档会话' : '历史会话'}</div>
      <nav>{sessions.map(item => <div key={item.session_id} className={active?.session_id === item.session_id ? 'session-wrap active' : 'session-wrap'}><button className="session" onClick={() => openSession(item.session_id)}><span>{item.name}</span><small>{new Date(item.updated_at).toLocaleString('zh-CN')}</small></button><div><button title="重命名" onClick={() => renameSession(item)}>✎</button><button title={item.archived ? '恢复' : '归档'} onClick={() => toggleArchive(item)}>{item.archived ? '↥' : '⌁'}</button></div></div>)}</nav>
      <div className="safety-note"><span>安全边界已启用</span><small>39 个 MCP Tool 均为 L0 只读；写动作只走独立审批与 Unix 执行器</small></div>
    </aside>

    <main className={`conversation ${view !== 'console' ? 'page-mode' : ''}`}>
      <header><div><span className="eyebrow">{viewTitle[view][1]}</span><h1>{view === 'console' ? active?.name || '正在加载会话' : viewTitle[view][0]}</h1></div><span className="status"><i /> {overview ? `MCP ${overview.mcp.healthy || 0}/${overview.mcp.total || 0}` : 'MCP 已连接'}</span></header>
      {view === 'console' ? renderConsole() : renderWorkspace()}
      {error && <div className="error global-error">{error}</div>}
    </main>

    <aside className="task-panel" aria-label="当前任务详情">
      <div className="panel-title"><span>当前任务</span><b className={`state ${task?.state || 'IDLE'}`}>{task?.state || '空闲'}</b></div>
      {!task && <p className="muted">发送请求或从系统概览选择任务后，这里会展示计划、证据和工具状态。</p>}
      {task && <>
        <div className="task-id">{task.task_id}</div>
        <h3>计划进度</h3>
        <ol>{task.plan?.map((step, index) => <li key={step.step_id} className={step.state.toLowerCase()}><span>{index + 1}</span><div>{step.description}<small>{step.tool}</small></div></li>)}</ol>
        <h3>证据发现</h3>
        <div className="findings">{task.findings?.length ? task.findings.map(item => <p key={item}>{item}</p>) : <p className="muted">等待新的系统证据</p>}</div>
        <div className="evidence-count">已绑定 {task.evidence_refs?.length || 0} 条 Trace 证据</div>
        {task.failure_reason && <div className="error">{task.failure_reason}</div>}
      </>}
      <div className="trace-legend"><h3>审计追踪 <b>{traceIntegrity || '等待证据'}</b></h3><p>任务、Guard、审批、执行与验证写入 Hash-Chained JSONL。</p>{traceEvents.slice(-8).map(event => <button className="trace-event" key={event.sequence} onClick={() => setView('audit')}><span>{event.sequence}</span><code>{event.type}</code></button>)}</div>
    </aside>
  </div>
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <section className="metric"><span>{label}</span><strong>{value}</strong><small>{detail}</small></section>
}
