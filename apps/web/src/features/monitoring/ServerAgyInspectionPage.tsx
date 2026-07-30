import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { IconRefreshCw, IconShield, IconTrash2 } from '@/components/ui/icons';
import { Input } from '@/components/ui/Input';
import { Select, type SelectOption } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { CodexInspectionConfigOverview } from '@/features/monitoring/components/CodexInspectionConfigOverview';
import { AgyInspectionModeTabs } from '@/features/monitoring/components/AgyInspectionModeTabs';
import { Panel } from '@/features/monitoring/components/CodexInspectionPanels';
import { CodexInspectionResultsPanel } from '@/features/monitoring/components/CodexInspectionResultsPanel';
import { InspectionConfigDrawer } from '@/features/monitoring/components/InspectionConfigDrawer';
import { InspectionConfigFields } from '@/features/monitoring/components/InspectionConfigFields';
import {
  SummaryCard as MonitoringSummaryCard,
  type SummaryCardProps as MonitoringSummaryCardProps,
} from '@/features/monitoring/components/MonitoringShared';
import {
  type CodexInspectionAction,
  type CodexInspectionResultItem,
  type CodexInspectionRunResult,
} from '@/features/monitoring/codexInspection';
import {
  CODEX_INSPECTION_RESULT_PAGE_SIZE_OPTIONS,
  buildCodexInspectionPaginationState,
  buildConfigOverviewItems,
  countHandlingStates,
  filterInspectionResults,
  formatActionLabel,
  formatTimestamp,
  getActionFilterCounts,
  getCanonicalServerCodexInspectionActionIds,
  getMixedServerCodexInspectionActionIds,
  isActionableServerCodexInspectionResult,
  normalizeServerCodexInspectionActionStatus,
  type ActionFilter,
  type HandlingFilter,
  type SharedInspectionConfigDraft,
  type SharedInspectionConfigField,
  type StatusTone,
  validateInspectionConfigDraft,
  validateInspectionConfigFields,
} from '@/features/monitoring/model/codexInspectionPresentation';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import { getUsageServiceErrorCode } from '@/services/api/usageService';
import {
  antigravityInspectionApi,
  type AntigravityInspectionLog as AgyInspectionLog,
  type AntigravityInspectionResult as AgyInspectionResult,
  type AntigravityInspectionRun as AgyInspectionRun,
  type AntigravityInspectionRunDetail as AgyInspectionRunDetail,
  type ManagerAntigravityInspectionConfig as ManagerCodexInspectionConfig,
  type ManagerAntigravityInspectionScheduleMode as ManagerCodexInspectionScheduleMode,
} from '@/services/api/antigravityInspectionService';
import { useAuthStore, useNotificationStore } from '@/stores';
import styles from './CodexInspectionPage.module.scss';

type ManagerConfig = {
  agyInspection?: ManagerCodexInspectionConfig | null;
};

type ServerAgyInspectionDraft = {
  enabled: boolean;
  scheduleMode: ManagerCodexInspectionScheduleMode;
  intervalMinutes: string;
  timePoints: string;
  timeZone: string;
  targetType: string;
  workers: string;
  deleteWorkers: string;
  timeout: string;
  retries: string;
  workerStartStaggerMs: string;
  accountTakeStaggerMs: string;
  userAgent: string;
  usedPercentThreshold: string;
  sampleSize: string;
  autoActionMode: string;
};

export type ServerInspectionDefaultConfig = {
  enabled: boolean;
  schedule: {
    mode: ManagerCodexInspectionScheduleMode;
    intervalMinutes: number;
    timePoints: string[];
    timeZone: string;
  };
  targetType: string;
  workers: number;
  deleteWorkers: number;
  timeout: number;
  retries: number;
  workerStartStaggerMs?: number;
  accountTakeStaggerMs?: number;
  userAgent: string;
  usedPercentThreshold: number;
  sampleSize: number;
  autoActionMode: string;
};

const DEFAULT_SERVER_CODEX_CONFIG: ServerInspectionDefaultConfig = {
  enabled: false,
  schedule: {
    mode: 'interval',
    intervalMinutes: 60,
    timePoints: [],
    timeZone: '',
  },
  targetType: 'antigravity',
  workers: 4,
  deleteWorkers: 4,
  timeout: 15000,
  retries: 0,
  userAgent: 'cpa-manager-plus-antigravity-inspection',
  usedPercentThreshold: 100,
  sampleSize: 0,
  autoActionMode: 'none',
};

export type ServerInspectionProviderAdapter = {
  defaultConfig: ServerInspectionDefaultConfig;
  renderModeTabs: () => ReactNode;
  getSettings: (
    base: string,
    managementKey: string | undefined
  ) => Promise<{ settings: ManagerCodexInspectionConfig; exists: boolean }>;
  saveSettings: (
    base: string,
    managementKey: string | undefined,
    settings: ManagerCodexInspectionConfig
  ) => Promise<{ settings: ManagerCodexInspectionConfig; exists: boolean }>;
  listRuns: (
    base: string,
    managementKey: string | undefined,
    limit: number
  ) => Promise<{ items: AgyInspectionRun[] }>;
  getRun: (
    base: string,
    managementKey: string | undefined,
    runId: number
  ) => Promise<AgyInspectionRunDetail>;
  run: (
    base: string,
    managementKey: string | undefined
  ) => Promise<AgyInspectionRunDetail>;
  executeActions: (
    base: string,
    managementKey: string | undefined,
    runId: number,
    resultIds: number[]
  ) => Promise<{
    outcomes: Array<{ success: boolean }>;
    detail: AgyInspectionRunDetail;
  }>;
  resultStatusLabel?: (result: AgyInspectionResult) => string;
  abnormalLabel?: string;
  supportsActionExecution?: boolean;
  showsPriorityAdjustmentSummary?: boolean;
  /** wXAi：高级配置展示 worker/取账号错峰秒数 */
  supportsProbeStagger?: boolean;
  quotaExhaustedLabel?: string;
  getQuotaExhaustedCount?: (run: AgyInspectionRun) => number;
  getAbnormalCount?: (run: AgyInspectionRun) => number;
  autoActionDescription?: string;
  userAgentSectionLabel?: string;
};

const ANTIGRAVITY_SERVER_INSPECTION_ADAPTER: ServerInspectionProviderAdapter = {
  defaultConfig: DEFAULT_SERVER_CODEX_CONFIG,
  renderModeTabs: () => <AgyInspectionModeTabs activeMode="server" />,
  getSettings: (base, managementKey) =>
    antigravityInspectionApi.getSettings(base, managementKey, 'server'),
  saveSettings: (base, managementKey, settings) =>
    antigravityInspectionApi.saveSettings(base, managementKey, settings, 'server'),
  listRuns: (base, managementKey, limit) =>
    antigravityInspectionApi.listRuns(base, managementKey, limit),
  getRun: (base, managementKey, runId) =>
    antigravityInspectionApi.getRun(base, managementKey, runId),
  run: (base, managementKey) => antigravityInspectionApi.run(base, managementKey, 'server'),
  executeActions: (base, managementKey, runId, resultIds) =>
    antigravityInspectionApi.executeActions(base, managementKey, runId, resultIds),
};

const RUNS_LIMIT = 30;

const COMMON_TIME_ZONES: ReadonlyArray<string> = [
  'UTC',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Asia/Hong_Kong',
  'Asia/Kolkata',
  'Europe/London',
  'Europe/Berlin',
  'Europe/Moscow',
  'America/New_York',
  'America/Los_Angeles',
];

const detectBrowserTimeZone = (): string => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || '';
  } catch {
    return '';
  }
};

const isScheduleMode = (value: unknown): value is ManagerCodexInspectionScheduleMode =>
  value === 'interval' || value === 'time_points';

