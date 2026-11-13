/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { API, copy, isAdmin, showError, showSuccess } from '../../helpers';
import { ITEMS_PER_PAGE } from '../../constants';

// 审计日志列表数据：管理员查 /api/audit（全部），普通用户查 /api/audit/self（自己的）。
export const useAuditLogsData = () => {
  const { t } = useTranslation();
  const isAdminUser = isAdmin();
  const [logs, setLogs] = useState([]);
  const [loading, setLoading] = useState(false);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(ITEMS_PER_PAGE);
  const [logCount, setLogCount] = useState(0);
  const [formApi, setFormApi] = useState(null);
  const [detail, setDetail] = useState(null);

  const formInitValues = {
    dateRange: [],
    username: '',
    category: '',
    action: '',
    success: '',
    request_id: '',
  };

  const loadLogs = async (page = 1, size = pageSize) => {
    const values = formApi ? formApi.getValues() : {};
    const params = { p: page, page_size: size };
    if (values.dateRange && values.dateRange.length === 2) {
      params.start_timestamp = Math.floor(
        new Date(values.dateRange[0]).getTime() / 1000,
      );
      params.end_timestamp = Math.floor(
        new Date(values.dateRange[1]).getTime() / 1000,
      );
    }
    for (const key of [
      'username',
      'category',
      'action',
      'success',
      'request_id',
    ]) {
      const value = (values[key] ?? '').toString().trim();
      if (value && !(key === 'username' && !isAdminUser)) params[key] = value;
    }
    setLoading(true);
    try {
      const res = await API.get(
        isAdminUser ? '/api/audit' : '/api/audit/self',
        {
          params,
        },
      );
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      setLogs((data.items || []).map((item) => ({ ...item, key: item.id })));
      setLogCount(data.total || 0);
      setActivePage(page);
      setPageSize(size);
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadLogs(1, pageSize).then();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const copyText = async (text) => {
    if (await copy(text)) {
      showSuccess(t('已复制：') + text);
    }
  };

  return {
    t,
    isAdminUser,
    logs,
    loading,
    activePage,
    pageSize,
    logCount,
    formApi,
    setFormApi,
    formInitValues,
    detail,
    setDetail,
    copyText,
    refresh: () => loadLogs(1, pageSize),
    handlePageChange: (page) => loadLogs(page, pageSize),
    handlePageSizeChange: (size) => loadLogs(1, size),
  };
};
