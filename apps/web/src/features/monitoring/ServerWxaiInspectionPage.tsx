import { WxaiInspectionModeTabs } from '@/features/monitoring/components/WxaiInspectionModeTabs';
import {
  ServerProviderInspectionPage,
  type ServerInspectionProviderAdapter,
} from '@/features/monitoring/ServerAgyInspectionPage';
import {
  wxaiInspectionApi,
  type WxaiInspectionResult,
  type WxaiInspectionRun,
} from '@/services/api/wxaiInspectionService';

const WXAI_SERVER_INSPECTION_ADAPTER: ServerInspectionProviderAdapter = {
  defaultConfig: {
    enabled: false,
    schedule: {
      mode: 'interval',
      intervalMinutes: 60,
      timePoints: [],
      timeZone: '',
    },
    targetType: 'xai',
    workers: 4,
    deleteWorkers: 4,
    timeout: 25000,
    retries: 1,
    workerStartStaggerMs: 10000,
    accountTakeStaggerMs: 10000,
    userAgent: 'grok-shell/0.2.99 (linux; x86_64)',
    usedPercentThreshold: 100,
    sampleSize: 0,
    autoActionMode: 'none',
  },
  supportsProbeStagger: true,
  renderModeTabs: () => <WxaiInspectionModeTabs activeMode="server" />,
  getSettings: (base, managementKey) => wxaiInspectionApi.getSettings(base, managementKey),
  saveSettings: (base, managementKey, settings) =>
    wxaiInspectionApi.saveSettings(base, managementKey, settings),
  listRuns: (base, managementKey, limit) =>
    wxaiInspectionApi.listRuns(base, managementKey, limit),
  getRun: (base, managementKey, runId) =>
    wxaiInspectionApi.getRun(base, managementKey, runId),
  run: (base, managementKey) => wxaiInspectionApi.run(base, managementKey),
  executeActions: (base, managementKey, runId, resultIds) =>
    wxaiInspectionApi.executeActions(base, managementKey, runId, resultIds),
  supportsActionExecution: false,
  showsPriorityAdjustmentSummary: true,
  userAgentSectionLabel: 'Grok',
  quotaExhaustedLabel: '额度耗尽',
  getQuotaExhaustedCount: (run) =>
    (run as unknown as WxaiInspectionRun).quotaExhaustedCount,
  getAbnormalCount: (run) => (run as unknown as WxaiInspectionRun).abnormalCount,
  autoActionDescription: 'wXAi 自动处理仅调整优先级：额度耗尽为 -1，普通异常为 -2，HTTP 401 为 -4；不禁用账号。',
  resultStatusLabel: (result) => getWxaiResultStatusLabel(result),
  abnormalLabel: '账号异常',
  supportsGrok2apiSync: true,
};

function getWxaiResultStatusLabel(result: WxaiInspectionResult): string {
  if (result.isQuota || result.errorKind === 'quota_exhausted') {
    return '额度耗尽';
  }
  if (result.errorKind === 'no_quota_data') {
    return '';
  }

  const hasFailedRequest =
    (typeof result.statusCode === 'number' && result.statusCode >= 400) ||
    result.actionStatus === 'failed' ||
    result.status === 'failed' ||
    result.state === 'failed' ||
    Boolean(result.errorKind || result.error || result.errorDetail || result.actionError);

  return hasFailedRequest ? '账号异常' : '';
}

export function ServerWxaiInspectionPage() {
  return <ServerProviderInspectionPage adapter={WXAI_SERVER_INSPECTION_ADAPTER} />;
}
