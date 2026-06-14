import { useCallback, useEffect, useMemo, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import {
  getUsageServiceErrorCode,
  usageServiceApi,
  type CodexAccountStatusItem,
  type CodexInspectionRun,
} from '@/services/api/usageService';
import { useAuthStore } from '@/stores';
import { CodexInspectionModeTabs } from './components/CodexInspectionModeTabs';
import styles from './CodexAccountStatusPage.module.scss';

type AccountStatusFilter = 'all' | 'enabled' | 'disabled' | 'unauthorized' | 'quotaExhausted';

type CodexAccountStatusRow = {
  id: number;
  name: string;
  accountType: string | null;
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
  rateLimitResetCreditsAvailableCount: number | null;
  checkedAtMs: number | null;
  error: string | null;
};

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

const isQuotaExhausted = (row: CodexAccountStatusRow) => {
  const quotaPercents = [
    row.usedPercent,
    row.fiveHourUsedPercent,
    row.weeklyUsedPercent,
    row.monthlyUsedPercent,
  ];
  return quotaPercents.some((value) => value !== null && value >= 100);
};

const isUnauthorized = (row: CodexAccountStatusRow) =>
  row.statusCode === 401 || Boolean(row.error && /(^|\D)401(\D|$)/.test(row.error));

const getStatusTone = (row: CodexAccountStatusRow): 'idle' | 'good' | 'warn' | 'bad' => {
  if (isUnauthorized(row)) return 'bad';
  if (row.disabled || isQuotaExhausted(row)) return 'warn';
  return 'good';
};

const getStatusLabel = (row: CodexAccountStatusRow, t: TFunction) => {
  if (isUnauthorized(row)) return '401';
  if (isQuotaExhausted(row)) return tr(t, 'monitoring.codex_account_status_quota_exhausted', '额度耗尽');
  if (row.disabled) return tr(t, 'common.disabled', '已停用');
  return tr(t, 'common.enabled', '已启用');
};

const buildRow = (item: CodexAccountStatusItem): CodexAccountStatusRow => ({
  id: item.id,
  name: item.displayAccount || item.fileName || item.accountKey || `#${item.id}`,
  accountType: item.accountType || null,
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
  rateLimitResetCreditsAvailableCount:
    typeof item.rateLimitResetCreditsAvailableCount === 'number'
      ? item.rateLimitResetCreditsAvailableCount
      : null,
  checkedAtMs: typeof item.checkedAtMs === 'number' ? item.checkedAtMs : item.resultCreatedAtMs,
  error: item.actionError || item.error || null,
});

const getLoadErrorMessage = (error: unknown, t: TFunction) => {
  const code = getUsageServiceErrorCode(error);
  if (code === 'UNAVAILABLE') {
    return tr(t, 'monitoring.server_codex_inspection_service_unavailable', '服务端巡检服务不可用');
  }
  if (error instanceof Error && error.message) return error.message;
  return tr(t, 'common.unknown_error', '未知错误');
};

export function CodexAccountStatusPage() {
  const { t, i18n } = useTranslation();
  const managementKey = useAuthStore((state) => state.managementKey);
  const featureAvailability = usePanelFeatureAvailability();
  const [rows, setRows] = useState<CodexAccountStatusRow[]>([]);
  const [latestRun, setLatestRun] = useState<CodexInspectionRun | null>(null);
  const [loading, setLoading] = useState(true);
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState<AccountStatusFilter>('all');
  const [loadError, setLoadError] = useState<string | null>(null);

  const loadLatestServerRun = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      if (!managementKey) {
        throw new Error(tr(t, 'monitoring.server_codex_inspection_connection_required', '请先连接 Manager Server'));
      }
      const serviceBase = featureAvailability.managerServiceBase;
      if (!serviceBase || !featureAvailability.serverCodexInspectionAvailable) {
        throw new Error(tr(t, 'monitoring.server_codex_inspection_service_unavailable', '服务端巡检服务不可用'));
      }

      const detail = await usageServiceApi.getCodexAccountStatusLatest(serviceBase, managementKey);
      setLatestRun(detail.run);
      setRows(detail.items.map(buildRow));
    } catch (error) {
      setLatestRun(null);
      setRows([]);
      setLoadError(getLoadErrorMessage(error, t));
    } finally {
      setLoading(false);
    }
  }, [featureAvailability.managerServiceBase, featureAvailability.serverCodexInspectionAvailable, managementKey, t]);

  useEffect(() => {
    if (featureAvailability.checking) return;
    void loadLatestServerRun();
  }, [featureAvailability.checking, loadLatestServerRun]);

  const filteredRows = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();
    return rows
      .filter((row) => {
        if (normalizedKeyword && !row.name.toLowerCase().includes(normalizedKeyword)) return false;
        if (statusFilter === 'enabled') return !row.disabled && !isUnauthorized(row);
        if (statusFilter === 'disabled') return row.disabled;
        if (statusFilter === 'unauthorized') return isUnauthorized(row);
        if (statusFilter === 'quotaExhausted') return isQuotaExhausted(row);
        return true;
      })
      .sort((left, right) => {
        const leftUsed = left.usedPercent ?? -1;
        const rightUsed = right.usedPercent ?? -1;
        if (rightUsed !== leftUsed) return rightUsed - leftUsed;
        return left.name.localeCompare(right.name, undefined, { numeric: true });
      });
  }, [keyword, rows, statusFilter]);

  const summary = useMemo(() => {
    const enabled = rows.filter((row) => !row.disabled && !isUnauthorized(row)).length;
    const disabled = rows.filter((row) => row.disabled).length;
    const unauthorized = rows.filter(isUnauthorized).length;
    const quotaExhausted = rows.filter(isQuotaExhausted).length;
    return { enabled, disabled, unauthorized, quotaExhausted };
  }, [rows]);

  const statusOptions: Array<{ value: AccountStatusFilter; label: string }> = [
    { value: 'all', label: tr(t, 'monitoring.codex_account_status_filter_all', '全部账号') },
    { value: 'enabled', label: tr(t, 'monitoring.codex_account_status_filter_enabled', '已启用') },
    { value: 'disabled', label: tr(t, 'monitoring.codex_account_status_filter_disabled', '已停用') },
    { value: 'unauthorized', label: tr(t, 'monitoring.codex_account_status_filter_unauthorized', '401 异常') },
    { value: 'quotaExhausted', label: tr(t, 'monitoring.codex_account_status_filter_quota_exhausted', '额度耗尽') },
  ];

  const runFinishedAt = latestRun?.finishedAtMs || latestRun?.updatedAtMs || null;

  return (
    <div className={styles.page}>
      <CodexInspectionModeTabs activeMode="status" showDescription={false} />

      <section className={[styles.panel, styles.codexAccountStatusPanel].filter(Boolean).join(' ')}>
        <div className={styles.accountStatusSummaryGrid}>
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_total', '总账号')} value={rows.length} meta={tr(t, 'monitoring.codex_account_status_latest_run', '最近服务端巡检')} tone="blue" />
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_enabled', '已启用')} value={summary.enabled} meta={tr(t, 'monitoring.codex_account_status_from_server', '来自服务端结果')} tone="green" />
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_disabled', '已停用')} value={summary.disabled} meta={tr(t, 'monitoring.codex_account_status_disabled_meta', '服务端标记停用')} tone="amber" />
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_unauthorized', '401 异常')} value={summary.unauthorized} meta={tr(t, 'monitoring.codex_account_status_unauthorized_meta', '需要重新登录')} tone="red" />
          <SummaryCard label={tr(t, 'monitoring.codex_account_status_quota_exhausted', '额度耗尽')} value={summary.quotaExhausted} meta={tr(t, 'monitoring.codex_account_status_quota_exhausted_meta', '用量达到 100%')} tone="violet" />
        </div>

        <div className={styles.accountStatusToolbar}>
          <input
            className={styles.accountStatusSearch}
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder={tr(t, 'monitoring.codex_account_status_search', '搜索账号')}
          />
          <select
            className={styles.accountStatusSelect}
            value={statusFilter}
            onChange={(event) => setStatusFilter(event.target.value as AccountStatusFilter)}
          >
            {statusOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>

        {latestRun ? (
          <span className={styles.accountStatusRunMeta}>
            {tr(t, 'monitoring.codex_account_status_run_meta', '数据来源：服务端巡检 #{{id}} · {{time}}', {
              id: latestRun.id,
              time: formatDateTime(runFinishedAt, i18n.language),
            })}
          </span>
        ) : null}

        {loadError ? <div className={styles.accountStatusError}>{loadError}</div> : null}

        <div className={styles.accountStatusTableWrap}>
          <table className={styles.accountStatusTable}>
            <thead>
              <tr>
                <th>{tr(t, 'monitoring.codex_account_status_col_account', '账号')}</th>
                <th>{tr(t, 'monitoring.codex_account_status_col_state', '状态')}</th>
                <th>{tr(t, 'monitoring.codex_account_status_col_quota', '额度窗口')}</th>
                <th>{tr(t, 'monitoring.codex_account_status_col_result', '账号信息')}</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.length === 0 ? (
                <tr>
                  <td colSpan={4} className={styles.accountStatusEmpty}>
                    {loading
                      ? tr(t, 'common.loading', '加载中...')
                      : tr(t, 'monitoring.codex_account_status_empty', '没有可展示的服务端巡检数据')}
                  </td>
                </tr>
              ) : (
                filteredRows.map((row) => (
                  <AccountStatusTableRow key={row.id} row={row} t={t} language={i18n.language} />
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function SummaryCard({
  label,
  value,
  meta,
  tone,
}: {
  label: string;
  value: number;
  meta: string;
  tone: 'blue' | 'green' | 'amber' | 'red' | 'violet';
}) {
  return (
    <article className={[styles.summaryCard, styles.accountStatusSummaryCard, styles[`accountStatusTone-${tone}`]].filter(Boolean).join(' ')}>
      <span className={styles.accountStatusSummaryLabel}>{label}</span>
      <strong className={styles.accountStatusSummaryValue}>{value}</strong>
      <span className={styles.accountStatusSummaryMeta}>{meta}</span>
    </article>
  );
}

function AccountStatusTableRow({
  row,
  t,
  language,
}: {
  row: CodexAccountStatusRow;
  t: TFunction;
  language: string;
}) {
  const statusTone = getStatusTone(row);
  const quotaItems = [
    {
      key: 'five-hour',
      label: tr(t, 'monitoring.codex_account_status_five_hour_limit', '5 小时限额'),
      usedPercent: row.fiveHourUsedPercent,
      resetAtMs: row.fiveHourResetAtMs,
    },
    {
      key: 'weekly',
      label: tr(t, 'monitoring.codex_account_status_weekly_limit', '周限额'),
      usedPercent: row.weeklyUsedPercent,
      resetAtMs: row.weeklyResetAtMs,
    },
    {
      key: 'monthly',
      label: tr(t, 'monitoring.codex_account_status_monthly_limit', '月限额'),
      usedPercent: row.monthlyUsedPercent,
      resetAtMs: row.monthlyResetAtMs,
    },
  ];

  return (
    <tr>
      <td>
        <div className={styles.accountStatusAccountCell}>
          <strong>{row.name}</strong>
        </div>
      </td>
      <td>
        <div className={styles.accountStatusStateCell}>
          <span className={[styles.statusBadge, styles[`tone-${statusTone}`]].filter(Boolean).join(' ')}>
            <span className={styles.statusDot} />
            {getStatusLabel(row, t)}
          </span>
          {row.error ? <span className={styles.accountStatusErrorText}>{row.error}</span> : null}
        </div>
      </td>
      <td>
        <div className={styles.accountStatusQuotaList}>
          {quotaItems.map((item) => {
            const quotaWidth = Math.max(0, Math.min(100, item.usedPercent ?? 0));
            return (
              <div key={item.key} className={styles.accountStatusQuotaItem}>
                <div className={styles.accountStatusQuotaHeader}>
                  <span>{item.label}</span>
                  <strong>{formatPercent(item.usedPercent)}</strong>
                </div>
                <div className={styles.accountStatusQuotaMeter}>
                  <span style={{ width: `${quotaWidth}%` }} />
                </div>
                <small>
                  {tr(t, 'monitoring.codex_account_status_reset_at', '重置时间')}: {formatDateTime(item.resetAtMs, language)}
                </small>
              </div>
            );
          })}
        </div>
      </td>
      <td>
        <div className={styles.accountStatusExtraCell}>
          <span>{tr(t, 'monitoring.codex_account_status_account_type', '账号类型')}: {row.accountType || '-'}</span>
          <span>
            {tr(t, 'monitoring.codex_account_status_reset_credits', '重置次数')}: {row.rateLimitResetCreditsAvailableCount ?? '-'}
          </span>
          <span>{tr(t, 'monitoring.codex_account_status_checked_at', '检查时间')}: {formatDateTime(row.checkedAtMs, language)}</span>
          <span>{tr(t, 'monitoring.codex_account_status_action', '动作')}: {row.action || '-'}</span>
          <span>{tr(t, 'monitoring.codex_account_status_action_status', '动作状态')}: {row.actionStatus || '-'}</span>
          <span>{tr(t, 'monitoring.codex_account_status_http_status', 'HTTP')}: {row.statusCode ?? '-'}</span>
          {row.actionReason ? <span>{row.actionReason}</span> : null}
        </div>
      </td>
    </tr>
  );
}
