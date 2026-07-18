import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'

type View = 'console' | 'overview' | 'tools' | 'safety' | 'rca' | 'audit' | 'allowlist' | 'llm'
type Message = { message_id: string; role: 'user' | 'assistant' | 'system'; content: string; task_id?: string; created_at: string }
type Session = { session_id: string; name: string; archived: boolean; messages?: Message[] | null; summary?: string; updated_at: string }
type TaskStep = { step_id: string; description: string; tool?: string; state: string }
type TargetRef = { type: string; id: string }
type TargetSnapshot = { type: string; id: string; pid?: number; start_ticks?: number; executable?: string; service_name?: string; canonical_path?: string; expect_absent?: boolean; parent_path?: string }
type ActionEnvelope = { proposal: { tool: string; target: TargetRef; batch_size: number; reversible: boolean; rollback_strategy?: string }; target_snapshot: TargetSnapshot; risk: { risk_level: string; risk_score: number; risk_factors?: string[] | null; risk_reason: string }; expires_at: string }
type Task = { task_id: string; session_id?: string; objective?: string; intent_type?: string; state: string; plan?: TaskStep[] | null; current_step: number; findings?: string[] | null; evidence_refs?: string[] | null; pending_approval_id?: string; pending_action?: ActionEnvelope; failure_reason?: string; updated_at?: string }
type Approval = { approval_id: string; status: string; reason?: string; expires_at: string; binding: { task_id: string; tool: string; risk_level: string; policy_version: string; target_snapshot_digest: string } }
type TraceEvent = { sequence: number; timestamp: string; type: string; event_hash: string; prev_hash?: string; data?: unknown }
type RuntimeEvent = { sequence: number; type: string; task_id: string; state: string; message: string; timestamp: string }
type Overview = { mcp: Record<string, number>; sessions: Record<string, number>; tasks: Record<string, number>; approvals: Record<string, number>; generated_at: string }
type ToolRecord = { name: string; description: string; schema_hash: string }
type DependencyState = { name: string; kind: string; available: boolean; resolved?: string; error?: string; checked_at: string }
type HealthRecord = { checked_at: string; status: string; error?: string; dependencies_healthy: boolean; duration_millis: number }
type DiscoveryRecord = { discovered_at: string; server_name: string; server_version: string; tool_set_hash: string; tool_count: number; tools_changed: boolean }
type MCPServer = { manifest: { id: string; display_name: string; version: string; description: string; enabled: boolean; capabilities?: string[] | null }; status: string; error?: string; actual_server_name?: string; actual_server_version?: string; tools?: ToolRecord[] | null; tool_set_hash?: string; tools_changed: boolean; dependencies_checked: boolean; dependencies_healthy: boolean; dependency_checks?: DependencyState[] | null; health_history?: HealthRecord[] | null; discovery_history?: DiscoveryRecord[] | null; last_checked: string }
type LLMConfig = { configured: boolean; base_url?: string; model?: string; api_key_configured: boolean; source?: string; updated_at?: string; last_configuration?: string }
type AllowlistConfig = { config_path: string; read_only_roots?: string[] | null; managed_roots?: string[] | null; candidate_roots?: string[] | null; allowed_file_roots?: string[] | null; quarantine_root: string; missing_roots?: string[] | null; requires_executor_restart: boolean; write_actions_enabled: boolean; updated_at?: string }
type BrowserMode = 'read' | 'write'
type PathBrowserEntry = { name: string; path: string; is_dir: boolean; size_bytes: number; mode: string; modified: string; selectable_read: boolean; selectable_write: boolean }
type PathBrowser = { path: string; parent?: string; mode: BrowserMode; read_only_roots?: string[] | null; managed_roots?: string[] | null; candidate_roots?: string[] | null; entries?: PathBrowserEntry[] | null; truncated: boolean; can_select_read: boolean; can_select_write: boolean; can_create_child: boolean; write_root_missing: boolean }
type StreamingReply = { messageID: string; glyphs: string[]; visibleCount: number }
type ConfirmDialog = { title: string; message: string; confirmLabel: string; cancelLabel?: string; tone?: 'default' | 'danger' }

const typewriterIntervalMS = 18

const api = async <T,>(path: string, init?: RequestInit): Promise<T> => {
  const response = await fetch(path, { ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } })
  const body = await response.json()
  if (!response.ok) throw new Error(body.error || `请求失败：${response.status}`)
  return body as T
}

const terminal = (state?: string) => ['COMPLETED', 'FAILED', 'CANCELLED'].includes(state || '')
const riskLabel = (level?: string) => ({ L0: 'L0 低风险 / 只读', L1: 'L1 中风险 / 可逆或受控', L2: 'L2 高风险 / 高影响', L3: 'L3 关键操作' }[level || ''] || level || '未评估')
const targetLabel = (snapshot?: TargetSnapshot) => snapshot ? snapshot.service_name || (snapshot.expect_absent && snapshot.canonical_path ? `${snapshot.canonical_path}（待创建）` : snapshot.canonical_path) || (snapshot.pid ? `PID ${snapshot.pid} / start ${snapshot.start_ticks}` : snapshot.id) : '未知目标'
const viewTitle: Record<View, [string, string]> = {
  console: ['智能体控制台', '与真实系统证据对话'],
  overview: ['系统概览', '运行状态与耐久任务'],
  tools: ['工具中心', 'MCP Server 与已发现能力'],
  safety: ['安全中心', '风险、审批与执行边界'],
  rca: ['根因分析', '候选原因、证据与缺失项'],
  audit: ['审计追踪', 'Hash-Chained 决策与执行事件'],
  allowlist: ['管控路径', 'Agent 可操作文件范围'],
  llm: ['LLM 配置', 'OpenAI-compatible Provider'],
}

const asArray = <T,>(value?: T[] | null): T[] => Array.isArray(value) ? value : []
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
const uniquePaths = (values: string[]) => Array.from(new Set(values.map(item => item.trim()).filter(Boolean)))
const splitPathLines = (value: string) => uniquePaths(value.split(/\r?\n/))
const joinPathLines = (values: string[]) => uniquePaths(values).join('\n')

const allowlistCandidateRoots = (config: AllowlistConfig | null, draftRoots: string[]) => {
  if (!config) return []
  const quarantine = config.quarantine_root
  const candidates = asArray(config.candidate_roots).length ? asArray(config.candidate_roots) : asArray(config.allowed_file_roots)
  return uniquePaths([...candidates, ...draftRoots]).filter(path => path !== quarantine)
}

const pathName = (path: string) => path.split('/').filter(Boolean).at(-1) || path

const normalizeSession = (value: Session): Session => ({
  ...value,
  messages: [...asArray(value.messages)],
})

const normalizeSessions = (values: Session[]): Session[] => values.map(normalizeSession)

