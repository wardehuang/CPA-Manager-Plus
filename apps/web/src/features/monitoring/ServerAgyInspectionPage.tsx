import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import {
  antigravityInspectionApi,
  type AntigravityInspectionLog,
  type AntigravityInspectionResult,
  type AntigravityInspectionRun,
} from '@/services/api/antigravityInspectionService';
import { useAuthStore } from '@/stores';
import { AgyInspectionLogsPanel } from './components/AgyInspectionLogsPanel';
import { AgyInspectionModeTabs } from './components/AgyInspectionModeTabs';
import { AgyInspectionResultsPanel } from './components/AgyInspectionResultsPanel';
import { AgyInspectionStatusPanel } from './components/AgyInspectionStatusPanel';
import styles from './CodexInspectionPage.module.scss';

export function ServerAgyInspectionPage() {
  const managementKey = useAuthStore((state) => state.managementKey);
  const availability = usePanelFeatureAvailability();
  const [run, setRun] = useState<AntigravityInspectionRun | null>(null);
  const [results, setResults] = useState<AntigravityInspectionResult[]>([]);
  const [logs, setLogs] = useState<AntigravityInspectionLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const serviceBase = availability.managerServiceBase;

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      if (!serviceBase || !managementKey) throw new Error('请先连接 Manager Server');
      const list = await antigravityInspectionApi.listRuns(serviceBase, managementKey, 1);
      const latest = list.items?.[0];
      if (!latest) {
        setRun(null);
        setResults([]);
        setLogs([]);
        return;
      }
      setRun(latest);
      setResults([]);
      setLogs([]);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : '加载 Agy 服务端巡检失败');
    } finally {
      setLoading(false);
    }
  }, [managementKey, serviceBase]);

  const runInspection = useCallback(async () => {
    setRunning(true);
    setError(null);
    try {
      if (!serviceBase || !managementKey) throw new Error('请先连接 Manager Server');
      const detail = await antigravityInspectionApi.run(serviceBase, managementKey, 'server');
      setRun(detail.run);
      setResults(detail.results || []);
      setLogs(detail.logs || []);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : '执行 Agy 服务端巡检失败');
    } finally {
      setRunning(false);
    }
  }, [managementKey, serviceBase]);

  useEffect(() => {
    if (availability.checking) return;
    void load();
  }, [availability.checking, load]);

  return (
    <div className={styles.page}>
      <AgyInspectionModeTabs activeMode="server" />
      <AgyInspectionStatusPanel title="服务器巡检" run={run} loading={loading || running} error={error} />
      <section className={styles.panel}>
        <Button type="button" onClick={() => void runInspection()} disabled={running || loading}>
          {running ? '巡检中...' : '立即服务端巡检'}
        </Button>
      </section>
      <AgyInspectionResultsPanel results={results} />
      <AgyInspectionLogsPanel logs={logs} />
    </div>
  );
}
