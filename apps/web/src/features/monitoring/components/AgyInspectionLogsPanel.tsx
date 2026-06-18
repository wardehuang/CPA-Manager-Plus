import type { AntigravityInspectionLog } from '@/services/api/antigravityInspectionService';
import styles from '../CodexInspectionPage.module.scss';

type AgyInspectionLogsPanelProps = {
  logs: AntigravityInspectionLog[];
};

export function AgyInspectionLogsPanel({ logs }: AgyInspectionLogsPanelProps) {
  return (
    <section className={styles.panel}>
      <h2>巡检日志</h2>
      {logs.length === 0 ? (
        <p>暂无日志</p>
      ) : (
        <ul>
          {logs.map((item) => (
            <li key={item.id}>
              [{item.level}] {item.message}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
