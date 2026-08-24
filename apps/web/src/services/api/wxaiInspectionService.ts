import axios from 'axios';
import { normalizeApiBase } from '@/utils/connection';

const REQUEST_TIMEOUT_MS = 15000;
const RUN_TIMEOUT_MS = 180000;
const TOOL_CALL_CHECK_TIMEOUT_MS = 10 * 60 * 1000;

export type ManagerWxaiInspectionScheduleMode = 'interval' | 'time_points';
export type ManagerWxaiInspectionAutoActionMode = 'none' | 'enable' | 'disable' | 'delete';

export interface ManagerWxaiInspectionScheduleConfig {
  mode?: ManagerWxaiInspectionScheduleMode | string;
  timePoints?: string[];
  intervalMinutes?: number;
  timeZone?: string;
}

export interface WxaiInspectionQuotaWindow {
  id: string;
  labelKey: string;
  labelParams?: Record<string, string | number>;
  usedPercent?: number | null;
  resetAtMs?: number;
  resetLabel?: string;
  limitWindowSeconds?: number | null;
}

export interface WxaiAccountWindowCost {
  accountKey: string;
  windowType: 'weekly' | 'monthly' | 'priority_cycle';
  windowStartAtMs: number;
  windowResetAtMs: number;
  estimatedCost: number;
  inputTokens: number;
  outputTokens: number;
  cachedTokens: number;
  isQuotaExhausted: boolean;
  calculatedAtMs: number;
  createdAtMs: number;
  updatedAtMs: number;
}

export interface WxaiInspectionRun {
  id: number;
  triggerType: string;
  triggerKey?: string;
  targetProvider?: string;
  status: string;
  startedAtMs: number;
  finishedAtMs?: number;
  totalFiles: number;
  probeSetCount: number;
  sampledCount: number;
  disabledCount: number;
  enabledCount: number;
  deleteCount: number;
  disableCount: number;
  enableCount: number;
  reauthCount: number;
  keepCount: number;
  quotaExhaustedCount: number;
  abnormalCount: number;
  error?: string;
  createdAtMs: number;
  updatedAtMs: number;
}

export interface WxaiInspectionResult {
  id: number;
  runId: number;
  accountKey: string;
  fileName: string;
  displayAccount: string;
  authIndex?: string;
  accountId?: string;
  provider: string;
  targetProvider?: string;
  disabled: boolean;
  status?: string;
  state?: string;
  action: string;
  actionReason: string;
  actionStatus?: string;
  executedAction?: string;
  actionError?: string;
  statusCode?: number;
  usedPercent?: number;
  isQuota: boolean;
  error?: string;
  planType?: string | null;
  quotaWindows?: WxaiInspectionQuotaWindow[];
  monthlyLimitCents?: number;
  monthlyUsedCents?: number;
  errorKind?: string;
  errorDetail?: string;
  windowCosts?: WxaiAccountWindowCost[];
  createdAtMs: number;
}

export interface WxaiAccountStatusItem extends WxaiInspectionResult {
  resultCreatedAtMs: number;
  priority?: number;
  originalPriority?: number;
  recoverAtMs?: number;
  accountType?: string;
  weeklyUsedPercent?: number;
  weeklyResetAtMs?: number;
  monthlyUsedPercent?: number;
  monthlyResetAtMs?: number;
  checkedAtMs?: number;
}

export interface WxaiAccountStatusResponse {
  run?: WxaiInspectionRun;
  items: WxaiAccountStatusItem[];
}

export interface WxaiInspectionLog {
  id: number;
  runId: number;
  level: string;
  message: string;
  detail?: unknown;
  createdAtMs: number;
}

export interface WxaiInspectionRunDetail {
  run: WxaiInspectionRun;
  results: WxaiInspectionResult[];
  logs: WxaiInspectionLog[];
}

