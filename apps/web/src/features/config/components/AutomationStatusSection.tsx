import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { IconRefreshCw } from '@/components/ui/icons';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import { usageServiceApi, type AutomationStatus } from '@/services/api/usageService';
import { useAuthStore, useNotificationStore } from '@/stores';
import styles from './AutomationStatusSection.module.scss';

type CapabilityKey = 'quotaCooldown' | 'accountActions' | 'accountActionsAutoDisable';

export function AutomationStatusSection() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const managementKey = useAuthStore((state) => state.managementKey);
  const { showNotification } = useNotificationStore();
  const featureAvailability = usePanelFeatureAvailability();
  const managerServiceBase = featureAvailability.managerServiceBase;

  const [status, setStatus] = useState<AutomationStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    if (!managerServiceBase || !managementKey) return;
    setLoading(true);
    setError('');
    try {
      const data = await usageServiceApi.getAutomationStatus(managerServiceBase, managementKey);
      setStatus(data);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err || 'request failed');
      setError(message);
      showNotification(
        t('automation.load_failed', { message, defaultValue: `Load failed: ${message}` }),
        'error'
      );
    } finally {
      setLoading(false);
    }
  }, [managerServiceBase, managementKey, showNotification, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const renderCapabilityCard = (key: CapabilityKey) => {
    const capability = status?.[key];
    if (!capability) return null;
    const enabled = Boolean(capability.enabled);
    const dependencyUnmet = Boolean(
      key === 'accountActionsAutoDisable' && status && !status.accountActions.enabled
    );

    return (
      <section className={styles.card} key={key}>
        <header className={styles.cardHeader}>
          <div className={styles.cardHeading}>
            <h4 className={styles.cardTitle}>{t(`automation.${key}_title`)}</h4>
            <span
              className={`${styles.badge} ${enabled ? styles.badgeOn : styles.badgeOff}`}
              data-testid={`automation-${key}-badge`}
            >
              {enabled
                ? t('automation.state_on', { defaultValue: 'On' })
                : t('automation.state_off', { defaultValue: 'Off' })}
            </span>
          </div>
          <p className={styles.cardDescription}>{t(`automation.${key}_description`)}</p>
        </header>

        <dl className={styles.metaList}>
          <div className={styles.metaRow}>
            <dt>{t('automation.meta_config_key', { defaultValue: 'Config key' })}</dt>
            <dd>
              <code>{capability.configFileKey}</code>
            </dd>
          </div>
          <div className={styles.metaRow}>
            <dt>{t('automation.meta_env_key', { defaultValue: 'Environment variable' })}</dt>
            <dd>
              <code>{capability.envKey}</code>
            </dd>
          </div>
        </dl>

        <ul className={styles.behaviorList}>
          {(
            t(`automation.${key}_behavior`, {
              returnObjects: true,
              defaultValue: [],
            }) as string[]
          ).map((line: string, idx: number) => (
            <li key={`${key}-behavior-${idx}`}>{line}</li>
          ))}
        </ul>

        {dependencyUnmet ? (
          <p className={styles.dependencyNote}>
            {t('automation.accountActionsAutoDisable_dependency_note')}
          </p>
        ) : null}

        {key === 'accountActions' ? (
          <div className={styles.cardActions}>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigate('/monitoring/account-actions')}
            >
              {t('automation.open_pending_accounts', { defaultValue: 'Open Auth Issues' })}
            </Button>
          </div>
        ) : null}
      </section>
    );
  };

  return (
    <section className={styles.section}>
      <div className={styles.sectionHeader}>
        <div className={styles.sectionHeaderText}>
          <h3 className={styles.sectionTitle}>
            {t('automation.section_title', { defaultValue: 'Automation Switches' })}
          </h3>
          <p className={styles.sectionHint}>
            {t('automation.section_hint', {
              defaultValue:
                'Effective automation switches for this Manager Server. Read-only here.',
            })}
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => void load()} disabled={loading}>
          <IconRefreshCw size={14} />
          {t('automation.refresh', { defaultValue: 'Refresh' })}
        </Button>
      </div>

      <div className={styles.restartNote}>
        <IconRefreshCw size={14} className={styles.restartIcon} />
        <span>
          {t('automation.restart_required_note', {
            defaultValue:
              'These switches take effect at startup. Change them via environment variables or config.json and restart the service to apply.',
          })}
        </span>
      </div>

      {error ? (
        <div className={styles.errorState}>
          <strong>{t('automation.load_failed_title', { defaultValue: 'Load failed' })}</strong>
          <span>{error}</span>
        </div>
      ) : (
        <div className={styles.cards}>
          {(['quotaCooldown', 'accountActions', 'accountActionsAutoDisable'] as CapabilityKey[]).map(
            renderCapabilityCard
          )}
        </div>
      )}
    </section>
  );
}
