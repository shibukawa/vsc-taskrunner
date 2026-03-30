export type Branch = {
  fullRef: string
  shortName: string
  isRemote: boolean
  commitHash: string
  commitDate?: string
  tasks?: BranchTask[]
  loadError?: string
  fetchedAt?: string
}

export type TaskInputOption = {
  label: string
  value: string
}

export type TaskInput = {
  id: string
  type: 'promptString' | 'pickString'
  description?: string
  default?: string
  options?: TaskInputOption[]
}

export type BranchTask = {
  label: string
  type: string
  group?: string
  dependsOn?: string[]
  dependsOrder?: string
  hidden?: boolean
  background?: boolean
  inputs?: TaskInput[]
  artifact?: boolean
  worktreeDisabled?: boolean
  preRunTasks?: PreRunTask[]
  artifacts?: ArtifactRule[]
  taskFilePath?: string
  resolvedTaskLabels?: string[]
}

export type ArtifactRule = {
  path: string
  format?: string
  nameTemplate?: string
}

export type TaskShell = {
  executable: string
  args?: string[]
}

export type PreRunTask = {
  command: string
  args?: string[]
  cwd?: string
  shell?: TaskShell
}

export type TaskRun = {
  label: string
  dependsOn?: string[]
  dependsOrder?: string
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  startTime?: string
  endTime?: string
  exitCode: number
  logPath?: string
  historical?: boolean
}

export type ArtifactRef = {
  source: string
  dest: string
  format?: string
}

export type ArtifactItem = {
  source: string
  path: string
  downloadUrl: string
  format?: string
  sizeBytes: number
  createdAt: string
  hashSha256: string
}

export type RunMeta = {
  runId: string
  runKey: string
  branch: string
  taskLabel: string
  runNumber: number
  status: 'running' | 'success' | 'failed'
  startTime: string
  endTime?: string
  exitCode: number
  commitHash?: string
  user?: string
  tokenLabel?: string
  inputValues?: Record<string, string>
  tasks?: TaskRun[]
  hasArtifacts?: boolean
  artifacts?: ArtifactRef[]
}

export type RunStartResponse = {
  runId: string
  runKey: string
  branch: string
  taskLabel: string
  runNumber: number
  status: 'running' | 'success' | 'failed'
  startTime: string
  endTime?: string
  exitCode: number
  user?: string
  tokenLabel?: string
  inputValues?: Record<string, string>
  tasks?: TaskRun[]
}

export type MeResponse = {
  authenticated: boolean
  subject?: string
  claims?: Record<string, string>
  canRun?: boolean
  isAdmin?: boolean
  canManageTokens?: boolean
  apiTokensEnabled?: boolean
}

export type APITokenItem = {
  id: string
  label: string
  scopes: string[]
  createdAt: string
  lastUsedAt?: string
  expiresAt: string
  revokedAt?: string
}

export type APITokenCreateResponse = {
  token: string
  item: APITokenItem
}

export type RouteState = {
  branch: string
  task: string
  runNumber: string
  runId: string
  childTask: string
}

export type SSETaskEvent = {
  type: 'task-start' | 'task-line' | 'task-finish' | 'task-skip'
  taskLabel?: string
  line?: string
  status?: string
  exitCode?: number
  startTime?: string
  endTime?: string
}

export type MetricsSnapshot = {
  updatedAt: string
  cpu: {
    percent: number
    timestamp: string
  }
  memory: {
    current: MemoryPoint
    history: MemoryPoint[]
  }
  storage: {
    timestamp: string
    historyBytes: number
    artifactBytes: number
    worktreeBytes: number
    freeBytes: number
  }
}

export type SettingsStoreSummary = {
  backend: string
  localPath?: string
  endpoint?: string
  bucket?: string
  region?: string
  prefix?: string
}

export type SettingsSummary = {
  repository: {
    source: string
    cachePath: string
    accessTokenConfigured: boolean
  }
  auth: {
    noAuth: boolean
    oidcIssuer: string
    apiTokensEnabled: boolean
    apiTokenStore: SettingsStoreSummary
  }
  execution: {
    maxParallelRuns: number
  }
  metrics: {
    enabled: boolean
    cpuInterval: number
    memoryInterval: number
    storageInterval: number
    memoryHistoryWindow: number
  }
  storage: {
    backend: string
    historyDir: string
    historyKeepCount: number
    worktree: {
      keepOnSuccess: number
      keepOnFailure: number
    }
    object: SettingsStoreSummary
  }
}

export type MemoryPoint = {
  timestamp: string
  usedBytes: number
  totalBytes: number
  usedPercent: number
}

export type ANSIStyleState = {
  fg: string | null
  bg: string | null
  bold: boolean
  dim: boolean
  italic: boolean
  underline: boolean
  strike: boolean
  inverse: boolean
}

export type LogSegment = {
  text: string
  class: string[]
  style: Record<string, string>
}
