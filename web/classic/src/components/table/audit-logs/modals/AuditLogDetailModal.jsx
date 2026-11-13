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
import { Descriptions, Modal, Tag } from '@douyinfe/semi-ui';
import { timestamp2string } from '../../../../helpers';
import {
  AUDIT_ACTION_LABELS,
  AUDIT_CATEGORY_LABELS,
  AUDIT_ROLE_LABELS,
} from '../auditLabels';

const AuditLogDetailModal = ({ detail, setDetail, t }) => {
  if (!detail) return null;
  const rows = [
    { key: t('时间'), value: timestamp2string(detail.created_at) },
    {
      key: t('操作人'),
      value: `${detail.username || '-'} (#${detail.user_id})`,
    },
    {
      key: t('角色'),
      value: t(
        AUDIT_ROLE_LABELS[detail.actor_role] || String(detail.actor_role),
      ),
    },
    {
      key: t('分类'),
      value: t(AUDIT_CATEGORY_LABELS[detail.category] || detail.category),
    },
    {
      key: t('操作'),
      value: `${t(AUDIT_ACTION_LABELS[detail.action] || detail.action)} (${detail.action})`,
    },
    { key: t('内容'), value: detail.content || '-' },
    {
      key: t('结果'),
      value: (
        <Tag color={detail.success ? 'green' : 'red'} shape='circle'>
          {detail.success ? t('成功') : t('失败')} {detail.status || ''}
        </Tag>
      ),
    },
    {
      key: t('请求'),
      value: [detail.method, detail.route].filter(Boolean).join(' ') || '-',
    },
    { key: 'IP', value: detail.ip || '-' },
    { key: 'User-Agent', value: detail.user_agent || '-' },
    { key: t('认证方式'), value: detail.auth_method || '-' },
    { key: t('请求 ID'), value: detail.request_id || '-' },
  ];
  return (
    <Modal
      title={t('审计日志详情')}
      visible={!!detail}
      onCancel={() => setDetail(null)}
      footer={null}
      width={720}
    >
      <Descriptions data={rows} />
      <div className='mt-3 mb-1 font-semibold'>{t('明细')}</div>
      <pre className='text-xs p-3 rounded-lg overflow-auto max-h-80 bg-[var(--semi-color-fill-0)]'>
        {JSON.stringify(detail.other ?? {}, null, 2)}
      </pre>
    </Modal>
  );
};

export default AuditLogDetailModal;
