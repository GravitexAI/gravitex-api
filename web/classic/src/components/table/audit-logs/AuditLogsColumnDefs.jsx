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

import React from 'react';
import { Button, Tag, Typography } from '@douyinfe/semi-ui';
import { timestamp2string } from '../../../helpers';
import {
  AUDIT_ACTION_LABELS,
  AUDIT_CATEGORY_LABELS,
  AUDIT_ROLE_LABELS,
} from './auditLabels';

export const getAuditLogsColumns = ({
  t,
  copyText,
  setDetail,
  isAdminUser,
}) => {
  const columns = [
    {
      key: 'created_at',
      title: t('时间'),
      dataIndex: 'created_at',
      render: (value) => timestamp2string(value),
    },
    {
      key: 'username',
      title: t('操作人'),
      dataIndex: 'username',
      render: (value, record) => (
        <span>
          {value || '-'}
          <Typography.Text type='tertiary' size='small'>
            {' '}
            #{record.user_id}
          </Typography.Text>
        </span>
      ),
    },
    {
      key: 'actor_role',
      title: t('角色'),
      dataIndex: 'actor_role',
      render: (value) => t(AUDIT_ROLE_LABELS[value] || String(value)),
    },
    {
      key: 'category',
      title: t('分类'),
      dataIndex: 'category',
      render: (value) => (
        <Tag color={value === 'security' ? 'orange' : 'blue'} shape='circle'>
          {t(AUDIT_CATEGORY_LABELS[value] || value)}
        </Tag>
      ),
    },
    {
      key: 'action',
      title: t('操作'),
      dataIndex: 'action',
      render: (value) => t(AUDIT_ACTION_LABELS[value] || value),
    },
    {
      key: 'content',
      title: t('内容'),
      dataIndex: 'content',
      render: (value) => (
        <Typography.Text
          ellipsis={{ showTooltip: true }}
          style={{ maxWidth: 280 }}
        >
          {value || '-'}
        </Typography.Text>
      ),
    },
    {
      key: 'success',
      title: t('结果'),
      dataIndex: 'success',
      render: (value, record) => (
        <Tag color={value ? 'green' : 'red'} shape='circle'>
          {value ? t('成功') : t('失败')}
          {record.status ? ` ${record.status}` : ''}
        </Tag>
      ),
    },
    {
      key: 'ip',
      title: 'IP',
      dataIndex: 'ip',
      render: (value) => value || '-',
    },
    {
      key: 'request_id',
      title: t('请求 ID'),
      dataIndex: 'request_id',
      render: (value) =>
        value ? (
          <Typography.Text
            link
            ellipsis={{ showTooltip: true }}
            style={{ maxWidth: 160 }}
            onClick={() => copyText(value)}
          >
            {value}
          </Typography.Text>
        ) : (
          '-'
        ),
    },
    {
      key: 'detail',
      title: '',
      dataIndex: 'operate',
      fixed: 'right',
      render: (_, record) => (
        <Button size='small' type='tertiary' onClick={() => setDetail(record)}>
          {t('详情')}
        </Button>
      ),
    },
  ];
  return isAdminUser ? columns : columns.filter((c) => c.key !== 'actor_role');
};
