import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Select } from '@/components/ui/Select';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import { getUsageServiceErrorCode } from '@/services/api/usageService';
import {
  antigravityInspectionApi,
  type AntigravityAccountWindowCost,
  type AntigravityAccountStatusItem,
  type AntigravityInspectionQuotaWindow,
  type AntigravityTargetProvider,
} from '@/services/api/antigravityInspectionService';
import { useAuthStore } from '@/stores';
import { AgyInspectionModeTabs } from './components/AgyInspectionModeTabs';
import styles from './CodexAccountStatusPage.module.scss';

type AccountStatusFilter = 'all' | 'enabled' | 'quota_exhausted' | 'disabled' | 'unauthorized';
type AccountStatusSortKey = 'accountType' | 'priority';
type SortDirection = 'asc' | 'desc';
type AccountMaskMode = 'masked' | 'full';
type AccountRowAction = 'refresh' | 'toggleDisabled' | 'priority';

type AgyAccountStatusRow = {
  id: number;
  name: string;
  accountType: string | null;
  priority: number | null;
  disabled: boolean;
  action: string;
  actionReason: string;
  actionStatus: string | null;
  statusCode: number | null;
  usedPercent: number | null;
  fiveHourUsedPercent: number | null;
  fiveHourResetAtMs: number | null;
  weeklyUsedPercent: number | null;
  weeklyResetAtMs: number | null;
  monthlyUsedPercent: number | null;
  monthlyResetAtMs: number | null;
  resetAtMs: number | null;
  rateLimitResetCreditsAvailableCount: number | null;
  checkedAtMs: number | null;
  originalPriority: number | null;
  quotaWindows: AntigravityInspectionQuotaWindow[];
  windowCosts: AntigravityAccountWindowCost[];
  error: string | null;
  raw: AntigravityAccountStatusItem;
};

type AgyQuotaItem = { key: string; label: string; usedPercent: number | null; resetAtMs: number | null };

type AgyDisplayWindowCost = Pick<AntigravityAccountWindowCost, 'windowType' | 'windowResetAtMs' | 'estimatedCost'> &
  Partial<Pick<AntigravityAccountWindowCost, 'inputTokens' | 'outputTokens' | 'cachedTokens'>>;

const PAGE_SIZE_OPTIONS = [20, 50, 100, 150];

const tr = (t: TFunction, key: string, fallback: string, options?: Record<string, unknown>) =>
  String(t(key, { defaultValue: fallback, ...(options ?? {}) }));

const normalizePercent = (value: unknown): number | null => {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
};

const formatPercent = (value: number | null) => {
  if (value === null) return '-';
  return `${Math.round(Math.max(0, Math.min(100, value)))}%`;
};

const formatAgyQuotaValue = (usedPercent: number | null, remainingPercent: number | null, t: TFunction) => {
  if (usedPercent === 0) return tr(t, 'antigravity_quota.quota_available', '额度可用');
  return formatPercent(remainingPercent);
};

const formatUsd = (value: number | null | undefined) => {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '$0.00';
  return `$${value.toFixed(2)}`;
};