export interface ManagerWxaiInspectionConfig {
  enabled?: boolean;
  schedule?: ManagerWxaiInspectionScheduleConfig;
  targetType?: string;
  workers?: number;
  deleteWorkers?: number;
  timeout?: number;
  retries?: number;
  /** worker 错峰启动间隔（毫秒）；0=不交错 */
  workerStartStaggerMs?: number;
  /** 全局取账号间隔（毫秒）；0=不限流 */
  accountTakeStaggerMs?: number;
  userAgent?: string;
  usedPercentThreshold?: number;
  sampleSize?: number;
  autoActionMode?: ManagerWxaiInspectionAutoActionMode | string;
  /** Grok2Api Console 同步 */
  grok2apiSyncEnabled?: boolean;
  grok2apiBaseUrl?: string;
  grok2apiAdminUsername?: string;
  /** 仅写入；服务端回显时置空，留空提交=保持原值 */
  grok2apiAdminPassword?: string;
}

export interface WxaiGrok2apiSyncResponse {
  trigger: string;
  synced: number;
  response?: unknown;
}

export interface WxaiInspectionSettingsResponse {
  settings: ManagerWxaiInspectionConfig;
  exists: boolean;
}

export interface WxaiInspectionRunsResponse {
  items: WxaiInspectionRun[];
}

export interface WxaiInspectionActionOutcome {
  resultId?: number;
  accountKey?: string;
  fileName: string;
  displayAccount: string;
  action: string;
  status: string;
  success: boolean;
  error?: string;
}

export interface WxaiInspectionActionsResponse {
  outcomes: WxaiInspectionActionOutcome[];
  detail: WxaiInspectionRunDetail;
}

export interface WxaiManualRefreshRequest {
  accountKey?: string;
  fileName?: string;
  authIndex?: string;
  reason?: string;
}

export interface WxaiToolCallCheckRequest {
  accountKey?: string;
  fileName?: string;
  authIndex?: string;
}

export interface WxaiToolCallCheckResult {
  checkId: string;
  endpoint: string;
  proxySource: 'auth' | 'global' | 'direct' | string;
  proxyMode: 'proxy' | 'direct' | string;
  proxyUrl?: string;
  stream: boolean;
  startedAtMs: number;
  finishedAtMs: number;
  durationMs: number;
  totalMs: number;
  ttfbMs?: number;
  firstTokenMs?: number;
  generationMs?: number;
  statusCode?: number;
  errorCode?: string;
  classification?: 'normal' | 'suspected_degradation' | 'quota_exhausted' | 'unknown' | string;
  qualityLevel?: 'healthy' | 'soft' | 'hard' | 'quota_exhausted' | 'unknown' | string;
  classificationReason?: string;
  outputTokens?: number;
  reasoningTokens?: number;
  thinkingDelta: boolean;
  visibleTokens?: number;
  expectedAnswer?: string;
  answerMatched: boolean;
  outputTokensPerSecond?: number;
  modelAnswer?: string;
  requestBody?: unknown;
  requestHeaders?: Record<string, string>;
  responseHeaders?: Record<string, string[]>;
  responseBody?: string;
  responseBodyTruncated: boolean;
  toolCallDetected: boolean;
  toolCallNames?: string[];
  error?: string;
  cleanupPath?: string;
  cleanupAttempted: boolean;
  cleanupDeleted: boolean;
  cleanupError?: string;
}

export interface WxaiToolCallCheckResponse {
  accountKey: string;
  fileName: string;
  displayAccount: string;
  authIndex?: string;
  result: WxaiToolCallCheckResult;
}

const buildUrl = (base: string, path: string): string => {
  const normalizedBase = normalizeApiBase(base).replace(/\/+$/, '');
  return `${normalizedBase}${path}`;
};

const authHeaders = (managementKey?: string) =>
  managementKey ? { Authorization: `Bearer ${managementKey}` } : undefined;

