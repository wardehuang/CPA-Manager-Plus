import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { SegmentedTabs, type SegmentedTabItem } from '@/components/ui/SegmentedTabs';
import styles from '../CodexInspectionPage.module.scss';

export type WxaiInspectionMode = 'status' | 'server';

type WxaiInspectionModeTabsProps = {
  activeMode: WxaiInspectionMode;
};

const MODES: ReadonlyArray<{
  mode: WxaiInspectionMode;
  path: string;
  labelKey: string;
  fallbackLabel: string;
}> = [
  {
    mode: 'status',
    path: '/wxai-inspection/status',
    labelKey: 'monitoring.wxai_inspection_mode_status',
    fallbackLabel: '账号状态',
  },
  {
    mode: 'server',
    path: '/wxai-inspection/server',
    labelKey: 'monitoring.wxai_inspection_mode_server',
    fallbackLabel: '服务端巡检',
  },
];

const getDescription = (activeMode: WxaiInspectionMode) =>
  activeMode === 'server'
    ? '通过 Manager Server 执行 wXAi 账号巡检，并查看运行结果、历史和日志。'
    : '查看 wXAi 账号状态、weekly/monthly 额度、停用状态和账号异常。';

export function WxaiInspectionModeTabs({ activeMode }: WxaiInspectionModeTabsProps) {
  const { t } = useTranslation();
  const activeModeConfig = MODES.find((item) => item.mode === activeMode) ?? MODES[0];
  const activeLabel = t(activeModeConfig.labelKey, {
    defaultValue: activeModeConfig.fallbackLabel,
  });
  const modeTabs: ReadonlyArray<SegmentedTabItem<WxaiInspectionMode>> = MODES.map((item) => ({
    id: item.mode,
    label: t(item.labelKey, { defaultValue: item.fallbackLabel }),
    to: item.path,
  }));

  return (
    <section
      className={styles.modeSwitchPanel}
      aria-label={t('monitoring.wxai_inspection_mode_label', { defaultValue: 'wXAi 巡检模式' })}
    >
      <div className={styles.modeSwitchMain}>
        <SegmentedTabs
          items={modeTabs}
          activeTab={activeMode}
          ariaLabel={t('monitoring.wxai_inspection_mode_label', {
            defaultValue: 'wXAi 巡检模式',
          })}
          equalWidth
          linkComponent={Link}
        />
        <div className={styles.modeSwitchCopy}>
          <span className={styles.modeSwitchEyebrow}>
            {t('monitoring.codex_inspection_mode_current', {
              defaultValue: '当前：{{mode}}',
              mode: activeLabel,
            })}
          </span>
          <p>{getDescription(activeMode)}</p>
        </div>
      </div>
    </section>
  );
}
