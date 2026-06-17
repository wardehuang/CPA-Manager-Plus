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
  code: {
    maxHeight: '52vh',
    overflow: 'auto',
    margin: 0,
    padding: 16,
    borderRadius: 14,
    border: '1px solid rgba(59, 130, 246, 0.24)',
    background: 'linear-gradient(180deg, rgba(2, 6, 23, 0.92), rgba(15, 23, 42, 0.92))',
    color: '#dbeafe',
    fontSize: 12,
    lineHeight: 1.65,
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-word',
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
  if (value === null || value === undefined || value === '') return '-';
  return String(value);
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

  const details = data
    ? [
        ['事件 ID', data.event.id],
        ['Event Hash', data.event.event_hash],
        ['请求 ID', data.event.request_id],
        ['时间', formatTime(data.event.timestamp_ms)],
        ['模型', data.event.model],
        ['Resolved Model', data.event.resolved_model],
        ['Endpoint', data.event.endpoint || `${data.event.method} ${data.event.path}`.trim()],
        ['账号', data.event.account_snapshot || data.event.auth_label_snapshot],
        ['Auth Index', data.event.auth_index],
        ['API Key Hash', data.event.api_key_hash],
        ['总 Tokens', data.event.total_tokens],
        ['耗时', data.event.latency_ms === null ? '-' : `${data.event.latency_ms} ms`],
      ]
    : [];

  const handleCopy = async () => {
    const copied = await copyToClipboard(rawText || JSON.stringify(data, null, 2));
    showNotification(copied ? '原始数据已复制' : '复制失败', copied ? 'success' : 'error');
  };

  return (
    <Modal open={open} title="原始数据" onClose={onClose} width="min(960px, 92vw)">
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
          <>
            <div style={layout.metaGrid}>
              <div style={layout.card}>
                <span style={layout.label}>状态</span>
                <span style={layout.value}>{formatStatus(data)}</span>
              </div>
              <div style={layout.card}>
                <span style={layout.label}>模型</span>
                <span style={layout.value}>{formatValue(data.event.model)}</span>
              </div>
              <div style={layout.card}>
                <span style={layout.label}>时间</span>
                <span style={layout.value}>{formatTime(data.event.timestamp_ms)}</span>
              </div>
              <div style={layout.card}>
                <span style={layout.label}>Tokens</span>
                <span style={layout.value}>{formatValue(data.event.total_tokens)}</span>
              </div>
            </div>

            <div style={layout.metaGrid}>
              {details.map(([label, value]) => (
                <div key={label} style={layout.card}>
                  <span style={layout.label}>{label}</span>
                  <span style={layout.value}>{formatValue(value)}</span>
                </div>
              ))}
            </div>

            <pre style={layout.code}>{rawText}</pre>
          </>
        ) : null}
      </div>
    </Modal>
  );
}