export const wxaiInspectionApi = {
  getSettings: async (
    base: string,
    managementKey: string | undefined
  ): Promise<WxaiInspectionSettingsResponse> => {
    const response = await axios.get<WxaiInspectionSettingsResponse>(
      buildUrl(base, '/v0/management/wxai-inspection/settings'),
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  saveSettings: async (
    base: string,
    managementKey: string | undefined,
    settings: ManagerWxaiInspectionConfig
  ): Promise<WxaiInspectionSettingsResponse> => {
    const response = await axios.put<WxaiInspectionSettingsResponse>(
      buildUrl(base, '/v0/management/wxai-inspection/settings'),
      { settings },
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  listRuns: async (
    base: string,
    managementKey: string | undefined,
    limit = 30
  ): Promise<WxaiInspectionRunsResponse> => {
    const response = await axios.get<WxaiInspectionRunsResponse>(
      buildUrl(base, '/v0/management/wxai-inspection/runs'),
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey), params: { limit } }
    );
    return response.data;
  },

  getRun: async (
    base: string,
    managementKey: string | undefined,
    runId: number
  ): Promise<WxaiInspectionRunDetail> => {
    const response = await axios.get<WxaiInspectionRunDetail>(
      buildUrl(base, `/v0/management/wxai-inspection/runs/${runId}`),
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  getLatest: async (
    base: string,
    managementKey: string | undefined
  ): Promise<WxaiAccountStatusResponse> => {
    const response = await axios.get<WxaiAccountStatusResponse>(
      buildUrl(base, '/v0/management/wxai-inspection/latest'),
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  run: async (
    base: string,
    managementKey: string | undefined
  ): Promise<WxaiInspectionRunDetail> => {
    const response = await axios.post<WxaiInspectionRunDetail>(
      buildUrl(base, '/v0/management/wxai-inspection/run'),
      { triggerType: 'manual', triggerKey: 'manual' },
      { timeout: RUN_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  refreshAccount: async (
    base: string,
    managementKey: string | undefined,
    payload: WxaiManualRefreshRequest
  ): Promise<WxaiInspectionRunDetail> => {
    const response = await axios.post<WxaiInspectionRunDetail>(
      buildUrl(base, '/v0/management/wxai-inspection/manual-refresh'),
      payload,
      { timeout: RUN_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  runToolCallCheck: async (
    base: string,
    managementKey: string | undefined,
    payload: WxaiToolCallCheckRequest
  ): Promise<WxaiToolCallCheckResponse> => {
    const response = await axios.post<WxaiToolCallCheckResponse>(
      buildUrl(base, '/v0/management/wxai-inspection/tool-call-check'),
      payload,
      { timeout: TOOL_CALL_CHECK_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  syncGrok2api: async (
    base: string,
    managementKey: string | undefined
  ): Promise<WxaiGrok2apiSyncResponse> => {
    const response = await axios.post<WxaiGrok2apiSyncResponse>(
      buildUrl(base, '/v0/management/wxai-inspection/grok2api-sync'),
      {},
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  testGrok2apiConnection: async (
    base: string,
    managementKey: string | undefined,
    payload: { baseUrl: string; username: string; password?: string }
  ): Promise<{ ok: boolean }> => {
    const response = await axios.post<{ ok: boolean }>(
      buildUrl(base, '/v0/management/wxai-inspection/grok2api-test'),
      payload,
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  patchAuthFileFields: async (
    base: string,
    managementKey: string | undefined,
    payload: Record<string, unknown>
  ): Promise<unknown> => {
    const response = await axios.patch(
      buildUrl(base, '/v0/management/auth-files/fields'),
      payload,
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  executeActions: async (
    base: string,
    managementKey: string | undefined,
    runId: number,
    resultIds: number[]
  ): Promise<WxaiInspectionActionsResponse> => {
    const response = await axios.post<WxaiInspectionActionsResponse>(
      buildUrl(base, `/v0/management/wxai-inspection/runs/${runId}/actions`),
      { resultIds },
      { timeout: RUN_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },
};