const formatTokenCount = (value: number | null | undefined) => {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '0';
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(value >= 10_000_000 ? 0 : 1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(value >= 100_000 ? 0 : 1)}K`;
  return String(Math.round(value));
};

const formatWindowCostTokenBreakdown = (cost: AgyDisplayWindowCost) =>
  [cost.inputTokens, cost.outputTokens, cost.cachedTokens].map((value) => formatTokenCount(value)).join(' / ');

const formatWindowCostCachePercent = (cost: AgyDisplayWindowCost) => {
  const inputTokens = typeof cost.inputTokens === 'number' && Number.isFinite(cost.inputTokens) ? cost.inputTokens : 0;
  // cachedTokens is cache-hit total from API (residual + cache_read).
  const cachedTokens = typeof cost.cachedTokens === 'number' && Number.isFinite(cost.cachedTokens) ? cost.cachedTokens : 0;
  if (inputTokens <= 0 || cachedTokens <= 0) return '0%';
  return `${Math.min(100, (cachedTokens / inputTokens) * 100).toFixed(1)}%`;
};

const formatDateTime = (value: number | null, language: string) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString(language, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
};

const formatQuotaResetTime = (value: number | null, language: string, t: TFunction) => {
  if (!value) return tr(t, 'monitoring.agy_account_status_reset_unknown', '重置时间未知');
  return formatDateTime(value, language);
};

const formatFullDateTime = (value: number | null | undefined, language: string) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString(language, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
};

const hasQuotaValue = (usedPercent: number | null, resetAtMs: number | null) =>
  usedPercent !== null || resetAtMs !== null;

const getRemainingPercent = (usedPercent: number | null) => {
  if (usedPercent === null) return null;
  return Math.max(0, Math.min(100, 100 - usedPercent));
};

const getQuotaGradient = (remainingPercent: number | null) => {
  const value = remainingPercent ?? 100;
  if (value <= 5) return 'linear-gradient(90deg, #fb7185, #ef4444)';
  if (value <= 20) return 'linear-gradient(90deg, #fbbf24, #f97316)';
  if (value <= 40) return 'linear-gradient(90deg, #a3e635, #f59e0b)';
  return 'linear-gradient(90deg, #34d399, #22c55e)';
};

const getAccountTypeTone = (accountType: string | null) => {
  const normalized = (accountType || '').toLowerCase();
  if (normalized === 'k12') return 'k12';
  if (normalized === 'plus') return 'plus';
  if (normalized === 'pro5x') return 'pro5x';
  if (normalized === 'pro20x') return 'pro20x';
  return 'free';
};

const getAccountTypeLabel = (accountType: string | null) => (accountType || 'free').toUpperCase();

const maskAccountName = (value: string) => {
  const trimmed = value.trim();
  const atIndex = trimmed.indexOf('@');
  if (atIndex <= 0) return trimmed;
  const name = trimmed.slice(0, atIndex);
  const domain = trimmed.slice(atIndex + 1);
  const domainSuffixIndex = domain.lastIndexOf('.');
  const maskedDomain = domainSuffixIndex > 0 ? `***${domain.slice(domainSuffixIndex)}` : '***';
  const maskedName =
    name.length <= 2
      ? name
      : `${name.slice(0, 2)}${'*'.repeat(Math.min(8, Math.max(3, name.length - 2)))}`;
  return `${maskedName}@${maskedDomain}`;
};

const isAccountAbnormal = (row: AgyAccountStatusRow) =>
  (typeof row.statusCode === 'number' && row.statusCode >= 400) ||
  Boolean(row.error && /(^|\D)(4\d{2}|5\d{2})(\D|$)/.test(row.error));

const isQuotaExhausted = (row: AgyAccountStatusRow) =>
  row.priority === -1 || row.raw.isQuota || row.usedPercent === 100 || row.windowCosts.some((cost) => cost.isQuotaExhausted);

const getStatusTone = (row: AgyAccountStatusRow): 'idle' | 'good' | 'warn' | 'bad' | 'violet' => {
  if (isQuotaExhausted(row)) return 'violet';
  if (isAccountAbnormal(row)) return 'bad';
  if (row.disabled) return 'warn';
  return 'good';
};

const getStatusLabel = (row: AgyAccountStatusRow, t: TFunction) => {
  if (isQuotaExhausted(row)) return tr(t, 'monitoring.codex_account_status_quota_exhausted', '额度耗尽');
  if (isAccountAbnormal(row)) return tr(t, 'monitoring.agy_account_status_abnormal', '账号异常');
  if (row.disabled) return tr(t, 'common.disabled', '已停用');
  return tr(t, 'common.enabled', '已启用');
};

const buildRow = (item: AntigravityAccountStatusItem): AgyAccountStatusRow => ({
  id: item.id,
  name: item.displayAccount || item.fileName || item.accountKey || `#${item.id}`,
  accountType: item.accountType || null,
  priority: typeof item.priority === 'number' ? item.priority : null,
  disabled: item.disabled,
  action: item.executedAction || item.action || '',
  actionReason: item.actionReason || '',
  actionStatus: item.actionStatus || null,
  statusCode: typeof item.statusCode === 'number' ? item.statusCode : null,
  usedPercent: normalizePercent(item.usedPercent),
  fiveHourUsedPercent: normalizePercent(item.fiveHourUsedPercent),
  fiveHourResetAtMs: typeof item.fiveHourResetAtMs === 'number' ? item.fiveHourResetAtMs : null,
  weeklyUsedPercent: normalizePercent(item.weeklyUsedPercent),
  weeklyResetAtMs: typeof item.weeklyResetAtMs === 'number' ? item.weeklyResetAtMs : null,
  monthlyUsedPercent: normalizePercent(item.monthlyUsedPercent),
  monthlyResetAtMs: typeof item.monthlyResetAtMs === 'number' ? item.monthlyResetAtMs : null,
  resetAtMs: typeof item.resetAtMs === 'number' ? item.resetAtMs : null,
  rateLimitResetCreditsAvailableCount:
    typeof item.rateLimitResetCreditsAvailableCount === 'number'
      ? item.rateLimitResetCreditsAvailableCount
      : null,
  checkedAtMs: typeof item.checkedAtMs === 'number' ? item.checkedAtMs : item.resultCreatedAtMs,
  originalPriority: typeof item.originalPriority === 'number' ? item.originalPriority : null,
  quotaWindows: Array.isArray(item.quotaWindows) ? item.quotaWindows : [],
  windowCosts: Array.isArray(item.windowCosts) ? item.windowCosts : [],
  error: item.actionError || item.errorDetail || item.error || null,
  raw: item,
});

const getLoadErrorMessage = (error: unknown, t: TFunction) => {
  const code = getUsageServiceErrorCode(error);
  if (code === 'UNAVAILABLE') {
    return tr(t, 'monitoring.server_codex_inspection_service_unavailable', '服务端巡检服务不可用');
  }
  if (error instanceof Error && error.message) return error.message;
  return tr(t, 'common.unknown_error', '未知错误');
};

const comparePriority = (left: AgyAccountStatusRow, right: AgyAccountStatusRow) => {
  const leftPriority = left.priority ?? Number.NEGATIVE_INFINITY;
  const rightPriority = right.priority ?? Number.NEGATIVE_INFINITY;
  return leftPriority - rightPriority;
};

const compareAccountType = (left: AgyAccountStatusRow, right: AgyAccountStatusRow) => {
  const leftType = getAccountTypeLabel(left.accountType);
  const rightType = getAccountTypeLabel(right.accountType);
  return leftType.localeCompare(rightType, undefined, { numeric: true });
};

const getAccountTextSize = (value: string) => {
  if (value.length >= 42) return 10;
  if (value.length >= 34) return 11;
  if (value.length >= 28) return 12;
  return 13;
};

const getFixedBadgeTextSize = (value: string) => {
  if (value.length >= 8) return 9;
  if (value.length >= 5) return 10;
  return 11;
};

const selectLatestWindowCost = (costs: AntigravityAccountWindowCost[]): AntigravityAccountWindowCost | null => {
  if (costs.length === 0) return null;
  return costs.reduce((latest, candidate) => {
    if (candidate.windowResetAtMs !== latest.windowResetAtMs) {
      return candidate.windowResetAtMs > latest.windowResetAtMs ? candidate : latest;
    }
    return candidate.calculatedAtMs > latest.calculatedAtMs ? candidate : latest;
  });
};

const getAgyQuotaGroup = (window: AntigravityInspectionQuotaWindow): 'claude' | 'gemini' | null => {
  const text = `${window.id || ''} ${window.labelKey || ''}`.toLowerCase();
  if (text.includes('claude') || text.includes('gpt-oss')) return 'claude';
  if (text.includes('gemini')) return 'gemini';
  return null;
};

