import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import type { TFunction } from 'i18next';
import { Modal } from '@/components/ui/Modal';
import { useRequestMonitoringAvailability } from '@/hooks/useRequestMonitoringAvailability';
import {
  monitoringAnalyticsApi,
  type MonitoringRawEventResponse,
} from '@/services/api/usageService';
import { useAuthStore, useNotificationStore } from '@/stores';
import { copyToClipboard } from '@/utils/clipboard';

type RawEventModalProps = {
  eventId: string | null;
  onClose: () => void;
  t: TFunction;
};

const layout = {
  stack: {
    display: 'flex',
    flexDirection: 'column',
    gap: 16,
  },
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  metaGrid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
    gap: 10,
  },
  card: {
    border: '1px solid rgba(148, 163, 184, 0.22)',
    borderRadius: 12,
    padding: '12px 14px',
    background: 'rgba(15, 23, 42, 0.36)',
  },
  statusSuccess: {
    color: '#86efac',
  },
  statusFailure: {
    color: '#fca5a5',
  },
  label: {
    display: 'block',
    marginBottom: 6,
    color: 'rgba(148, 163, 184, 0.95)',
    fontSize: 12,
  },
  value: {
    color: 'rgba(226, 232, 240, 0.98)',
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
    fontSize: 13,
    overflowWrap: 'anywhere',
  },
  muted: {
    color: 'rgba(148, 163, 184, 0.95)',
    fontSize: 13,
  },
  button: {
    border: '1px solid rgba(96, 165, 250, 0.45)',
    borderRadius: 10,
    padding: '8px 12px',
    background: 'rgba(37, 99, 235, 0.14)',
    color: '#bfdbfe',
    cursor: 'pointer',
  },
} satisfies Record<string, CSSProperties>;

type RawRecord = Record<string, unknown>;

type DetailItem = {
  label: string;
  value: unknown;
  valueStyle?: CSSProperties;
};

const isRecord = (value: unknown): value is RawRecord =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const readPath = (record: RawRecord | null, path: string[]) => {
  let current: unknown = record;
  for (const key of path) {
    if (!isRecord(current)) return undefined;
    current = current[key];
  }
  return current;
};

