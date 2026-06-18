import type { AntigravityInspectionRun } from '@/services/api/antigravityInspectionService';
import styles from '../CodexInspectionPage.module.scss';

type AgyInspectionStatusPanelProps = {
  title: string;
  run?: AntigravityInspectionRun | null;
  loading?: boolean;
  error?: string | null;
};

const formatDateTime = (value?: number) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString();
};

export function AgyInspectionStatusPanel({ title, run, loading, error }: AgyInspectionStatusPanelProps) {
  return (
    <section className={styles.panel}>
      <div>
        <h2>{title}</h2>
        <p>{loading ? '加载中...' : error || 'Antigravity 独立巡检状态'}</p>
      </div>
      {run ? (
        <div>
          <p>状态：{run.status}</p>
          <p>目标：{run.targetProvider || '-'}</p>
          <p>账号：{run.sampledCount}/{run.probeSetCount}</p>
          <p>最近运行：{formatDateTime(run.startedAtMs)}</p>
        </div>
      ) : null}
    </section>
  );
}