const normalizeTask = (value: Task): Task => ({
  ...value,
  plan: asArray(value.plan),
  findings: asArray(value.findings),
  evidence_refs: asArray(value.evidence_refs),
})

const normalizeMCPServer = (value: MCPServer): MCPServer => ({
  ...value,
  manifest: { ...value.manifest, capabilities: asArray(value.manifest.capabilities) },
  tools: asArray(value.tools),
  dependency_checks: asArray(value.dependency_checks),
  health_history: asArray(value.health_history),
  discovery_history: asArray(value.discovery_history),
})

function SafeMarkdown({ content, streaming = false }: { content: string; streaming?: boolean }) {
  return <div className={`markdown ${streaming ? 'streaming-markdown' : ''}`}>{content.split(/\r?\n/).map((line, index) => {
    if (line.startsWith('### ')) return <h4 key={index}>{line.slice(4)}</h4>
    if (line.startsWith('## ')) return <h3 key={index}>{line.slice(3)}</h3>
    if (line.startsWith('- ')) return <div className="md-list" key={index}><i />{line.slice(2)}</div>
    if (line.startsWith('`') && line.endsWith('`')) return <code key={index}>{line.slice(1, -1)}</code>
    return line ? <p key={index}>{line}</p> : <br key={index} />
  })}{streaming && <span className="typewriter-cursor" aria-hidden="true" />}</div>
}

function sessionPreview(session: Session) {
  const latest = asArray(session.messages).at(-1)
  return latest?.content?.replace(/\s+/g, ' ').slice(0, 72) || session.summary || '尚未开始对话'
}

function isDefaultSessionName(name: string) {
  return ['', '新会话', '新对话', '系统感知会话'].includes(name.trim())
}