const resolveServerInspectionConfig = (
  config: ManagerCodexInspectionConfig | null | undefined,
  defaultConfig: ServerInspectionDefaultConfig
): ServerInspectionDefaultConfig => {
  const schedule = config?.schedule ?? {};
  const scheduleMode = isScheduleMode(schedule.mode)
    ? schedule.mode
    : schedule.timePoints && schedule.timePoints.length > 0
      ? 'time_points'
      : defaultConfig.schedule.mode;

  return {
    ...defaultConfig,
    ...config,
    enabled: config?.enabled ?? defaultConfig.enabled,
    schedule: {
      mode: scheduleMode,
      intervalMinutes:
        schedule.intervalMinutes && schedule.intervalMinutes > 0
          ? schedule.intervalMinutes
          : defaultConfig.schedule.intervalMinutes,
      timePoints: schedule.timePoints ?? defaultConfig.schedule.timePoints,
      timeZone:
        typeof schedule.timeZone === 'string' ? schedule.timeZone : defaultConfig.schedule.timeZone,
    },
    targetType: config?.targetType || defaultConfig.targetType,
    workers: config?.workers && config.workers > 0 ? config.workers : defaultConfig.workers,
    deleteWorkers:
      config?.deleteWorkers && config.deleteWorkers > 0
        ? config.deleteWorkers
        : defaultConfig.deleteWorkers,
    timeout: config?.timeout && config.timeout > 0 ? config.timeout : defaultConfig.timeout,
    retries:
      config?.retries !== undefined && config.retries >= 0 ? config.retries : defaultConfig.retries,
    workerStartStaggerMs: resolveNonNegativeMs(
      config?.workerStartStaggerMs,
      defaultConfig.workerStartStaggerMs ?? 10000
    ),
    accountTakeStaggerMs: resolveNonNegativeMs(
      config?.accountTakeStaggerMs,
      defaultConfig.accountTakeStaggerMs ?? 10000
    ),
    userAgent: config?.userAgent || defaultConfig.userAgent,
    usedPercentThreshold:
      config?.usedPercentThreshold !== undefined
        ? config.usedPercentThreshold
        : defaultConfig.usedPercentThreshold,
    sampleSize:
      config?.sampleSize !== undefined && config.sampleSize >= 0
        ? config.sampleSize
        : defaultConfig.sampleSize,
    autoActionMode: config?.autoActionMode || defaultConfig.autoActionMode,
  };
};

const toDraft = (
  config: ManagerCodexInspectionConfig | null | undefined,
  defaultConfig: ServerInspectionDefaultConfig
): ServerAgyInspectionDraft => {
  const resolved = resolveServerInspectionConfig(config, defaultConfig);
  return {
    enabled: resolved.enabled,
    scheduleMode: resolved.schedule.mode as ManagerCodexInspectionScheduleMode,
    intervalMinutes: String(resolved.schedule.intervalMinutes),
    timePoints: resolved.schedule.timePoints.join(', '),
    timeZone: resolved.schedule.timeZone,
    targetType: resolved.targetType,
    workers: String(resolved.workers),
    deleteWorkers: String(resolved.deleteWorkers),
    timeout: String(resolved.timeout),
    retries: String(resolved.retries),
    workerStartStaggerMs: String(resolved.workerStartStaggerMs ?? 10000),
    accountTakeStaggerMs: String(resolved.accountTakeStaggerMs ?? 10000),
    userAgent: resolved.userAgent,
    usedPercentThreshold: String(resolved.usedPercentThreshold),
    sampleSize: String(resolved.sampleSize),
    autoActionMode: resolved.autoActionMode,
  };
};

const toSharedInspectionDraft = (
  draft: ServerAgyInspectionDraft
): SharedInspectionConfigDraft => ({
  targetTypes: 'codex',
  workers: draft.workers,
  deleteWorkers: draft.deleteWorkers,
  timeout: draft.timeout,
  retries: draft.retries,
  userAgent: draft.userAgent,
  xaiInferenceUserAgent: '',
  xaiInferenceEnabled: false,
  xaiInferenceModel: '',
  xaiInferencePrompt: '',
  usedPercentThreshold: draft.usedPercentThreshold,
  sampleSize: draft.sampleSize,
  autoActionMode: draft.autoActionMode,
  autoRecoverEnabled: false,
});

const toConfigOverviewSettings = (config: ServerInspectionDefaultConfig) => ({
  targetTypes: ['codex'],
  targetType: config.targetType,
  workers: config.workers,
  timeout: config.timeout,
  usedPercentThreshold: config.usedPercentThreshold,
  sampleSize: config.sampleSize,
  xaiInferenceEnabled: false,
  xaiInferenceModel: '',
  xaiInferencePrompt: '',
  autoActionMode: config.autoActionMode,
  autoRecoverEnabled: false,
});

const normalizeTimePoint = (value: string): string | null => {
  const match = value.trim().match(/^(\d{1,2}):(\d{1,2})$/);
  if (!match) return null;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (!Number.isInteger(hour) || !Number.isInteger(minute)) return null;
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59) return null;
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
};

const splitTimePointTokens = (raw: string): string[] =>
  raw
    .split(/[\s,;，；]+/)
    .map((value) => value.trim())
    .filter(Boolean);

const parseTimePoints = (raw: string): string[] =>
  Array.from(
    new Set(
      splitTimePointTokens(raw)
        .map(normalizeTimePoint)
        .filter((value): value is string => Boolean(value))
    )
  ).sort();

const normalizeTimePointList = (values: string[]): string[] =>
  Array.from(
    new Set(
      values
        .map(normalizeTimePoint)
        .filter((value): value is string => Boolean(value))
    )
  ).sort();

const readScheduleInteger = (raw: string, min: number): number | null => {
  const value = Number(raw);
  if (!Number.isInteger(value) || value < min) return null;
  return value;
};

const resolveNonNegativeMs = (value: number | undefined, fallback: number): number => {
  if (typeof value === 'number' && Number.isInteger(value) && value >= 0) {
    return value;
  }
  return fallback;
};

const readNonNegativeMs = (raw: string): number | null => {
  const value = Number(raw.trim());
  if (!Number.isFinite(value) || !Number.isInteger(value) || value < 0) {
    return null;
  }
  return value;
};

const createConfigFromDraft = (
  draft: ServerAgyInspectionDraft,
  t: TFunction,
  defaultConfig: ServerInspectionDefaultConfig
): ManagerCodexInspectionConfig | null => {
  const validation = validateInspectionConfigDraft(toSharedInspectionDraft(draft), t);
  if (!validation.ok) {
    return null;
  }

  const parsedIntervalMinutes = readScheduleInteger(draft.intervalMinutes, 1);
  const intervalMinutes = parsedIntervalMinutes ?? defaultConfig.schedule.intervalMinutes;
  const hasInvalidTimePoint =
    draft.scheduleMode === 'time_points' &&
    splitTimePointTokens(draft.timePoints).some((value) => normalizeTimePoint(value) === null);
  const timePoints = parseTimePoints(draft.timePoints);

  if (
    draft.scheduleMode === 'interval' && parsedIntervalMinutes === null
  ) {
    return null;
  }

  if (draft.scheduleMode === 'time_points' && (hasInvalidTimePoint || timePoints.length === 0)) {
    return null;
  }

  const workerStartStaggerMs = readNonNegativeMs(draft.workerStartStaggerMs);
  const accountTakeStaggerMs = readNonNegativeMs(draft.accountTakeStaggerMs);
  if (workerStartStaggerMs === null || accountTakeStaggerMs === null) {
    return null;
  }

  return {
    enabled: draft.enabled,
    schedule:
      draft.scheduleMode === 'time_points'
        ? {
            mode: 'time_points',
            timePoints,
            intervalMinutes,
            timeZone: draft.timeZone.trim(),
          }
        : {
            mode: 'interval',
            intervalMinutes,
            timePoints,
            timeZone: draft.timeZone.trim(),
          },
    targetType: draft.targetType.trim(),
    workers: validation.values.workers,
    deleteWorkers: validation.values.deleteWorkers,
    timeout: validation.values.timeout,
    retries: validation.values.retries,
    workerStartStaggerMs,
    accountTakeStaggerMs,
    userAgent: validation.values.userAgent,
    usedPercentThreshold: validation.values.usedPercentThreshold,
    sampleSize: validation.values.sampleSize,
    autoActionMode: validation.values.autoActionMode,
  };
};

const statusToneClass: Record<StatusTone, string> = {
  idle: styles['tone-idle'],
  info: styles['tone-info'],
  good: styles['tone-good'],
  warn: styles['tone-warn'],
  bad: styles['tone-bad'],
};

const logLevelClass: Record<string, string> = {
  info: styles.logInfo,
  success: styles.logSuccess,
  warning: styles.logWarning,
  error: styles.logError,
};

function getRunTone(run?: AgyInspectionRun | null): StatusTone {
  switch (run?.status) {
    case 'completed':
      return 'good';
    case 'failed':
      return 'bad';
    case 'running':
      return 'info';
    default:
      return 'idle';
  }
}

function getRunStatusLabel(run: AgyInspectionRun | null | undefined, t: ReturnType<typeof useTranslation>['t']) {
  switch (run?.status) {
    case 'completed':
      return t('monitoring.codex_inspection_status_success');
    case 'failed':
      return t('monitoring.codex_inspection_status_error');
    case 'running':
      return t('monitoring.codex_inspection_status_running');
    default:
      return t('monitoring.codex_inspection_status_idle');
  }
}

function formatDuration(run: AgyInspectionRun | null | undefined, t: ReturnType<typeof useTranslation>['t']) {
  if (!run?.startedAtMs || !run.finishedAtMs) return t('common.not_set');
  const seconds = Math.max(0, Math.round((run.finishedAtMs - run.startedAtMs) / 1000));
  return t('monitoring.server_codex_inspection_duration_value', { seconds });
}

function formatTrigger(run: AgyInspectionRun | null | undefined, t: ReturnType<typeof useTranslation>['t']) {
  if (!run) return t('common.not_set');
  if (run.triggerType === 'scheduled') return t('monitoring.server_codex_inspection_trigger_scheduled');
  return t('monitoring.server_codex_inspection_trigger_manual');
}

