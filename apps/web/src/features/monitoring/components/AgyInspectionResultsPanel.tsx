import type { AntigravityInspectionResult } from '@/services/api/antigravityInspectionService';
import styles from '../CodexInspectionPage.module.scss';

type AgyInspectionResultsPanelProps = {
  results: AntigravityInspectionResult[];
};

export function AgyInspectionResultsPanel({ results }: AgyInspectionResultsPanelProps) {
  return (
    <section className={styles.panel}>
      <h2>巡检结果</h2>
      {results.length === 0 ? (
        <p>暂无结果</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderSpacing: 0 }}>
            <thead>
              <tr>
                <th align="left">账号</th>
                <th align="left">文件</th>
                <th align="left">状态码</th>
                <th align="left">动作</th>
                <th align="left">原因</th>
              </tr>
            </thead>
            <tbody>
              {results.map((item) => (
                <tr key={`${item.id}-${item.accountKey}`}>
                  <td>{item.displayAccount}</td>
                  <td>{item.fileName}</td>
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
  );
}
