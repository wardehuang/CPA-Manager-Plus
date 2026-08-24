import axios from 'axios';
import { normalizeApiBase } from '@/utils/connection';

const REQUEST_TIMEOUT_MS = 15000;
const RUN_TIMEOUT_MS = 120000;

export type AntigravityTargetProvider = 'claude' | 'gemini' | 'server';
export type ManagerAntigravityInspectionScheduleMode = 'interval' | 'time_points';
export type ManagerAntigravityInspectionAutoActionMode = 'none' | 'enable' | 'disable' | 'delete';

export interface ManagerAntigravityInspectionScheduleConfig {
  mode?: ManagerAntigravityInspectionScheduleMode | string;
  timePoints?: string[];
  intervalMinutes?: number;
  timeZone?: string;
}

export interface ManagerAntigravityInspectionConfig {
  enabled?: boolean;
  schedule?: ManagerAntigravityInspectionScheduleConfig;
  targetType?: string;
  targetProvider?: AntigravityTargetProvider | string;
  workers?: number;
  deleteWorkers?: number;
  timeout?: number;
  retries?: number;
  workerStartStaggerMs?: number;
  accountTakeStaggerMs?: number;
  userAgent?: string;
  usedPercentThreshold?: number;
  sampleSize?: number;
  autoActionMode?: ManagerAntigravityInspectionAutoActionMode | string;
  /** Grok2Api Console 同步（仅 wXAi 服务端巡检使用） */
  grok2apiSyncEnabled?: boolean;
  grok2apiBaseUrl?: string;
  grok2apiAdminUsername?: string;
  /** 仅写入；服务端回显时置空，留空提交=保持原值 */
  grok2apiAdminPassword?: string;
}

export interface AntigravityInspectionRun {
  id: number;
  triggerType: string;
  triggerKey?: string;
  targetProvider?: AntigravityTargetProvider | string;
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
  error?: string;
  createdAtMs: number;
  updatedAtMs: number;
}

export interface AntigravityInspectionResult {
  id: number;
  runId: number;
  accountKey: string;
  fileName: string;
  displayAccount: string;
  authIndex?: string;
  accountId?: string;
  provider: string;
  targetProvider?: AntigravityTargetProvider | string;
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
  quotaWindows?: AntigravityInspectionQuotaWindow[];
  errorKind?: string;
  errorDetail?: string;
  createdAtMs: number;
}

export interface AntigravityInspectionQuotaWindow {
  id: string;
  labelKey: string;
  labelParams?: Record<string, string | number>;
  usedPercent?: number | null;
  resetAtMs?: number;
  resetLabel?: string;
  limitWindowSeconds?: number | null;
}

export interface AntigravityInspectionLog {
  id: number;
  runId: number;
  level: string;
  message: string;
  detail?: unknown;
  createdAtMs: number;
}

export interface AntigravityInspectionRunDetail {
  run: AntigravityInspectionRun;
  results: AntigravityInspectionResult[];
  logs: AntigravityInspectionLog[];
}

export interface AntigravityAccountWindowCost {
  accountKey: string;
  targetProvider: string;
  windowType: string;
  windowStartAtMs: number;
  windowResetAtMs: number;
  estimatedCost: number;
  inputTokens?: number;
  outputTokens?: number;
  cachedTokens?: number;
  isQuotaExhausted: boolean;
  calculatedAtMs: number;
}

export interface AntigravityAccountStatusItem extends AntigravityInspectionResult {
  resultCreatedAtMs: number;
  priority?: number;
  accountType?: string;
  fiveHourUsedPercent?: number;
  fiveHourResetAtMs?: number;
  weeklyUsedPercent?: number;
  weeklyResetAtMs?: number;
  monthlyUsedPercent?: number;
  monthlyResetAtMs?: number;
  rateLimitResetCreditsAvailableCount?: number;
  resetAtMs?: number;
  checkedAtMs?: number;
  originalPriority?: number;
  quotaWindows?: AntigravityInspectionQuotaWindow[];
  windowCosts?: AntigravityAccountWindowCost[];
}

export interface AntigravityAccountStatusResponse {
  run: AntigravityInspectionRun;
  items: AntigravityAccountStatusItem[];
}

export interface AntigravityInspectionRunsResponse {
  items: AntigravityInspectionRun[];
}

export interface AntigravityInspectionActionOutcome {
  resultId?: number;
  accountKey?: string;
  fileName: string;
  displayAccount: string;
  action: string;
  status: string;
  success: boolean;
  error?: string;
}

