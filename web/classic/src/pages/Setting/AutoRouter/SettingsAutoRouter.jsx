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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../helpers';
import { useTranslation } from 'react-i18next';

// options 表中存放 Auto 路由配置的键，整份配置以 JSON 字符串形式存取
export const AUTO_ROUTER_OPTION_KEY = 'AutoRouter';

// 成本档位，与后端 setting/auto_router.go 的 autoCostTiers 保持一致
const TIERS = ['low', 'medium', 'high', 'max'];

// 任务类型，与后端 service/auto_router.go 的 classifyAutoTask 返回值保持一致
const TASKS = [
  'general',
  'code',
  'reasoning',
  'translation',
  'vision',
  'agent',
];

// 与后端 defaultAutoRouterSetting() 对齐的默认值
const DEFAULT_STATE = {
  enabled: false,
  default_tier: 'medium',
  stickiness_ttl: 1800,
  tier_low: [],
  tier_medium: [],
  tier_high: [],
  tier_max: [],
  task_general: [],
  task_code: [],
  task_reasoning: [],
  task_translation: [],
  task_vision: [],
  task_agent: [],
  weights_json: '{}',
  capabilities_json: '{}',
};

/** 把任意值安全转成字符串数组，非法结构一律退化为空数组 */
const toStringArray = (value) => {
  if (!Array.isArray(value)) return [];
  return value.filter((item) => typeof item === 'string' && item.trim() !== '');
};

/** 把对象格式化成便于阅读的 JSON 文本 */
const toPrettyJson = (value) => {
  if (!value || typeof value !== 'object') return '{}';
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '{}';
  }
};

/** 解析 options 中的 AutoRouter 原始字符串为扁平表单状态 */
const parseAutoRouter = (raw) => {
  const next = { ...DEFAULT_STATE };
  if (!raw || typeof raw !== 'string' || raw.trim() === '') return next;

  let cfg;
  try {
    cfg = JSON.parse(raw);
  } catch (e) {
    // 保留默认值，避免整页崩溃；用户可以重新保存来修复脏数据
    console.error('Invalid JSON for option AutoRouter:', e);
    return next;
  }
  if (!cfg || typeof cfg !== 'object') return next;

  next.enabled = cfg.enabled === true;
  if (TIERS.includes(cfg.default_tier)) {
    next.default_tier = cfg.default_tier;
  }
  if (Number.isFinite(Number(cfg.stickiness_ttl))) {
    next.stickiness_ttl = Math.max(0, Number(cfg.stickiness_ttl));
  }
  TIERS.forEach((tier) => {
    next[`tier_${tier}`] = toStringArray(cfg.tiers?.[tier]);
  });
  TASKS.forEach((task) => {
    next[`task_${task}`] = toStringArray(cfg.task_prefer?.[task]);
  });
  next.weights_json = toPrettyJson(cfg.weights);
  next.capabilities_json = toPrettyJson(cfg.capabilities);
  return next;
};

/**
 * 把 JSON 文本解析成对象；空文本视为空对象。
 * 解析失败时抛出带字段名的错误，交由调用方提示用户。
 */
const parseJsonObjectField = (text, fieldLabel) => {
  const trimmed = (text || '').trim();
  if (trimmed === '') return {};
  let parsed;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    throw new Error(fieldLabel);
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(fieldLabel);
  }
  return parsed;
};

