import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { Modal } from '@/components/ui/Modal';
import { useRequestMonitoringAvailability } from '@/hooks/useRequestMonitoringAvailability';
import { monitoringAnalyticsApi, type MonitoringRawEventResponse } from '@/services/api/usageService';
import { useAuthStore, useNotificationStore } from '@/stores';
import { copyToClipboard } from '@/utils/clipboard';

type RawEventModalProps = {
  eventId: string | null;
  onClose: () => void;
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

const formatTime = (timestampMs?: number) => {
  if (!timestampMs) return '-';
  const date = new Date(timestampMs);
  return Number.isFinite(date.getTime()) ? date.toLocaleString() : '-';
};

const formatStatus = (data: MonitoringRawEventResponse | null) => {
  if (!data) return '-';
  return data.event.failed
    ? `失败${data.event.fail_status_code ? ` ${data.event.fail_status_code}` : ''}`
    : '成功';
};

const formatValue = (value: unknown) => {
  if (value === null || value === undefined || value === '') return '';
  return String(value);
};

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

const formatProxyRoute = (mode: string, scheme: string) => {
  switch (mode) {
    case 'direct':
      return '直连';
    case 'proxy':
      return scheme ? `代理（${scheme}）` : '代理';
    case 'relay':
      return 'WebSocket Relay';
    case 'unknown':
      return '未知';
    default:
      return mode || '未记录';
  }
};

const formatProxySource = (source: string, proxyURL: string) => {
  const sourceLabel = (() => {
    switch (source) {
      case 'auth':
        return '账号代理';
      case 'global':
        return '全局代理';
      case 'environment':
        return '环境变量';
      case 'context':
        return '上下文 Transport';
      case 'fallback':
        return '回退路径';
      case 'websocket':
        return 'WebSocket';
      case 'default':
        return '默认 Transport';
      case 'unknown':
        return '未知';
      default:
        return source || '未记录';
    }
  })();
  return proxyURL ? `${sourceLabel}：${proxyURL}` : sourceLabel;
};

const formatEgressIP = (ip: string, status: string) => {
  if (ip) return ip;
  switch (status) {
    case 'verified':
      return '已验证，但未返回 IP';
    case 'unavailable':
      return '未获取';
    case 'not_supported':
      return '当前传输方式不支持探测';
    case 'pending':
      return '探测中';
    default:
      return '未记录';
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

export function RawEventModal({ eventId, onClose }: RawEventModalProps) {
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
      setError('Manager Server 未连接');
      return;
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
  }, [availability.serviceBase, eventId, managementKey]);

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
        label: '状态',
        value: formatStatus(data),
        valueStyle: data.event.failed ? layout.statusFailure : layout.statusSuccess,
      },
      {
        label: '模型',
        value: readStringPath(rawRecord, ['model'], ['alias']) || data.event.model,
      },
      { label: '时间', value: formatTime(data.event.timestamp_ms) },
      { label: '输入 token', value: formatTokenCount(inputTokens) },
      { label: '输出 token', value: formatTokenCount(outputTokens) },
      { label: '思考 token', value: formatTokenCount(reasoningTokens) },
      {
        label: '缓存 token',
        value: `${formatTokenCount(cachedTokens)}${formatCacheHit(cachedTokens, cacheDenominator)}`,
      },
      {
        label: '请求 ID',
        value: readStringPath(rawRecord, ['request_id'], ['requestId']) || data.event.request_id,
      },
      { label: 'Endpoint', value: endpoint },
      {
        label: '账号',
        value: readStringPath(rawRecord, ['metadata', 'selected_auth_id'], ['selected_auth_id']),
      },
      {
        label: '总时间',
        value: formatDuration(readNumberPath(rawRecord, ['latency_ms']) ?? data.event.latency_ms),
      },
      {
        label: '首字时间',
        value: formatDuration(readNumberPath(rawRecord, ['ttft_ms']) ?? data.event.ttft_ms),
      },
      {
        label: '项目 ID',
        value: readStringPath(rawRecord, ['metadata', 'project_id'], ['metadata', 'cpa.project_id'], ['project_id']),
      },
      {
        label: 'prompt_cache_key',
        value: readStringPath(
          rawRecord,
          ['metadata', 'upstream_prompt_cache_key'],
          ['metadata', 'cpa.upstream_prompt_cache_key'],
          ['upstream_prompt_cache_key'],
          ['cpa.upstream_prompt_cache_key']
        ),
      },
      { label: 'compact', value: compactDetected ? 'True' : 'False' },
      { label: '网络路径', value: formatProxyRoute(proxyMode, proxyScheme) },
      { label: '网络来源', value: formatProxySource(proxySource, proxyURL) },
      { label: '出口 IP', value: formatEgressIP(egressIP, egressIPStatus) },
    ];
  }, [data, rawRecord]);

  const handleCopy = async () => {
    const copied = await copyToClipboard(rawText || JSON.stringify(data, null, 2));
    showNotification(copied ? '原始数据已复制' : '复制失败', copied ? 'success' : 'error');
  };

  return (
    <Modal open={open} title="原始数据" onClose={onClose} width="min(960px, 92vw)" closeImmediately>
      <div style={layout.stack}>
        <div style={layout.toolbar}>
          <div style={layout.muted}>从当前数据库 usage_raw 读取上游原始 raw 并格式化展示。</div>
          <button type="button" style={layout.button} onClick={handleCopy} disabled={!data}>
            复制 JSON
          </button>
        </div>

        {loading ? <div style={layout.muted}>正在读取原始数据...</div> : null}
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
