import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { EmptyState } from '@/components/ui/EmptyState';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { apiClient, pluginsApi } from '@/services/api';
import { useAuthStore, useThemeStore } from '@/stores';
import { getErrorMessage, isRecord } from '@/utils/helpers';
import { MANAGEMENT_API_PREFIX } from '@/utils/constants';
import type { PluginListResponse } from '@/types';
import { createPluginHostStyleBridge, type PluginHostStyleBridge } from './pluginHostStyle';
import {
  collectPluginResourceEntries,
  PLUGIN_RESOURCES_REFRESH_EVENT,
  resolvePluginAssetURL,
} from './pluginResources';
import styles from './PluginResourcePage.module.scss';

const hasStatus = (error: unknown, status: number) => isRecord(error) && error.status === status;

const safeDecodeURIComponent = (value = '') => {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
};

const parseMenuIndex = (value = '') => {
  const index = Number.parseInt(value, 10);
  return Number.isInteger(index) && index >= 0 ? index : -1;
};

export function PluginResourcePage() {
  const { t } = useTranslation();
  const params = useParams<{ pluginId: string; menuIndex: string }>();
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const apiBase = useAuthStore((state) => state.apiBase);
  const supportsPlugin = useAuthStore((state) => state.supportsPlugin);
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);

  const [data, setData] = useState<PluginListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const hostStyleBridgeRef = useRef<PluginHostStyleBridge | null>(null);

  const connected = connectionStatus === 'connected';
  const pluginID = useMemo(() => safeDecodeURIComponent(params.pluginId), [params.pluginId]);
  const menuIndex = useMemo(() => parseMenuIndex(params.menuIndex), [params.menuIndex]);

  const loadResource = useCallback(async () => {
    if (!connected) {
      setLoading(false);
      setError(t('notification.connection_required'));
      return;
    }
    if (!supportsPlugin) {
      setLoading(false);
      setError(t('plugin_resource.unsupported_backend'));
      return;
    }

    setLoading(true);
    setError('');
    try {
      const plugins = await pluginsApi.list();
      setData(plugins);
    } catch (err: unknown) {
      setError(
        hasStatus(err, 404)
          ? t('plugin_resource.unsupported_backend')
          : getErrorMessage(err, t('plugin_resource.load_failed'))
      );
    } finally {
      setLoading(false);
    }
  }, [connected, supportsPlugin, t]);

  useHeaderRefresh(loadResource, connected && supportsPlugin);

  useEffect(() => {
    void loadResource();
  }, [loadResource]);

  useEffect(() => {
    window.addEventListener(PLUGIN_RESOURCES_REFRESH_EVENT, loadResource);

    return () => {
      window.removeEventListener(PLUGIN_RESOURCES_REFRESH_EVENT, loadResource);
    };
  }, [loadResource]);

  const resource = useMemo(() => {
    const entries = collectPluginResourceEntries(data?.plugins ?? []);
    return entries.find((entry) => entry.pluginID === pluginID && entry.menuIndex === menuIndex);
  }, [data?.plugins, menuIndex, pluginID]);

  const iframeSrc = resource ? resolvePluginAssetURL(resource.menu.path, apiBase) : '';
  const iframeOrigin = useMemo(() => {
    if (!iframeSrc) return '';
    return new URL(iframeSrc, window.location.href).origin;
  }, [iframeSrc]);
  const pluginManagementPrefix = `${MANAGEMENT_API_PREFIX}/${pluginID}/`;

  const handlePluginAPIRequest = useCallback(
    (event: MessageEvent) => {
      const source = event.source as Window | null;
      if (!source || source !== iframeRef.current?.contentWindow) return;
      if (event.origin !== iframeOrigin) return;
      const data = event.data;
      if (!data || data.type !== 'cpa-plugin-api-request') return;
      const endpoint = data.endpoint;
      const method = typeof data.method === 'string' ? data.method.toUpperCase() : '';
      if (
        typeof endpoint !== 'string' ||
        !endpoint.startsWith(pluginManagementPrefix) ||
        endpoint.includes('://') ||
        endpoint.includes('?') ||
        endpoint.includes('#') ||
        endpoint.includes('..') ||
        !['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].includes(method)
      )
        return;
      if (typeof data.requestId !== 'string') return;

      void apiClient
        .requestRaw({
          method,
          url: endpoint.slice(MANAGEMENT_API_PREFIX.length),
          data: data.body,
          // The plugin iframe reports its own path-specific timeout at 45 seconds.
          timeout: 60 * 1000,
          headers: { 'X-CPA-Plugin-UI': pluginID },
        })
        .then((response) => {
          source.postMessage(
            {
              type: 'cpa-plugin-api-response',
              requestId: data.requestId,
              ok: true,
              response: response.data,
            },
            event.origin || '*'
          );
        })
        .catch((error: unknown) => {
          source.postMessage(
            {
              type: 'cpa-plugin-api-response',
              requestId: data.requestId,
              ok: false,
              error: getErrorMessage(error, t('plugin_resource.load_failed')),
            },
            event.origin || '*'
          );
        });
    },
    [iframeOrigin, pluginID, pluginManagementPrefix, t]
  );

  useLayoutEffect(() => {
    window.addEventListener('message', handlePluginAPIRequest);
    return () => window.removeEventListener('message', handlePluginAPIRequest);
  }, [handlePluginAPIRequest]);

  const refreshPluginHostStyle = useCallback(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;

    const options = { theme: resolvedTheme };
    const bridge = hostStyleBridgeRef.current;
    if (bridge?.refresh(options)) {
      return;
    }

    bridge?.disconnect();
    hostStyleBridgeRef.current = createPluginHostStyleBridge(iframe, options);
  }, [resolvedTheme]);

  const handleIframeLoad = useCallback(() => {
    hostStyleBridgeRef.current?.disconnect();
    hostStyleBridgeRef.current = null;
    refreshPluginHostStyle();
  }, [refreshPluginHostStyle]);

  useEffect(() => {
    if (!iframeSrc) {
      hostStyleBridgeRef.current?.disconnect();
      hostStyleBridgeRef.current = null;
      return;
    }

    refreshPluginHostStyle();
  }, [iframeSrc, refreshPluginHostStyle]);

  useEffect(
    () => () => {
      hostStyleBridgeRef.current?.disconnect();
      hostStyleBridgeRef.current = null;
    },
    []
  );

  return (
    <div className={styles.page}>
      {loading ? (
        <div className={styles.stateShell}>
          <div className={styles.statusPanel}>{t('common.loading')}</div>
        </div>
      ) : error ? (
        <div className={styles.stateShell}>
          <EmptyState title={t('plugin_resource.unavailable')} description={error} />
        </div>
      ) : !resource ? (
        <div className={styles.stateShell}>
          <EmptyState
            title={t('plugin_resource.not_found')}
            description={t('plugin_resource.not_found_desc')}
          />
        </div>
      ) : !iframeSrc ? (
        <div className={styles.stateShell}>
          <EmptyState
            title={t('plugin_resource.empty_src')}
            description={t('plugin_resource.empty_src_desc')}
          />
        </div>
      ) : (
        <iframe
          ref={iframeRef}
          className={styles.frame}
          src={iframeSrc}
          title={resource.label}
          referrerPolicy="no-referrer"
          allow="clipboard-read; clipboard-write"
          onLoad={handleIframeLoad}
        />
      )}
    </div>
  );
}
