import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { SegmentedTabs, type SegmentedTabItem } from '@/components/ui/SegmentedTabs';
import styles from '../CodexInspectionPage.module.scss';

export type AgyInspectionMode = 'claude' | 'gemini' | 'server';

type AgyInspectionModeTabsProps = {
  activeMode: AgyInspectionMode;
  showDescription?: boolean;
};

const MODES: ReadonlyArray<{
  mode: AgyInspectionMode;
  path: string;
  labelKey: string;
  fallbackLabel: string;
}> = [
  {
    mode: 'claude',
    path: '/agy-inspection/claude',
    labelKey: 'monitoring.agy_claude_account_status',
    fallbackLabel: 'Claude 账号状态',
  },
  {
    mode: 'gemini',
    path: '/agy-inspection/gemini',
    labelKey: 'monitoring.agy_gemini_account_status',
    fallbackLabel: 'Gemini 账号状态',
  },
  {
    mode: 'server',
    path: '/agy-inspection/server',
    labelKey: 'monitoring.agy_server_inspection',
    fallbackLabel: '服务器巡检',
  },
];

const getDescription = (activeMode: AgyInspectionMode) => {
  if (activeMode === 'server') {
    return '由 Manager Server 独立巡检 Antigravity 授权文件，结果写入 antigravity 专用表。';
  }
  if (activeMode === 'gemini') {
    return '查看 Antigravity Gemini 额度状态';
  }
  return '查看 Antigravity Claude 额度状态';
};

export function AgyInspectionModeTabs({ activeMode, showDescription = true }: AgyInspectionModeTabsProps) {
  const { t } = useTranslation();
  const activeModeConfig = MODES.find((item) => item.mode === activeMode) ?? MODES[0];
  const activeLabel = t(activeModeConfig.labelKey, { defaultValue: activeModeConfig.fallbackLabel });
  const modeTabs: ReadonlyArray<SegmentedTabItem<AgyInspectionMode>> = MODES.map((item) => ({
    id: item.mode,
    label: t(item.labelKey, { defaultValue: item.fallbackLabel }),
    to: item.path,
  }));

  return (
    <section className={styles.modeSwitchPanel} aria-label={t('monitoring.agy_inspection_mode_label', { defaultValue: 'Agy 巡检模式' })}>
      <div className={styles.modeSwitchMain}>
        <SegmentedTabs
          items={modeTabs}
          activeTab={activeMode}
          ariaLabel={t('monitoring.agy_inspection_mode_label', { defaultValue: 'Agy 巡检模式' })}
          equalWidth
          linkComponent={Link}
        />

        {showDescription ? (
          <div className={styles.modeSwitchCopy}>
            <span className={styles.modeSwitchEyebrow}>
              {t('monitoring.codex_inspection_mode_current', { mode: activeLabel })}
            </span>
            <p>{getDescription(activeMode)}</p>
          </div>
        ) : null}
      </div>
    </section>
  );
}
