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

import React, { useEffect, useState, useRef } from 'react';
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, showWarning } from '../../../helpers';

const CREDIT_LIMIT_DEFAULTS = {
  QuotaForNewUser: '',
  PreConsumedQuota: '',
  QuotaForInviter: '',
  QuotaForInvitee: '',
  'quota_setting.enable_free_model_pre_consume': true,
  'quota_setting.minimum_remaining_quota': '0',
  'quota_setting.model_quota_reserve': '',
};

function parseModelQuotaReserve(value) {
  if (!value || !value.trim()) return [];
  try {
    const parsed = JSON.parse(value);
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object')
      return [];
    return Object.entries(parsed).map(([model, quota], index) => ({
      id: `${model}-${index}`,
      model,
      quota: String(quota),
    }));
  } catch {
    return [];
  }
}

function serializeModelQuotaReserve(rows) {
  const result = {};
  rows.forEach((row) => {
    const model = row.model.trim();
    const quota = Number(row.quota);
    if (model && Number.isInteger(quota) && quota >= 0) result[model] = quota;
  });
  return JSON.stringify(result);
}

function ModelQuotaReserveEditor({ value, onChange, t }) {
  const [mode, setMode] = useState('table');
  const [rows, setRows] = useState(() => parseModelQuotaReserve(value));
  const lastValueRef = useRef(value);

  useEffect(() => {
    if (value === lastValueRef.current) return;
    lastValueRef.current = value;
    setRows(parseModelQuotaReserve(value));
  }, [value]);

  const updateRows = (nextRows) => {
    const serializedValue = serializeModelQuotaReserve(nextRows);
    setRows(nextRows);
    lastValueRef.current = serializedValue;
    onChange(serializedValue);
  };

  return (
    <div style={{ width: 560, maxWidth: '100%' }}>
      <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
        <Button
          type='tertiary'
          theme={mode === 'table' ? 'solid' : 'light'}
          onClick={() => setMode('table')}
        >
          {t('表格填写')}
        </Button>
        <Button
          type='tertiary'
          theme={mode === 'manual' ? 'solid' : 'light'}
          onClick={() => setMode('manual')}
        >
          {t('手动填写')}
        </Button>
      </div>
      {mode === 'manual' ? (
        <Form.TextArea
          value={value}
          onChange={onChange}
          placeholder='{"seedance*": 10000, "seedance-2-0": 20000}'
          autosize={{ minRows: 4, maxRows: 8 }}
        />
      ) : (
        <div
          style={{
            border: '1px solid var(--semi-color-border)',
            borderRadius: 6,
            padding: 12,
          }}
        >
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              marginBottom: 8,
            }}
          >
            <span style={{ color: 'var(--semi-color-text-2)' }}>
              {t('可填写精确模型名或以 * 结尾的前缀，精确匹配优先')}
            </span>
            <Button
              type='tertiary'
              theme='light'
              onClick={() =>
                updateRows([
                  ...rows,
                  { id: `${Date.now()}`, model: '', quota: '0' },
                ])
              }
            >
              + {t('新增规则')}
            </Button>
          </div>
          {rows.map((row, index) => (
            <div
              key={row.id}
              style={{ display: 'flex', gap: 8, marginBottom: 8 }}
            >
              <input
                value={row.model}
                placeholder='seedance*'
                onChange={(event) => {
                  const nextRows = [...rows];
                  nextRows[index] = { ...row, model: event.target.value };
                  updateRows(nextRows);
                }}
                style={{
                  flex: 1,
                  padding: '8px 10px',
                  border: '1px solid var(--semi-color-border)',
                  borderRadius: 6,
                }}
              />
              <input
                type='number'
                min='0'
                step='1'
                value={row.quota}
                onChange={(event) => {
                  const nextRows = [...rows];
                  nextRows[index] = { ...row, quota: event.target.value };
                  updateRows(nextRows);
                }}
                style={{
                  width: 160,
                  padding: '8px 10px',
                  border: '1px solid var(--semi-color-border)',
                  borderRadius: 6,
                }}
              />
              <Button
                type='tertiary'
                theme='light'
                onClick={() =>
                  updateRows(rows.filter((_, rowIndex) => rowIndex !== index))
                }
              >
                {t('删除')}
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export default function SettingsCreditLimit(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({ ...CREDIT_LIMIT_DEFAULTS });
  const refForm = useRef();
  const dirtyKeysRef = useRef(new Set());
  const [inputsRow, setInputsRow] = useState(inputs);
  const complianceConfirmed =
    props.options?.['payment_setting.compliance_confirmed'] === true ||
    props.options?.['payment_setting.compliance_confirmed'] === 'true';

  function onSubmit() {
    const updateKeys = new Set(dirtyKeysRef.current);
    Object.keys(inputs).forEach((key) => {
      if (
        !Object.prototype.hasOwnProperty.call(inputsRow, key) ||
        inputs[key] !== inputsRow[key]
      ) {
        updateKeys.add(key);
      }
    });
    const updateArray = [...updateKeys].map((key) => ({ key }));
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = inputs[item.key];
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        dirtyKeysRef.current.clear();
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = { ...CREDIT_LIMIT_DEFAULTS };
    for (const key of Object.keys(CREDIT_LIMIT_DEFAULTS)) {
      if (Object.prototype.hasOwnProperty.call(props.options, key)) {
        currentInputs[key] = props.options[key];
      }
    }
    dirtyKeysRef.current.clear();
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);
  return (
    <>
      <Spin spinning={loading}>
        {!complianceConfirmed && (
          <Banner
            type='warning'
            description={t(
              '设置非零邀请奖励额度前，需要先在支付设置中确认合规声明。',
            )}
            closeIcon={null}
            className='!rounded-lg mb-3'
          />
        )}
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('额度设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('新用户初始额度')}
                  field={'QuotaForNewUser'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  placeholder={''}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      QuotaForNewUser: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('请求预扣费额度')}
                  field={'PreConsumedQuota'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={t('请求结束后多退少补')}
                  placeholder={''}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      PreConsumedQuota: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('邀请新用户奖励额度')}
                  field={'QuotaForInviter'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={
                    !complianceConfirmed ? t('非零值需先确认合规声明') : ''
                  }
                  placeholder={t('例如：2000')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      QuotaForInviter: String(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Col xs={24} sm={12} md={8} lg={8} xl={6}>
                <Form.InputNumber
                  label={t('新用户使用邀请码奖励额度')}
                  field={'QuotaForInvitee'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={
                    !complianceConfirmed ? t('非零值需先确认合规声明') : ''
                  }
                  placeholder={t('例如：1000')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      QuotaForInvitee: String(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('最低剩余额度')}
                  field={'quota_setting.minimum_remaining_quota'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={t('模型请求预扣后必须保留的额度')}
                  onChange={(value) => {
                    dirtyKeysRef.current.add(
                      'quota_setting.minimum_remaining_quota',
                    );
                    setInputs((currentInputs) => ({
                      ...currentInputs,
                      'quota_setting.minimum_remaining_quota': String(value),
                    }));
                  }}
                />
              </Col>
            </Row>
            <Row>
              <Col xs={24}>
                <div style={{ marginBottom: 8, fontWeight: 600 }}>
                  {t('模型额度保留规则')}
                </div>
                <ModelQuotaReserveEditor
                  value={inputs['quota_setting.model_quota_reserve']}
                  onChange={(value) => {
                    dirtyKeysRef.current.add(
                      'quota_setting.model_quota_reserve',
                    );
                    setInputs((currentInputs) => ({
                      ...currentInputs,
                      'quota_setting.model_quota_reserve': value,
                    }));
                  }}
                  t={t}
                />
                <div
                  style={{ marginTop: 8, color: 'var(--semi-color-text-2)' }}
                >
                  {t('模型请求预扣后必须保留的额度')}
                </div>
              </Col>
            </Row>
            <Row>
              <Col>
                <Form.Switch
                  label={t('对免费模型启用预消耗')}
                  field={'quota_setting.enable_free_model_pre_consume'}
                  extraText={t(
                    '开启后，对免费模型（倍率为0，或者价格为0）的模型也会预消耗额度',
                  )}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'quota_setting.enable_free_model_pre_consume': value,
                    })
                  }
                />
              </Col>
            </Row>

            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存额度设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