function formatResultStateHeader(
  run: AgyInspectionRun | null | undefined,
  t: ReturnType<typeof useTranslation>['t']
) {
  if (run?.triggerType === 'scheduled') {
    return t('monitoring.server_codex_inspection_result_state_scheduled');
  }
  if (run?.triggerType === 'manual') {
    return t('monitoring.server_codex_inspection_result_state_manual');
  }
  return t('monitoring.server_codex_inspection_result_state_snapshot');
}

function formatResultsDescription(
  run: AgyInspectionRun | null | undefined,
  locale: string,
  t: ReturnType<typeof useTranslation>['t']
) {
  const time = run?.finishedAtMs ? formatTimestamp(run.finishedAtMs, locale) : t('common.not_set');
  if (run?.triggerType === 'manual') {
    return t('monitoring.server_codex_inspection_results_desc_manual', { time });
  }
  if (run?.triggerType === 'scheduled') {
    return t('monitoring.server_codex_inspection_results_desc_scheduled', { time });
  }
  return t('monitoring.server_codex_inspection_results_desc');
}

function formatSchedule(config: ServerInspectionDefaultConfig, t: ReturnType<typeof useTranslation>['t']) {
  if (config.schedule.mode === 'time_points') {
    const base = t('monitoring.server_codex_inspection_schedule_time_points_value', {
      points: config.schedule.timePoints.join(', '),
    });
    const tz = config.schedule.timeZone?.trim();
    return tz ? `${base} (${tz})` : base;
  }
  return t('monitoring.server_codex_inspection_schedule_interval_value', {
    minutes: config.schedule.intervalMinutes,
  });
}

function getComparableConfig(config: ServerInspectionDefaultConfig) {
  return {
    enabled: config.enabled,
    scheduleMode: config.schedule.mode,
    intervalMinutes: config.schedule.intervalMinutes,
    timePoints: normalizeTimePointList(config.schedule.timePoints),
    timeZone: (config.schedule.timeZone || '').trim(),
    targetType: config.targetType.trim(),
    workers: config.workers,
    deleteWorkers: config.deleteWorkers,
    timeout: config.timeout,
    retries: config.retries,
    workerStartStaggerMs: config.workerStartStaggerMs ?? 10000,
    accountTakeStaggerMs: config.accountTakeStaggerMs ?? 10000,
    userAgent: config.userAgent.trim(),
    usedPercentThreshold: config.usedPercentThreshold,
    sampleSize: config.sampleSize,
    autoActionMode: config.autoActionMode,
  };
}

function configsEquivalent(
  current: ServerInspectionDefaultConfig,
  next: ServerInspectionDefaultConfig
) {
  return JSON.stringify(getComparableConfig(current)) === JSON.stringify(getComparableConfig(next));
}

function resolveActionLabel(action: string, t: ReturnType<typeof useTranslation>['t']) {
  if (
    action === 'delete' ||
    action === 'disable' ||
    action === 'enable' ||
    action === 'reauth' ||
    action === 'keep'
  ) {
    return formatActionLabel(action, t);
  }
  return action || t('common.not_set');
}

function formatServerActionStatusLabel(
  item: AgyInspectionResult,
  t: ReturnType<typeof useTranslation>['t']
) {
  const status = normalizeServerCodexInspectionActionStatus(item);
  if (status === 'success') {
    return t('monitoring.server_codex_inspection_action_status_success', {
      action: resolveActionLabel(item.executedAction || item.action, t),
    });
  }
  if (status === 'failed') {
    return t('monitoring.server_codex_inspection_action_status_failed');
  }
  if (status === 'skipped') {
    return t('monitoring.server_codex_inspection_action_status_skipped');
  }
  if (status === 'needs_review') {
    return t('monitoring.server_codex_inspection_action_status_needs_review');
  }
  if (status === 'pending') {
    return t('monitoring.server_codex_inspection_action_status_pending');
  }
  return '';
}

function formatServerResultStateToken(
  value: string | undefined,
  t: ReturnType<typeof useTranslation>['t']
) {
  const normalized = (value ?? '').trim().toLowerCase();
  if (!normalized) return '';
  if (normalized === 'active') return t('monitoring.state_active');
  if (normalized === 'disabled') return t('monitoring.state_disabled');
  if (normalized === 'inactive') return t('monitoring.state_inactive');
  if (normalized === 'enabled') return t('monitoring.codex_inspection_state_enabled');
  return value?.trim() ?? '';
}

function formatServerResultStateDetail(
  item: AgyInspectionResult,
  t: ReturnType<typeof useTranslation>['t'],
  adapter: ServerInspectionProviderAdapter
) {
  const providerStatusLabel = adapter.resultStatusLabel?.(item);
  if (providerStatusLabel) return providerStatusLabel;

  const errorText = item.actionError || item.errorDetail || item.error;
  if (errorText) return errorText;

  return (
    formatServerResultStateToken(item.status, t) ||
    formatServerResultStateToken(item.state, t) ||
    '--'
  );
}

function normalizeServerResultAction(action: string): CodexInspectionAction {
  if (
    action === 'delete' ||
    action === 'disable' ||
    action === 'enable' ||
    action === 'reauth' ||
    action === 'keep'
  ) {
    return action;
  }
  return 'keep';
}

function toServerResultItem(
  item: AgyInspectionResult,
  t: ReturnType<typeof useTranslation>['t'],
  adapter: ServerInspectionProviderAdapter
): CodexInspectionResultItem {
  const actionStatusLabel = formatServerActionStatusLabel(item, t);
  const providerStatusLabel = adapter.resultStatusLabel?.(item) ?? '';
  const reasonParts =
    adapter.supportsActionExecution === false
      ? []
      : [item.actionReason, actionStatusLabel].filter(Boolean);
  return {
    key: `server-${item.id || item.accountKey}`,
    fileName: item.fileName,
    displayAccount: item.displayAccount,
    authIndex: item.authIndex ?? null,
    accountId: item.accountId ?? null,
    provider: item.provider,
    disabled: item.disabled,
    autoRecoverEligible: false,
    autoRecoverOwned: false,
    status: item.status ?? '',
    state: item.state ?? '',
    raw: item as unknown as CodexInspectionResultItem['raw'],
    action:
      adapter.supportsActionExecution === false
        ? 'keep'
        : normalizeServerResultAction(item.action),
    actionReason: reasonParts.join(' · '),
    statusCode: item.statusCode ?? null,
    usedPercent: item.usedPercent ?? null,
    isQuota: item.isQuota,
    error: providerStatusLabel || item.error || '',
    planType: item.planType ?? null,
    quotaWindows: item.quotaWindows?.map((window) => ({
      id: window.id,
      labelKey: window.labelKey,
      labelParams: window.labelParams,
      usedPercent: window.usedPercent ?? null,
      resetLabel: window.resetLabel ?? '',
      limitWindowSeconds: window.limitWindowSeconds ?? null,
    })),
    errorKind: item.errorKind,
    errorDetail: providerStatusLabel ? '' : item.actionError || item.errorDetail || '',
  };
}

function countServerResultActions(items: AgyInspectionResult[]) {
  const counts = {
    delete: 0,
    disable: 0,
    enable: 0,
  };
  items.forEach((item) => {
    if (item.action === 'delete') counts.delete += 1;
    if (item.action === 'disable') counts.disable += 1;
    if (item.action === 'enable') counts.enable += 1;
  });
  return counts;
}

function getServerActionIcon(action: string) {
  if (action === 'delete') return IconTrash2;
  if (action === 'disable') return IconShield;
  return IconRefreshCw;
}

function getUsageServiceDisplayError(error: unknown, t: ReturnType<typeof useTranslation>['t']) {
  const code = getUsageServiceErrorCode(error);
  if (code) {
    return t(`usage_service_errors.${code}`, {
      defaultValue: t('usage_service_errors.request_failed'),
    });
  }
  if (error instanceof Error && error.message) return error.message;
  return t('usage_service_errors.request_failed');
}

function formatServiceHost(base: string): string {
  if (!base) return '';
  try {
    const url = new URL(base);
    return url.host;
  } catch {
    return base;
  }
}

export function ServerAgyInspectionPage() {
  return <ServerProviderInspectionPage adapter={ANTIGRAVITY_SERVER_INSPECTION_ADAPTER} />;
}

type ServerProviderInspectionPageProps = {
  adapter: ServerInspectionProviderAdapter;
};

