import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import axios from 'axios';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Select } from '@/components/ui/Select';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import {
  wxaiInspectionApi,
  type WxaiAccountStatusItem,
  type WxaiAccountWindowCost,
  type WxaiInspectionQuotaWindow,
  type WxaiToolCallCheckResponse,
} from '@/services/api/wxaiInspectionService';
import { useAuthStore, useNotificationStore } from '@/stores';
import { WxaiInspectionModeTabs } from './components/WxaiInspectionModeTabs';
import styles from './CodexAccountStatusPage.module.scss';

const readWxaiRowActionError = (error: unknown): string => {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data;
    if (data && typeof data === 'object') {
      const payload = data as { error?: unknown; message?: unknown };
      if (typeof payload.error === 'string' && payload.error.trim()) {
        return payload.error.trim();
      }
      if (typeof payload.message === 'string' && payload.message.trim()) {
        return payload.message.trim();
      }
    }
    if (typeof error.message === 'string' && error.message.trim()) {
      return error.message.trim();
    }
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message.trim();
  }
  return '未知错误';
};

type AccountStatusFilter = 'all' | 'enabled' | 'quota_exhausted' | 'disabled' | 'abnormal';
type AccountStatusSortKey = 'accountType' | 'priority';
type SortDirection = 'asc' | 'desc';
type AccountMaskMode = 'masked' | 'full';
type AccountRowAction = 'refresh' | 'toggleDisabled' | 'priority' | 'toolCallCheck';

type WxaiAccountStatusRow = {
  id: number;
  name: string;
  exitIp: string;
  accountType: string | null;
  priority: number | null;
  scheduleGroup: number | null;
  originalPriority: number | null;
  recoverAtMs: number | null;
  statusCode: number | null;
  weeklyUsedPercent: number | null;
  weeklyResetAtMs: number | null;
  monthlyUsedPercent: number | null;
  monthlyResetAtMs: number | null;
  monthlyLimitCents: number | null;
  monthlyUsedCents: number | null;
  windowCosts: WxaiAccountWindowCost[];
  checkedAtMs: number | null;
  raw: WxaiAccountStatusItem;
};

const PAGE_SIZE_OPTIONS = [20, 50, 100, 150];
const USD_SYMBOL = String.fromCharCode(36);

let cachedLastToolCallCheckResult: WxaiToolCallCheckResponse | null = null;

const normalizeNumber = (value: unknown): number | null =>
  typeof value === 'number' && Number.isFinite(value) ? value : null;

const buildRow = (item: WxaiAccountStatusItem): WxaiAccountStatusRow => ({
  id: item.id,
  name: item.displayAccount || item.fileName || item.accountKey || '#' + item.id,
  exitIp: item.exitIp,
  accountType: item.accountType || item.planType || null,
  priority: normalizeNumber(item.priority),
  scheduleGroup: normalizeNumber(item.scheduleGroup),
  originalPriority: normalizeNumber(item.originalPriority),
  recoverAtMs: normalizeNumber(item.recoverAtMs),
  statusCode: normalizeNumber(item.statusCode),
  weeklyUsedPercent: normalizeNumber(item.weeklyUsedPercent),
  weeklyResetAtMs: normalizeNumber(item.weeklyResetAtMs),
  monthlyUsedPercent: normalizeNumber(item.monthlyUsedPercent),
  monthlyResetAtMs: normalizeNumber(item.monthlyResetAtMs),
  monthlyLimitCents: normalizeNumber(item.monthlyLimitCents),
  monthlyUsedCents: normalizeNumber(item.monthlyUsedCents),
  windowCosts: Array.isArray(item.windowCosts) ? item.windowCosts : [],
  checkedAtMs: normalizeNumber(item.checkedAtMs) ?? normalizeNumber(item.resultCreatedAtMs),
  raw: item,
});

const isOAuthSlowDown = (row: WxaiAccountStatusRow) => {
  if (row.raw.errorKind === 'oauth_slow_down') return true;
  const errorText = [row.raw.errorDetail, row.raw.error, row.raw.actionReason]
    .filter((value): value is string => typeof value === 'string')
    .join(' ')
    .toLowerCase();
  return errorText.includes('slow_down') || errorText.includes('slow down');
};

const isAccountDisabled = (row: WxaiAccountStatusRow) => row.priority === -5;

const isQuotaExhausted = (row: WxaiAccountStatusRow) =>
  !isAccountDisabled(row) &&
  !isOAuthSlowDown(row) &&
  (row.raw.errorKind === 'quota_exhausted' || row.priority === -1);

const isAccountAbnormal = (row: WxaiAccountStatusRow) =>
  !isAccountDisabled(row) && (
    row.priority === -3 || row.priority === -4 || isOAuthSlowDown(row) || (
      !isQuotaExhausted(row) &&
      row.raw.errorKind !== 'no_quota_data' &&
      (row.priority === -2 || Boolean(row.raw.errorKind) || (row.statusCode !== null && row.statusCode >= 400))
    )
  );

const getStatusTone = (row: WxaiAccountStatusRow): 'good' | 'warn' | 'bad' | 'violet' => {
  if (isQuotaExhausted(row)) return 'violet';
  if (isAccountAbnormal(row)) return 'bad';
  if (isAccountDisabled(row)) return 'warn';
  return 'good';
};

const getStatusLabel = (row: WxaiAccountStatusRow) => {
  if (isQuotaExhausted(row)) return '额度耗尽';
  if (isAccountAbnormal(row)) return '账号异常';
  if (isAccountDisabled(row)) return '已停用';
  return '已启用';
};

const getAccountTypeLabel = (accountType: string | null) => accountType?.toUpperCase() || 'UNKNOWN';