export default function SettingsAutoRouter(props) {
  const { t } = useTranslation();

  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(DEFAULT_STATE);
  const refForm = useRef();

  const updateInput = (key, value) => {
    setInputs((prev) => ({ ...prev, [key]: value }));
  };

  async function onSubmit() {
    let weights;
    let capabilities;
    try {
      weights = parseJsonObjectField(inputs.weights_json, t('模型权重'));
      capabilities = parseJsonObjectField(
        inputs.capabilities_json,
        t('模型能力标注'),
      );
    } catch (error) {
      showError(`${t('JSON 格式错误')}：${error.message}`);
      return;
    }

    const tiers = {};
    TIERS.forEach((tier) => {
      tiers[tier] = toStringArray(inputs[`tier_${tier}`]);
    });
    // 只提交非空的任务偏好，避免写入大量空数组
    const taskPrefer = {};
    TASKS.forEach((task) => {
      const models = toStringArray(inputs[`task_${task}`]);
      if (models.length > 0) taskPrefer[task] = models;
    });

    const payload = {
      enabled: inputs.enabled,
      default_tier: inputs.default_tier,
      stickiness_ttl: Number(inputs.stickiness_ttl) || 0,
      tiers,
      task_prefer: taskPrefer,
      weights,
      capabilities,
    };

    setLoading(true);
    try {
      // 后端 option.Value 为 any 且以 fmt.Sprintf("%v") 转字符串，
      // 因此必须提交 JSON 字符串而非对象，否则会被写成 Go 的 map 打印格式
      const res = await API.put('/api/option/', {
        key: AUTO_ROUTER_OPTION_KEY,
        value: JSON.stringify(payload),
      });
      if (res?.data?.success) {
        showSuccess(t('保存成功'));
        props.refresh();
      } else {
        showError(res?.data?.message || t('保存失败，请重试'));
      }
    } catch (error) {
      console.error(error);
      showError(t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    const currentInputs = parseAutoRouter(
      props.options?.[AUTO_ROUTER_OPTION_KEY],
    );
    setInputs(currentInputs);
    if (refForm.current) {
      refForm.current.setValues(currentInputs);
    }
  }, [props.options]);

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('Auto 路由基础设置')}>
          <Row>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                label={t('启用 Auto 虚拟模型路由')}
                field={'enabled'}
                onChange={(value) => updateInput('enabled', value)}
                extraText={t(
                  '开启后用户可请求 auto / auto:low / auto:medium / auto:high / auto:max，由系统自动选择真实模型。',
                )}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Select
                label={t('默认成本档位')}
                field={'default_tier'}
                style={{ width: '100%' }}
                onChange={(value) => updateInput('default_tier', value)}
                extraText={t(
                  '用户请求 auto 且未通过 auto:tier 或 X-Cost-Tier 请求头指定档位时使用。',
                )}
              >
                {TIERS.map((tier) => (
                  <Form.Select.Option key={tier} value={tier}>
                    {tier}
                  </Form.Select.Option>
                ))}
              </Form.Select>
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                label={t('会话粘性时长（秒）')}
                field={'stickiness_ttl'}
                min={0}
                step={60}
                style={{ width: '100%' }}
                onChange={(value) => updateInput('stickiness_ttl', value)}
                extraText={t(
                  '同一会话在该时长内固定命中同一个模型，填 0 表示关闭粘性。',
                )}
              />
            </Col>
          </Row>
        </Form.Section>

        <Form.Section text={t('各档位模型池')}>
          <Row>
            {TIERS.map((tier) => (
              <Col key={tier} xs={24} sm={24} md={12} lg={12} xl={12}>
                <Form.TagInput
                  label={`${t('档位')} ${tier}`}
                  field={`tier_${tier}`}
                  style={{ width: '100%' }}
                  placeholder={t('输入模型名称后回车添加')}
                  onChange={(value) => updateInput(`tier_${tier}`, value)}
                  extraText={t(
                    '留空则回退到该用户分组下的全部可用模型。实际候选还会与分组可用模型、令牌白名单取交集。',
                  )}
                />
              </Col>
            ))}
          </Row>
        </Form.Section>

        <Form.Section text={t('任务类型偏好')}>
          <Row>
            {TASKS.map((task) => (
              <Col key={task} xs={24} sm={24} md={12} lg={8} xl={8}>
                <Form.TagInput
                  label={`${t('任务')} ${task}`}
                  field={`task_${task}`}
                  style={{ width: '100%' }}
                  placeholder={t('输入模型名称后回车添加')}
                  onChange={(value) => updateInput(`task_${task}`, value)}
                />
              </Col>
            ))}
          </Row>
          <div style={{ color: 'var(--semi-color-text-2)', fontSize: 12 }}>
            {t(
              '命中该任务类型时优先在这些模型中选择；若与当前候选池无交集则忽略偏好。',
            )}
          </div>
        </Form.Section>

        <Form.Section text={t('模型权重与能力')}>
          <Row>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                label={t('模型权重（JSON）')}
                field={'weights_json'}
                autosize={{ minRows: 6, maxRows: 14 }}
                onChange={(value) => updateInput('weights_json', value)}
                extraText={`${t('按权重加权随机选择，未配置的模型按权重 1 处理。')}${t('示例')}：{"gpt-4.1": 3, "gemini-2.5-pro": 1}`}
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                label={t('模型能力标注（JSON）')}
                field={'capabilities_json'}
                autosize={{ minRows: 6, maxRows: 14 }}
                onChange={(value) => updateInput('capabilities_json', value)}
                extraText={`${t('用于请求带工具/图片/JSON 输出时过滤模型，未标注的模型不会被排除。')}${t('示例')}：{"gpt-4.1": {"tools": true, "vision": true, "json": true}}`}
              />
            </Col>
          </Row>
        </Form.Section>

        <Button type='primary' onClick={onSubmit}>
          {t('保存 Auto 路由设置')}
        </Button>
      </Form>
    </Spin>
  );
}