export interface AntigravityManualRefreshRequest {
  accountKey?: string;
  fileName?: string;
  authIndex?: string;
  targetProvider?: AntigravityTargetProvider;
  reason?: string;
}

export interface AntigravityInspectionActionsResponse {
  outcomes: AntigravityInspectionActionOutcome[];
  detail: AntigravityInspectionRunDetail;
}

export interface AntigravityInspectionSettingsResponse {
  settings: ManagerAntigravityInspectionConfig;
  exists: boolean;
}

const buildUrl = (base: string, path: string): string => {
  const normalized = normalizeApiBase(base).replace(/\/+$/, '');
  return `${normalized}${path}`;
};

const authHeaders = (managementKey?: string) =>
  managementKey ? { Authorization: `Bearer ${managementKey}` } : undefined;

export const antigravityInspectionApi = {
  getAccountStatusLatest: async (
    base: string,
    managementKey: string | undefined,
    provider: AntigravityTargetProvider
  ): Promise<AntigravityAccountStatusResponse> => {
    const response = await axios.get<AntigravityAccountStatusResponse>(
      buildUrl(base, '/v0/management/antigravity-account-status/latest'),
      {
        timeout: REQUEST_TIMEOUT_MS,
        headers: authHeaders(managementKey),
        params: { provider },
      }
    );
    return response.data;
  },

  getSettings: async (
    base: string,
    managementKey: string | undefined,
    provider: AntigravityTargetProvider = 'server'
  ): Promise<AntigravityInspectionSettingsResponse> => {
    const response = await axios.get<AntigravityInspectionSettingsResponse>(
      buildUrl(base, '/v0/management/antigravity-inspection/settings'),
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey), params: { provider } }
    );
    return response.data;
  },

  saveSettings: async (
    base: string,
    managementKey: string | undefined,
    settings: ManagerAntigravityInspectionConfig,
    provider: AntigravityTargetProvider = 'server'
  ): Promise<AntigravityInspectionSettingsResponse> => {
    const response = await axios.put<AntigravityInspectionSettingsResponse>(
      buildUrl(base, '/v0/management/antigravity-inspection/settings'),
      { settings },
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey), params: { provider } }
    );
    return response.data;
  },

  patchAuthFileFields: async (
    base: string,
    managementKey: string | undefined,
    payload: Record<string, unknown>
  ): Promise<unknown> => {
    const response = await axios.patch(buildUrl(base, '/v0/management/auth-files/fields'), payload, {
      timeout: REQUEST_TIMEOUT_MS,
      headers: authHeaders(managementKey),
    });
    return response.data;
  },

  refreshAccount: async (
    base: string,
    managementKey: string | undefined,
    payload: AntigravityManualRefreshRequest
  ): Promise<AntigravityInspectionRunDetail> => {
    const response = await axios.post<AntigravityInspectionRunDetail>(
      buildUrl(base, '/v0/management/antigravity-inspection/manual-refresh'),
      payload,
      { timeout: RUN_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  listRuns: async (
    base: string,
    managementKey: string | undefined,
    limit = 20
  ): Promise<AntigravityInspectionRunsResponse> => {
    const response = await axios.get<AntigravityInspectionRunsResponse>(
      buildUrl(base, '/v0/management/antigravity-inspection/runs'),
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey), params: { limit } }
    );
    return response.data;
  },

  getRun: async (
    base: string,
    managementKey: string | undefined,
    id: number
  ): Promise<AntigravityInspectionRunDetail> => {
    const response = await axios.get<AntigravityInspectionRunDetail>(
      buildUrl(base, `/v0/management/antigravity-inspection/runs/${id}`),
      { timeout: REQUEST_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  run: async (
    base: string,
    managementKey: string | undefined,
    provider: AntigravityTargetProvider
  ): Promise<AntigravityInspectionRunDetail> => {
    const response = await axios.post<AntigravityInspectionRunDetail>(
      buildUrl(base, '/v0/management/antigravity-inspection/run'),
      { targetProvider: provider },
      { timeout: RUN_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },

  executeActions: async (
    base: string,
    managementKey: string | undefined,
    runId: number,
    resultIds: number[]
  ): Promise<AntigravityInspectionActionsResponse> => {
    const response = await axios.post<AntigravityInspectionActionsResponse>(
      buildUrl(base, `/v0/management/antigravity-inspection/runs/${runId}/actions`),
      { resultIds },
      { timeout: RUN_TIMEOUT_MS, headers: authHeaders(managementKey) }
    );
    return response.data;
  },
};