const getAccountTypeTone = (accountType: string | null) => {
  const normalizedType = (accountType || '').toLowerCase();
  if (normalizedType.includes('premium') || normalizedType.includes('heavy')) return 'pro20x';
  if (normalizedType.includes('super') || normalizedType.includes('pro')) return 'pro5x';
  if (normalizedType.includes('plus')) return 'plus';
  return 'free';
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

const formatDetailValue = (value: unknown) => {
  if (value === null || value === undefined || value === '') return '-';
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : '-';
  return String(value);
};

const maskAccountName = (value: string) =>
  value.replace(/(^.{2}).*(@.*$)/, (_match, prefix: string, suffix: string) => prefix + '***' + suffix);

const formatCents = (value: number | null | undefined) => {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-';
  return USD_SYMBOL + (value / 100).toFixed(2);
};

const formatPercent = (value: number | null) =>
  value === null ? '-' : Math.round(Math.max(0, Math.min(100, value))) + '%';

const getQuotaGradient = (remainingPercent: number | null) => {
  const value = remainingPercent ?? 100;
  if (value <= 5) return 'linear-gradient(90deg, #fb7185, #ef4444)';
  if (value <= 20) return 'linear-gradient(90deg, #fbbf24, #f97316)';
  if (value <= 40) return 'linear-gradient(90deg, #a3e635, #f59e0b)';
  return 'linear-gradient(90deg, #34d399, #22c55e)';
};

const formatUsd = (value: number | null | undefined) => {
  if (typeof value !== 'number' || !Number.isFinite(value)) return USD_SYMBOL + '0.00';
  return USD_SYMBOL + value.toFixed(2);
};

const formatTokenCount = (value: number | null | undefined) => {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '0';
  if (value >= 1_000_000) return (value / 1_000_000).toFixed(value >= 10_000_000 ? 0 : 1) + 'M';
  if (value >= 1_000) return (value / 1_000).toFixed(value >= 100_000 ? 0 : 1) + 'K';
  return String(Math.round(value));
};

const formatWindowCostTokenBreakdown = (cost: WxaiAccountWindowCost) =>
  [cost.inputTokens, cost.outputTokens, cost.cachedTokens].map(formatTokenCount).join(' / ');

const formatWindowCostCachePercent = (cost: WxaiAccountWindowCost) => {
  const inputTokens = normalizeNumber(cost.inputTokens) ?? 0;
  const cachedTokens = normalizeNumber(cost.cachedTokens) ?? 0;
  if (inputTokens <= 0 || cachedTokens <= 0) return '0%';
  return Math.min(100, (cachedTokens / inputTokens) * 100).toFixed(1) + '%';
};

const sortWindowCosts = (costs: WxaiAccountWindowCost[]) => [...costs].sort((left, right) => {
  const order: Record<string, number> = { weekly: 1, monthly: 2, priority_cycle: 3 };
  return (order[left.windowType] ?? 9) - (order[right.windowType] ?? 9);
});

const sortQuotaWindows = (windows: WxaiInspectionQuotaWindow[]) => [...windows].sort((left, right) => {
  const order: Record<string, number> = { weekly: 1, monthly: 2 };
  return (order[left.id] ?? 9) - (order[right.id] ?? 9);
});

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

const getQuotaItems = (row: WxaiAccountStatusRow): WxaiInspectionQuotaWindow[] => {
  const windows = Array.isArray(row.raw.quotaWindows) ? row.raw.quotaWindows : [];
  if (windows.length > 0) {
    const quotaWindowsWithResetTimes = windows.map((window) => {
      if (window.resetAtMs) return window;
      if (window.id === 'weekly' && row.weeklyResetAtMs !== null) {
        return { ...window, resetAtMs: row.weeklyResetAtMs };
      }
      if (window.id === 'monthly' && row.monthlyResetAtMs !== null) {
        return { ...window, resetAtMs: row.monthlyResetAtMs };
      }
      return window;
    });
    return sortQuotaWindows(quotaWindowsWithResetTimes);
  }

  const fallbackWindows: WxaiInspectionQuotaWindow[] = [];
  if (row.weeklyUsedPercent !== null || row.weeklyResetAtMs !== null) {
    fallbackWindows.push({
      id: 'weekly',
      labelKey: '周限额',
      usedPercent: row.weeklyUsedPercent,
      resetAtMs: row.weeklyResetAtMs ?? undefined,
    });
  }
  if (row.monthlyUsedPercent !== null || row.monthlyResetAtMs !== null) {
    fallbackWindows.push({
      id: 'monthly',
      labelKey: '月限额',
      usedPercent: row.monthlyUsedPercent,
      resetAtMs: row.monthlyResetAtMs ?? undefined,
    });
  }
  return sortQuotaWindows(fallbackWindows);
};

const getRestoredPriority = (row: WxaiAccountStatusRow) => {
  if (!isAccountDisabled(row) && row.priority !== null) return row.priority;
  return row.originalPriority ?? 1;
};

const renderViewportPortal = (node: ReactNode) => {
  if (typeof document === 'undefined') return node;
  return createPortal(node, document.body);
};

export function WxaiInspectionPage() {
  const { i18n } = useTranslation();
  const managementKey = useAuthStore((state) => state.managementKey);
  const featureAvailability = usePanelFeatureAvailability();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const [rows, setRows] = useState<WxaiAccountStatusRow[]>([]);
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
  const [priorityDialogRow, setPriorityDialogRow] = useState<WxaiAccountStatusRow | null>(null);
  const [priorityInput, setPriorityInput] = useState('');
  const [prioritySubmitting, setPrioritySubmitting] = useState(false);
  const [lastToolCallCheckResult, setLastToolCallCheckResult] = useState<WxaiToolCallCheckResponse | null>(
    () => cachedLastToolCallCheckResult
  );
  const [toolCallCheckDialogOpen, setToolCallCheckDialogOpen] = useState(false);
  const [toolCallCheckRow, setToolCallCheckRow] = useState<WxaiAccountStatusRow | null>(null);
  const [toolCallCheckModel, setToolCallCheckModel] = useState('');
  const [rowOperationLoading, setRowOperationLoading] = useState(false);
  const [rowOperationMessage, setRowOperationMessage] = useState('');

  const loadLatest = useCallback(async () => {
    if (!featureAvailability.managerServiceBase || !managementKey) {
      setLoadError('请先连接 Manager Server');
      setLoading(false);
      return;
    }

    setLoading(true);
    setLoadError(null);
    try {
      const response = await wxaiInspectionApi.getLatest(
        featureAvailability.managerServiceBase,
        managementKey
      );
      setRows(response.items.map(buildRow));
    } catch (error) {
      setRows([]);
      setLoadError(error instanceof Error ? error.message : '未知错误');
    } finally {
      setLoading(false);
    }
  }, [featureAvailability.managerServiceBase, managementKey]);

  useEffect(() => {
    if (!featureAvailability.checking) void loadLatest();
  }, [featureAvailability.checking, loadLatest]);

  useEffect(() => {
    setPage(1);
    setExpandedRowId(null);
  }, [keyword, pageSize, statusFilter]);

  const filteredRows = useMemo(() => rows
    .filter((row) => {
      if (keyword.trim() && !row.name.toLowerCase().includes(keyword.trim().toLowerCase())) return false;
      if (statusFilter === 'enabled') {
        return !isAccountDisabled(row) && !isQuotaExhausted(row) && !isAccountAbnormal(row);
      }
      if (statusFilter === 'quota_exhausted') return isQuotaExhausted(row);
      if (statusFilter === 'disabled') {
        return isAccountDisabled(row) && !isQuotaExhausted(row) && !isAccountAbnormal(row);
      }
      return statusFilter !== 'abnormal' || isAccountAbnormal(row);
    })
    .sort((left, right) => {
      const leftValue = sortKey === 'priority'
        ? left.priority ?? Number.NEGATIVE_INFINITY
        : getAccountTypeLabel(left.accountType);
      const rightValue = sortKey === 'priority'
        ? right.priority ?? Number.NEGATIVE_INFINITY
        : getAccountTypeLabel(right.accountType);
      const comparison = typeof leftValue === 'number' && typeof rightValue === 'number'
        ? leftValue - rightValue
        : String(leftValue).localeCompare(String(rightValue), undefined, { numeric: true });
      if (comparison === 0) {
        if (left.raw.fileName < right.raw.fileName) return -1;
        if (left.raw.fileName > right.raw.fileName) return 1;
        return 0;
      }
      return sortDirection === 'asc' ? comparison : -comparison;
    }), [keyword, rows, sortDirection, sortKey, statusFilter]);

  const summary = useMemo(() => {
    const quotaExhausted = rows.filter(isQuotaExhausted).length;
    const abnormal = rows.filter(isAccountAbnormal).length;
    const disabled = rows.filter(
      (row) => isAccountDisabled(row) && !isQuotaExhausted(row) && !isAccountAbnormal(row)
    ).length;
    return {
      quotaExhausted,
      abnormal,
      disabled,
      enabled: rows.length - quotaExhausted - abnormal - disabled,
    };
  }, [rows]);

  const totalPages = Math.max(1, Math.ceil(filteredRows.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const pagedRows = filteredRows.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const statusOptions: Array<{ value: AccountStatusFilter; label: string }> = [
    { value: 'all', label: '全部账号' },
    { value: 'enabled', label: '已启用' },
    { value: 'quota_exhausted', label: '额度耗尽' },
    { value: 'disabled', label: '已停用' },
    { value: 'abnormal', label: '账号异常' },
  ];

  const openLastToolCallCheckResult = () => {
    if (lastToolCallCheckResult) {
      setToolCallCheckDialogOpen(true);
    }
  };

  const handleSort = (nextSortKey: AccountStatusSortKey) => {
    setSortDirection(
      sortKey === nextSortKey
        ? sortDirection === 'asc' ? 'desc' : 'asc'
        : nextSortKey === 'priority' ? 'desc' : 'asc'
    );
    setSortKey(nextSortKey);
  };

  const requireServiceConnection = () => {
    if (!featureAvailability.managerServiceBase || !managementKey) {
      throw new Error('请先连接 Manager Server');
    }
    return {
      serviceBase: featureAvailability.managerServiceBase,
      activeManagementKey: managementKey,
    };
  };

  const patchAccountPriority = async (row: WxaiAccountStatusRow, priority: number) => {
    const { serviceBase, activeManagementKey } = requireServiceConnection();
    await wxaiInspectionApi.patchAuthFileFields(serviceBase, activeManagementKey, {
      name: row.raw.fileName,
      priority,
    });
  };

  const runManualRefresh = async (row: WxaiAccountStatusRow, reason: string) => {
    const { serviceBase, activeManagementKey } = requireServiceConnection();
    await wxaiInspectionApi.refreshAccount(serviceBase, activeManagementKey, {
      accountKey: row.raw.accountKey,
      fileName: row.raw.fileName,
      authIndex: row.raw.authIndex,
      reason,
    });
    await loadLatest();
  };

  const runToolCallCheck = async (row: WxaiAccountStatusRow, model: string) => {
    const { serviceBase, activeManagementKey } = requireServiceConnection();
    return wxaiInspectionApi.runToolCallCheck(serviceBase, activeManagementKey, {
      accountKey: row.raw.accountKey,
      fileName: row.raw.fileName,
      authIndex: row.raw.authIndex,
      model,
    });
  };

  const openToolCallCheckDialog = async (row: WxaiAccountStatusRow) => {
    const { serviceBase, activeManagementKey } = requireServiceConnection();
    const config = await wxaiInspectionApi.getToolCallCheckConfig(serviceBase, activeManagementKey);
    setToolCallCheckModel(config.defaultModel);
    setToolCallCheckRow(row);
  };

  const closeToolCallCheckSetup = () => {
    if (rowOperationLoading) return;
    setToolCallCheckRow(null);
    setToolCallCheckModel('');
  };

  const submitToolCallCheck = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!toolCallCheckRow || rowOperationLoading) return;
    const model = toolCallCheckModel.trim();
    if (!model) {
      showNotification('检测模型不能为空', 'error');
      return;
    }
    setRowOperationLoading(true);
    setRowOperationMessage('降智检测中...');
    try {
      const result = await runToolCallCheck(toolCallCheckRow, model);
      cachedLastToolCallCheckResult = result;
      setLastToolCallCheckResult(result);
      setToolCallCheckRow(null);
      setToolCallCheckModel('');
      setToolCallCheckDialogOpen(true);
    } catch (error) {
      showNotification(readWxaiRowActionError(error), 'error');
    } finally {
      setRowOperationLoading(false);
      setRowOperationMessage('');
    }
  };

  const openPriorityDialog = (row: WxaiAccountStatusRow) => {
    setPriorityInput(String(row.priority ?? getRestoredPriority(row)));
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

    const nextPriority = Number(priorityInput.trim());
    if (!Number.isInteger(nextPriority)) {
      showNotification('优先级必须是整数', 'error');
      return;
    }

    setPrioritySubmitting(true);
    setRowOperationLoading(true);
    setRowOperationMessage('修改优先级后巡检中...');
    try {
      await patchAccountPriority(priorityDialogRow, nextPriority);
      await runManualRefresh(priorityDialogRow, '手动修改优先级后巡检');
      setPriorityDialogRow(null);
      setPriorityInput('');
    } catch (error) {
      showNotification(readWxaiRowActionError(error), 'error');
    } finally {
      setPrioritySubmitting(false);
      setRowOperationLoading(false);
      setRowOperationMessage('');
    }
  };

  const handleRowAction = async (row: WxaiAccountStatusRow, action: AccountRowAction) => {
    if (rowOperationLoading) return;

    if (action === 'priority') {
      openPriorityDialog(row);
      return;
    }

    setRowOperationLoading(true);
    try {
      if (action === 'toolCallCheck') {
        setToolCallCheckDialogOpen(false);
        setRowOperationMessage('读取降智检测配置...');
        await openToolCallCheckDialog(row);
        return;
      }
      if (action === 'refresh') {
        setRowOperationMessage('手动刷新巡检中...');
        await runManualRefresh(row, '手动刷新');
        return;
      }

      const targetPriority = isAccountDisabled(row) ? getRestoredPriority(row) : -5;
      setRowOperationMessage(`${isAccountDisabled(row) ? '启用' : '禁用'}后巡检中...`);
      await patchAccountPriority(row, targetPriority);
      await runManualRefresh(row, isAccountDisabled(row) ? '手动启用后巡检' : '手动禁用后巡检');
    } catch (error) {
      showNotification(readWxaiRowActionError(error), 'error');
    } finally {
      setRowOperationLoading(false);
      setRowOperationMessage('');
    }
  };

  return (
    <div className={styles.page}>
      <WxaiInspectionModeTabs activeMode="status" />
      <section className={[styles.panel, styles.codexAccountStatusPanel].join(' ')}>
        <div className={styles.accountStatusSummaryGrid}>
          <SummaryCard label="总账号" value={rows.length} tone="blue" icon="A" />
          <SummaryCard label="已启用" value={summary.enabled} tone="green" icon="E" />
          <SummaryCard label="额度耗尽" value={summary.quotaExhausted} tone="violet" icon="Q" />
          <SummaryCard label="已停用" value={summary.disabled} tone="amber" icon="D" />
          <SummaryCard label="账号异常" value={summary.abnormal} tone="red" icon="!" />
        </div>

        <div className={styles.accountStatusDataPanel}>
          <div className={styles.accountStatusDataHeader}>
            <input
              className={styles.accountStatusSearch}
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
              placeholder="搜索账号"
            />
            <div className={styles.accountStatusDataActions}>
              <button
                type="button"
                className={styles.accountStatusRefreshButton}
                onClick={() => void loadLatest()}
                disabled={loading}
              >
                {loading ? '刷新中' : '刷新'}
              </button>
              <div className={styles.maskToggle}>
                <button
                  type="button"
                  className={maskMode === 'full' ? styles.activeToggle : ''}
                  onClick={() => setMaskMode('full')}
                >
                  完整
                </button>
                <button
                  type="button"
                  className={maskMode === 'masked' ? styles.activeToggle : ''}
                  onClick={() => setMaskMode('masked')}
                >
                  脱敏
                </button>
              </div>
              <div className={styles.filterPills}>
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

          <div className={styles.accountStatusTableWrap}>
            <table className={styles.accountStatusTable}>
              <colgroup>
                <col className={styles.accountStatusColAccount} />
                <col className={styles.accountStatusColExitIp} />
                <col className={styles.accountStatusColType} />
                <col className={styles.accountStatusColStatus} />
                <col className={styles.accountStatusColPriority} />
                <col className={styles.accountStatusColScheduleGroup} />
                <col className={styles.accountStatusColQuota} />
                <col className={styles.accountStatusColCost} />
                <col className={styles.accountStatusColInfo} />
              </colgroup>
              <thead>
                <tr>
                  <th>账号</th>
                  <th>出口IP</th>
                  <SortableHeader
                    label="账号类型"
                    sortKey="accountType"
                    activeSortKey={sortKey}
                    direction={sortDirection}
                    onClick={handleSort}
                  />
                  <th>状态</th>
                  <SortableHeader
                    label="优先级"
                    sortKey="priority"
                    activeSortKey={sortKey}
                    direction={sortDirection}
                    onClick={handleSort}
                  />
                  <th>调度组</th>
                  <th>额度窗口</th>
                  <th>预计花费</th>
                  <th>最后巡检</th>
                </tr>
              </thead>
              <tbody>
                {pagedRows.length === 0 ? (
                  <tr>
                    <td colSpan={9} className={styles.accountStatusEmpty}>
                      {loading ? (
                        <div className={styles.accountStatusLoadingState} role="status" aria-live="polite">
                          <span className={styles.accountStatusLoadingOrb} aria-hidden="true">
                            <span />
                            <span />
                            <span />
                          </span>
                          <span className={styles.accountStatusLoadingText}>加载中...</span>
                        </div>
                      ) : '没有可展示的 wXAi 巡检数据'}
                    </td>
                  </tr>
                ) : pagedRows.map((row) => (
                  <AccountRow
                    key={row.id}
                    row={row}
                    language={i18n.language}
                    maskMode={maskMode}
                    expanded={expandedRowId === row.id}
                    operationLoading={rowOperationLoading}
                    hasLastToolCallCheckResult={lastToolCallCheckResult !== null}
                    onToggle={() => {
                      if (!rowOperationLoading) {
                        setExpandedRowId((currentId) => currentId === row.id ? null : row.id);
                      }
                    }}
                    onAction={(action) => void handleRowAction(row, action)}
                    onOpenLastToolCallCheckResult={openLastToolCallCheckResult}
                  />
                ))}
              </tbody>
            </table>
          </div>

          <div className={styles.accountStatusPagination}>
            <span className={styles.accountStatusPaginationInfo}>
              第 {currentPage} / {totalPages} 页，显示{' '}
              {filteredRows.length === 0 ? 0 : (currentPage - 1) * pageSize + 1} -{' '}
              {Math.min(currentPage * pageSize, filteredRows.length)} / {filteredRows.length}
            </span>
            <div className={styles.accountStatusPagerControls}>
              <label className={styles.accountStatusPageSizeField}>
                每页
                <Select
                  className={styles.accountStatusPageSizeSelect}
                  triggerClassName={styles.accountStatusPageSizeSelectTrigger}
                  value={String(pageSize)}
                  options={PAGE_SIZE_OPTIONS.map((option) => ({
                    value: String(option),
                    label: option + ' 条/页',
                  }))}
                  onChange={(value) => setPageSize(Number(value))}
                  ariaLabel="每页"
                  fullWidth={false}
                />
              </label>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setPage(Math.max(1, currentPage - 1));
                  setExpandedRowId(null);
                }}
                disabled={currentPage <= 1}
              >
                上一页
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setPage(Math.min(totalPages, currentPage + 1));
                  setExpandedRowId(null);
                }}
                disabled={currentPage >= totalPages}
              >
                下一页
              </Button>
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
          <form
            className={styles.accountStatusPriorityDialog}
            onSubmit={submitPriorityDialog}
            onClick={(event) => event.stopPropagation()}
          >
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

      {toolCallCheckRow ? renderViewportPortal(
        <div className={styles.accountStatusDialogBackdrop} onClick={closeToolCallCheckSetup} role="presentation">
          <form
            className={styles.accountStatusPriorityDialog}
            onSubmit={submitToolCallCheck}
            onClick={(event) => event.stopPropagation()}
          >
            <div className={styles.accountStatusPriorityDialogHeader}>
              <span>降智检测</span>
              <button type="button" onClick={closeToolCallCheckSetup} aria-label="关闭" disabled={rowOperationLoading}>×</button>
            </div>
            <div className={styles.accountStatusPriorityDialogBody}>
              <p>{maskMode === 'masked' ? maskAccountName(toolCallCheckRow.name) : toolCallCheckRow.name}</p>
              <label>
                <span>检测模型</span>
                <input
                  type="text"
                  value={toolCallCheckModel}
                  disabled={rowOperationLoading}
                  onChange={(event) => setToolCallCheckModel(event.target.value)}
                  autoFocus
                />
              </label>
              <small className={styles.accountStatusToolCallNotice}>
                默认读取 xAI IP Switcher 的 qualityProbeModel；判定阈值与 Realtime Guard 保持一致。
              </small>
            </div>
            <div className={styles.accountStatusPriorityDialogActions}>
              <button type="button" onClick={closeToolCallCheckSetup} disabled={rowOperationLoading}>取消</button>
              <button type="submit" disabled={rowOperationLoading}>开始检测</button>
            </div>
          </form>
        </div>
      ) : null}

      {toolCallCheckDialogOpen && lastToolCallCheckResult ? renderViewportPortal(
        <ToolCallCheckResultDialog
          result={lastToolCallCheckResult}
          language={i18n.language}
          maskMode={maskMode}
          onClose={() => setToolCallCheckDialogOpen(false)}
        />
      ) : null}
    </div>
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
    <article
      className={[
        styles.summaryCard,
        styles.accountStatusSummaryCard,
        styles['accountStatusTone-' + tone],
      ].join(' ')}
    >
      <div className={styles.accountStatusSummaryHeader}>
        <span className={styles.accountStatusSummaryIcon}>{icon}</span>
        <span className={styles.accountStatusSummaryLabel}>{label}</span>
      </div>
      <strong>{value}</strong>
    </article>
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
  return (
    <th>
      <button
        type="button"
        className={[
          styles.sortButton,
          activeSortKey === sortKey ? styles.sortButtonActive : '',
        ].join(' ')}
        onClick={() => onClick(sortKey)}
      >
        {label}
        <span>{activeSortKey === sortKey ? direction === 'asc' ? '↑' : '↓' : '↕'}</span>
      </button>
    </th>
  );
}

