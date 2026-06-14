import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { SegmentedTabs, type SegmentedTabItem } from '@/components/ui/SegmentedTabs';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import styles from '../CodexInspectionPage.module.scss';

export type CodexInspectionMode = 'server' | 'status' | 'local';

type CodexInspectionModeTabsProps = {
  activeMode: CodexInspectionMode;
  showDescription?: boolean;
};

const MODES: ReadonlyArray<{
  mode: CodexInspectionMode;
  path: string;
  labelKey: string;
  fallbackLabel: string;
}> = [
  {
    mode: 'server',
    path: '/codex-inspection/server',
    labelKey: 'monitoring.codex_inspection_mode_server',
    fallbackLabel: '服务端巡检',
  },
  {
    mode: 'status',
    path: '/codex-inspection/status',
    labelKey: 'monitoring.codex_inspection_mode_status',
    fallbackLabel: '账号状态',
  },
  {
    mode: 'local',
    path: '/codex-inspection/local',
    labelKey: 'monitoring.codex_inspection_mode_local',
    fallbackLabel: '本机巡检',
  },
];

const getModeDescription = (activeMode: CodexInspectionMode, t: ReturnType<typeof useTranslation>['t']) => {
  if (activeMode === 'server') {
    return String(
      t('monitoring.codex_inspection_mode_server_desc', {
        defaultValue: '通过 Manager Server 执行账号巡检、变更判定、后台运行和历史记录。',
      })
    );
  }
  if (activeMode === 'status') {
    return String(
      t('monitoring.codex_inspection_mode_status_desc', {
        defaultValue: '查看 Codex 账号状态、配额窗口、401 异常和重置时间。',
      })
    );
  }
  return String(
    t('monitoring.codex_inspection_mode_local_desc', {
      defaultValue: '在当前浏览器会话中执行一次性本机巡检。',
    })
  );
};

export function CodexInspectionModeTabs({ activeMode, showDescription = true }: CodexInspectionModeTabsProps) {
  const { t } = useTranslation();
  const availability = usePanelFeatureAvailability();
  const activeModeConfig = MODES.find((item) => item.mode === activeMode) ?? MODES[0];
  const activeLabel = t(activeModeConfig.labelKey, { defaultValue: activeModeConfig.fallbackLabel });
  const visibleModes = MODES.filter(
    (item) =>
      item.mode !== 'local' &&
      (item.mode !== 'server' ||
        item.mode === activeMode ||
        availability.checking ||
        availability.serverCodexInspectionAvailable)
  );
  const modeTabs: ReadonlyArray<SegmentedTabItem<CodexInspectionMode>> = visibleModes.map(
    (item) => ({
      id: item.mode,
      label: t(item.labelKey, { defaultValue: item.fallbackLabel }),
      to: item.path,
    })
  );

  return (
    <section
      className={styles.modeSwitchPanel}
      aria-label={t('monitoring.codex_inspection_mode_label')}
    >
      <div className={styles.modeSwitchMain}>
        <SegmentedTabs
          items={modeTabs}
          activeTab={activeMode}
          ariaLabel={t('monitoring.codex_inspection_mode_label')}
          equalWidth
          linkComponent={Link}
        />

        {showDescription ? (
          <div className={styles.modeSwitchCopy}>
            <span className={styles.modeSwitchEyebrow}>
              {t('monitoring.codex_inspection_mode_current', { mode: activeLabel })}
            </span>
            <p>{getModeDescription(activeMode, t)}</p>
          </div>
        ) : null}
      </div>
    </section>
  );
}
