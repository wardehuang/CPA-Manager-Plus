import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { IconRefreshCw } from '@/components/ui/icons';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import {
  usageServiceApi,
  getUsageServiceErrorCode,
  type AccountProcessingPolicy,
  type AccountProcessingPolicyPatch,
} from '@/services/api/usageService';
import { useAuthStore, useNotificationStore } from '@/stores';
import styles from './AccountProcessingPolicySection.module.scss';

type CapabilityKey =
  | 'codexQuotaCooldown'
  | 'authIssueQueue'
  | 'authIssueAutoDisable';

const patchKeyByCapability: Record<CapabilityKey, keyof AccountProcessingPolicyPatch> = {
  codexQuotaCooldown: 'codexQuotaCooldownEnabled',
  authIssueQueue: 'authIssueQueueEnabled',
  authIssueAutoDisable: 'authIssueAutoDisableEnabled',
};

export function AccountProcessingPolicySection() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const managementKey = useAuthStore((state) => state.managementKey);
  const { showNotification } = useNotificationStore();
  const featureAvailability = usePanelFeatureAvailability();
  const managerServiceBase = featureAvailability.managerServiceBase;

  const [status, setStatus] = useState<AccountProcessingPolicy | null>(null);
  const [loading, setLoading] = useState(false);
  const [savingKey, setSavingKey] = useState<CapabilityKey | null>(null);
  const [loadError, setLoadError] = useState('');
  const [saveError, setSaveError] = useState<{ key: CapabilityKey; message: string } | null>(null);

  const load = useCallback(async () => {
    if (!managerServiceBase || !managementKey) return;
    setLoading(true);
    setLoadError('');
    setSaveError(null);
    try {
      const data = await usageServiceApi.getAccountProcessingPolicy(managerServiceBase, managementKey);
      setStatus(data);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err || 'request failed');
      setLoadError(message);
      showNotification(
        t('accountPolicy.load_failed', { message, defaultValue: `Load failed: ${message}` }),
        'error'
      );
    } finally {
      setLoading(false);
    }
  }, [managerServiceBase, managementKey, showNotification, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const updateCapability = useCallback(
    async (key: CapabilityKey, value: boolean) => {
      if (!managerServiceBase || !managementKey) return;
      if (key === 'authIssueAutoDisable' && value) {
        const confirmed = window.confirm(
          t('accountPolicy.authIssueAutoDisable_confirm', {
            defaultValue:
              'Enable auth issue auto-disable? It only disables matching auth files, never deletes them, never auto-recovers them, and still requires manual handling.',
          })
        );
        if (!confirmed) return;
      }
      setSavingKey(key);
      setSaveError(null);
      try {
        const patch: AccountProcessingPolicyPatch = { [patchKeyByCapability[key]]: value };
        const data = await usageServiceApi.updateAccountProcessingPolicy(
          managerServiceBase,
          managementKey,
          patch
        );
        setStatus(data);
        showNotification(
          t('accountPolicy.save_success', { defaultValue: 'Account processing policy updated.' }),
          'success'
        );
      } catch (err) {
        const code = getUsageServiceErrorCode(err);
        const message =
          code === 'account_processing_policy_env_locked'
            ? t('accountPolicy.env_locked_hint', {
                defaultValue:
                  'This switch is locked by an environment variable. Update the service environment variable and restart to change it.',
              })
            : err instanceof Error
              ? err.message
              : String(err || 'request failed');
        setSaveError({ key, message });
        showNotification(
          t('accountPolicy.save_failed', { message, defaultValue: `Save failed: ${message}` }),
          'error'
        );
      } finally {
        setSavingKey(null);
      }
    },
    [managerServiceBase, managementKey, showNotification, t]
  );

  const renderCapabilityCard = (key: CapabilityKey) => {
    const capability = status?.[key];
    if (!capability) return null;
    const enabled = Boolean(capability.enabled);
    const configured = capability.configured ?? capability.enabled;
    const locked = Boolean(capability.locked);
    const source = capability.source || 'startup';
    const dependencyUnmet = Boolean(
      key === 'authIssueAutoDisable' && status && !status.authIssueQueue.enabled
    );
    const toggleDisabled =
      loading ||
      savingKey !== null ||
      locked ||
      (key === 'authIssueAutoDisable' && dependencyUnmet && !configured);

    return (
      <section className={styles.card} key={key}>
        <header className={styles.cardHeader}>
          <div className={styles.cardHeading}>
            <h4 className={styles.cardTitle}>{t(`accountPolicy.${key}_title`)}</h4>
            <div className={styles.effectiveRow}>
              <span className={styles.effectiveLabel}>
                {t('accountPolicy.effective_label', { defaultValue: 'Effective' })}
              </span>
              <span
                className={`${styles.badge} ${enabled ? styles.badgeOn : styles.badgeOff}`}
                data-testid={`account-policy-${key}-badge`}
              >
                {enabled
                  ? t('accountPolicy.state_on', { defaultValue: 'On' })
                  : t('accountPolicy.state_off', { defaultValue: 'Off' })}
              </span>
            </div>
          </div>
          <p className={styles.cardDescription}>{t(`accountPolicy.${key}_description`)}</p>
        </header>

        <div className={styles.controlRow}>
          <div className={styles.controlLeft}>
            <strong>
              {locked
                ? t('accountPolicy.locked_by_env', { defaultValue: 'Locked by environment' })
                : t('accountPolicy.runtime_editable', { defaultValue: 'Runtime editable' })}
            </strong>
            <span>
              {t(`accountPolicy.source_${source}`, { defaultValue: source })}
            </span>
          </div>
          <ToggleSwitch
            checked={Boolean(configured)}
            onChange={(value) => void updateCapability(key, value)}
            disabled={toggleDisabled}
            ariaLabel={t(`accountPolicy.${key}_title`)}
          />
        </div>

        {locked ? (
          <p className={styles.envLockedReason}>
            {t('accountPolicy.env_locked_reason', {
              envKey: capability.envKey,
              defaultValue:
                'Locked by the {{envKey}} environment variable. Update it and restart the service to change this switch.',
            })}
          </p>
        ) : null}

        <details className={styles.advancedInfo}>
          <summary className={styles.advancedSummary}>
            {t('accountPolicy.advanced_summary', { defaultValue: 'Advanced: config field / environment variable' })}
          </summary>
          <div className={styles.advancedBody}>
            <div className={styles.metaRow}>
              <span className={styles.metaLabel}>
                {t('accountPolicy.meta_config_key', { defaultValue: 'Config key' })}
              </span>
              <span className={styles.metaValue}>
                <code>{capability.configFileKey}</code>
              </span>
            </div>
            <div className={styles.metaRow}>
              <span className={styles.metaLabel}>
                {t('accountPolicy.meta_env_key', { defaultValue: 'Environment variable' })}
              </span>
              <span className={styles.metaValue}>
                <code>{capability.envKey}</code>
              </span>
            </div>
          </div>
        </details>

        <ul className={styles.behaviorList}>
          {(
            t(`accountPolicy.${key}_behavior`, {
              returnObjects: true,
              defaultValue: [],
            }) as string[]
          ).map((line: string, idx: number) => (
            <li key={`${key}-behavior-${idx}`}>{line}</li>
          ))}
        </ul>

        {dependencyUnmet ? (
          <p className={styles.dependencyNote}>
            {configured
              ? t('accountPolicy.authIssueAutoDisable_configured_dependency_note')
              : t('accountPolicy.authIssueAutoDisable_dependency_note')}
          </p>
        ) : null}

        {key === 'authIssueQueue' ? (
          <div className={styles.cardActions}>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigate('/monitoring/account-actions')}
            >
              {t('accountPolicy.open_auth_issues', { defaultValue: 'Open Auth Issues' })}
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
            {t('accountPolicy.section_title', { defaultValue: 'Account Processing Policy' })}
          </h3>
          <p className={styles.sectionHint}>
            {t('accountPolicy.section_hint', {
              defaultValue:
                'Controls how Manager Server handles quota cooldowns, auth issues, and auto-disable based on request-monitoring events.',
            })}
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => void load()} disabled={loading || savingKey !== null}>
          <IconRefreshCw size={14} />
          {t('accountPolicy.refresh', { defaultValue: 'Refresh' })}
        </Button>
      </div>

      <div className={styles.runtimeNote}>
        <IconRefreshCw size={14} className={styles.runtimeNoteIcon} />
        <span>
          {t('accountPolicy.runtime_note', {
            defaultValue:
              'Unlocked switches are saved to this Manager Server and take effect without restarting. Switches locked by environment variables cannot be changed here.',
          })}
        </span>
      </div>

      <p className={styles.gatingNote}>
        {t('accountPolicy.gating_note', {
          defaultValue:
            'Disabling a switch only stops new request-monitoring events from being processed. Tasks already queued or in progress will continue to finish.',
        })}
      </p>

      {loadError && !status ? (
        <div className={styles.errorState}>
          <strong>{t('accountPolicy.load_failed_title', { defaultValue: 'Load failed' })}</strong>
          <span>{loadError}</span>
        </div>
      ) : (
        <>
          {saveError ? (
            <div className={styles.saveErrorBanner} role="alert">
              <strong>{t(`accountPolicy.${saveError.key}_title`)}</strong>
              <span>{saveError.message}</span>
            </div>
          ) : null}
          {loadError ? (
            <div className={styles.saveErrorBanner} role="alert">
              <strong>{t('accountPolicy.load_failed_title', { defaultValue: 'Load failed' })}</strong>
              <span>{loadError}</span>
            </div>
          ) : null}
          <div className={styles.cards}>
            {(
              ['codexQuotaCooldown', 'authIssueQueue', 'authIssueAutoDisable'] as CapabilityKey[]
            ).map(renderCapabilityCard)}
          </div>
        </>
      )}
    </section>
  );
}