export function ServerProviderInspectionPage({ adapter }: ServerProviderInspectionPageProps) {
  const { t, i18n } = useTranslation();
  const managementKey = useAuthStore((state) => state.managementKey);
  const featureAvailability = usePanelFeatureAvailability();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);

  const [serviceBase, setServiceBase] = useState('');
  const [managerConfig, setManagerConfig] = useState<ManagerConfig | null>(null);
  const [draft, setDraft] = useState<ServerAgyInspectionDraft>(() =>
    toDraft(null, adapter.defaultConfig)
  );
  const [runs, setRuns] = useState<AgyInspectionRun[]>([]);
  const [detail, setDetail] = useState<AgyInspectionRunDetail | null>(null);
  const [selectedRunId, setSelectedRunId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');
  const [logsCollapsed, setLogsCollapsed] = useState(false);
  const [actionFilter, setActionFilter] = useState<ActionFilter>('all');
  const [handlingFilter, setHandlingFilter] = useState<HandlingFilter>('all');
  const [resultPage, setResultPage] = useState(1);
  const [resultPageSize, setResultPageSize] = useState<number>(
    CODEX_INSPECTION_RESULT_PAGE_SIZE_OPTIONS[0]
  );
  const [logLevelFilter, setLogLevelFilter] = useState<'all' | 'info' | 'success' | 'warning' | 'error'>('all');
  const [executingResultIds, setExecutingResultIds] = useState<Set<number>>(() => new Set());
  const [executingAllActions, setExecutingAllActions] = useState(false);
  const [configDrawerOpen, setConfigDrawerOpen] = useState(false);
  const [configFocusField, setConfigFocusField] = useState<string | null>(null);
  const refreshInFlightRef = useRef(false);
  const actionInFlightRef = useRef(false);

  const loadRunDetail = useCallback(
    async (base: string, id: number) => {
      const nextDetail = await adapter.getRun(base, managementKey, id);
      setDetail(nextDetail);
      setSelectedRunId(nextDetail.run.id);
      return nextDetail;
    },
    [adapter, managementKey]
  );

  const loadPageData = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const resolvedBase = featureAvailability.managerServiceBase;
      if (!resolvedBase) {
        throw new Error(t('monitoring.server_codex_inspection_service_unavailable'));
      }
      const response = await adapter.getSettings(resolvedBase, managementKey);

      setServiceBase(resolvedBase);
      setManagerConfig({ agyInspection: response.settings });
      setDraft(toDraft(response.settings, adapter.defaultConfig));

      const runsResponse = await adapter.listRuns(
        resolvedBase,
        managementKey,
        RUNS_LIMIT
      );
      setRuns(runsResponse.items);
      const nextSelectedId = runsResponse.items[0]?.id;
      if (nextSelectedId) {
        await loadRunDetail(resolvedBase, nextSelectedId);
      } else {
        setDetail(null);
        setSelectedRunId(null);
      }
    } catch (error: unknown) {
      setError(getUsageServiceDisplayError(error, t));
      setRuns([]);
      setDetail(null);
      setSelectedRunId(null);
    } finally {
      setLoading(false);
    }
  }, [
    adapter,
    featureAvailability.managerServiceBase,
    loadRunDetail,
    managementKey,
    t,
  ]);

  useEffect(() => {
    if (featureAvailability.checking) {
      return;
    }
    if (!managementKey) {
      setLoading(false);
      setError(t('monitoring.server_codex_inspection_connection_required'));
      return;
    }
    void loadPageData();
  }, [
    featureAvailability.checking,
    loadPageData,
    managementKey,
    t,
  ]);

  const selectedConfig = useMemo(
    () => resolveServerInspectionConfig(managerConfig?.agyInspection, adapter.defaultConfig),
    [adapter.defaultConfig, managerConfig?.agyInspection]
  );
  const draftConfig = useMemo(
    () => createConfigFromDraft(draft, t, adapter.defaultConfig),
    [adapter.defaultConfig, draft, t]
  );
  const normalizedDraftConfig = useMemo(
    () =>
      draftConfig
        ? resolveServerInspectionConfig(draftConfig, adapter.defaultConfig)
        : null,
    [adapter.defaultConfig, draftConfig]
  );
  const hasUnsavedChanges = Boolean(
    managerConfig && (!normalizedDraftConfig || !configsEquivalent(selectedConfig, normalizedDraftConfig))
  );
  const savedScheduleLabel = formatSchedule(selectedConfig, t);
  const hasRunningRun = runs.some((run) => run.status === 'running') || detail?.run.status === 'running';
  const latestRun = runs[0] ?? null;
  const activeRun = detail?.run ?? latestRun;
  const activeTone = getRunTone(activeRun);
  const abnormalLabel =
    adapter.abnormalLabel ?? String(t('monitoring.codex_inspection_reauth_count'));
  const supportsActionExecution = adapter.supportsActionExecution !== false;

  const resultRows = useMemo(() => detail?.results ?? [], [detail?.results]);
  const resultItems = useMemo(
    () => resultRows.map((item) => toServerResultItem(item, t, adapter)),
    [adapter, resultRows, t]
  );
  const resultByKey = useMemo(() => {
    const map = new Map<string, AgyInspectionResult>();
    resultRows.forEach((item) => {
      map.set(`server-${item.id || item.accountKey}`, item);
    });
    return map;
  }, [resultRows]);
  const filteredResultRows = useMemo(
    () => filterInspectionResults(resultItems, handlingFilter, actionFilter),
    [actionFilter, handlingFilter, resultItems]
  );
  const resultPagination = useMemo(
    () => buildCodexInspectionPaginationState(filteredResultRows, resultPage, resultPageSize),
    [filteredResultRows, resultPage, resultPageSize]
  );

  useEffect(() => {
    setResultPage(1);
  }, [actionFilter, handlingFilter, detail?.run.id]);

  useEffect(() => {
    if (resultPage === resultPagination.currentPage) return;
    setResultPage(resultPagination.currentPage);
  }, [resultPage, resultPagination.currentPage]);

  const handleResultPageSizeChange = useCallback((pageSize: number) => {
    setResultPageSize(pageSize);
    setResultPage(1);
  }, []);

  const scheduleOptions = useMemo(
    () => [
      { value: 'interval', label: t('monitoring.server_codex_inspection_schedule_interval') },
      { value: 'time_points', label: t('monitoring.server_codex_inspection_schedule_time_points') },
    ],
    [t]
  );

  const browserTimeZone = useMemo(detectBrowserTimeZone, []);
  const timeZoneOptions = useMemo(() => {
    const seen = new Set<string>();
    const options: SelectOption[] = [
      { value: '', label: t('monitoring.server_codex_inspection_time_zone_server_default') },
    ];
    const push = (value: string, label: string) => {
      if (!value || seen.has(value)) return;
      seen.add(value);
      options.push({ value, label });
    };
    if (browserTimeZone && browserTimeZone !== 'UTC') {
      push(
        browserTimeZone,
        t('monitoring.server_codex_inspection_time_zone_browser', { tz: browserTimeZone })
      );
    }
    COMMON_TIME_ZONES.forEach((zone) => push(zone, zone));
    if (draft.timeZone && !seen.has(draft.timeZone)) {
      push(draft.timeZone, draft.timeZone);
    }
    return options;
  }, [browserTimeZone, draft.timeZone, t]);

  const updateDraft = <K extends keyof ServerAgyInspectionDraft>(
    key: K,
    value: ServerAgyInspectionDraft[K]
  ) => {
    setDraft((previous) => ({ ...previous, [key]: value }));
  };

  const updateSharedDraftField = (field: SharedInspectionConfigField, value: string) => {
    switch (field) {
      case 'workers':
      case 'deleteWorkers':
      case 'timeout':
      case 'retries':
      case 'userAgent':
      case 'usedPercentThreshold':
      case 'sampleSize':
        updateDraft(field, value);
        return;
      case 'targetTypes':
      case 'xaiInferenceUserAgent':
      case 'xaiInferenceModel':
      case 'xaiInferencePrompt':
        throw new Error(`Antigravity inspection does not support shared field: ${field}`);
    }
  };

  const rejectUnsupportedAgyConfigChange = () => {
    throw new Error('Antigravity inspection does not support this shared configuration option');
  };

  const refreshRuns = useCallback(async (options?: { silent?: boolean }) => {
    if (refreshInFlightRef.current) return;
    refreshInFlightRef.current = true;
    const silent = options?.silent ?? false;
    if (!serviceBase) {
      try {
        await loadPageData();
      } finally {
        refreshInFlightRef.current = false;
      }
      return;
    }
    if (!silent) {
      setLoading(true);
      setError('');
    }
    try {
      const response = await adapter.listRuns(
        serviceBase,
        managementKey,
        RUNS_LIMIT
      );
      setRuns(response.items);
      const selectionStillValid =
        selectedRunId != null && response.items.some((run) => run.id === selectedRunId);
      if (selectionStillValid) {
        // 静默轮询时保留用户正在查看的历史详情,避免每 30s 重建详情导致结果表/日志
        // 重渲染、打断操作;但正在运行的巡检或尚无详情时仍需刷新以获取最新进度。
        const watchingRunning = detail?.run.status === 'running';
        if (!silent || !detail || watchingRunning) {
          await loadRunDetail(serviceBase, selectedRunId);
        }
      } else {
        const fallbackId = response.items[0]?.id;
        if (fallbackId) {
          await loadRunDetail(serviceBase, fallbackId);
        } else {
          setDetail(null);
          setSelectedRunId(null);
        }
      }
    } catch (error: unknown) {
      if (!silent) setError(getUsageServiceDisplayError(error, t));
    } finally {
      if (!silent) setLoading(false);
      refreshInFlightRef.current = false;
    }
  }, [adapter, detail, loadPageData, loadRunDetail, managementKey, selectedRunId, serviceBase, t]);

  useEffect(() => {
    if (!serviceBase || (!selectedConfig.enabled && !hasRunningRun)) return;
    const timer = window.setInterval(() => {
      if (saving || running || actionInFlightRef.current) return;
      void refreshRuns({ silent: true });
    }, 30_000);

    return () => window.clearInterval(timer);
  }, [hasRunningRun, refreshRuns, running, saving, selectedConfig.enabled, serviceBase]);

  const handleSave = async () => {
    if (!serviceBase || !managerConfig) {
      showNotification(t('monitoring.server_codex_inspection_service_unavailable'), 'warning');
      return;
    }
    const agyInspection = createConfigFromDraft(draft, t, adapter.defaultConfig);
    if (!agyInspection) {
      showNotification(t('monitoring.server_codex_inspection_config_invalid'), 'warning');
      return;
    }
    setSaving(true);
    try {
      const response = await adapter.saveSettings(
        serviceBase,
        managementKey,
        agyInspection
      );
      setManagerConfig({ agyInspection: response.settings });
      setDraft(toDraft(response.settings, adapter.defaultConfig));
      showNotification(t('monitoring.server_codex_inspection_config_saved'), 'success');
      setConfigDrawerOpen(false);
    } catch (error: unknown) {
      showNotification(
        `${t('notification.save_failed')}: ${getUsageServiceDisplayError(error, t)}`,
        'error'
      );
    } finally {
      setSaving(false);
    }
  };

  const handleCloseConfigDrawer = useCallback(() => {
    if (hasUnsavedChanges) {
      showConfirmation({
        title: t('monitoring.server_codex_inspection_close_confirm_title'),
        message: t('monitoring.server_codex_inspection_close_unsaved_hint'),
        confirmText: t('monitoring.server_codex_inspection_discard'),
        cancelText: t('common.cancel'),
        variant: 'danger',
        onConfirm: () => {
          setDraft(toDraft(managerConfig?.agyInspection, adapter.defaultConfig));
          setConfigDrawerOpen(false);
        },
      });
      return;
    }
    setConfigDrawerOpen(false);
  }, [adapter.defaultConfig, hasUnsavedChanges, managerConfig, showConfirmation, t]);

  const openConfigDrawer = useCallback((field?: string) => {
    setConfigFocusField(field ?? null);
    setConfigDrawerOpen(true);
  }, []);

  const executeServerRun = useCallback(async () => {
    if (!serviceBase) {
      showNotification(t('monitoring.server_codex_inspection_service_unavailable'), 'warning');
      return;
    }
    setRunning(true);
    setError('');
    try {
      const nextDetail = await adapter.run(serviceBase, managementKey);
      setDetail(nextDetail);
      setSelectedRunId(nextDetail.run.id);
      const response = await adapter.listRuns(
        serviceBase,
        managementKey,
        RUNS_LIMIT
      );
      setRuns(response.items);
      showNotification(t('monitoring.server_codex_inspection_run_success'), 'success');
    } catch (error: unknown) {
      const message = getUsageServiceDisplayError(error, t);
      showNotification(`${t('monitoring.server_codex_inspection_run_failed')}: ${message}`, 'error');
      await refreshRuns();
    } finally {
      setRunning(false);
    }
  }, [adapter, managementKey, refreshRuns, serviceBase, showNotification, t]);

  const handleRunNow = () => {
    showConfirmation({
      title: t('monitoring.server_codex_inspection_run_confirm_title'),
      message: t('monitoring.server_codex_inspection_run_confirm_body'),
      confirmText: t('monitoring.server_codex_inspection_run_now'),
      cancelText: t('common.cancel'),
      variant: selectedConfig.autoActionMode === 'delete' ? 'danger' : 'primary',
      onConfirm: executeServerRun,
    });
  };

  const executeServerActions = useCallback(
    async (targets: AgyInspectionResult[], scope: 'single' | 'bulk') => {
      if (!serviceBase || !detail) {
        showNotification(t('monitoring.server_codex_inspection_service_unavailable'), 'warning');
        return;
      }
      const resultIds = Array.from(
        new Set(targets.filter(isActionableServerCodexInspectionResult).map((item) => item.id))
      );
      if (resultIds.length === 0) {
        showNotification(t('monitoring.server_codex_inspection_no_actions'), 'warning');
        return;
      }
      setExecutingResultIds(new Set(resultIds));
      setExecutingAllActions(scope === 'bulk');
      actionInFlightRef.current = true;
      try {
        const response = await adapter.executeActions(
          serviceBase,
          managementKey,
          detail.run.id,
          resultIds
        );
        setDetail(response.detail);
        setSelectedRunId(response.detail.run.id);

        const runsResponse = await adapter.listRuns(
          serviceBase,
          managementKey,
          RUNS_LIMIT
        );
        setRuns(runsResponse.items);

        const failed = response.outcomes.filter((item) => !item.success);
        if (failed.length > 0) {
          showNotification(
            t('monitoring.server_codex_inspection_execute_partial', {
              failed: failed.length,
              total: response.outcomes.length,
            }),
            'warning'
          );
        } else {
          showNotification(t('monitoring.server_codex_inspection_execute_success'), 'success');
        }
      } catch (error: unknown) {
        showNotification(
          `${t('monitoring.server_codex_inspection_execute_failed')}: ${getUsageServiceDisplayError(error, t)}`,
          'error'
        );
      } finally {
        actionInFlightRef.current = false;
        setExecutingResultIds(new Set());
        setExecutingAllActions(false);
      }
    },
    [adapter, detail, managementKey, serviceBase, showNotification, t]
  );

  const handleExecuteServerActions = useCallback(
    (targets: AgyInspectionResult[], scope: 'single' | 'bulk') => {
      if (targets.length === 0) return;
      const counts = countServerResultActions(targets);
      const hasDelete = targets.some((item) => item.action === 'delete');
      const first = targets[0];
      showConfirmation({
        title:
          scope === 'bulk'
            ? t('monitoring.server_codex_inspection_execute_confirm_title')
            : t('monitoring.server_codex_inspection_execute_single_title'),
        message:
          scope === 'bulk'
            ? t('monitoring.server_codex_inspection_execute_confirm_body', {
                total: targets.length,
                delete: counts.delete,
                disable: counts.disable,
                enable: counts.enable,
              })
            : t('monitoring.server_codex_inspection_execute_single_body', {
                account: first.displayAccount,
                action: resolveActionLabel(first.action, t),
              }),
        confirmText:
          scope === 'bulk'
            ? t('monitoring.server_codex_inspection_execute_all')
            : resolveActionLabel(first.action, t),
        cancelText: t('common.cancel'),
        variant: hasDelete ? 'danger' : 'primary',
        onConfirm: () => executeServerActions(targets, scope),
      });
    },
    [executeServerActions, showConfirmation, t]
  );


  const handleSelectRun = async (runID: number) => {
    if (!serviceBase || runID === selectedRunId) return;
    setSelectedRunId(runID);
    try {
      await loadRunDetail(serviceBase, runID);
    } catch (error: unknown) {
      showNotification(getUsageServiceDisplayError(error, t), 'error');
    }
  };

  const renderStatusPanel = () => {
    const lastRunTime = activeRun?.finishedAtMs
      ? new Date(activeRun.finishedAtMs).toLocaleTimeString(i18n.language)
      : '--';
    const durationLabel = formatDuration(activeRun, t);
    const serviceHost = formatServiceHost(serviceBase);
    const summaryBlankValue = '--';
    const configOverviewItems = buildConfigOverviewItems(toConfigOverviewSettings(selectedConfig), {
      mode: 'server',
      t,
      scheduleEnabled: selectedConfig.enabled,
      scheduleLabel: savedScheduleLabel,
      includeProviderItems: false,
      includeAutoRecoverItem: false,
    });

    return (
      <Panel
        className={styles.statusPanel}
      >
        <div className={styles.statusBar}>
          <div className={styles.statusInfo}>
            <span className={`${styles.statusBadge} ${statusToneClass[activeTone]}`}>
              <span className={styles.statusDot} aria-hidden="true" />
              {getRunStatusLabel(activeRun, t)}
            </span>
            <span
              className={`${styles.statusBadge} ${
                selectedConfig.enabled ? statusToneClass.good : statusToneClass.idle
              }`}
            >
              <span className={styles.statusDot} aria-hidden="true" />
              {selectedConfig.enabled
                ? t('monitoring.server_codex_inspection_schedule_enabled')
                : t('monitoring.server_codex_inspection_schedule_disabled')}
            </span>
            <div className={styles.statusMeta}>
              <span>
                {t('monitoring.server_codex_inspection_last_run')}: {lastRunTime}
                {activeRun?.finishedAtMs ? ` · ${durationLabel}` : ''}
              </span>
              {serviceHost ? (
                <span className={styles.statusMetaHost} title={serviceBase}>
                  {serviceHost}
                </span>
              ) : null}
            </div>
          </div>
          <div className={styles.statusActions}>
            <Button variant="secondary" size="sm" onClick={() => void refreshRuns()} loading={loading}>
              {t('common.refresh')}
            </Button>
            <Button size="sm" onClick={handleRunNow} loading={running} disabled={!serviceBase || running}>
              {t('monitoring.server_codex_inspection_run_now')}
            </Button>
          </div>
        </div>

        <details className={styles.infoNote}>
          <summary>{t('monitoring.server_codex_inspection_info_summary')}</summary>
          <ul className={styles.infoNoteList}>
            <li>
              <strong>{t('monitoring.server_codex_inspection_worker_poll')}:</strong>{' '}
              {t('monitoring.server_codex_inspection_effect_hint')}
            </li>
            <li>
              <strong>{t('monitoring.server_codex_inspection_time_basis')}:</strong>{' '}
              {t('monitoring.server_codex_inspection_server_time_hint')}
            </li>
            <li>
              <strong>{t('monitoring.server_codex_inspection_history_refresh')}:</strong>{' '}
              {t('monitoring.server_codex_inspection_auto_refresh_hint')}
            </li>
          </ul>
        </details>

        <CodexInspectionConfigOverview
          title={t('monitoring.codex_inspection_config_overview_title')}
          editLabel={t('monitoring.codex_inspection_config_overview_edit')}
          ariaLabel={t('monitoring.server_codex_inspection_config_summary_title')}
          copyLabel={t('monitoring.codex_inspection_settings_copy_prompt')}
          copiedLabel={t('common.copied')}
          items={configOverviewItems}
          onEdit={openConfigDrawer}
          compact
          embedded
        />

        <div className={styles.summaryGrid}>
          {[
            {
              key: 'probe-total',
              label: t('monitoring.codex_inspection_total_accounts'),
              value: activeRun ? String(activeRun.probeSetCount) : summaryBlankValue,
              meta: t('monitoring.server_codex_inspection_total_files', {
                count: activeRun?.totalFiles ?? 0,
              }),
              icon: 'probe' as const,
              accent: 'blue' as const,
            },
            {
              key: 'sampled',
              label: t('monitoring.codex_inspection_sampled_accounts'),
              value: activeRun ? String(activeRun.sampledCount) : summaryBlankValue,
              meta: getRunStatusLabel(activeRun, t),
              icon: 'sampled' as const,
              accent: 'cyan' as const,
            },
            {
              key: 'delete',
              label: t('monitoring.codex_inspection_delete_count'),
              value: activeRun ? String(activeRun.deleteCount) : summaryBlankValue,
              meta: t('monitoring.codex_inspection_delete_meta'),
              tone: 'bad' as const,
              icon: 'delete' as const,
              accent: 'red' as const,
            },
            {
              key: 'disable',
              label:
                adapter.quotaExhaustedLabel ?? t('monitoring.codex_inspection_disable_count'),
              value: activeRun
                ? String(adapter.getQuotaExhaustedCount?.(activeRun) ?? activeRun.disableCount)
                : summaryBlankValue,
              meta: adapter.showsPriorityAdjustmentSummary
                ? '仅调整优先级'
                : `${t('monitoring.codex_inspection_threshold')} ${selectedConfig.usedPercentThreshold}%`,
              tone: 'warn' as const,
              icon: 'disable' as const,
              accent: 'amber' as const,
            },
            {
              key: 'enable',
              label: t('monitoring.codex_inspection_enable_count'),
              value: activeRun ? String(activeRun.enableCount) : summaryBlankValue,
              meta: t('monitoring.codex_inspection_enable_meta'),
              tone: 'good' as const,
              icon: 'enable' as const,
              accent: 'green' as const,
            },
            {
              key: 'reauth',
              label: abnormalLabel,
              value: activeRun
                ? String(adapter.getAbnormalCount?.(activeRun) ?? activeRun.reauthCount)
                : summaryBlankValue,
              meta: adapter.abnormalLabel
                ? abnormalLabel
                : t('monitoring.codex_inspection_reauth_meta'),
              icon: 'reauth' as const,
              accent: 'violet' as const,
            },
          ].map((card) => {
            const tone: MonitoringSummaryCardProps['tone'] = card.tone;
            return (
              <MonitoringSummaryCard
                key={card.key}
                label={card.label}
                value={card.value}
                meta={card.meta}
                icon={card.icon}
                accent={card.accent}
                tone={tone}
              />
            );
          })}
        </div>
      </Panel>
    );
  };

  const handleDiscard = () => {
    if (!managerConfig) return;
    setDraft(toDraft(managerConfig.agyInspection, adapter.defaultConfig));
  };

  const renderConfigDrawer = () => {
    const sharedDraft = toSharedInspectionDraft(draft);
    const fieldErrors = validateInspectionConfigFields(sharedDraft, t);

    return (
      <InspectionConfigDrawer
        open={configDrawerOpen}
        title={t('monitoring.server_codex_inspection_config_title')}
        description={t('monitoring.server_codex_inspection_config_desc')}
        closeLabel={t('common.close')}
        focusField={configFocusField}
        onClose={handleCloseConfigDrawer}
        footer={
          <>
            <div className={styles.configDrawerStatus}>
              {hasUnsavedChanges ? (
                <span className={styles.serverUnsavedBadge}>
                  {t('monitoring.server_codex_inspection_unsaved')}
                </span>
              ) : (
                <span>{t('monitoring.server_codex_inspection_saved_applied')}</span>
              )}
            </div>
            <div className={styles.configDrawerActions}>
              <Button
                variant="secondary"
                size="sm"
                onClick={handleDiscard}
                disabled={saving || !hasUnsavedChanges}
              >
                {t('monitoring.server_codex_inspection_discard')}
              </Button>
              <Button
                size="sm"
                onClick={handleSave}
                loading={saving}
                disabled={loading || saving || !hasUnsavedChanges}
              >
                {t('monitoring.server_codex_inspection_save_apply')}
              </Button>
            </div>
          </>
        }
      >
        <section className={styles.configSection} id="schedule">
          <header className={styles.configSectionHeader}>
            <span>{t('monitoring.server_codex_inspection_config_group_schedule')}</span>
          </header>
          <div className={styles.serverConfigGrid}>
            <div className={`${styles.serverField} ${styles.serverFieldWide}`}>
              <ToggleSwitch
                checked={draft.enabled}
                onChange={(value) => updateDraft('enabled', value)}
                label={t('monitoring.server_codex_inspection_enable_schedule')}
              />
            </div>

            <div className={`${styles.serverField} ${styles.serverFieldWide}`}>
              <span className={styles.serverFieldLabel}>
                {t('monitoring.server_codex_inspection_schedule_mode')}
              </span>
              <div className={styles.scheduleSegmented} role="tablist" aria-label={t('monitoring.server_codex_inspection_schedule_mode')}>
                {scheduleOptions.map((opt) => {
                  const active = draft.scheduleMode === opt.value;
                  return (
                    <button
                      key={opt.value}
                      type="button"
                      role="tab"
                      aria-selected={active}
                      className={`${styles.scheduleSegmentButton} ${active ? styles.scheduleSegmentButtonActive : ''}`}
                      onClick={() =>
                        updateDraft(
                          'scheduleMode',
                          isScheduleMode(opt.value)
                            ? opt.value
                            : adapter.defaultConfig.schedule.mode
                        )
                      }
                    >
                      {opt.label}
                    </button>
                  );
                })}
              </div>
            </div>

            {draft.scheduleMode === 'interval' ? (
              <div className={styles.serverField}>
                <Input
                  id="intervalMinutes"
                  label={t('monitoring.server_codex_inspection_interval_minutes')}
                  type="number"
                  min="1"
                  value={draft.intervalMinutes}
                  onChange={(event) => updateDraft('intervalMinutes', event.target.value)}
                />
              </div>
            ) : (
              <>
                <div className={`${styles.serverField} ${styles.serverFieldHalf}`}>
                  <Input
                    id="timePoints"
                    label={t('monitoring.server_codex_inspection_time_points')}
                    value={draft.timePoints}
                    onChange={(event) => updateDraft('timePoints', event.target.value)}
                    placeholder="09:00, 13:30, 22:00"
                    hint={t('monitoring.server_codex_inspection_time_points_hint')}
                  />
                </div>
                <div className={`${styles.serverField} ${styles.serverFieldHalf}`}>
                  <span className={styles.serverFieldLabel}>
                    {t('monitoring.server_codex_inspection_time_zone')}
                  </span>
                  <Select
                    value={draft.timeZone}
                    options={timeZoneOptions}
                    onChange={(value) => updateDraft('timeZone', value)}
                    ariaLabel={t('monitoring.server_codex_inspection_time_zone')}
                  />
                </div>
              </>
            )}
          </div>
        </section>

        {adapter.autoActionDescription ? (
          <p className={styles.infoNote}>{adapter.autoActionDescription}</p>
        ) : null}

        <InspectionConfigFields
          draft={sharedDraft}
          errors={fieldErrors}
          t={t}
          showTargetConfiguration={false}
          autoRecoveryAvailable={false}
          userAgentSectionLabel={adapter.userAgentSectionLabel ?? 'Antigravity'}
          onFieldChange={updateSharedDraftField}
          onXaiInferenceEnabledChange={rejectUnsupportedAgyConfigChange}
          onAutoActionModeChange={(value) => updateDraft('autoActionMode', value)}
          onAutoRecoverEnabledChange={rejectUnsupportedAgyConfigChange}
        />

        {adapter.supportsProbeStagger ? (
          <section className={styles.configSection}>
            <header className={styles.configSectionHeader}>
              <span>{t('monitoring.wxai_inspection_settings_group_stagger')}</span>
            </header>
            <div className={styles.serverConfigGrid}>
              <div className={styles.serverField}>
                <Input
                  id="workerStartStaggerMs"
                  label={t('monitoring.wxai_inspection_settings_worker_start_stagger_label')}
                  hint={t('monitoring.wxai_inspection_settings_worker_start_stagger_hint')}
                  type="number"
                  min={0}
                  step={1}
                  value={draft.workerStartStaggerMs}
                  onChange={(event) =>
                    updateDraft('workerStartStaggerMs', event.target.value)
                  }
                />
              </div>
              <div className={styles.serverField}>
                <Input
                  id="accountTakeStaggerMs"
                  label={t('monitoring.wxai_inspection_settings_account_take_stagger_label')}
                  hint={t('monitoring.wxai_inspection_settings_account_take_stagger_hint')}
                  type="number"
                  min={0}
                  step={1}
                  value={draft.accountTakeStaggerMs}
                  onChange={(event) =>
                    updateDraft('accountTakeStaggerMs', event.target.value)
                  }
                />
              </div>
            </div>
          </section>
        ) : null}
      </InspectionConfigDrawer>
    );
  };

  const renderRunsPanel = () => (
    <Panel
      title={t('monitoring.server_codex_inspection_history_title')}
      subtitle={t('monitoring.server_codex_inspection_history_desc')}
    >
      {runs.length > 0 ? (
        <div className={styles.runHistoryList} role="tablist" aria-label={t('monitoring.server_codex_inspection_history_title')}>
          {runs.map((run) => {
            const tone = getRunTone(run);
            const selected = run.id === selectedRunId;
            const ariaLabel = `${getRunStatusLabel(run, t)} · #${run.id} · ${formatTimestamp(run.startedAtMs, i18n.language)}`;
            return (
              <button
                type="button"
                key={run.id}
                role="tab"
                aria-selected={selected}
                aria-label={ariaLabel}
                className={`${styles.runHistoryCard} ${selected ? styles.runHistoryCardActive : ''}`}
                onClick={() => void handleSelectRun(run.id)}
              >
                <div className={styles.runHistoryCardHead}>
                  <span className={`${styles.statusBadge} ${statusToneClass[tone]}`}>
                    <span className={styles.statusDot} aria-hidden="true" />
                    {getRunStatusLabel(run, t)}
                  </span>
                  <span className={styles.runHistoryCardId}>#{run.id}</span>
                </div>
                <div className={styles.runHistoryCardMeta}>
                  <span>{formatTimestamp(run.startedAtMs, i18n.language)}</span>
                  <span>{formatTrigger(run, t)} · {t('monitoring.codex_inspection_sampled_accounts')}: {run.sampledCount}</span>
                </div>
                <div className={styles.runHistoryCardActionPills}>
                  {supportsActionExecution ? (
                    <>
                      {run.deleteCount > 0 ? (
                        <span className={`${styles.runHistoryCardPill} ${styles.runHistoryCardPillDelete}`}>
                          {t('monitoring.codex_inspection_action_delete')} {run.deleteCount}
                        </span>
                      ) : null}
                      {run.disableCount > 0 ? (
                        <span className={`${styles.runHistoryCardPill} ${styles.runHistoryCardPillDisable}`}>
                          {t('monitoring.codex_inspection_action_disable')} {run.disableCount}
                        </span>
                      ) : null}
                      {run.enableCount > 0 ? (
                        <span className={`${styles.runHistoryCardPill} ${styles.runHistoryCardPillEnable}`}>
                          {t('monitoring.codex_inspection_action_enable')} {run.enableCount}
                        </span>
                      ) : null}
                    </>
                  ) : null}
                  {run.reauthCount > 0 ? (
                    <span className={`${styles.runHistoryCardPill} ${styles.runHistoryCardPillReauth}`}>
                      {abnormalLabel} {run.reauthCount}
                    </span>
                  ) : null}
                  {run.keepCount > 0 ? (
                    <span className={`${styles.runHistoryCardPill} ${styles.runHistoryCardPillKeep}`}>
                      {t('monitoring.codex_inspection_action_keep')} {run.keepCount}
                    </span>
                  ) : null}
                </div>
              </button>
            );
          })}
        </div>
      ) : (
        <div className={styles.emptyBlock}>{t('monitoring.server_codex_inspection_history_empty')}</div>
      )}
    </Panel>
  );

  const renderResultsPanel = () => {
    const canonicalExecutableIds = getCanonicalServerCodexInspectionActionIds(resultRows);
    const mixedActionIds = getMixedServerCodexInspectionActionIds(resultRows);
    const executableResults = resultRows.filter((item) => canonicalExecutableIds.has(item.id));
    const canExecuteActions = detail?.run.status === 'completed';
    const resultsRun = detail?.run ?? null;
    const actionFilterCounts = getActionFilterCounts(resultItems);
    const handlingFilterCounts = countHandlingStates(resultItems);
    const panelResult: CodexInspectionRunResult | null = resultsRun
      ? {
          settings: {
            baseUrl: serviceBase,
            token: '',
            targetTypes: ['codex'],
            targetType: selectedConfig.targetType,
            workers: selectedConfig.workers,
            deleteWorkers: selectedConfig.deleteWorkers,
            timeout: selectedConfig.timeout,
            retries: selectedConfig.retries,
            userAgent: selectedConfig.userAgent,
            xaiInferenceUserAgent: '',
            xaiInferenceEnabled: false,
            xaiInferenceModel: '',
            xaiInferencePrompt: '',
            usedPercentThreshold: selectedConfig.usedPercentThreshold,
            sampleSize: selectedConfig.sampleSize,
          },
          files: [],
          results: resultItems,
          summary: {
            totalFiles: resultsRun.totalFiles,
            probeSetCount: resultsRun.probeSetCount,
            sampledCount: resultsRun.sampledCount,
            disabledCount: resultsRun.disabledCount,
            enabledCount: resultsRun.enabledCount,
            deleteCount: resultsRun.deleteCount,
            disableCount: resultsRun.disableCount,
            enableCount: resultsRun.enableCount,
            reauthCount: resultsRun.reauthCount,
            keepCount: resultsRun.keepCount,
            usedPercentThreshold: selectedConfig.usedPercentThreshold,
            sampled: selectedConfig.sampleSize > 0,
            plannedActionPreview: [],
          },
          startedAt: resultsRun.startedAtMs,
          finishedAt: resultsRun.finishedAtMs ?? resultsRun.updatedAtMs,
        }
      : null;

    const filterLabel = (filter: ActionFilter) => {
      switch (filter) {
        case 'delete':
          return t('monitoring.codex_inspection_filter_delete');
        case 'disable':
          return t('monitoring.codex_inspection_filter_disable');
        case 'enable':
          return t('monitoring.codex_inspection_filter_enable');
        case 'reauth':
          return abnormalLabel;
        case 'keep':
          return t('monitoring.codex_inspection_action_keep');
        case 'all':
        default:
          return t('monitoring.codex_inspection_filter_all');
      }
    };

    const handlingFilterLabel = (filter: HandlingFilter) => {
      switch (filter) {
        case 'pending':
          return t('monitoring.codex_inspection_handling_filter_pending');
        case 'no_action':
          return t('monitoring.codex_inspection_handling_filter_no_action');
        case 'all':
        default:
          return t('monitoring.codex_inspection_handling_filter_all');
      }
    };

    const renderOperation = (item: CodexInspectionResultItem) => {
      const source = resultByKey.get(item.key);
      if (!source) {
        return <span className={styles.primaryReason}>{t('monitoring.codex_inspection_no_action')}</span>;
      }

      const actionStatus = normalizeServerCodexInspectionActionStatus(source);
      const detailText = formatServerResultStateDetail(source, t, adapter);
      const showDetail =
        detailText &&
        detailText !== '--' &&
        !source.actionError &&
        !source.errorDetail &&
        !source.error;

      return (
        <div className={styles.serverResultOperation}>
          {showDetail ? <span className={styles.primaryReason}>{detailText}</span> : null}
          {canonicalExecutableIds.has(source.id) ? (
            <Button
              size="xs"
              variant={source.action === 'delete' ? 'danger' : 'secondary'}
              loading={executingResultIds.has(source.id)}
              disabled={!canExecuteActions || executingResultIds.size > 0}
              className={styles.serverResultActionButton}
              onClick={() => handleExecuteServerActions([source], 'single')}
            >
              {(() => {
                const ActionIcon = getServerActionIcon(source.action);
                return <ActionIcon size={13} />;
              })()}
              {resolveActionLabel(source.action, t)}
            </Button>
          ) : actionStatus === 'needs_review' || mixedActionIds.has(source.id) ? (
            <span className={styles.primaryReason}>
              {t('monitoring.server_codex_inspection_action_needs_review_hint')}
            </span>
          ) : isActionableServerCodexInspectionResult(source) ? (
            <span className={styles.primaryReason}>
              {t('monitoring.server_codex_inspection_file_level_action_hint')}
            </span>
          ) : source.action === 'reauth' ? (
            <span className={styles.primaryReason}>{abnormalLabel}</span>
          ) : source.action === 'keep' ? (
            <span className={styles.primaryReason}>
              {t('monitoring.codex_inspection_no_action')}
            </span>
          ) : null}
        </div>
      );
    };

    return (
      <CodexInspectionResultsPanel
        result={panelResult}
        filteredResults={resultPagination.pageItems}
        suggestedResults={resultItems.filter((item) => item.action !== 'keep')}
        pendingActionCount={executableResults.length}
        manualActionCount={actionFilterCounts.reauth}
        handlingFilterCounts={handlingFilterCounts}
        filterCounts={actionFilterCounts}
        handlingFilter={handlingFilter}
        actionFilter={actionFilter}
        pagination={resultPagination}
        pageSize={resultPageSize}
        pageSizeOptions={CODEX_INSPECTION_RESULT_PAGE_SIZE_OPTIONS}
        executing={executingAllActions}
        isInspectionInFlight={Boolean(hasRunningRun)}
        t={t}
        title={t('monitoring.codex_inspection_results_title')}
        subtitle={formatResultsDescription(resultsRun, i18n.language, t)}
        stateHeaderLabel={formatResultStateHeader(resultsRun, t)}
        onActionFilterChange={setActionFilter}
        onHandlingFilterChange={setHandlingFilter}
        onPageChange={setResultPage}
        onPageSizeChange={handleResultPageSizeChange}
        onExecutePlanned={() => handleExecuteServerActions(executableResults, 'bulk')}
        onExecuteSingle={() => undefined}
        filterLabel={filterLabel}
        handlingFilterLabel={handlingFilterLabel}
        renderOperation={renderOperation}
        summarizeError={adapter.resultStatusLabel ? (item) => item.error : undefined}
        formatAction={(action) =>
          action === 'reauth' ? abnormalLabel : formatActionLabel(action, t)
        }
        formatState={
          adapter.resultStatusLabel
            ? (item) => formatServerResultStateDetail(resultByKey.get(item.key)!, t, adapter)
            : undefined
        }
        manualActionLabel={adapter.abnormalLabel ? abnormalLabel : undefined}
        showActionControls={supportsActionExecution}
      />
    );
  };

  const handleCopyLogs = useCallback(
    async (logs: AgyInspectionLog[]) => {
      if (!logs.length) return;
      const lines = logs.map((entry) => {
        const ts = new Date(entry.createdAtMs).toISOString();
        const detail = entry.detail
          ? ` ${typeof entry.detail === 'string' ? entry.detail : JSON.stringify(entry.detail)}`
          : '';
        return `[${ts}] [${entry.level}] ${entry.message}${detail}`;
      });
      try {
        await navigator.clipboard.writeText(lines.join('\n'));
        showNotification(t('monitoring.server_codex_inspection_logs_copied'), 'success');
      } catch {
        showNotification(t('monitoring.server_codex_inspection_logs_copy_failed'), 'error');
      }
    },
    [showNotification, t]
  );

  const renderLogsPanel = (logs: AgyInspectionLog[]) => {
    const counts: Record<'all' | 'info' | 'success' | 'warning' | 'error', number> = {
      all: logs.length,
      info: 0,
      success: 0,
      warning: 0,
      error: 0,
    };
    for (const entry of logs) {
      if (entry.level === 'info' || entry.level === 'success' || entry.level === 'warning' || entry.level === 'error') {
        counts[entry.level] += 1;
      }
    }
    const filterOptions: ReadonlyArray<{ value: typeof logLevelFilter; label: string }> = [
      { value: 'all', label: t('monitoring.server_codex_inspection_filter_all') },
      { value: 'info', label: t('monitoring.server_codex_inspection_log_level_info') },
      { value: 'success', label: t('monitoring.server_codex_inspection_log_level_success') },
      { value: 'warning', label: t('monitoring.server_codex_inspection_log_level_warning') },
      { value: 'error', label: t('monitoring.server_codex_inspection_log_level_error') },
    ];
    const filtered = logLevelFilter === 'all' ? logs : logs.filter((entry) => entry.level === logLevelFilter);
    return (
      <Panel
        title={t('monitoring.codex_inspection_logs_title')}
        subtitle={t('monitoring.server_codex_inspection_logs_desc')}
        extra={
          <div className={styles.logToolbar}>
            {logs.length > 0 ? (
              <div className={styles.logFilterGroup} role="tablist" aria-label={t('monitoring.codex_inspection_logs_title')}>
                <div className={styles.segmentedControl}>
                  {filterOptions.map((opt) => {
                    const active = logLevelFilter === opt.value;
                    return (
                      <button
                        key={opt.value}
                        type="button"
                        role="tab"
                        aria-selected={active}
                        className={`${styles.segmentButton} ${active ? styles.segmentButtonActive : ''}`}
                        onClick={() => setLogLevelFilter(opt.value)}
                      >
                        {opt.label}
                        <span className={styles.segmentCount}>{counts[opt.value]}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ) : <span />}
            <div className={styles.logToolbarRight}>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void handleCopyLogs(logs)}
                disabled={logs.length === 0}
                aria-label={t('monitoring.server_codex_inspection_logs_copy')}
              >
                {t('monitoring.server_codex_inspection_logs_copy')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setLogsCollapsed((previous) => !previous)}
                disabled={logs.length === 0}
              >
                {logsCollapsed
                  ? t('monitoring.codex_inspection_expand_logs')
                  : t('monitoring.codex_inspection_fold_logs')}
              </Button>
            </div>
          </div>
        }
      >
        {!logsCollapsed ? (
          <div className={styles.logList}>
            {filtered.length > 0 ? (
              filtered.map((entry) => (
                <div
                  key={entry.id}
                  className={`${styles.logRow} ${logLevelClass[entry.level] ?? styles.logInfo}`}
                >
                  <span className={styles.logTime}>{formatTimestamp(entry.createdAtMs, i18n.language)}</span>
                  <span className={styles.logMessage}>
                    {entry.message}
                    {entry.detail ? (
                      <small className={styles.serverLogDetail}>
                        {typeof entry.detail === 'string'
                          ? entry.detail
                          : JSON.stringify(entry.detail)}
                      </small>
                    ) : null}
                  </span>
                </div>
              ))
            ) : (
              <div className={styles.emptyBlockSmall}>{t('monitoring.codex_inspection_logs_empty')}</div>
            )}
          </div>
        ) : (
          <div className={styles.logCollapsedBar}>
            <span>{t('monitoring.codex_inspection_logs_collapsed', { count: logs.length })}</span>
          </div>
        )}
      </Panel>
    );
  };

  return (
    <div className={styles.page}>
      {adapter.renderModeTabs()}

      {error ? (
        <div className={styles.topErrorBar} role="alert" aria-live="polite">
          <span>{error}</span>
          <div className={styles.topErrorActions}>
            <Button variant="secondary" size="sm" onClick={() => void refreshRuns()} loading={loading}>
              {t('common.retry')}
            </Button>
          </div>
        </div>
      ) : null}
      {renderStatusPanel()}
      <div className={styles.serverDetailGrid}>
        {renderRunsPanel()}
        <div className={styles.serverDetailPanels}>
          {detail?.run.error ? <div className={styles.serverError} role="alert">{detail.run.error}</div> : null}
          {renderResultsPanel()}
          {renderLogsPanel(detail?.logs ?? [])}
        </div>
      </div>
      {renderConfigDrawer()}
    </div>
  );
}
