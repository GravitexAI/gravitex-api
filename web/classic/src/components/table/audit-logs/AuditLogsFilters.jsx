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
import { Button, Form } from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';

import { DATE_RANGE_PRESETS } from '../../../constants/console.constants';
import { AUDIT_ACTION_LABELS, AUDIT_CATEGORY_LABELS } from './auditLabels';

const AuditLogsFilters = ({
  formInitValues,
  setFormApi,
  formApi,
  refresh,
  loading,
  isAdminUser,
  t,
}) => (
  <Form
    initValues={formInitValues}
    getFormApi={(api) => setFormApi(api)}
    onSubmit={refresh}
    allowEmpty={true}
    autoComplete='off'
    layout='vertical'
    trigger='change'
    stopValidateWithError={false}
  >
    <div className='flex flex-col gap-2'>
      <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-2'>
        <div className='col-span-1 lg:col-span-2'>
          <Form.DatePicker
            field='dateRange'
            className='w-full'
            type='dateTimeRange'
            placeholder={[t('开始时间'), t('结束时间')]}
            showClear
            pure
            size='small'
            presets={DATE_RANGE_PRESETS.map((preset) => ({
              text: t(preset.text),
              start: preset.start(),
              end: preset.end(),
            }))}
          />
        </div>
        {isAdminUser && (
          <Form.Input
            field='username'
            prefix={<IconSearch />}
            placeholder={t('用户名')}
            showClear
            pure
            size='small'
          />
        )}
        <Form.Select
          field='category'
          placeholder={t('分类')}
          showClear
          pure
          size='small'
          optionList={Object.entries(AUDIT_CATEGORY_LABELS).map(
            ([value, label]) => ({ value, label: t(label) }),
          )}
        />
        <Form.Select
          field='action'
          placeholder={t('操作')}
          showClear
          filter
          pure
          size='small'
          optionList={Object.entries(AUDIT_ACTION_LABELS).map(
            ([value, label]) => ({ value, label: `${t(label)} (${value})` }),
          )}
        />
        <Form.Select
          field='success'
          placeholder={t('结果')}
          showClear
          pure
          size='small'
          optionList={[
            { value: 'true', label: t('成功') },
            { value: 'false', label: t('失败') },
          ]}
        />
        <Form.Input
          field='request_id'
          prefix={<IconSearch />}
          placeholder={t('请求 ID')}
          showClear
          pure
          size='small'
        />
      </div>
      <div className='flex justify-end gap-2'>
        <Button
          type='tertiary'
          htmlType='submit'
          loading={loading}
          size='small'
        >
          {t('查询')}
        </Button>
        <Button
          type='tertiary'
          size='small'
          onClick={() => {
            if (!formApi) return;
            formApi.reset();
            setTimeout(() => refresh(), 100);
          }}
        >
          {t('重置')}
        </Button>
      </div>
    </div>
  </Form>
);

export default AuditLogsFilters;