function sessionTitleFromContent(content: string) {
  const title = content.replace(/\s+/g, ' ').trim()
  return Array.from(title).slice(0, 36).join('') || '新会话'
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
  const [llmConfig, setLLMConfig] = useState<LLMConfig | null>(null)
  const [llmForm, setLLMForm] = useState({ base_url: '', api_key: '', model: '' })
  const [allowlistConfig, setAllowlistConfig] = useState<AllowlistConfig | null>(null)
  const [allowlistText, setAllowlistText] = useState('')
  const [pathBrowser, setPathBrowser] = useState<PathBrowser | null>(null)
  const [pathBrowserMode, setPathBrowserMode] = useState<BrowserMode>('read')
  const [pathBrowserPath, setPathBrowserPath] = useState('/')
  const [pathBrowserLoading, setPathBrowserLoading] = useState(false)
  const [pathBrowserError, setPathBrowserError] = useState('')
  const [newFolderName, setNewFolderName] = useState('')
  const [input, setInput] = useState('')
  const [search, setSearch] = useState('')
  const [toolSearch, setToolSearch] = useState('')
  const [showArchived, setShowArchived] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [progress, setProgress] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [resolving, setResolving] = useState(false)
  const [savingLLM, setSavingLLM] = useState(false)
  const [savingAllowlist, setSavingAllowlist] = useState(false)
  const [error, setError] = useState('')
  const [renameDialog, setRenameDialog] = useState<{ session: Session; name: string } | null>(null)
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialog | null>(null)
  const [renamingSession, setRenamingSession] = useState(false)
  const [streamingReply, setStreamingReply] = useState<StreamingReply | null>(null)
  const [pendingSessionID, setPendingSessionID] = useState<string | null>(null)
  const streamRef = useRef<EventSource | null>(null)
  const confirmResolverRef = useRef<((confirmed: boolean) => void) | null>(null)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const messagesRef = useRef<HTMLElement | null>(null)
  const initializedRef = useRef(false)
  const allowlistInitializedRef = useRef(false)
  const lastSequenceRef = useRef<Record<string, number>>({})
  const streamedTaskIDsRef = useRef<Set<string>>(new Set())

  const requestConfirmation = useCallback((dialog: ConfirmDialog) => new Promise<boolean>(resolve => {
    confirmResolverRef.current?.(false)
    confirmResolverRef.current = resolve
    setConfirmDialog({ ...dialog, cancelLabel: dialog.cancelLabel || '取消' })
  }), [])

  const closeConfirmDialog = useCallback((confirmed: boolean) => {
    const resolve = confirmResolverRef.current
    confirmResolverRef.current = null
    setConfirmDialog(null)
    resolve?.(confirmed)
  }, [])

  const syncTask = useCallback(async (taskID: string) => {
    const value = normalizeTask(await api<Task>(`/api/v1/tasks/${taskID}`))
    setTask(value)
    if (value.state === 'WAITING_APPROVAL' && value.pending_approval_id) {
      setApproval(await api<Approval>(`/api/v1/approvals/${value.pending_approval_id}`))
    } else setApproval(null)
    try {
      const audit = await api<{ integrity: string; events: TraceEvent[] }>(`/api/v1/tasks/${taskID}/trace`)
      setTraceIntegrity(audit.integrity)
      setTraceEvents(asArray(audit.events))
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
    const values = normalizeSessions(result.sessions || [])
    setSessions(values)
    return values
  }, [search, showArchived])

  const openSession = useCallback(async (id: string, animateTaskID?: string) => {
    const value = normalizeSession(await api<Session>(`/api/v1/sessions/${id}`))
    let nextStreamingReply: StreamingReply | null = null
    if (animateTaskID && !streamedTaskIDsRef.current.has(animateTaskID)) {
      streamedTaskIDsRef.current.add(animateTaskID)
      const reply = [...asArray(value.messages)].reverse().find(message => message.role === 'assistant' && message.task_id === animateTaskID)
      const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
      if (reply?.content && !reduceMotion) nextStreamingReply = { messageID: reply.message_id, glyphs: Array.from(reply.content), visibleCount: 0 }
    }
    setActive(value)
    setStreamingReply(nextStreamingReply)
    setSidebarOpen(false)
    setView('console')
    const latestTask = [...(value.messages || [])].reverse().find(message => message.task_id)?.task_id
    if (latestTask) {
      try { await syncTask(latestTask) } catch { setTask(null); setApproval(null); setTraceEvents([]) }
    } else {
      setTask(null); setApproval(null); setTraceEvents([]); setTraceIntegrity('')
    }
    return value
  }, [syncTask])

  const createSession = useCallback(async () => {
    const value = normalizeSession(await api<Session>('/api/v1/sessions', { method: 'POST', body: JSON.stringify({ name: '' }) }))
    setShowArchived(false)
    setSearch('')
    setActive(value); setTask(null); setApproval(null); setTraceEvents([]); setProgress([]); setStreamingReply(null); setPendingSessionID(null); setView('console'); setSidebarOpen(false)
    const result = await api<{ sessions: Session[] }>('/api/v1/sessions?archived=false')
    setSessions(normalizeSessions(result.sessions || []))
  }, [])

  const loadWorkspace = useCallback(async () => {
    const [overviewValue, serverValue, approvalValue, taskValue] = await Promise.all([
      api<Overview>('/api/v1/overview'),
      api<{ servers: MCPServer[] }>('/api/v1/mcp/servers'),
      api<{ approvals: Approval[] }>('/api/v1/approvals'),
      api<{ tasks: Task[] }>('/api/v1/tasks?limit=200'),
    ])
    setOverview(overviewValue)
    setMcpServers(asArray(serverValue.servers).map(normalizeMCPServer))
    setApprovals(asArray(approvalValue.approvals))
    setTasks(asArray(taskValue.tasks).map(normalizeTask))
  }, [])

  const loadLLMConfig = useCallback(async () => {
    const value = await api<LLMConfig>('/api/v1/llm/config')
    setLLMConfig(value)
    setLLMForm(current => ({ base_url: value.base_url || '', api_key: current.api_key, model: value.model || '' }))
    return value
  }, [])

  const loadAllowlistConfig = useCallback(async () => {
    const value = await api<AllowlistConfig>('/api/v1/executor/allowlist')
    setAllowlistConfig(value)
    setAllowlistText(asArray(value.managed_roots).join('\n'))
    return value
  }, [])

  const loadPathBrowser = useCallback(async (path: string, mode: BrowserMode) => {
    const query = new URLSearchParams({ mode, path: path || '' })
    setPathBrowserLoading(true)
    setPathBrowserError('')
    try {
      const value = await api<PathBrowser>(`/api/v1/executor/path-browser?${query}`)
      setPathBrowser(value)
      setPathBrowserMode(value.mode)
      setPathBrowserPath(value.path)
      return value
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setPathBrowserError(message)
      throw err
    } finally {
      setPathBrowserLoading(false)
    }
  }, [])

  useEffect(() => {
    if (initializedRef.current) return
    initializedRef.current = true
    refreshSessions().then(async values => {
      if (values[0]) await openSession(values[0].session_id)
      else await createSession()
    }).catch(err => setError(String(err)))
    return () => streamRef.current?.close()
  }, [createSession, openSession, refreshSessions])

  useEffect(() => () => {
    confirmResolverRef.current?.(false)
    confirmResolverRef.current = null
  }, [])

  useEffect(() => {
    if (!initializedRef.current) return
    const timer = window.setTimeout(() => refreshSessions().catch(err => setError(String(err))), 180)
    return () => window.clearTimeout(timer)
  }, [refreshSessions])

  useEffect(() => {
    if (view !== 'console') loadWorkspace().catch(err => setError(err instanceof Error ? err.message : String(err)))
  }, [loadWorkspace, view])

  useEffect(() => {
    if (view === 'llm') loadLLMConfig().catch(err => setError(err instanceof Error ? err.message : String(err)))
  }, [loadLLMConfig, view])

  useEffect(() => {
    if (view !== 'allowlist') {
      allowlistInitializedRef.current = false
      return
    }
    if (allowlistInitializedRef.current) return
    allowlistInitializedRef.current = true
    loadAllowlistConfig().then(value => {
      const initialPath = pathBrowserMode === 'write' ? asArray(value.managed_roots)[0] || asArray(value.candidate_roots)[0] || '/' : asArray(value.read_only_roots)[0] || '/'
      return loadPathBrowser(initialPath, pathBrowserMode)
    }).catch(err => setError(err instanceof Error ? err.message : String(err)))
  }, [loadAllowlistConfig, loadPathBrowser, pathBrowserMode, view])

  useEffect(() => {
    const element = inputRef.current
    if (!element) return
    element.style.height = 'auto'
    element.style.height = `${Math.min(element.scrollHeight, 180)}px`
  }, [input])

  useEffect(() => {
    if (!streamingReply) return
    if (streamingReply.visibleCount >= streamingReply.glyphs.length) {
      const timer = window.setTimeout(() => setStreamingReply(current => current?.messageID === streamingReply.messageID ? null : current), 90)
      return () => window.clearTimeout(timer)
    }
    const step = Math.max(1, Math.ceil(streamingReply.glyphs.length / 280))
    const timer = window.setTimeout(() => {
      setStreamingReply(current => current?.messageID === streamingReply.messageID
        ? { ...current, visibleCount: Math.min(current.glyphs.length, current.visibleCount + step) }
        : current)
    }, typewriterIntervalMS)
    return () => window.clearTimeout(timer)
  }, [streamingReply])

  useEffect(() => {
    if ((!streamingReply && !pendingSessionID) || !messagesRef.current) return
    messagesRef.current.scrollTop = messagesRef.current.scrollHeight
  }, [pendingSessionID, streamingReply])

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
        source.close()
        try { await Promise.all([openSession(sessionID, taskID), refreshSessions()]) }
        finally { setBusy(false); setPendingSessionID(null) }
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
          source.close()
          try { await Promise.all([openSession(sessionID, taskID), refreshSessions()]) }
          finally { setBusy(false); setPendingSessionID(null) }
        }
      } catch { /* EventSource keeps its bounded reconnect loop */ }
    }
  }

  const resolveApproval = async (decision: 'APPROVE' | 'REJECT') => {
    if (!approval || !task || resolving) return
    const verb = decision === 'APPROVE' ? '批准' : '拒绝'
    const confirmed = await requestConfirmation({
      title: `${verb}精确绑定操作`,
      message: `确认${verb}这个精确绑定的操作？审批不能授权其他目标。`,
      confirmLabel: verb,
      tone: decision === 'REJECT' ? 'danger' : 'default',
    })
    if (!confirmed) return
    setResolving(true); setError('')
    try {
      const result = await api<{ approval: Approval; task: Task }>(`/api/v1/approvals/${approval.approval_id}/resolve`, { method: 'POST', body: JSON.stringify({ decision, reason: `Web 控制台人工${verb}` }) })
      setTask(normalizeTask(result.task)); setProgress(items => [...items, `${verb}结果已持久化，任务自动恢复`])
      await syncTask(result.task.task_id)
      if (terminal(result.task.state) && active) {
        try { await Promise.all([openSession(active.session_id, result.task.task_id), refreshSessions()]) }
        finally { setBusy(false); setPendingSessionID(null) }
      }
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
    finally { setResolving(false) }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!active || !input.trim() || busy) return
    const content = input.trim()
    const sessionID = active.session_id
    const previousName = active.name
    const shouldUseFirstQuestionAsName = asArray(active.messages).length === 0 && isDefaultSessionName(active.name)
    const optimisticName = shouldUseFirstQuestionAsName ? sessionTitleFromContent(content) : ''
    const optimisticID = `pending_${Date.now()}`
    const optimisticMessage: Message = { message_id: optimisticID, role: 'user', content, created_at: new Date().toISOString() }
    setBusy(true); setError(''); setProgress([]); setStreamingReply(null); setPendingSessionID(sessionID); setApproval(null); setInput('')
    setActive(current => current?.session_id === sessionID
      ? { ...current, name: optimisticName || current.name, messages: [...asArray(current.messages), optimisticMessage], updated_at: optimisticMessage.created_at }
      : current)
    if (optimisticName) {
      setSessions(items => items.map(item => item.session_id === sessionID ? { ...item, name: optimisticName, updated_at: optimisticMessage.created_at } : item))
    }
    try {
      const result = await api<{ task_id: string; session_id: string }>(`/api/v1/sessions/${sessionID}/messages`, { method: 'POST', body: JSON.stringify({ content }) })
      setActive(current => current?.session_id === sessionID
        ? { ...current, messages: asArray(current.messages).map(message => message.message_id === optimisticID ? { ...message, task_id: result.task_id } : message) }
        : current)
      setTask({ task_id: result.task_id, session_id: result.session_id, state: 'NEW', plan: [], current_step: 0, findings: [], evidence_refs: [] })
      followTask(result.task_id, result.session_id)
    } catch (err) {
      setActive(current => current?.session_id === sessionID
        ? { ...current, name: optimisticName ? previousName : current.name, messages: asArray(current.messages).filter(message => message.message_id !== optimisticID) }
        : current)
      if (optimisticName) {
        setSessions(items => items.map(item => item.session_id === sessionID ? { ...item, name: previousName } : item))
      }
      setInput(current => current || content)
      setBusy(false); setPendingSessionID(null); setError(err instanceof Error ? err.message : String(err))
    }
  }

  const beginRenameSession = (value: Session) => {
    setError('')
    setRenameDialog({ session: value, name: value.name })
  }

  const submitRenameSession = async (event: FormEvent) => {
    event.preventDefault()
    if (!renameDialog || renamingSession) return
    const name = renameDialog.name.trim()
    if (!name) {
      setError('会话名称不能为空')
      return
    }
    if (name === renameDialog.session.name) {
      setRenameDialog(null)
      return
    }
    setRenamingSession(true)
    try {
      const updated = normalizeSession(await api<Session>(`/api/v1/sessions/${renameDialog.session.session_id}`, { method: 'PATCH', body: JSON.stringify({ name }) }))
      if (active?.session_id === updated.session_id) setActive(updated)
      await refreshSessions()
      setRenameDialog(null)
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
    finally { setRenamingSession(false) }
  }

  const toggleArchive = async (value: Session) => {
    const action = value.archived ? '恢复' : '归档'
    const confirmed = await requestConfirmation({
      title: `${action}会话`,
      message: `确认${action}会话“${value.name}”？存在未终结任务时后端会拒绝归档。`,
      confirmLabel: action,
      tone: value.archived ? 'default' : 'danger',
    })
    if (!confirmed) return
    try {
      await api<Session>(`/api/v1/sessions/${value.session_id}`, { method: 'PATCH', body: JSON.stringify({ archived: !value.archived }) })
      if (active?.session_id === value.session_id) setActive(null)
      await refreshSessions()
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
  }

  const changeServer = async (server: MCPServer, action: 'enable' | 'disable' | 'rediscover' | 'health') => {
    if (action === 'disable') {
      const confirmed = await requestConfirmation({
        title: '停用 MCP Server',
        message: `确认停用 MCP Server “${server.manifest.display_name}”？只影响新的工具调用。`,
        confirmLabel: '停用',
        tone: 'danger',
      })
      if (!confirmed) return
    }
    try {
      await api(`/api/v1/mcp/servers/${server.manifest.id}/${action}`, { method: 'POST', body: '{}' })
      await loadWorkspace()
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
  }

  const saveLLMConfig = async (event: FormEvent) => {
    event.preventDefault()
    if (savingLLM) return
    setSavingLLM(true); setError('')
    try {
      const saved = await api<LLMConfig>('/api/v1/llm/config', { method: 'PUT', body: JSON.stringify(llmForm) })
      setLLMConfig(saved)
      setLLMForm({ base_url: saved.base_url || '', api_key: '', model: saved.model || '' })
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
    finally { setSavingLLM(false) }
  }

  const clearLLMConfig = async () => {
    const confirmed = await requestConfirmation({
      title: '清除 LLM 配置',
      message: '确认清除 Web 保存的 LLM 配置？API key 会从 SafeOps 数据目录删除。',
      confirmLabel: '确认清除',
      tone: 'danger',
    })
    if (!confirmed) return
    setSavingLLM(true); setError('')
    try {
      const cleared = await api<LLMConfig>('/api/v1/llm/config', { method: 'DELETE' })
      setLLMConfig(cleared)
      setLLMForm({ base_url: '', api_key: '', model: '' })
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
    finally { setSavingLLM(false) }
  }

  const saveAllowlistConfig = async (event: FormEvent) => {
    event.preventDefault()
    if (savingAllowlist) return
    const managed_roots = splitPathLines(allowlistText)
    setSavingAllowlist(true); setError('')
    try {
      const saved = await api<AllowlistConfig>('/api/v1/executor/allowlist', { method: 'PUT', body: JSON.stringify({ managed_roots }) })
      setAllowlistConfig(saved)
      setAllowlistText(asArray(saved.managed_roots).join('\n'))
    } catch (err) { setError(err instanceof Error ? err.message : String(err)) }
    finally { setSavingAllowlist(false) }
  }

  const allowlistDraftRoots = splitPathLines(allowlistText)
  const allowlistCandidates = allowlistCandidateRoots(allowlistConfig, allowlistDraftRoots)
  const allowlistMissingRoots = asArray(allowlistConfig?.missing_roots)
  const allowlistAllowedPreview = uniquePaths([...allowlistDraftRoots, allowlistConfig?.quarantine_root || '']).filter(Boolean)
  const toggleAllowlistCandidate = (path: string) => {
    const selected = allowlistDraftRoots.includes(path)
    setAllowlistText(joinPathLines(selected ? allowlistDraftRoots.filter(item => item !== path) : [...allowlistDraftRoots, path]))
  }
  const addWritableRoot = (path: string) => {
    setAllowlistText(joinPathLines([...allowlistDraftRoots, path]))
  }
  const switchPathBrowserMode = (mode: BrowserMode) => {
    const path = mode === 'write' ? allowlistDraftRoots[0] || allowlistCandidates[0] || pathBrowserPath : asArray(allowlistConfig?.read_only_roots)[0] || '/'
    void loadPathBrowser(path, mode)
  }
  const createBrowserDirectory = async () => {
    const name = newFolderName.trim()
    if (!pathBrowser || !name || pathBrowserLoading) return
    setPathBrowserLoading(true)
    setPathBrowserError('')
    try {
      const value = await api<PathBrowser>('/api/v1/executor/path-browser/directories', { method: 'POST', body: JSON.stringify({ parent: pathBrowser.path, name }) })
      setPathBrowser(value)
      setPathBrowserMode(value.mode)
      setPathBrowserPath(value.path)
      addWritableRoot(value.path)
      setNewFolderName('')
    } catch (err) {
      setPathBrowserError(err instanceof Error ? err.message : String(err))
    } finally {
      setPathBrowserLoading(false)
    }
  }

  const renderConsole = () => <>
    <section className="messages" ref={messagesRef}>
      {!(active?.messages || []).length && <div className="welcome"><div className="orb">S</div><h2>从真实系统证据开始</h2><p>输入需要调查、恢复或受控处理的系统问题。涉及写动作时，SafeOps 会先展示精确目标、风险、过期时间和独立审批。</p></div>}
      {(active?.messages || []).map(message => {
        const isStreaming = streamingReply?.messageID === message.message_id && streamingReply.visibleCount < streamingReply.glyphs.length
        const visibleContent = isStreaming ? streamingReply.glyphs.slice(0, streamingReply.visibleCount).join('') : message.content
        return <article key={message.message_id} className={`message ${message.role} ${isStreaming ? 'is-streaming' : ''}`}><div className="avatar">{message.role === 'user' ? '你' : 'S'}</div><div><span className="role">{message.role === 'user' ? '运维人员' : 'SafeOps'}</span><div aria-hidden={isStreaming || undefined}><SafeMarkdown content={visibleContent} streaming={isStreaming} /></div>{isStreaming && <span className="sr-only" aria-live="polite">{message.content}</span>}</div></article>
      })}
      {busy && pendingSessionID === active?.session_id && approval?.status !== 'PENDING' && <article className="message assistant thinking-message" role="status" aria-live="polite"><div className="avatar">S</div><div><span className="role">SafeOps</span><div className="thinking-status"><span>思考中</span><span className="thinking-dots" aria-hidden="true"><i /><i /><i /></span></div></div></article>}
      {asArray(progress).length > 0 && <div className="progress-card"><strong>{busy ? '任务运行中' : '任务事件'}</strong>{asArray(progress).map((item, index) => <div key={`${index}-${item}`}><i className={index === asArray(progress).length - 1 && busy ? 'pulse' : ''} />{item}</div>)}</div>}
      {approval?.status === 'PENDING' && task?.pending_action && <section className="approval-card" aria-live="polite">
        <div className="approval-heading"><div><span>人工审批</span><h3>{task.pending_action.proposal.tool}</h3></div><b className={`risk ${task.pending_action.risk.risk_level}`}>{riskLabel(task.pending_action.risk.risk_level)}</b></div>
        <dl><div><dt>精确目标</dt><dd>{targetLabel(task.pending_action.target_snapshot)}</dd></div><div><dt>风险分数</dt><dd>{task.pending_action.risk.risk_score} / 100</dd></div><div><dt>批量范围</dt><dd>{task.pending_action.proposal.batch_size || 1} 个目标</dd></div><div><dt>可逆性</dt><dd>{task.pending_action.proposal.reversible ? `可逆：${task.pending_action.proposal.rollback_strategy || '已声明回滚'}` : '不可逆；执行器仅允许固定验证策略'}</dd></div></dl>
        <p>{task.pending_action.risk.risk_reason}</p>
        <div className="risk-factors">{asArray(task.pending_action.risk.risk_factors).map((factor, index) => <span key={`${index}-${factor}`}>{factor}</span>)}</div>
        <small>审批 ID：{approval.approval_id}<br />过期：{new Date(approval.expires_at).toLocaleString('zh-CN')}<br />目标快照摘要：{approval.binding.target_snapshot_digest.slice(0, 18)}…</small>
        <div className="approval-actions"><button className="reject" disabled={resolving} onClick={() => resolveApproval('REJECT')}>拒绝并安全结束</button><button className="approve" disabled={resolving} onClick={() => resolveApproval('APPROVE')}>{resolving ? '正在提交' : '批准精确动作'}</button></div>
      </section>}
      {task && terminal(task.state) && <section className={`result-card ${task.state.toLowerCase()}`}><span>执行结果</span><h3>{task.state === 'COMPLETED' ? '完成条件已满足' : '任务已安全结束'}</h3><p>{task.failure_reason || asArray(task.findings).at(-1) || '权威结果已写入持久 Task 与审计链。'}</p><small>{asArray(task.evidence_refs).length} 条证据引用 · Trace {traceIntegrity || '待校验'}</small></section>}
    </section>
    <form className="composer" onSubmit={submit} aria-busy={busy}>
      <label className="sr-only" htmlFor="agent-input">描述希望调查的系统问题</label>
      <div className="composer-main">
        <textarea
          id="agent-input"
          ref={inputRef}
          value={input}
          onChange={event => setInput(event.target.value)}
          onKeyDown={event => {
            if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
              event.preventDefault()
              event.currentTarget.form?.requestSubmit()
            }
          }}
          placeholder="描述你希望调查的系统问题…"
          rows={1}
          maxLength={2000}
          spellCheck={false}
        />
        <button className="send-button" disabled={busy || !active || !input.trim()} aria-label={busy ? '处理中' : '发送'} title={busy ? '处理中' : '发送'}><span>{busy ? '…' : '↑'}</span></button>
      </div>
      <div className="composer-footer"><span className={busy ? 'composer-state busy' : 'composer-state'}>{busy ? '任务运行中' : active ? '安全策略已启用' : '未选择会话'}</span><span className="composer-meta">{input.length}/2000</span></div>
    </form>
  </>

  const filteredServers = asArray(mcpServers).map(server => ({ ...server, tools: asArray(server.tools).filter(tool => !toolSearch.trim() || tool.name.toLowerCase().includes(toolSearch.trim().toLowerCase()) || tool.description.toLowerCase().includes(toolSearch.trim().toLowerCase())) })).filter(server => !toolSearch.trim() || asArray(server.tools).length > 0 || server.manifest.display_name.includes(toolSearch.trim()))
  const rcaEvents = asArray(traceEvents).filter(event => event.type === 'RCA_RESULT' || event.type === 'KNOWLEDGE_RETRIEVED')

  const renderWorkspace = () => <section className="workspace-page">
    {view === 'overview' && <><div className="page-lead"><h2>系统运行全景</h2><p>来自持久 Session/Task、审批 Store 与 MCP Registry 的实时汇总；不是静态演示数字。</p></div>
      <div className="metric-grid">
        <Metric label="MCP 健康" value={`${overview?.mcp.healthy || 0} / ${overview?.mcp.total || 0}`} detail={`${overview?.mcp.tools || 0} 个已发现 Tool`} />
        <Metric label="活动会话" value={String(overview?.sessions.active || 0)} detail={`${overview?.sessions.archived || 0} 个已归档`} />
        <Metric label="等待审批" value={String(overview?.tasks.WAITING_APPROVAL || 0)} detail={`${overview?.approvals.PENDING || 0} 个 Pending Approval`} />
        <Metric label="已完成任务" value={String(overview?.tasks.COMPLETED || 0)} detail={`${overview?.tasks.FAILED || 0} 失败 · ${overview?.tasks.CANCELLED || 0} 取消`} />
      </div>
      <div className="workspace-grid"><section className="workspace-card"><h3>耐久任务状态</h3>{Object.entries(overview?.tasks || {}).map(([state, count]) => <div className="bar-row" key={state}><span>{state}</span><b>{count}</b></div>)}</section><section className="workspace-card"><h3>最近任务</h3>{asArray(tasks).slice(0, 8).map(item => <button className="task-row" key={item.task_id} onClick={() => { setTask(normalizeTask(item)); if (item.task_id) void syncTask(item.task_id); setView('audit') }}><span>{item.objective || item.task_id}</span><b className={`state ${item.state}`}>{item.state}</b></button>)}</section></div>
    </>}
    {view === 'tools' && <><div className="page-lead"><h2>MCP 插件与工具</h2><p>全部来自官方协议 initialize/tools-list；生命周期操作不修改 Linux 业务状态。</p><input className="page-search" value={toolSearch} onChange={event => setToolSearch(event.target.value)} placeholder="搜索 Tool 名称或说明" /></div>
      <div className="server-grid">{filteredServers.map(server => {
        const dependencies = asArray(server.dependency_checks)
        const availableDependencies = dependencies.filter(item => item.available).length
        const discoveries = asArray(server.discovery_history)
        return <section className="server-card" key={server.manifest.id}>
          <div><span>{server.manifest.display_name}</span><b className={`server-status ${server.status}`}>{server.status}</b></div>
          <h3>{server.manifest.id} · v{server.actual_server_version || server.manifest.version}</h3>
          <p>{server.manifest.description}</p>
          <small>{asArray(server.tools).length} tools · {asArray(server.manifest.capabilities).join(' / ')}</small>
          <div className="server-facts"><span>实际实现 <b>{server.actual_server_name || '-'}</b></span><span>依赖 <b>{server.dependencies_checked ? `${availableDependencies} / ${dependencies.length}` : '未检查'}</b></span><span>健康记录 <b>{asArray(server.health_history).length}</b></span></div>
          <details className="server-observability"><summary>依赖与发现历史</summary>
            <div className="dependency-list">{dependencies.length ? dependencies.map(item => <span key={`${item.kind}-${item.name}`} className={item.available ? 'available' : 'missing'}><b>{item.available ? '可用' : '缺失'}</b><code>{item.name || '<empty>'}</code></span>) : <small>Manifest 未声明依赖。</small>}</div>
            <div className="discovery-list">{discoveries.slice(-3).reverse().map(item => <span key={`${item.discovered_at}-${item.tool_set_hash}`}><time>{formatTime(item.discovered_at)}</time><b>v{item.server_version}</b><small>{item.tool_count} tools{item.tools_changed ? ' · 工具集变化' : ''}</small></span>)}</div>
            <small>最近检查：{server.last_checked ? formatTime(server.last_checked) : '尚未检查'}</small>
          </details>
          <div className="tool-list">{asArray(server.tools).map(tool => <details key={tool.name}><summary>{tool.name}</summary><p>{tool.description}</p><code>{tool.schema_hash.slice(0, 16)}…</code></details>)}</div>
          <div className="server-actions"><button onClick={() => changeServer(server, 'health')}>健康检查</button><button onClick={() => changeServer(server, 'rediscover')}>重新发现</button><button onClick={() => changeServer(server, server.manifest.enabled ? 'disable' : 'enable')}>{server.manifest.enabled ? '停用' : '启用'}</button></div>
        </section>
      })}</div>
    </>}
    {view === 'safety' && <><div className="page-lead"><h2>本地安全决策面</h2><p>Tool 自报风险不可信；本地 Policy、Intent Guard、目标快照与执行器重验证才是授权依据。</p></div>
      <div className="boundary-grid"><Metric label="任意命令工具" value="0" detail="未知写能力 fail closed" /><Metric label="待审批" value={String(overview?.approvals.PENDING || 0)} detail="每个动作精确绑定" /><Metric label="执行边界" value="Unix Socket" detail="无公网特权 TCP" /><Metric label="永久清理" value="无 Handler" detail="L3 默认拒绝" /></div>
      <section className="workspace-card"><h3>审批记录</h3>{asArray(approvals).length ? asArray(approvals).slice(0, 20).map(item => <div className="approval-row" key={item.approval_id}><div><span>{item.binding.tool}</span><small>{item.approval_id} · policy {item.binding.policy_version}</small></div><b className={`risk ${item.binding.risk_level}`}>{item.binding.risk_level} · {item.status}</b></div>) : <p className="muted">暂无审批记录</p>}</section>
    </>}
    {view === 'rca' && <><div className="page-lead"><h2>当前任务根因视图</h2><p>展示候选原因、已用证据和缺失证据；D3 不会伪装成确定根因。</p></div>
      <div className="workspace-grid"><section className="workspace-card"><h3>任务发现</h3>{asArray(task?.findings).length ? asArray(task?.findings).map((item, index) => <p className="evidence-item" key={`${index}-${item}`}>{item}</p>) : <p className="muted">选择含 RCA 的任务后显示发现。</p>}<div className="evidence-count">证据引用：{asArray(task?.evidence_refs).length}</div></section><section className="workspace-card"><h3>RCA / Knowledge 事件</h3>{rcaEvents.length ? rcaEvents.map(event => <details className="audit-detail" key={event.sequence}><summary>#{event.sequence} {event.type}</summary><pre>{JSON.stringify(event.data, null, 2)}</pre></details>) : <p className="muted">当前 Trace 尚无 RCA_RESULT。</p>}</section></div>
    </>}
    {view === 'audit' && <><div className="page-lead"><h2>可审计推理轨迹</h2><p>只展示结构化 DecisionRecord、Guard、工具、审批、执行与验证；不保存模型隐藏思维过程。</p><span className={`integrity ${traceIntegrity}`}>{traceIntegrity || '请选择任务'}</span></div>
      <section className="audit-timeline">{asArray(traceEvents).length ? asArray(traceEvents).map(event => <article key={event.sequence}><span>{event.sequence}</span><div><header><b>{event.type}</b><time>{new Date(event.timestamp).toLocaleString('zh-CN')}</time></header><code>{event.event_hash.slice(0, 20)}…</code>{event.data !== undefined && <details className="audit-detail"><summary>结构化事件数据</summary><pre>{JSON.stringify(event.data, null, 2)}</pre></details>}</div></article>) : <p className="muted">从会话或系统概览选择任务后显示完整 Trace。</p>}</section>
    </>}
    {view === 'allowlist' && <><div className="page-lead"><h2>Agent 管控路径</h2><p>配置文件写动作允许触达的目录。保存后 server 立即使用新路径生成审批快照；这些路径只能缩小管理员在执行端配置的范围，因此无需重启 safeops-privexec。</p></div>
      <div className="workspace-grid allowlist-layout"><section className="workspace-card"><h3>当前管控范围</h3><div className="bar-row"><span>写动作流程</span><b>{allowlistConfig?.write_actions_enabled ? '已启用' : '未启用'}</b></div><div className="bar-row"><span>配置文件</span><b>{allowlistConfig?.config_path || '-'}</b></div><div className="bar-row"><span>隔离目录</span><b>{allowlistConfig?.quarantine_root || '-'}</b></div><div className="bar-row"><span>执行端重启</span><b>{allowlistConfig?.requires_executor_restart ? '需要' : '不需要'}</b></div>{asArray(allowlistConfig?.managed_roots).length ? <div className="path-list">{asArray(allowlistConfig?.managed_roots).map(root => <code key={root}>{root}</code>)}</div> : <p className="muted">尚未加载管控路径。</p>}{allowlistMissingRoots.length > 0 && <div className="warning-card"><strong>以下目录当前不存在</strong>{allowlistMissingRoots.map(root => <code key={root}>{root}</code>)}</div>}</section>
        <section className="workspace-card"><h3>选择管控路径</h3><div className="browser-toolbar"><div className="segmented" role="group" aria-label="浏览模式"><button type="button" className={pathBrowserMode === 'read' ? 'active' : ''} onClick={() => switchPathBrowserMode('read')}>只读浏览</button><button type="button" className={pathBrowserMode === 'write' ? 'active' : ''} onClick={() => switchPathBrowserMode('write')}>可写选择</button></div><div className="browser-path"><input aria-label="资源管理器路径" value={pathBrowserPath} onChange={event => setPathBrowserPath(event.target.value)} /><button type="button" disabled={pathBrowserLoading} onClick={() => void loadPathBrowser(pathBrowserPath, pathBrowserMode)}>打开</button></div></div>{pathBrowserError && <p className="browser-error">{pathBrowserError}</p>}{pathBrowser && <div className="file-browser" aria-label="资源管理器视图"><div className="browser-current"><button type="button" disabled={!pathBrowser.parent || pathBrowserLoading} onClick={() => pathBrowser.parent && void loadPathBrowser(pathBrowser.parent, pathBrowserMode)}>上一级</button><code>{pathBrowser.path}</code>{pathBrowserMode === 'write' && pathBrowser.can_select_write && !pathBrowser.write_root_missing && <button type="button" onClick={() => addWritableRoot(pathBrowser.path)}>选择当前目录</button>}</div>{pathBrowser.write_root_missing && <p className="browser-warning">当前可写根目录尚未创建；只能在已存在的可写根目录内新建子目录。</p>}{pathBrowserMode === 'write' && pathBrowser.can_create_child && <div className="new-folder"><input aria-label="新建文件夹名称" value={newFolderName} onChange={event => setNewFolderName(event.target.value)} placeholder="新建文件夹名称" /><button type="button" disabled={pathBrowserLoading || !newFolderName.trim()} onClick={createBrowserDirectory}>新建文件夹</button></div>}<div className="browser-entries">{asArray(pathBrowser.entries).length ? asArray(pathBrowser.entries).map(entry => <div className="browser-entry" key={entry.path}><button type="button" onClick={() => void loadPathBrowser(entry.path, pathBrowserMode)}><span>▣</span><b>{entry.name}</b><small>{entry.mode} · {new Date(entry.modified).toLocaleString('zh-CN')}</small></button>{pathBrowserMode === 'write' && entry.selectable_write && <button type="button" onClick={() => addWritableRoot(entry.path)}>选择</button>}</div>) : <p className="muted">当前目录没有可浏览子目录。</p>}{pathBrowser.truncated && <p className="muted">目录项已截断，请输入更精确路径。</p>}</div></div>}<div className="path-picker" aria-label="候选管控路径">{allowlistCandidates.length ? allowlistCandidates.map(path => {
          const selected = allowlistDraftRoots.includes(path)
          const missing = allowlistMissingRoots.includes(path)
          return <label className={`path-option ${selected ? 'selected' : ''} ${missing ? 'missing' : ''}`} key={path}><input type="checkbox" aria-label={`选择 ${path}`} checked={selected} onChange={() => toggleAllowlistCandidate(path)} /><span><b>{pathName(path)}</b><code>{path}</code><small>{missing ? '当前目录不存在，保存不会创建目录' : selected ? '将纳入 Agent 管控快照范围' : '可选择为 Agent 管控路径'}</small></span></label>
        }) : <p className="muted">尚未加载可选择路径。</p>}</div></section>
        <section className="workspace-card allowlist-editor"><h3>保存预览</h3><form className="settings-form" onSubmit={saveAllowlistConfig}><div className="path-preview" aria-label="管控路径预览"><span>将保存 {allowlistDraftRoots.length} 个管控根</span>{allowlistDraftRoots.length ? allowlistDraftRoots.map(root => <code key={root}>{root}</code>) : <p className="muted">请选择候选路径，或在高级编辑中输入绝对路径。</p>}<small>执行端实际允许根会同时包含隔离目录：</small>{allowlistAllowedPreview.map(root => <code key={`allowed-${root}`} className={root === allowlistConfig?.quarantine_root ? 'quarantine' : ''}>{root}</code>)}</div><label>管控路径<textarea className="path-textarea" value={allowlistText} onChange={event => setAllowlistText(event.target.value)} placeholder="/home&#10;/home/safeops" rows={6} /></label><div className="settings-actions"><button type="button" disabled={savingAllowlist} onClick={() => void loadAllowlistConfig()}>重新加载</button><button type="button" disabled={savingAllowlist || allowlistDraftRoots.length === 0} onClick={() => setAllowlistText('')}>清空选择</button><button disabled={savingAllowlist || allowlistDraftRoots.length === 0}>{savingAllowlist ? '保存中' : '保存路径'}</button></div><small>每行一个绝对路径；必须位于管理员配置的可写路径内，不能使用根目录或与隔离目录重叠。保存不会创建目录。</small></form></section></div>
    </>}
    {view === 'llm' && <><div className="page-lead"><h2>LLM Provider 配置</h2><p>配置 OpenAI-compatible Chat Completions 接口。API key 仅写入后端数据目录，不会通过查询接口回显。</p></div>
      <div className="workspace-grid"><section className="workspace-card"><h3>当前状态</h3><div className="bar-row"><span>Provider</span><b>{llmConfig?.configured ? '已启用' : '未配置'}</b></div><div className="bar-row"><span>来源</span><b>{llmConfig?.source || '-'}</b></div><div className="bar-row"><span>API Key</span><b>{llmConfig?.api_key_configured ? '已保存' : '未保存'}</b></div><div className="bar-row"><span>模型</span><b>{llmConfig?.model || '-'}</b></div>{llmConfig?.updated_at && <p className="muted">更新时间：{new Date(llmConfig.updated_at).toLocaleString('zh-CN')}</p>}</section>
        <section className="workspace-card"><h3>接口参数</h3><form className="settings-form" onSubmit={saveLLMConfig}><label>接口地址<input value={llmForm.base_url} onChange={event => setLLMForm(value => ({ ...value, base_url: event.target.value }))} placeholder="https://api.example.com/v1" /></label><label>API Key<input type="password" value={llmForm.api_key} onChange={event => setLLMForm(value => ({ ...value, api_key: event.target.value }))} placeholder={llmConfig?.api_key_configured ? '留空则保留当前密钥' : '输入 API key'} autoComplete="new-password" /></label><label>模型<input value={llmForm.model} onChange={event => setLLMForm(value => ({ ...value, model: event.target.value }))} placeholder="gpt-4.1-mini 或兼容模型名" /></label><div className="settings-actions"><button type="button" disabled={savingLLM || !llmConfig?.configured} onClick={clearLLMConfig}>清除配置</button><button disabled={savingLLM}>{savingLLM ? '保存中' : '保存并启用'}</button></div><small>保存后，多步骤通用任务会使用该 Provider；CPU/内存确定性纵切片不依赖 LLM。</small></form></section></div>
    </>}
  </section>

  return <div className={`shell ${sidebarOpen ? 'sidebar-open' : ''}`}>
    <aside className="sidebar" aria-label="会话与导航">
      <div className="brand"><span className="brand-mark">S</span><div><strong>SafeOps</strong><small>安全自治运维智能体</small></div></div>
      <button className="new-session" onClick={createSession}>＋ 新建会话</button>
      <div className="session-tools"><input value={search} onChange={event => setSearch(event.target.value)} placeholder="搜索历史会话" aria-label="搜索历史会话" /><button onClick={() => setShowArchived(value => !value)}>{showArchived ? '查看活动' : '查看归档'}</button></div>
      <div className="section-label">{showArchived ? '已归档会话' : '历史会话'}</div>
      <nav>{asArray(sessions).map(item => <div key={item.session_id} className={active?.session_id === item.session_id ? 'session-wrap active' : 'session-wrap'}><button className="session" onClick={() => openSession(item.session_id)}><span>{item.name}</span><small>{formatTime(item.updated_at)}</small><p>{sessionPreview(item)}</p></button><div><button title="重命名" onClick={() => beginRenameSession(item)}>✎</button><button title={item.archived ? '恢复' : '归档'} onClick={() => toggleArchive(item)}>{item.archived ? '↥' : '⌁'}</button></div></div>)}</nav>
      <div className="page-nav" aria-label="主要页面">{(Object.keys(viewTitle) as View[]).map(item => <button key={item} className={view === item ? 'active' : ''} onClick={() => { setView(item); setSidebarOpen(false) }}>{viewTitle[item][0]}</button>)}</div>
      <div className="safety-note"><span>安全边界已启用</span><small>39 个 MCP Tool 均为 L0 只读；写动作只走独立审批与 Unix 执行器</small></div>
    </aside>

    {renameDialog && <div className="dialog-backdrop" role="presentation">
      <form className="rename-dialog" role="dialog" aria-modal="true" aria-labelledby="rename-title" onSubmit={submitRenameSession}>
        <h2 id="rename-title">修改会话名称</h2>
        <label>会话名称<input autoFocus value={renameDialog.name} onChange={event => setRenameDialog(current => current ? { ...current, name: event.target.value } : current)} maxLength={128} /></label>
        <div className="settings-actions"><button type="button" disabled={renamingSession} onClick={() => setRenameDialog(null)}>取消</button><button disabled={renamingSession}>{renamingSession ? '保存中' : '保存'}</button></div>
      </form>
    </div>}

    {confirmDialog && <div className="dialog-backdrop" role="presentation">
      <section className={`confirm-dialog ${confirmDialog.tone === 'danger' ? 'danger' : ''}`} role="dialog" aria-modal="true" aria-labelledby="confirm-title">
        <h2 id="confirm-title">{confirmDialog.title}</h2>
        <p>{confirmDialog.message}</p>
        <div className="settings-actions">
          <button type="button" onClick={() => closeConfirmDialog(false)}>{confirmDialog.cancelLabel || '取消'}</button>
          <button type="button" onClick={() => closeConfirmDialog(true)}>{confirmDialog.confirmLabel}</button>
        </div>
      </section>
    </div>}

    <main className={`conversation ${view !== 'console' ? 'page-mode' : ''}`}>
      <header><button className="sidebar-toggle" onClick={() => setSidebarOpen(true)} aria-label="打开会话列表">☰</button><div><span className="eyebrow">{viewTitle[view][1]}</span><h1>{view === 'console' ? active?.name || '正在加载会话' : viewTitle[view][0]}</h1></div><span className="status"><i /> {overview ? `MCP ${overview.mcp.healthy || 0}/${overview.mcp.total || 0}` : 'MCP 已连接'}</span></header>
      {view === 'console' ? renderConsole() : renderWorkspace()}
      {error && <div className="error global-error">{error}</div>}
    </main>

    <aside className="task-panel" aria-label="当前任务详情">
      <div className="panel-title"><span>当前任务</span><b className={`state ${task?.state || 'IDLE'}`}>{task?.state || '空闲'}</b></div>
      {!task && <p className="muted">发送请求或从系统概览选择任务后，这里会展示计划、证据和工具状态。</p>}
      {task && <>
        <div className="task-id">{task.task_id}</div>
        <h3>计划进度</h3>
        <ol>{asArray(task.plan).map((step, index) => <li key={step.step_id} className={step.state.toLowerCase()}><span>{index + 1}</span><div>{step.description}<small>{step.tool}</small></div></li>)}</ol>
        <h3>证据发现</h3>
        <div className="findings">{asArray(task.findings).length ? asArray(task.findings).map((item, index) => <p key={`${index}-${item}`}>{item}</p>) : <p className="muted">等待新的系统证据</p>}</div>
        <div className="evidence-count">已绑定 {asArray(task.evidence_refs).length} 条 Trace 证据</div>
        {task.failure_reason && <div className="error">{task.failure_reason}</div>}
      </>}
      <div className="trace-legend"><h3>审计追踪 <b>{traceIntegrity || '等待证据'}</b></h3><p>任务、Guard、审批、执行与验证写入 Hash-Chained JSONL。</p>{asArray(traceEvents).slice(-8).map(event => <button className="trace-event" key={event.sequence} onClick={() => setView('audit')}><span>{event.sequence}</span><code>{event.type}</code></button>)}</div>
    </aside>
  </div>
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <section className="metric"><span>{label}</span><strong>{value}</strong><small>{detail}</small></section>
}
