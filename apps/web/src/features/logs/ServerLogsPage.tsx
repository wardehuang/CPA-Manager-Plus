import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { IconDownload, IconRefreshCw } from '@/components/ui/icons';
import { PaginationControls } from '@/features/monitoring/components/MonitoringShared';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { serverLogsApi, type ServerLogFile } from '@/services/api/serverLogs';
import { useAuthStore, useNotificationStore } from '@/stores';
import { downloadBlob } from '@/utils/download';
import { formatDateTime, formatFileSize } from '@/utils/format';
import styles from '@/features/monitoring/MonitoringCenterPage.module.scss';
import localStyles from './ServerLogsPage.module.scss';

const PAGE_SIZE_OPTIONS = [12, 24, 50, 100] as const;

const getErrorMessage = (err: unknown): string => {
  if (err instanceof Error) return err.message;
  if (typeof err === 'string') return err;
  return '';
};

export function ServerLogsPage() {
  const { t, i18n } = useTranslation();
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const apiBase = useAuthStore((state) => state.apiBase);
  const sessionPanelBase = useAuthStore((state) => state.sessionPanelBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const showNotification = useNotificationStore((state) => state.showNotification);
  const [files, setFiles] = useState<ServerLogFile[]>([]);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState<number>(PAGE_SIZE_OPTIONS[0]);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [downloadingName, setDownloadingName] = useState<string | null>(null);

  const canLoad = connectionStatus === 'connected';
  const serverBase = sessionPanelBase || apiBase;

  const loadFiles = useCallback(async () => {
    if (!canLoad) {
      setFiles([]);
      setLoading(false);
      return;
    }

    setLoading(true);
    setError('');
    try {
      const response = await serverLogsApi.list(serverBase, managementKey, { page, pageSize });
      setFiles(Array.isArray(response.files) ? response.files : []);
      setTotal(response.total || 0);
      setTotalPages(Math.max(response.total_pages || 1, 1));
      if (response.page && response.page !== page) {
        setPage(response.page);
      }
    } catch (err: unknown) {
      const message = getErrorMessage(err);
      setFiles([]);
      setTotal(0);
      setTotalPages(1);
      setError(
        message
          ? `${t('server_logs.load_error')}: ${message}`
          : t('server_logs.load_error')
      );
    } finally {
      setLoading(false);
    }
  }, [canLoad, managementKey, page, pageSize, serverBase, t]);

  useHeaderRefresh(loadFiles, canLoad);

  useEffect(() => {
    void loadFiles();
  }, [loadFiles]);

  const pagination = useMemo(() => {
    const startItem = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const endItem = total === 0 ? 0 : Math.min(startItem + files.length - 1, total);
    return { startItem, endItem };
  }, [files.length, page, pageSize, total]);

  const handlePageSizeChange = (nextPageSize: number) => {
    setPageSize(nextPageSize);
    setPage(1);
  };

  const downloadFile = async (file: ServerLogFile) => {
    setDownloadingName(file.name);
    try {
      const response = await serverLogsApi.download(serverBase, managementKey, file.name);
      downloadBlob({
        filename: file.name,
        blob: new Blob([response.data], { type: 'text/plain' }),
      });
      showNotification(t('server_logs.download_success'), 'success');
    } catch (err: unknown) {
      const message = getErrorMessage(err);
      showNotification(
        message
          ? `${t('server_logs.download_failed')}: ${message}`
          : t('server_logs.download_failed'),
        'error'
      );
    } finally {
      setDownloadingName(null);
    }
  };

  return (
    <div className={`${styles.page} ${localStyles.page}`}>
      <section className={styles.masthead}>
        <div className={styles.mastheadCopy}>
          <span className={styles.eyebrow}>{t('server_logs.eyebrow')}</span>
          <div>
            <h1 className={styles.title}>{t('server_logs.title')}</h1>
            <p className={styles.subtitle}>{t('server_logs.description')}</p>
          </div>
        </div>
        <div className={styles.mastheadControls}>
          <div className={styles.statusBar}>
            <div className={styles.statusMeta}>
              <span>{t('server_logs.total_files', { count: total })}</span>
            </div>
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void loadFiles()}
            loading={loading}
            disabled={!canLoad}
          >
            <span className={styles.buttonContent}>
              <IconRefreshCw size={14} aria-hidden="true" />
              {t('common.refresh')}
            </span>
          </Button>
        </div>
      </section>

      <section className={`${styles.dataPanel} ${localStyles.panel}`}>
        <div className={styles.dataPanelHeader}>
          <div className={styles.dataPanelTabs}>
            <div className={`${styles.inlineMetrics} ${styles.realtimeHeaderActions}`}>
              <span>{t('server_logs.table_title')}</span>
            </div>
          </div>
          <div className={styles.dataPanelActions}>
            <div className={`${styles.inlineMetrics} ${styles.realtimeHeaderActions}`}>
              <span>{t('monitoring.pagination_info', {
                current: page,
                total: totalPages,
                start: pagination.startItem,
                end: pagination.endItem,
                count: total,
              })}</span>
            </div>
          </div>
        </div>

        <div className={`${styles.dataPanelBody} ${localStyles.panelBody}`}>
          {error ? <div className="error-box">{error}</div> : null}
          <div className={`${styles.tableWrapper} ${localStyles.tableWrapper}`}>
            <table className={`${styles.table} ${localStyles.table}`}>
              <thead>
                <tr>
                  <th>{t('server_logs.column_name')}</th>
                  <th>{t('server_logs.column_time')}</th>
                  <th>{t('server_logs.column_size')}</th>
                  <th>{t('server_logs.column_actions')}</th>
                </tr>
              </thead>
              <tbody>
                {files.map((file) => (
                  <tr key={file.name}>
                    <td>
                      <div className={styles.primaryCell}>
                        <span className={styles.monoCell}>{file.name}</span>
                      </div>
                    </td>
                    <td>{formatDateTime(file.time, i18n.language)}</td>
                    <td>{formatFileSize(file.size)}</td>
                    <td>
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => void downloadFile(file)}
                        loading={downloadingName === file.name}
                      >
                        <span className={styles.buttonContent}>
                          <IconDownload size={14} aria-hidden="true" />
                          {t('server_logs.download')}
                        </span>
                      </Button>
                    </td>
                  </tr>
                ))}
                {!loading && files.length === 0 ? (
                  <tr>
                    <td colSpan={4}>
                      <EmptyState
                        title={t('server_logs.empty_title')}
                        description={t('server_logs.empty_desc')}
                      />
                    </td>
                  </tr>
                ) : null}
                {loading && files.length === 0 ? (
                  <tr>
                    <td colSpan={4}>
                      <EmptyState title={t('server_logs.loading')} />
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
          <PaginationControls
            count={total}
            currentPage={page}
            totalPages={totalPages}
            startItem={pagination.startItem}
            endItem={pagination.endItem}
            pageSize={pageSize}
            pageSizeOptions={PAGE_SIZE_OPTIONS}
            onPageChange={setPage}
            onPageSizeChange={handlePageSizeChange}
            t={t}
          />
        </div>
      </section>
    </div>
  );
}