function AccountRow({
  row,
  language,
  maskMode,
  expanded,
  operationLoading,
  hasLastToolCallCheckResult,
  onToggle,
  onAction,
  onOpenLastToolCallCheckResult,
}: {
  row: WxaiAccountStatusRow;
  language: string;
  maskMode: AccountMaskMode;
  expanded: boolean;
  operationLoading: boolean;
  hasLastToolCallCheckResult: boolean;
  onToggle: () => void;
  onAction: (action: AccountRowAction) => void;
  onOpenLastToolCallCheckResult: () => void;
}) {
  const displayName = maskMode === 'masked' ? maskAccountName(row.name) : row.name;
  const quotaItems = getQuotaItems(row);
  const statusLabel = getStatusLabel(row);
  const windowCosts = sortWindowCosts(row.windowCosts);
  const isSingleLineRow = quotaItems.length <= 1 && windowCosts.length <= 1;
  const cooldownTime = isQuotaExhausted(row) ? row.recoverAtMs : null;

  return (
    <>
      <tr
        className={[
          styles.accountStatusClickableRow,
          isSingleLineRow ? styles.accountStatusSingleLineRow : '',
          expanded ? styles.accountStatusExpandedRow : '',
        ].filter(Boolean).join(' ')}
        onClick={onToggle}
        aria-expanded={expanded}
      >
        <td>
          <div
            className={[
              styles.accountStatusAccountCell,
              styles.wxaiAccountStatusAccountCell,
            ].join(' ')}
          >
            <strong style={{ fontSize: getAccountTextSize(displayName) }}>{displayName}</strong>
            {cooldownTime !== null ? (
              <small
                className={styles.accountStatusCooldownTime}
                title={'冷却截止：' + formatFullDateTime(cooldownTime, language)}
              >
                冷却至 {formatDateTime(cooldownTime, language)}
              </small>
            ) : null}
          </div>
        </td>
        <td>
          <span className={styles.accountStatusExitIp} title={row.exitIp}>
            {row.exitIp}
          </span>
        </td>
        <td>
          <span
            className={[
              styles.accountTypeBadge,
              styles['accountType-' + getAccountTypeTone(row.accountType)],
            ].join(' ')}
            style={{ fontSize: getFixedBadgeTextSize(getAccountTypeLabel(row.accountType)) }}
          >
            {getAccountTypeLabel(row.accountType)}
          </span>
        </td>
        <td>
          <div className={styles.accountStatusStateCell}>
            <span
              className={[
                styles.statusBadge,
                styles['tone-' + getStatusTone(row)],
              ].join(' ')}
              style={{ fontSize: getFixedBadgeTextSize(statusLabel) }}
            >
              <span className={styles.statusDot} />
              {statusLabel}
            </span>
          </div>
        </td>
        <td>
          <span
            className={styles.priorityBadge}
            style={{ fontSize: getFixedBadgeTextSize(String(row.priority ?? '-')) }}
          >
            {row.priority ?? '-'}
          </span>
        </td>
        <td>
          <span
            className={styles.priorityBadge}
            style={{ fontSize: getFixedBadgeTextSize(String(row.scheduleGroup ?? '-')) }}
          >
            {row.scheduleGroup ?? '-'}
          </span>
        </td>
        <td>
          <div className={styles.accountStatusQuotaList}>
            {quotaItems.length === 0 ? (
              <span className={styles.accountStatusNoQuotaState}>
                <span aria-hidden="true">—</span>
                暂无额度
                <span aria-hidden="true">—</span>
              </span>
            ) : quotaItems.map((item) => {
              const usedPercent = normalizeNumber(item.usedPercent);
              const remainingPercent = usedPercent === null
                ? null
                : Math.max(0, Math.min(100, 100 - usedPercent));
              return (
                <div key={item.id} className={styles.accountStatusQuotaItem}>
                  <div className={styles.accountStatusQuotaHeader}>
                    <span>
                      {item.labelKey}
                      <small>{formatDateTime(normalizeNumber(item.resetAtMs), language)}</small>
                    </span>
                    <strong>{formatPercent(remainingPercent)}</strong>
                  </div>
                  <div className={styles.accountStatusQuotaMeter}>
                    <span
                      style={{
                        width: (remainingPercent ?? 0) + '%',
                        background: getQuotaGradient(remainingPercent),
                      }}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </td>
        <td>
          <div className={styles.accountStatusCostList}>
            {windowCosts.length === 0 ? (
              <span className={styles.accountStatusMutedText}>-</span>
            ) : windowCosts.map((cost) => {
              const tokenBreakdown = formatWindowCostTokenBreakdown(cost);
              const cachePercent = formatWindowCostCachePercent(cost);
              return (
                <span
                  key={cost.windowType + '-' + cost.windowResetAtMs}
                  className={styles.accountStatusCostBadge}
                  aria-label={cost.windowType + ' Token ' + tokenBreakdown + ' ' + formatUsd(cost.estimatedCost)}
                >
                  <span className={styles.accountStatusCostMetric}>
                    <small>
                      Token <span className={styles.accountStatusCostMetricHint}>(I/O/C)</span>
                    </small>
                    <strong>{tokenBreakdown}</strong>
                    <span className={styles.accountStatusCostMetricSubline}>缓存 {cachePercent}</span>
                  </span>
                  <span className={styles.accountStatusCostMetric}>
                    <small>费用</small>
                    <strong>{formatUsd(cost.estimatedCost)}</strong>
                  </span>
                </span>
              );
            })}
          </div>
        </td>
        <td>
          <div className={styles.accountStatusExtraCell}>
            <span className={styles.accountStatusLastInspection}>
              {formatDateTime(row.checkedAtMs, language)}
            </span>
          </div>
        </td>
      </tr>
      {expanded ? (
        <tr className={styles.accountStatusDetailRow}>
          <td colSpan={9}>
            <AccountDetailPanel
              row={row}
              language={language}
              maskMode={maskMode}
              operationLoading={operationLoading}
              hasLastToolCallCheckResult={hasLastToolCallCheckResult}
              onAction={onAction}
              onOpenLastToolCallCheckResult={onOpenLastToolCallCheckResult}
            />
          </td>
        </tr>
      ) : null}
    </>
  );
}

function AccountDetailPanel({
  row,
  language,
  maskMode,
  operationLoading,
  hasLastToolCallCheckResult,
  onAction,
  onOpenLastToolCallCheckResult,
}: {
  row: WxaiAccountStatusRow;
  language: string;
  maskMode: AccountMaskMode;
  operationLoading: boolean;
  hasLastToolCallCheckResult: boolean;
  onAction: (action: AccountRowAction) => void;
  onOpenLastToolCallCheckResult: () => void;
}) {
  const displayName = maskMode === 'masked' ? maskAccountName(row.name) : row.name;
  const fileName = maskMode === 'masked' ? maskAccountName(row.raw.fileName) : row.raw.fileName;
  const accountId = maskMode === 'masked' && row.raw.accountId
    ? maskAccountName(row.raw.accountId)
    : row.raw.accountId;
  const detailFields = [
    { label: 'File Name', value: fileName },
    { label: 'Account ID', value: accountId },
    { label: 'Auth Index', value: row.raw.authIndex },
    { label: '出口IP', value: row.exitIp },
    { label: 'HTTP Status', value: row.statusCode },
    { label: '当前优先级', value: row.priority },
    { label: '原始优先级', value: row.originalPriority },
    { label: '月额度', value: formatCents(row.monthlyLimitCents) },
    { label: '月已使用', value: formatCents(row.monthlyUsedCents) },
  ];

  return (
    <section className={styles.accountStatusDetailPanel} onClick={(event) => event.stopPropagation()}>
      <div className={styles.accountStatusDetailHero}>
        <div>
          <span className={styles.accountStatusDetailEyebrow}>账号详情</span>
          <h3>{displayName}</h3>
        </div>
        <div className={styles.accountStatusDetailBadges}>
          <span
            className={[
              styles.accountTypeBadge,
              styles['accountType-' + getAccountTypeTone(row.accountType)],
            ].join(' ')}
          >
            {getAccountTypeLabel(row.accountType)}
          </span>
          <span className={[styles.statusBadge, styles['tone-' + getStatusTone(row)]].join(' ')}>
            <span className={styles.statusDot} />
            {getStatusLabel(row)}
          </span>
          <span className={styles.priorityBadge}>{formatDetailValue(row.priority)}</span>
        </div>
        <div className={styles.accountStatusDetailActions}>
          <button
            type="button"
            className={[styles.accountStatusActionButton, styles.accountStatusActionRefresh].join(' ')}
            onClick={() => onAction('refresh')}
            disabled={operationLoading}
          >
            刷新
          </button>
          <button
            type="button"
            className={[
              styles.accountStatusActionButton,
              isAccountDisabled(row) ? styles.accountStatusActionEnable : styles.accountStatusActionDisable,
            ].join(' ')}
            onClick={() => onAction('toggleDisabled')}
            disabled={operationLoading}
          >
            {isAccountDisabled(row) ? '启用' : '禁用'}
          </button>
          <button
            type="button"
            className={[styles.accountStatusActionButton, styles.accountStatusActionPriority].join(' ')}
            onClick={() => onAction('priority')}
            disabled={operationLoading}
          >
            修改优先级
          </button>
          <button
            type="button"
            className={[styles.accountStatusActionButton, styles.accountStatusActionToolCall].join(' ')}
            onClick={() => onAction('toolCallCheck')}
            disabled={operationLoading}
          >
            降智检测
          </button>
          <button
            type="button"
            className={[styles.accountStatusActionButton, styles.accountStatusActionToolCall].join(' ')}
            onClick={(event) => {
              event.stopPropagation();
              onOpenLastToolCallCheckResult();
            }}
            disabled={!hasLastToolCallCheckResult}
          >
            上次检测结果
          </button>
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
          <h4>巡检信息</h4>
          <dl className={styles.accountStatusDetailList}>
            <div><dt>巡检时间</dt><dd>{formatFullDateTime(row.checkedAtMs, language)}</dd></div>
            <div><dt>Error Kind</dt><dd>{formatDetailValue(row.raw.errorKind)}</dd></div>
            <div className={styles.accountStatusDetailWideField}>
              <dt>Error Detail</dt>
              <dd title={formatDetailValue(row.raw.errorDetail)}>{formatDetailValue(row.raw.errorDetail)}</dd>
            </div>
            <div className={styles.accountStatusDetailWideField}>
              <dt>Action Reason</dt>
              <dd title={formatDetailValue(row.raw.actionReason)}>{formatDetailValue(row.raw.actionReason)}</dd>
            </div>
          </dl>
        </div>
      </div>
    </section>
  );
}

const formatToolCallCheckValue = (value: unknown): string => {
  if (value === null || value === undefined || value === '') return '-';
  if (typeof value === 'string') {
    if (!value.trim()) return '-';
    try {
      const parsedValue: unknown = JSON.parse(value);
      return JSON.stringify(parsedValue, null, 2);
    } catch {
      return value;
    }
  }
  return JSON.stringify(value, null, 2) || '-';
};

function ToolCallCheckResultDialog({
  result,
  language,
  maskMode,
  onClose,
}: {
  result: WxaiToolCallCheckResponse;
  language: string;
  maskMode: AccountMaskMode;
  onClose: () => void;
}) {
  const checkResult = result.result;
  const statusCode = checkResult.statusCode ?? 0;
  const classification = checkResult.classification ?? 'unknown';
  const qualityLevel = checkResult.qualityLevel ?? 'unknown';
  const totalMilliseconds = checkResult.totalMs ?? checkResult.durationMs;
  const displayAccount = maskMode === 'masked'
    ? maskAccountName(result.displayAccount)
    : result.displayAccount;
  const statusLabel = classification === 'normal'
    ? '正常'
    : classification === 'suspected_degradation'
      ? qualityLevel === 'hard' ? '疑似降智（硬阈值）' : '疑似降智（软阈值）'
      : classification === 'quota_exhausted'
        ? '额度耗尽'
        : checkResult.error
          ? '请求失败，无法判定'
          : '无法判定';
  const statusClassName = classification === 'normal'
    ? styles.accountStatusToolCallStatusGood
    : classification === 'quota_exhausted' || checkResult.error || statusCode >= 400 || qualityLevel === 'hard'
      ? styles.accountStatusToolCallStatusBad
      : styles.accountStatusToolCallStatusWarn;
  const responseHeaders = checkResult.responseHeaders && Object.keys(checkResult.responseHeaders).length > 0
    ? checkResult.responseHeaders
    : '-';
  const requestHeaders = checkResult.requestHeaders && Object.keys(checkResult.requestHeaders).length > 0
    ? checkResult.requestHeaders
    : '-';
  const formatMilliseconds = (value?: number) => typeof value === 'number' && value >= 0 ? `${value} ms` : '-';
  const formatTokensPerSecond = (value?: number) => typeof value === 'number' ? value.toFixed(2) : '-';
  const responseType = checkResult.toolCallOnly
    ? '纯工具调用'
    : checkResult.toolCallDetected
      ? '正文 + 工具调用'
      : '正文';

  return (
    <div
      className={styles.accountStatusDialogBackdrop}
      onClick={onClose}
      role="presentation"
    >
      <section
        className={styles.accountStatusToolCallDialog}
        onClick={(event) => event.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-labelledby="wxai-tool-call-check-title"
      >
        <div className={styles.accountStatusToolCallDialogHeader}>
          <div>
            <span className={styles.accountStatusDetailEyebrow}>wXAi Stream Check</span>
            <h3 id="wxai-tool-call-check-title">降智检测结果</h3>
            <p>{displayAccount || result.fileName}</p>
            <small className={styles.accountStatusToolCallNotice}>
              使用 xAI IP Switcher 当前 Realtime Guard 阈值判定
            </small>
          </div>
          <button type="button" onClick={onClose} aria-label="关闭">×</button>
        </div>

        <div className={styles.accountStatusToolCallDialogBody}>
          <div className={styles.accountStatusToolCallSummary}>
            <span className={[styles.accountStatusToolCallStatus, statusClassName].join(' ')}>
              {statusLabel}
            </span>
            <span>HTTP {statusCode || '-'}</span>
            <span>首字节 TTFB {formatMilliseconds(checkResult.ttfbMs)}</span>
            <span>首生成 {formatMilliseconds(checkResult.firstTokenMs)}</span>
            <span>生成耗时 {formatMilliseconds(checkResult.generationMs)}</span>
            <span>Total {formatMilliseconds(totalMilliseconds)}</span>
            <span>TPS {formatTokensPerSecond(checkResult.outputTokensPerSecond)}</span>
          </div>

          <dl className={styles.accountStatusToolCallMetaGrid}>
            <div><dt>Check ID</dt><dd>{checkResult.checkId}</dd></div>
            <div><dt>检测时间</dt><dd>{formatFullDateTime(checkResult.startedAtMs, language)}</dd></div>
            <div className={styles.accountStatusToolCallMetaWide}><dt>Endpoint</dt><dd>{checkResult.endpoint}</dd></div>
            <div><dt>模型</dt><dd>{checkResult.model}</dd></div>
            <div><dt>流式请求</dt><dd>{checkResult.stream ? 'stream=true' : 'stream=false'}</dd></div>
            <div><dt>代理来源</dt><dd>{checkResult.proxySource}</dd></div>
            <div><dt>代理地址</dt><dd>{checkResult.proxyUrl || '直连'}</dd></div>
            <div><dt>质量等级</dt><dd>{qualityLevel}</dd></div>
            <div><dt>判定原因</dt><dd>{checkResult.classificationReason || '-'}</dd></div>
            <div><dt>Output tokens</dt><dd>{checkResult.outputTokens ?? '-'}</dd></div>
            <div><dt>Reasoning tokens</dt><dd>{checkResult.reasoningTokens ?? '-'}</dd></div>
            <div><dt>判定 tokens</dt><dd>{checkResult.evaluatedTokens}</dd></div>
            <div><dt>Visible tokens</dt><dd>{checkResult.visibleTokens ?? '-'}</dd></div>
            <div><dt>是否有 thinking_delta</dt><dd>{checkResult.thinkingDelta ? '是' : '否'}</dd></div>
            <div><dt>真实 Thinking</dt><dd>{checkResult.isRealThinking ? '是' : '否'}</dd></div>
            <div><dt>Thinking 原因</dt><dd>{checkResult.realThinkingReason || '-'}</dd></div>
            <div><dt>Summary 字符</dt><dd>{checkResult.summaryChars}</dd></div>
            <div><dt>Encrypted</dt><dd>{checkResult.encryptedBytes}/{checkResult.encryptedFloor} bytes</dd></div>
            <div><dt>响应类型</dt><dd>{responseType}</dd></div>
            <div><dt>正文字符</dt><dd>{checkResult.outputTextChars}</dd></div>
            <div><dt>已完成正文</dt><dd>{checkResult.completedMessageCount}</dd></div>
            <div><dt>已完成工具调用</dt><dd>{checkResult.completedFunctionCallCount}</dd></div>
            <div><dt>工具</dt><dd>{checkResult.toolCallNames?.join(', ') || '-'}</dd></div>
            <div><dt>Refusal</dt><dd>{checkResult.refusalDetected ? '是' : '否'}</dd></div>
            <div><dt>可见倾倒窗口</dt><dd>{formatMilliseconds(checkResult.visibleFlushMs)}</dd></div>
            <div><dt>答案（应为 391）</dt><dd>{checkResult.answerMatched ? '是' : '否'}</dd></div>
            <div><dt>错误码</dt><dd>{checkResult.errorCode || '-'}</dd></div>
            <div><dt>Soft TPS</dt><dd>{checkResult.qualityPolicy.softTokensPerSecond}</dd></div>
            <div><dt>Hard TPS</dt><dd>{checkResult.qualityPolicy.hardTokensPerSecond}</dd></div>
            <div><dt>TTFB 阈值</dt><dd>{checkResult.qualityPolicy.ttfbSeconds} s</dd></div>
            {checkResult.error ? (
              <div className={styles.accountStatusToolCallMetaWide}>
                <dt>请求错误</dt>
                <dd className={styles.accountStatusToolCallError}>{checkResult.error}</dd>
              </div>
            ) : null}
          </dl>

          <div className={[styles.accountStatusToolCallSection, styles.accountStatusToolCallSectionWide].join(' ')}>
            <h4>答案：</h4>
            <pre>{checkResult.modelAnswer || '-'}</pre>
          </div>
          <div className={styles.accountStatusToolCallSection}>
            <h4>请求体</h4>
            <pre>{formatToolCallCheckValue(checkResult.requestBody)}</pre>
          </div>
          <div className={styles.accountStatusToolCallSection}>
            <h4>请求头</h4>
            <pre>{formatToolCallCheckValue(requestHeaders)}</pre>
          </div>
          <div className={styles.accountStatusToolCallSection}>
            <h4>上游响应头</h4>
            <pre>{formatToolCallCheckValue(responseHeaders)}</pre>
          </div>
          <div className={[styles.accountStatusToolCallSection, styles.accountStatusToolCallSectionWide].join(' ')}>
            <h4>上游流式响应（SSE）{checkResult.responseBodyTruncated ? '（已截断）' : ''}</h4>
            <pre>{formatToolCallCheckValue(checkResult.responseBody)}</pre>
          </div>
        </div>

        <div className={styles.accountStatusToolCallDialogActions}>
          <button type="button" onClick={onClose}>关闭</button>
        </div>
      </section>
    </div>
  );
}
