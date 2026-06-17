import axios from 'axios';
import { LOGS_TIMEOUT_MS } from '@/utils/constants';
import { normalizeApiBase } from '@/utils/connection';

export interface ServerLogFile {
  name: string;
  time: string;
  size: number;
}

export interface ServerLogsQuery {
  page?: number;
  pageSize?: number;
}

export interface ServerLogsResponse {
  files: ServerLogFile[];
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
}

const buildUrl = (base: string, path: string) =>
  `${normalizeApiBase(base).replace(/\/$/, '')}${path}`;

const authHeaders = (managementKey?: string) =>
  managementKey ? { Authorization: `Bearer ${managementKey}` } : undefined;

export const serverLogsApi = {
  list: async (
    base: string,
    managementKey: string | undefined,
    { page = 1, pageSize = 12 }: ServerLogsQuery = {}
  ): Promise<ServerLogsResponse> => {
    const response = await axios.get<ServerLogsResponse>(buildUrl(base, '/v0/management/server-logs'), {
      params: { page, page_size: pageSize },
      headers: authHeaders(managementKey),
      timeout: LOGS_TIMEOUT_MS,
    });
    return response.data;
  },

  download: (base: string, managementKey: string | undefined, name: string) =>
    axios.get(buildUrl(base, '/v0/management/server-logs/download'), {
      params: { name },
      headers: authHeaders(managementKey),
      responseType: 'blob',
      timeout: LOGS_TIMEOUT_MS,
    }),
};