const readStringPath = (record: RawRecord | null, ...paths: string[][]) => {
  for (const path of paths) {
    const value = readPath(record, path);
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return '';
};

const readNumberPath = (record: RawRecord | null, ...paths: string[][]) => {
  for (const path of paths) {
    const value = readPath(record, path);
    const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : NaN;
    if (Number.isFinite(parsed)) return parsed;
  }
  return null;
};

const readBooleanPath = (record: RawRecord | null, ...paths: string[][]) => {
  for (const path of paths) {
    const value = readPath(record, path);
    if (typeof value === 'boolean') return value;
    if (typeof value === 'string' && value.trim()) return value.trim().toLowerCase() === 'true';
  }
  return null;
};

const readProxyMetadata = (record: RawRecord | null, field: string) =>
  readStringPath(
    record,
    ['metadata', `cpa.proxy.${field}`],
    ['metadata', 'cpa', 'proxy', field],
    [`cpa.proxy.${field}`],
    ['cpa', 'proxy', field]
  );

const formatTime = (timestampMs: number) => {
  if (!timestampMs) return '-';
  const date = new Date(timestampMs);
  return Number.isFinite(date.getTime()) ? date.toLocaleString() : '-';
};

const formatStatus = (data: MonitoringRawEventResponse, t: TFunction) => {
  if (!data.event.failed) return t('monitoring.raw_event_success');
  return data.event.fail_status_code
    ? t('monitoring.raw_event_failed_with_code', { code: data.event.fail_status_code })
    : t('monitoring.raw_event_failed');
};

const formatValue = (value: unknown) => {
  if (value === null || value === undefined || value === '') return '';
  return String(value);
};

const formatProxyRoute = (mode: string, scheme: string, t: TFunction) => {
  switch (mode) {
    case 'direct':
      return t('monitoring.raw_event_route_direct');
    case 'proxy':
      return scheme
        ? t('monitoring.raw_event_route_proxy_with_scheme', { scheme })
        : t('monitoring.raw_event_route_proxy');
    case 'relay':
      return t('monitoring.raw_event_route_relay');
    case 'unknown':
      return t('monitoring.raw_event_unknown');
    default:
      return mode || t('monitoring.raw_event_unrecorded');
  }
};

const formatProxySource = (source: string, proxyURL: string, t: TFunction) => {
  const labels: Record<string, string> = {
    auth: t('monitoring.raw_event_proxy_source_auth'),
    global: t('monitoring.raw_event_proxy_source_global'),
    environment: t('monitoring.raw_event_proxy_source_environment'),
    context: t('monitoring.raw_event_proxy_source_context'),
    fallback: t('monitoring.raw_event_proxy_source_fallback'),
    websocket: t('monitoring.raw_event_proxy_source_websocket'),
    default: t('monitoring.raw_event_proxy_source_default'),
    unknown: t('monitoring.raw_event_unknown'),
  };
  const sourceLabel = labels[source] || source || t('monitoring.raw_event_unrecorded');
  return proxyURL ? `${sourceLabel}: ${proxyURL}` : sourceLabel;
};

const formatEgressIP = (ip: string, status: string, t: TFunction) => {
  if (ip) return ip;
  switch (status) {
    case 'verified':
      return t('monitoring.raw_event_egress_verified');
    case 'unavailable':
      return t('monitoring.raw_event_egress_unavailable');
    case 'not_supported':
      return t('monitoring.raw_event_egress_not_supported');
    case 'pending':
      return t('monitoring.raw_event_egress_pending');
    default:
      return t('monitoring.raw_event_unrecorded');
  }
};

const formatTokenCount = (value: number | null | undefined) => {
  if (value === null || value === undefined || !Number.isFinite(value)) return '-';
  const abs = Math.abs(value);
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(abs >= 10_000_000 ? 1 : 2)}M`;
  if (abs >= 1_000) return `${(value / 1_000).toFixed(abs >= 100_000 ? 1 : 2)}K`;
  return String(value);
};

const formatDuration = (value: number | null | undefined) => {
  if (value === null || value === undefined || !Number.isFinite(value)) return '-';
  if (value < 1000) return `${Math.round(value)} ms`;
  const seconds = value / 1000;
  return `${seconds.toFixed(seconds < 10 ? 2 : 1)} s`;
};

const formatCacheHit = (cachedTokens: number, denominator: number) => {
  if (!Number.isFinite(cachedTokens) || !Number.isFinite(denominator) || denominator <= 0) return '';
  return ` (${((cachedTokens / denominator) * 100).toFixed(1)}%)`;
};

export function RawEventModal({ eventId, onClose, t }: RawEventModalProps) {
  const managementKey = useAuthStore((state) => state.managementKey);
  const availability = useRequestMonitoringAvailability();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const [data, setData] = useState<MonitoringRawEventResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const open = Boolean(eventId);

  useEffect(() => {
    if (!eventId) {
      setData(null);
      setError('');
      setLoading(false);
      return;
    }

    let cancelled = false;
    setLoading(true);
    setError('');
    setData(null);

    if (!availability.serviceBase) {
      setLoading(false);
      setError(t('monitoring.raw_event_service_unavailable'));
      return () => {
        cancelled = true;
      };
    }

    void monitoringAnalyticsApi
      .getRawEvent(availability.serviceBase, managementKey, eventId)
      .then((response) => {
        if (!cancelled) setData(response);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [availability.serviceBase, eventId, managementKey, t]);

  const rawText = useMemo(() => {
    if (!data) return '';
    if (data.raw_json !== null && data.raw_json !== undefined) {
      return JSON.stringify(data.raw_json, null, 2);
    }
    return data.raw_json_text || JSON.stringify(data.event, null, 2);
  }, [data]);

  const rawRecord = useMemo(() => {
    if (data?.raw_json && isRecord(data.raw_json)) return data.raw_json;
    if (!data?.raw_json_text) return null;
    try {
      const parsed: unknown = JSON.parse(data.raw_json_text);
      return isRecord(parsed) ? parsed : null;
    } catch {
      return null;
    }
  }, [data]);

  const details = useMemo<DetailItem[]>(() => {
    if (!data) return [];
    const inputTokens = readNumberPath(rawRecord, ['tokens', 'input_tokens']) ?? data.event.input_tokens;
    const outputTokens = readNumberPath(rawRecord, ['tokens', 'output_tokens']) ?? data.event.output_tokens;
    const reasoningTokens =
      readNumberPath(rawRecord, ['tokens', 'reasoning_tokens']) ?? data.event.reasoning_tokens;
    const cachedTokens = readNumberPath(rawRecord, ['tokens', 'cached_tokens']) ?? data.event.cached_tokens;
    const cacheDenominator = inputTokens + outputTokens + reasoningTokens;
    const compactDetected =
      readBooleanPath(
        rawRecord,
        ['metadata', 'cpa.compact.detected'],
        ['cpa.compact.detected'],
        ['cpa', 'compact', 'detected']
      ) ?? false;
    const endpoint =
      readStringPath(rawRecord, ['endpoint']) ||
      data.event.endpoint ||
      `${data.event.method} ${data.event.path}`.trim();
    const proxyMode = readProxyMetadata(rawRecord, 'mode');
    const proxySource = readProxyMetadata(rawRecord, 'source');
    const proxyScheme = readProxyMetadata(rawRecord, 'scheme');
    const proxyURL = readProxyMetadata(rawRecord, 'url');
    const egressIP = readProxyMetadata(rawRecord, 'egress_ip');
    const egressIPStatus = readProxyMetadata(rawRecord, 'egress_ip_status');

    return [
      {
        label: t('monitoring.raw_event_status'),
        value: formatStatus(data, t),
        valueStyle: data.event.failed ? layout.statusFailure : layout.statusSuccess,
      },
      {
        label: t('monitoring.raw_event_model'),
        value: readStringPath(rawRecord, ['model'], ['alias']) || data.event.model,
      },
      { label: t('monitoring.raw_event_time'), value: formatTime(data.event.timestamp_ms) },
      { label: t('monitoring.raw_event_input_tokens'), value: formatTokenCount(inputTokens) },
      { label: t('monitoring.raw_event_output_tokens'), value: formatTokenCount(outputTokens) },
      { label: t('monitoring.raw_event_reasoning_tokens'), value: formatTokenCount(reasoningTokens) },
      {
        label: t('monitoring.raw_event_cached_tokens'),
        value: `${formatTokenCount(cachedTokens)}${formatCacheHit(cachedTokens, cacheDenominator)}`,
      },
      {
        label: t('monitoring.raw_event_request_id'),
        value: readStringPath(rawRecord, ['request_id'], ['requestId']) || data.event.request_id,
      },
      { label: t('monitoring.raw_event_endpoint'), value: endpoint },
      {
        label: t('monitoring.raw_event_account'),
        value: readStringPath(rawRecord, ['metadata', 'selected_auth_id'], ['selected_auth_id']),
      },
      {
        label: t('monitoring.raw_event_total_time'),
        value: formatDuration(readNumberPath(rawRecord, ['latency_ms']) ?? data.event.latency_ms),
      },
      {
        label: t('monitoring.raw_event_first_token_time'),
        value: formatDuration(readNumberPath(rawRecord, ['ttft_ms']) ?? data.event.ttft_ms),
      },
      {
        label: t('monitoring.raw_event_project_id'),
        value: readStringPath(rawRecord, ['metadata', 'project_id'], ['metadata', 'cpa.project_id'], ['project_id']),
      },
      {
        label: t('monitoring.raw_event_prompt_cache_key'),
        value: readStringPath(
          rawRecord,
          ['metadata', 'upstream_prompt_cache_key'],
          ['metadata', 'cpa.upstream_prompt_cache_key'],
          ['upstream_prompt_cache_key'],
          ['cpa.upstream_prompt_cache_key']
        ),
      },
      {
        label: t('monitoring.raw_event_compact'),
        value: compactDetected ? t('monitoring.raw_event_true') : t('monitoring.raw_event_false'),
      },
      {
        label: t('monitoring.raw_event_network_path'),
        value: formatProxyRoute(proxyMode, proxyScheme, t),
      },
      {
        label: t('monitoring.raw_event_network_source'),
        value: formatProxySource(proxySource, proxyURL, t),
      },
      {
        label: t('monitoring.raw_event_egress_ip'),
        value: formatEgressIP(egressIP, egressIPStatus, t),
      },
    ];
  }, [data, rawRecord, t]);

  const handleCopy = async () => {
    const copied = await copyToClipboard(rawText || JSON.stringify(data, null, 2));
    showNotification(
      t(copied ? 'monitoring.raw_event_copy_success' : 'monitoring.raw_event_copy_failed'),
      copied ? 'success' : 'error'
    );
  };

  return (
    <Modal
      open={open}
      title={t('monitoring.raw_event_title')}
      onClose={onClose}
      width="min(960px, 92vw)"
    >
      <div style={layout.stack}>
        <div style={layout.toolbar}>
          <div style={layout.muted}>{t('monitoring.raw_event_source')}</div>
          <button type="button" style={layout.button} onClick={handleCopy} disabled={!data}>
            {t('monitoring.raw_event_copy_json')}
          </button>
        </div>

        {loading ? <div style={layout.muted}>{t('monitoring.raw_event_loading')}</div> : null}
        {error ? <div style={{ ...layout.card, color: '#fecaca' }}>{error}</div> : null}

        {data ? (
          <div style={layout.metaGrid}>
            {details.map((item) => (
              <div key={item.label} style={layout.card}>
                <span style={layout.label}>{item.label}</span>
                <span style={{ ...layout.value, ...item.valueStyle }}>{formatValue(item.value)}</span>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