const getAgyQuotaWindowType = (
  window: AntigravityInspectionQuotaWindow,
  row: AgyAccountStatusRow
): 'five-hour' | 'weekly' | 'monthly' | null => {
  const seconds = typeof window.limitWindowSeconds === 'number' ? window.limitWindowSeconds : null;
  if (seconds !== null && seconds > 0) {
    if (seconds <= 6 * 60 * 60) return 'five-hour';
    if (seconds <= 8 * 24 * 60 * 60) return 'weekly';
    return 'monthly';
  }
  if (typeof window.resetAtMs !== 'number' || window.resetAtMs <= 0) return null;
  const checkedAtMs = row.checkedAtMs || row.raw.resultCreatedAtMs || Date.now();
  const deltaMs = Math.max(0, window.resetAtMs - checkedAtMs);
  if (deltaMs <= 6 * 60 * 60 * 1000) return 'five-hour';
  if (deltaMs <= 8 * 24 * 60 * 60 * 1000) return 'weekly';
  return 'monthly';
};

const getAgyQuotaWindowLabel = (type: 'five-hour' | 'weekly' | 'monthly', t: TFunction) => {
  if (type === 'five-hour') return tr(t, 'monitoring.codex_account_status_five_hour_limit', '5 小时限额');
  if (type === 'weekly') return tr(t, 'monitoring.codex_account_status_weekly_limit', '周限额');
  return tr(t, 'monitoring.codex_account_status_monthly_limit', '月限额');
};

const buildLegacyAgyQuotaGroupItem = (
  row: AgyAccountStatusRow,
  provider: Extract<AntigravityTargetProvider, 'claude' | 'gemini'>,
  t: TFunction
) => ({
  key: provider,
  label: provider === 'gemini'
    ? tr(t, 'monitoring.codex_account_status_monthly_limit', '月限额')
    : tr(t, 'monitoring.codex_account_status_weekly_limit', '周限额'),
  usedPercent: provider === 'gemini' ? row.monthlyUsedPercent ?? row.usedPercent : row.weeklyUsedPercent ?? row.usedPercent,
  resetAtMs:
    provider === 'gemini'
      ? row.monthlyResetAtMs || row.resetAtMs
      : row.weeklyResetAtMs || row.resetAtMs,
});

const buildAgyQuotaItems = (
  windows: AntigravityInspectionQuotaWindow[],
  provider: Extract<AntigravityTargetProvider, 'claude' | 'gemini'>,
  row: AgyAccountStatusRow,
  t: TFunction
) => {
  const groupWindows = windows.filter((window) => getAgyQuotaGroup(window) === provider);
  const byType = new Map<'five-hour' | 'weekly' | 'monthly', AntigravityInspectionQuotaWindow>();
  for (const window of groupWindows) {
    const type = getAgyQuotaWindowType(window, row);
    if (!type) continue;
    const selected = byType.get(type);
    if (!selected) {
      byType.set(type, window);
      continue;
    }
    const current = normalizePercent(window.usedPercent) ?? -1;
    const best = normalizePercent(selected.usedPercent) ?? -1;
    const currentHasReset = typeof window.resetAtMs === 'number' && window.resetAtMs > 0;
    const bestHasReset = typeof selected.resetAtMs === 'number' && selected.resetAtMs > 0;
    if (current > best || (current === best && currentHasReset && !bestHasReset)) byType.set(type, window);
  }
  return (['five-hour', 'weekly', 'monthly'] as const)
    .map((type): AgyQuotaItem | null => {
      const window = byType.get(type);
      if (!window) return null;
      return {
        key: `${provider}-${type}`,
        label: getAgyQuotaWindowLabel(type, t),
        usedPercent: normalizePercent(window.usedPercent),
        resetAtMs: typeof window.resetAtMs === 'number' ? window.resetAtMs : null,
      };
    })
    .filter((item): item is AgyQuotaItem => item !== null);
};

const formatDetailValue = (value: unknown, empty = '-') => {
  if (value === null || value === undefined || value === '') return empty;
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : empty;
  return String(value);
};

const maskDetailValue = (value: unknown, maskMode: AccountMaskMode) => {
  if (maskMode === 'full' || typeof value !== 'string') return value;
  return maskAccountName(value);
};

const getDefaultPriority = (row: AgyAccountStatusRow) => {
  if (row.priority !== null && row.priority !== -1) return row.priority;
  return row.originalPriority ?? row.priority;
};

const renderViewportPortal = (node: ReactNode) => {
  if (typeof document === 'undefined') return node;
  return createPortal(node, document.body);
};

type AgyAccountStatusPageProps = {
  provider: Extract<AntigravityTargetProvider, 'claude' | 'gemini'>;
};

