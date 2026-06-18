import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import {
  antigravityInspectionApi,
  type AntigravityAccountStatusItem,
  type AntigravityTargetProvider,
} from '@/services/api/antigravityInspectionService';
import { useAuthStore } from '@/stores';
import { AgyInspectionModeTabs } from './components/AgyInspectionModeTabs';
import styles from './CodexInspectionPage.module.scss';

type AgyAccountStatusPageProps = {
  provider: Extract<AntigravityTargetProvider, 'claude' | 'gemini'>;
};

const formatDateTime = (value?: number) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString();
};

const getTitle = (provider: AgyAccountStatusPageProps['provider']) =>
  provider === 'claude' ? 'Claude 账号状态' : 'Gemini 账号状态';

export function AgyAccountStatusPage({ provider }: AgyAccountStatusPageProps) {
  const { t } = useTranslation();
  const managementKey = useAuthStore((state) => state.managementKey);
  const availability = usePanelFeatureAvailability();
  const [items, setItems] = useState<AntigravityAccountStatusItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastRunAt, setLastRunAt] = useState<number | undefined>();

  const serviceBase = availability.managerServiceBase;
  const title = getTitle(provider);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      if (!serviceBase || !managementKey) {
        throw new Error('请先连接 Manager Server');
      }
      const detail = await antigravityInspectionApi.getAccountStatusLatest(serviceBase, managementKey, provider);
      setItems(detail.items || []);
      setLastRunAt(detail.run?.startedAtMs);
    } catch (caught) {
      setItems([]);
      setError(caught instanceof Error ? caught.message : '加载 Agy 账号状态失败');
    } finally {
      setLoading(false);
    }
  }, [managementKey, provider, serviceBase]);

  const run = useCallback(async () => {
    setRunning(true);
    setError(null);
    try {
      if (!serviceBase || !managementKey) {
        throw new Error('请先连接 Manager Server');
      }
      const detail = await antigravityInspectionApi.run(serviceBase, managementKey, provider);
      setItems(
        (detail.results || []).map((item) => ({
          ...item,
          resultCreatedAtMs: item.createdAtMs,
        }))
      );
      setLastRunAt(detail.run?.startedAtMs);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : '执行 Agy 巡检失败');
    } finally {
      setRunning(false);
    }
  }, [managementKey, provider, serviceBase]);

  useEffect(() => {
    if (availability.checking) return;
    void load();
  }, [availability.checking, load]);

  const summary = useMemo(() => {
    const disabled = items.filter((item) => item.disabled).length;
    const unauthorized = items.filter((item) => item.statusCode === 401).length;
    return { total: items.length, disabled, unauthorized };
  }, [items]);

  return (
    <div className={styles.page}>
      <AgyInspectionModeTabs activeMode={provider} />
      <section className={styles.panel}>
        <div>
          <h1>{t(`monitoring.agy_${provider}_account_status`, { defaultValue: title })}</h1>
          <p>最近巡检：{formatDateTime(lastRunAt)}；共 {summary.total} 个账号，停用 {summary.disabled} 个，401 {summary.unauthorized} 个。</p>
        </div>
        <Button type="button" onClick={() => void run()} disabled={running || loading}>
          {running ? '巡检中...' : '立即巡检'}
        </Button>
        {error ? <p role="alert">{error}</p> : null}
      </section>
      <section className={styles.panel}>
        <h2>{title}</h2>
        {loading ? (
          <p>加载中...</p>
        ) : items.length === 0 ? (
          <p>暂无 Agy 账号状态，先执行一次巡检。</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderSpacing: 0 }}>
              <thead>
                <tr>
                  <th align="left">账号</th>
                  <th align="left">文件</th>
                  <th align="left">优先级</th>
                  <th align="left">状态码</th>
                  <th align="left">动作</th>
                  <th align="left">原因</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => (
                  <tr key={`${item.id}-${item.accountKey}`}>
                    <td>{item.displayAccount}</td>
                    <td>{item.fileName}</td>
                    <td>{item.priority ?? '-'}</td>
                    <td>{item.statusCode ?? '-'}</td>
                    <td>{item.action}</td>
                    <td>{item.error || item.actionReason || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
