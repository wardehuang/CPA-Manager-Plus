import axios from 'axios';
import { normalizeApiBase } from '@/utils/connection';

const REQUEST_TIMEOUT_MS = 15000;
const RUN_TIMEOUT_MS = 120000;

export type AntigravityTargetProvider = 'claude' | 'gemini' | 'server';

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
  createdAtMs: number;
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
  isQuotaExhausted: boolean;
  calculatedAtMs: number;
}

export interface AntigravityAccountStatusItem extends AntigravityInspectionResult {
  resultCreatedAtMs: number;
  priority?: number;
  accountType?: string;
  resetAtMs?: number;
  checkedAtMs?: number;
  originalPriority?: number;
  windowCosts?: AntigravityAccountWindowCost[];
}

export interface AntigravityAccountStatusResponse {
  run: AntigravityInspectionRun;
  items: AntigravityAccountStatusItem[];
}

export interface AntigravityInspectionRunsResponse {
  items: AntigravityInspectionRun[];
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
};