export function AgyAccountStatusPage({ provider }: AgyAccountStatusPageProps) {
  const { t, i18n } = useTranslation();
  const managementKey = useAuthStore((state) => state.managementKey);
  const featureAvailability = usePanelFeatureAvailability();
  const [rows, setRows] = useState<AgyAccountStatusRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState<AccountStatusFilter>('all');
  const [sortKey, setSortKey] = useState<AccountStatusSortKey>('priority');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');
  const [maskMode, setMaskMode] = useState<AccountMaskMode>('full');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(100);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [expandedRowId, setExpandedRowId] = useState<number | null>(null);
  const [rowActionError, setRowActionError] = useState<string | null>(null);
  const [priorityDialogRow, setPriorityDialogRow] = useState<AgyAccountStatusRow | null>(null);
  const [priorityInput, setPriorityInput] = useState('');
  const [prioritySubmitting, setPrioritySubmitting] = useState(false);
  const [rowOperationLoading, setRowOperationLoading] = useState(false);
  const [rowOperationMessage, setRowOperationMessage] = useState('');

  const loadLatestServerRun = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      if (!managementKey) {
        throw new Error(tr(t, 'monitoring.server_codex_inspection_connection_required', '请先连接 Manager Server'));
      }
      const serviceBase = featureAvailability.managerServiceBase;
      if (!serviceBase) {
        throw new Error(tr(t, 'monitoring.server_codex_inspection_service_unavailable', '服务端巡检服务不可用'));
      }

      const detail = await antigravityInspectionApi.getAccountStatusLatest(serviceBase, managementKey, provider);
      setRows(detail.items.map(buildRow));
    } catch (error) {
      setRows([]);
      setLoadError(getLoadErrorMessage(error, t));
    } finally {
      setLoading(false);
    }
  }, [featureAvailability.managerServiceBase, managementKey, provider, t]);

  useEffect(() => {
    if (featureAvailability.checking) return;
    void loadLatestServerRun();
  }, [featureAvailability.checking, loadLatestServerRun]);

  useEffect(() => {
    setPage(1);
  }, [keyword, pageSize, statusFilter]);

  useEffect(() => {
    setExpandedRowId(null);
  }, [keyword, pageSize, statusFilter, sortDirection, sortKey]);

  const filteredRows = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();
    return rows
      .filter((row) => {
        if (normalizedKeyword && !row.name.toLowerCase().includes(normalizedKeyword)) return false;
        if (statusFilter === 'enabled') return !row.disabled && !isAccountAbnormal(row) && !isQuotaExhausted(row);
        if (statusFilter === 'quota_exhausted') return isQuotaExhausted(row);
        if (statusFilter === 'disabled') return row.disabled && !isQuotaExhausted(row);
        if (statusFilter === 'unauthorized') return isAccountAbnormal(row) && !isQuotaExhausted(row);
        return true;
      })
      .sort((left, right) => {
        const base = sortKey === 'priority' ? comparePriority(left, right) : compareAccountType(left, right);
        if (base !== 0) return sortDirection === 'asc' ? base : -base;
        return left.name.localeCompare(right.name, undefined, { numeric: true });
      });
  }, [keyword, rows, sortDirection, sortKey, statusFilter]);

  const totalPages = Math.max(1, Math.ceil(filteredRows.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const pagedRows = filteredRows.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const displayStart = filteredRows.length === 0 ? 0 : (currentPage - 1) * pageSize + 1;
  const displayEnd = Math.min(currentPage * pageSize, filteredRows.length);

  const summary = useMemo(() => {
    const quotaExhausted = rows.filter(isQuotaExhausted).length;
    const enabled = rows.filter((row) => !row.disabled && !isAccountAbnormal(row) && !isQuotaExhausted(row)).length;
    const disabled = rows.filter((row) => row.disabled && !isQuotaExhausted(row)).length;
    const unauthorized = rows.filter((row) => isAccountAbnormal(row) && !isQuotaExhausted(row)).length;
    return { enabled, quotaExhausted, disabled, unauthorized };
  }, [rows]);

  const statusOptions: Array<{ value: AccountStatusFilter; label: string }> = [
    { value: 'all', label: tr(t, 'monitoring.codex_account_status_filter_all', '全部账号') },
    { value: 'enabled', label: tr(t, 'monitoring.codex_account_status_filter_enabled', '已启用') },
    { value: 'quota_exhausted', label: tr(t, 'monitoring.codex_account_status_filter_quota_exhausted', '额度耗尽') },
    { value: 'disabled', label: tr(t, 'monitoring.codex_account_status_filter_disabled', '已停用') },
    { value: 'unauthorized', label: tr(t, 'monitoring.agy_account_status_filter_abnormal', '账号异常') },
  ];

  const updateSort = (nextKey: AccountStatusSortKey) => {
    if (sortKey === nextKey) {
      setSortDirection((value) => (value === 'asc' ? 'desc' : 'asc'));
      return;
    }
    setSortKey(nextKey);
    setSortDirection(nextKey === 'priority' ? 'desc' : 'asc');
  };

  const requireServiceConnection = () => {
    const serviceBase = featureAvailability.managerServiceBase;
    if (!serviceBase || !managementKey) {
      throw new Error(tr(t, 'monitoring.server_codex_inspection_connection_required', '请先连接 Manager Server'));
    }
    return { serviceBase, managementKey };
  };

  const patchAuthFile = async (row: AgyAccountStatusRow, payload: Record<string, unknown>) => {
    const { serviceBase, managementKey: activeManagementKey } = requireServiceConnection();
    await antigravityInspectionApi.patchAuthFileFields(serviceBase, activeManagementKey, {
      name: row.raw.fileName,
      ...payload,
    });
  };

  const runManualRefresh = async (row: AgyAccountStatusRow, reason: string) => {
    const { serviceBase, managementKey: activeManagementKey } = requireServiceConnection();
    await antigravityInspectionApi.refreshAccount(serviceBase, activeManagementKey, {
      accountKey: row.raw.accountKey,
      fileName: row.raw.fileName,
      authIndex: row.raw.authIndex,
      targetProvider: provider,
      reason,
    });
    await loadLatestServerRun();
  };

  const toggleRow = (row: AgyAccountStatusRow) => {
    setExpandedRowId((value) => (value === row.id ? null : row.id));
  };

  const openPriorityDialog = (row: AgyAccountStatusRow) => {
    setRowActionError(null);
    setPriorityInput(String(row.priority ?? getDefaultPriority(row) ?? 0));
    setPriorityDialogRow(row);
  };

  const closePriorityDialog = () => {
    if (prioritySubmitting || rowOperationLoading) return;
    setPriorityDialogRow(null);
    setPriorityInput('');
  };

  const submitPriorityDialog = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!priorityDialogRow || rowOperationLoading) return;
    setRowActionError(null);
    const nextPriority = Number(priorityInput.trim());
    if (!Number.isInteger(nextPriority)) {
      setRowActionError('优先级必须是整数');
      return;
    }
    setPrioritySubmitting(true);
    setRowOperationLoading(true);
    setRowOperationMessage('修改优先级后巡检中...');
    try {
      await patchAuthFile(priorityDialogRow, { [provider === 'claude' ? 'priority_claude' : 'priority_gemini']: nextPriority });
      await runManualRefresh(priorityDialogRow, '手动修改优先级后巡检');
      setPriorityDialogRow(null);
      setPriorityInput('');
    } catch (error) {
      setRowActionError(getLoadErrorMessage(error, t));
    } finally {
      setPrioritySubmitting(false);
      setRowOperationLoading(false);
      setRowOperationMessage('');
    }
  };

  const handleRowAction = async (row: AgyAccountStatusRow, action: AccountRowAction) => {
    if (rowOperationLoading) return;
    setRowActionError(null);
    try {
      if (action === 'refresh') {
        setRowOperationLoading(true);
        setRowOperationMessage('手动刷新巡检中...');
        await runManualRefresh(row, '手动刷新');
        return;
      }
      if (action === 'toggleDisabled') {
        setRowOperationLoading(true);
        setRowOperationMessage(`${row.disabled ? '启用' : '禁用'}后巡检中...`);
        await patchAuthFile(row, { disabled: !row.disabled });
        await runManualRefresh(row, row.disabled ? '手动启用后巡检' : '手动禁用后巡检');
        return;
      }
      openPriorityDialog(row);
    } catch (error) {
      setRowActionError(getLoadErrorMessage(error, t));
    } finally {
      if (action !== 'priority') {
        setRowOperationLoading(false);
        setRowOperationMessage('');
      }
    }
  };

  return (
    <div className={styles.page}>
      <AgyInspectionModeTabs activeMode={provider} />

      <section className={[styles.panel, styles.codexAccountStatusPanel].filter(Boolean).join(' ')}>
        <div className={styles.accountStatusSummaryGrid}>
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_total', '总账号')} value={rows.length} tone="blue" icon={<SummaryTotalIcon size={16} />} />
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_enabled', '已启用')} value={summary.enabled} tone="green" icon={<SummaryEnabledIcon size={16} />} />
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_quota_exhausted', '额度耗尽')} value={summary.quotaExhausted} tone="violet" icon={<SummaryQuotaIcon size={16} />} />
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_disabled', '已停用')} value={summary.disabled} tone="amber" icon={<SummaryDisabledIcon size={16} />} />
          <SummaryCard label={tr(t, 'monitoring.agy_account_status_abnormal', '账号异常')} value={summary.unauthorized} tone="red" icon={<SummaryUnauthorizedIcon size={16} />} />
        </div>

        <div className={styles.accountStatusDataPanel}>
          <div className={styles.accountStatusDataHeader}>
            <div className={styles.accountStatusDataTabs}>
              <input
                className={styles.accountStatusSearch}
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                placeholder={tr(t, 'monitoring.codex_account_status_search', '搜索账号')}
              />
            </div>
            <div className={styles.accountStatusDataActions}>
              <button
                type="button"
                className={styles.accountStatusRefreshButton}
                onClick={() => void loadLatestServerRun()}
                disabled={loading || featureAvailability.checking}
              >
                {loading ? tr(t, 'common.refreshing', '刷新中') : tr(t, 'common.refresh', '刷新')}
              </button>
              <div className={styles.maskToggle} role="group" aria-label={tr(t, 'monitoring.codex_account_status_mask_mode', '账号显示模式')}>
                <button
                  type="button"
                  className={maskMode === 'full' ? styles.activeToggle : ''}
                  onClick={() => setMaskMode('full')}
                >
                  {tr(t, 'common.full', '完整')}
                </button>
                <button
                  type="button"
                  className={maskMode === 'masked' ? styles.activeToggle : ''}
                  onClick={() => setMaskMode('masked')}
                >
                  {tr(t, 'common.masked', '脱敏')}
                </button>
              </div>
              <div
                className={styles.filterPills}
                role="group"
                aria-label={tr(t, 'monitoring.codex_account_status_filter', '账号状态筛选')}
              >
                {statusOptions.map((option) => (
                  <button
                    key={option.value}
                    type="button"
                    className={statusFilter === option.value ? styles.activeToggle : ''}
                    onClick={() => setStatusFilter(option.value)}
                  >
                    {option.label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {loadError ? <div className={styles.accountStatusError}>{loadError}</div> : null}
          {rowActionError ? <div className={styles.accountStatusError}>{rowActionError}</div> : null}

          <div className={styles.accountStatusTableWrap}>
            <table className={styles.accountStatusTable}>
              <colgroup>
                <col className={styles.accountStatusColAccount} />
                <col className={styles.accountStatusColType} />
                <col className={styles.accountStatusColStatus} />
                <col className={styles.accountStatusColPriority} />
                <col className={styles.accountStatusColQuota} />
                <col className={styles.accountStatusColCost} />
                <col className={styles.accountStatusColInfo} />
              </colgroup>
              <thead>
                <tr>
                  <th>{tr(t, 'monitoring.codex_account_status_col_account', '账号')}</th>
                  <SortableHeader
                    label={tr(t, 'monitoring.codex_account_status_col_account_type', '账号类型')}
                    sortKey="accountType"
                    activeSortKey={sortKey}
                    direction={sortDirection}
                    onClick={updateSort}
                  />
                  <th>{tr(t, 'monitoring.codex_account_status_col_state', '状态')}</th>
                  <SortableHeader
                    label={tr(t, 'monitoring.codex_account_status_col_priority', '优先级')}
                    sortKey="priority"
                    activeSortKey={sortKey}
                    direction={sortDirection}
                    onClick={updateSort}
                  />
                  <th>{tr(t, 'monitoring.codex_account_status_col_quota', '额度窗口')}</th>
                  <th>{tr(t, 'monitoring.codex_account_status_col_estimated_cost', '预计花费')}</th>
                  <th>{tr(t, 'monitoring.codex_account_status_col_last_checked', '最后巡检')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredRows.length === 0 ? (
                  <tr>
                    <td colSpan={7} className={styles.accountStatusEmpty}>
                      {loading ? (
                        <div className={styles.accountStatusLoadingState} role="status" aria-live="polite">
                          <span className={styles.accountStatusLoadingOrb} aria-hidden="true">
                            <span />
                            <span />
                            <span />
                          </span>
                          <span className={styles.accountStatusLoadingText}>
                            {tr(t, 'common.loading', '加载中...')}
                          </span>
                        </div>
                      ) : (
                        tr(t, 'monitoring.codex_account_status_empty', '没有可展示的服务端巡检数据')
                      )}
                    </td>
                  </tr>
                ) : (
                  pagedRows.map((row) => (
                    <AccountStatusTableRow
                      key={row.id}
                      row={row}
                      provider={provider}
                      t={t}
                      language={i18n.language}
                      maskMode={maskMode}
                      expanded={expandedRowId === row.id}
                      operationLoading={rowOperationLoading}
                      onToggle={() => {
                        if (!rowOperationLoading) toggleRow(row);
                      }}
                      onAction={(action) => void handleRowAction(row, action)}
                    />
                  ))
                )}
              </tbody>
            </table>
          </div>

          <div className={styles.accountStatusPagination}>
            <span className={styles.accountStatusPaginationInfo}>
              {tr(t, 'monitoring.codex_account_status_pagination', '第 {{page}} / {{pages}} 页，显示 {{start}} - {{end}} / {{total}}', {
                page: currentPage,
                pages: totalPages,
                start: displayStart,
                end: displayEnd,
                total: filteredRows.length,
              })}
            </span>
            <div className={styles.accountStatusPagerControls}>
              <label className={styles.accountStatusPageSizeField}>
                {tr(t, 'monitoring.codex_account_status_page_size_prefix', '每页')}
                <Select
                  className={styles.accountStatusPageSizeSelect}
                  triggerClassName={styles.accountStatusPageSizeSelectTrigger}
                  value={String(pageSize)}
                  options={PAGE_SIZE_OPTIONS.map((option) => ({
                    value: String(option),
                    label: `${option} 条/页`,
                  }))}
                  onChange={(value) => setPageSize(Number(value))}
                  ariaLabel={tr(t, 'monitoring.codex_account_status_page_size_prefix', '每页')}
                  fullWidth={false}
                />
              </label>
              <Button variant="secondary" size="sm" onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={currentPage <= 1}>
                {tr(t, 'monitoring.codex_account_status_pagination_prev', '上一页')}
              </Button>
              <Button variant="secondary" size="sm" onClick={() => setPage((value) => Math.min(totalPages, value + 1))} disabled={currentPage >= totalPages}>
                {tr(t, 'monitoring.codex_account_status_pagination_next', '下一页')}
              </Button>
              <label className={styles.accountStatusPageJumpField}>
                {tr(t, 'monitoring.codex_account_status_page_jump_prefix', '前往')}
                <input
                  className={styles.accountStatusPageJump}
                  type="number"
                  min={1}
                  max={totalPages}
                  value={currentPage}
                  onChange={(event) => {
                    const nextPage = Number(event.target.value);
                    if (Number.isFinite(nextPage)) {
                      setPage(Math.min(totalPages, Math.max(1, nextPage)));
                    }
                  }}
                />
                {tr(t, 'monitoring.codex_account_status_page_jump_suffix', '页')}
              </label>
            </div>
          </div>
        </div>
      </section>

      {rowOperationLoading ? renderViewportPortal(
        <div className={styles.accountStatusOperationOverlay} role="status" aria-live="polite">
          <div className={styles.accountStatusOperationCard}>
            <span className={styles.accountStatusOperationSpinner} aria-hidden="true" />
            <strong>{rowOperationMessage || '巡检中...'}</strong>
            <small>正在写入最近一次服务端巡检记录，请稍候</small>
          </div>
        </div>
      ) : null}

      {priorityDialogRow ? renderViewportPortal(
        <div className={styles.accountStatusDialogBackdrop} onClick={closePriorityDialog} role="presentation">
          <form className={styles.accountStatusPriorityDialog} onSubmit={submitPriorityDialog} onClick={(event) => event.stopPropagation()}>
            <div className={styles.accountStatusPriorityDialogHeader}>
              <span>修改优先级</span>
              <button type="button" onClick={closePriorityDialog} aria-label="关闭" disabled={prioritySubmitting}>×</button>
            </div>
            <div className={styles.accountStatusPriorityDialogBody}>
              <p>{maskMode === 'masked' ? maskAccountName(priorityDialogRow.name) : priorityDialogRow.name}</p>
              <label>
                <span>新优先级</span>
                <input
                  type="number"
                  step={1}
                  value={priorityInput}
                  disabled={prioritySubmitting || rowOperationLoading}
                  onChange={(event) => setPriorityInput(event.target.value)}
                />
              </label>
            </div>
            <div className={styles.accountStatusPriorityDialogActions}>
              <button type="button" onClick={closePriorityDialog} disabled={prioritySubmitting}>取消</button>
              <button type="submit" disabled={prioritySubmitting}>{prioritySubmitting ? '保存中...' : '保存'}</button>
            </div>
          </form>
        </div>
      ) : null}
    </div>
  );
}

function SortableHeader({
  label,
  sortKey,
  activeSortKey,
  direction,
  onClick,
}: {
  label: string;
  sortKey: AccountStatusSortKey;
  activeSortKey: AccountStatusSortKey;
  direction: SortDirection;
  onClick: (key: AccountStatusSortKey) => void;
}) {
  const active = activeSortKey === sortKey;
  return (
    <th>
      <button type="button" className={[styles.sortButton, active ? styles.sortButtonActive : ''].filter(Boolean).join(' ')} onClick={() => onClick(sortKey)}>
        {label}
        <span>{active ? (direction === 'asc' ? '↑' : '↓') : '↕'}</span>
      </button>
    </th>
  );
}

function SummaryTotalIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </svg>
  );
}

function SummaryEnabledIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="9" />
      <path d="m8 12 2.5 2.5L16 9" />
    </svg>
  );
}

function SummaryQuotaIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M7 7h11a3 3 0 0 1 3 3v4a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3v-4a3 3 0 0 1 3-3Z" />
      <path d="M2 11v2" />
      <path d="M12 9v4" />
      <path d="M12 16h.01" />
    </svg>
  );
}

function SummaryDisabledIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="9" />
      <path d="m5.6 5.6 12.8 12.8" />
    </svg>
  );
}

function SummaryUnauthorizedIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z" />
      <path d="M12 8v4" />
      <path d="M12 16h.01" />
    </svg>
  );
}

function SummaryCard({
  label,
  value,
  tone,
  icon,
}: {
  label: string;
  value: number;
  tone: 'blue' | 'green' | 'amber' | 'red' | 'violet';
  icon: ReactNode;
}) {
  return (
    <article className={[styles.summaryCard, styles.accountStatusSummaryCard, styles[`accountStatusTone-${tone}`]].filter(Boolean).join(' ')}>
      <div className={styles.accountStatusSummaryHeader}>
        <span className={styles.accountStatusSummaryIcon} aria-hidden="true">
          {icon}
        </span>
        <span className={styles.accountStatusSummaryLabel}>{label}</span>
      </div>
      <div className={styles.accountStatusSummaryBody}>
        <strong className={styles.accountStatusSummaryValue}>{value}</strong>
      </div>
      <span className={styles.accountStatusSummarySparkline} aria-hidden="true" />
    </article>
  );
}

function AccountStatusTableRow({
  row,
  provider,
  t,
  language,
  maskMode,
  expanded,
  operationLoading,
  onToggle,
  onAction,
}: {
  row: AgyAccountStatusRow;
  provider: Extract<AntigravityTargetProvider, 'claude' | 'gemini'>;
  t: TFunction;
  language: string;
  maskMode: AccountMaskMode;
  expanded: boolean;
  operationLoading: boolean;
  onToggle: () => void;
  onAction: (action: AccountRowAction) => void;
}) {
  const statusTone = getStatusTone(row);
  const displayName = maskMode === 'masked' ? maskAccountName(row.name) : row.name;
  const accountTypeLabel = getAccountTypeLabel(row.accountType);
  const statusLabel = getStatusLabel(row, t);
  const priorityLabel = String(row.priority ?? '-');
  const quotaItems: AgyQuotaItem[] = row.quotaWindows.length > 0
    ? buildAgyQuotaItems(row.quotaWindows, provider, row, t)
    : [buildLegacyAgyQuotaGroupItem(row, provider, t)].filter((item) => hasQuotaValue(item.usedPercent, item.resetAtMs));

  if (quotaItems.length === 0 && hasQuotaValue(row.usedPercent, null)) {
    quotaItems.push({
      key: 'used-percent',
      label: tr(t, 'monitoring.codex_account_status_quota', '额度窗口'),
      usedPercent: row.usedPercent,
      resetAtMs: row.resetAtMs,
    });
  }

  const latestWindowCost = selectLatestWindowCost(row.windowCosts);
  const displayWindowCosts: AgyDisplayWindowCost[] = latestWindowCost
    ? [latestWindowCost]
    : quotaItems.slice(0, 1).map((item, index) => ({
        windowType: item.key,
        windowResetAtMs: item.resetAtMs ?? index,
        estimatedCost: 0,
      }));

  return (
    <>
    <tr
      className={[
        styles.accountStatusClickableRow,
        quotaItems.length <= 1 && row.windowCosts.length <= 1 ? styles.accountStatusSingleLineRow : '',
        expanded ? styles.accountStatusExpandedRow : '',
      ].filter(Boolean).join(' ')}
      onClick={onToggle}
      aria-expanded={expanded}
    >
      <td>
        <div className={styles.accountStatusAccountCell} title={maskMode === 'masked' ? row.name : undefined}>
          <strong style={{ fontSize: getAccountTextSize(displayName) }}>{displayName}</strong>
        </div>
      </td>
      <td>
        <span
          className={[styles.accountTypeBadge, styles[`accountType-${getAccountTypeTone(row.accountType)}`]].filter(Boolean).join(' ')}
          style={{ fontSize: getFixedBadgeTextSize(accountTypeLabel) }}
        >
          {accountTypeLabel}
        </span>
      </td>
      <td>
        <div className={styles.accountStatusStateCell}>
          <span
            className={[styles.statusBadge, styles[`tone-${statusTone}`]].filter(Boolean).join(' ')}
            style={{ fontSize: getFixedBadgeTextSize(statusLabel) }}
          >
            <span className={styles.statusDot} />
            {statusLabel}
          </span>
          {row.error ? <span className={styles.accountStatusErrorText}>{row.error}</span> : null}
        </div>
      </td>
      <td>
        <span className={styles.priorityBadge} style={{ fontSize: getFixedBadgeTextSize(priorityLabel) }}>
          {priorityLabel}
        </span>
      </td>
      <td>
        <div className={styles.accountStatusQuotaList}>
          {quotaItems.length === 0 ? (
            <span className={styles.accountStatusMutedText}>-</span>
          ) : quotaItems.map((item) => {
            const remainingPercent = getRemainingPercent(item.usedPercent);
            const quotaWidth = remainingPercent ?? 0;
            return (
              <div key={item.key} className={styles.accountStatusQuotaItem}>
                <div className={styles.accountStatusQuotaHeader}>
                  <span>
                    {item.label}
                    <small>{formatQuotaResetTime(item.resetAtMs, language, t)}</small>
                  </span>
                  <strong>{formatAgyQuotaValue(item.usedPercent, remainingPercent, t)}</strong>
                </div>
                <div className={styles.accountStatusQuotaMeter}>
                  <span style={{ width: `${quotaWidth}%`, background: getQuotaGradient(remainingPercent) }} />
                </div>
              </div>
            );
          })}
        </div>
      </td>
      <td>
        <div className={styles.accountStatusCostList}>
          {displayWindowCosts.length === 0 ? (
            <span className={styles.accountStatusMutedText}>-</span>
          ) : displayWindowCosts.map((cost) => {
            const tokenBreakdown = formatWindowCostTokenBreakdown(cost);
            const cachePercent = formatWindowCostCachePercent(cost);
            return (
              <span key={`${cost.windowType}-${cost.windowResetAtMs}`} className={styles.accountStatusCostBadge}>
                <span className={styles.accountStatusCostMetric}>
                  <small>
                    {tr(t, 'monitoring.codex_account_status_tokens_label', 'Token')}
                    <span className={styles.accountStatusCostMetricHint}> (I/O/C)</span>
                  </small>
                  <strong>{tokenBreakdown}</strong>
                  <span className={styles.accountStatusCostMetricSubline}>
                    {tr(t, 'monitoring.codex_account_status_cache_percent_label', '缓存')} {cachePercent}
                  </span>
                </span>
                <span className={styles.accountStatusCostMetric}>
                  <small>{tr(t, 'monitoring.codex_account_status_cost_label', '费用')}</small>
                  <strong>{formatUsd(cost.estimatedCost)}</strong>
                </span>
              </span>
            );
          })}
        </div>
      </td>
      <td>
        <div className={styles.accountStatusExtraCell}>
          <span className={styles.accountStatusLastInspection}>{formatDateTime(row.checkedAtMs, language)}</span>
        </div>
      </td>
    </tr>
    {expanded ? (
      <tr className={styles.accountStatusDetailRow}>
        <td colSpan={7}>
          <AccountStatusDetailPanel
            row={row}
            t={t}
            language={language}
            maskMode={maskMode}
            operationLoading={operationLoading}
            onAction={onAction}
          />
        </td>
      </tr>
    ) : null}
    </>
  );
}

function AccountStatusDetailPanel({
  row,
  t,
  language,
  maskMode,
  operationLoading,
  onAction,
}: {
  row: AgyAccountStatusRow;
  t: TFunction;
  language: string;
  maskMode: AccountMaskMode;
  operationLoading: boolean;
  onAction: (action: AccountRowAction) => void;
}) {
  const detailName = maskMode === 'masked' ? maskAccountName(row.name) : row.name;
  const statusLabel = getStatusLabel(row, t);
  const detailFields = [
    { label: 'File Name', value: maskDetailValue(row.raw.fileName, maskMode) },
    { label: 'Account ID', value: maskDetailValue(row.raw.accountId, maskMode) },
    { label: 'Auth Index', value: row.raw.authIndex },
    { label: '可用重置次数', value: row.rateLimitResetCreditsAvailableCount },
    { label: '当前优先级', value: row.priority },
    { label: '默认优先级', value: getDefaultPriority(row) },
  ];

  return (
    <section className={styles.accountStatusDetailPanel} onClick={(event) => event.stopPropagation()}>
      <div className={styles.accountStatusDetailHero}>
        <div>
          <span className={styles.accountStatusDetailEyebrow}>账号详情</span>
          <h3>{detailName}</h3>
        </div>
        <div className={styles.accountStatusDetailBadges}>
          <span className={[styles.accountTypeBadge, styles[`accountType-${getAccountTypeTone(row.accountType)}`]].filter(Boolean).join(' ')}>{getAccountTypeLabel(row.accountType)}</span>
          <span className={[styles.statusBadge, styles[`tone-${getStatusTone(row)}`]].filter(Boolean).join(' ')}><span className={styles.statusDot} />{statusLabel}</span>
          <span className={styles.priorityBadge}>{formatDetailValue(row.priority)}</span>
        </div>
        <div className={styles.accountStatusDetailActions}>
          <button type="button" className={[styles.accountStatusActionButton, styles.accountStatusActionRefresh].filter(Boolean).join(' ')} onClick={() => onAction('refresh')} disabled={operationLoading}>刷新</button>
          <button type="button" className={[styles.accountStatusActionButton, row.disabled ? styles.accountStatusActionEnable : styles.accountStatusActionDisable].filter(Boolean).join(' ')} onClick={() => onAction('toggleDisabled')} disabled={operationLoading}>{row.disabled ? '启用' : '禁用'}</button>
          <button type="button" className={[styles.accountStatusActionButton, styles.accountStatusActionPriority].filter(Boolean).join(' ')} onClick={() => onAction('priority')} disabled={operationLoading}>修改优先级</button>
        </div>
      </div>

      <div className={styles.accountStatusDetailGrid}>
        <div className={styles.accountStatusDetailSection}>
          <h4>基础信息</h4>
          <dl className={styles.accountStatusDetailList}>
            {detailFields.map((field) => (
              <div key={field.label}>
                <dt>{field.label}</dt>
                <dd title={formatDetailValue(field.value)}>{formatDetailValue(field.value)}</dd>
              </div>
            ))}
          </dl>
        </div>

        <div className={styles.accountStatusDetailSection}>
          <h4>执行信息</h4>
          <dl className={styles.accountStatusDetailList}>
            <div><dt>Action</dt><dd>{formatDetailValue(row.action)}</dd></div>
            <div><dt>Action Time</dt><dd>{formatFullDateTime(row.checkedAtMs, language)}</dd></div>
            <div className={styles.accountStatusDetailWideField}><dt>Action Reason</dt><dd title={formatDetailValue(row.actionReason)}>{formatDetailValue(row.actionReason)}</dd></div>
          </dl>
        </div>
      </div>
    </section>
  );
}
