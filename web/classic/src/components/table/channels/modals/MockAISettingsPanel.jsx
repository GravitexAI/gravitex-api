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

import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Card,
  Checkbox,
  InputNumber,
  Select,
  Switch,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus, IconServer } from '@douyinfe/semi-icons';

const { Text } = Typography;

// Mock AI 渠道配置在 channel settings JSON 中的键名，与后端
// relaykit/dto/channel_settings.go 的 `mock_ai` tag 一致。
export const MOCK_AI_SETTINGS_KEY = 'mock_ai';

// 以下两个硬上限必须与 relaykit/dto/mock_ai.go 的常量保持一致：
// MockAIMaxTokens 对齐 relay/helper/valid_request.go 的 maxTokensLimit；
// MockAIMaxLatencyMs 受流式空闲看门狗（constant.StreamingTimeout，
// 默认 300 秒）约束，所以是 240000 而不是设计文档里的 600000。
export const MOCK_AI_MAX_TOKENS = 1073741823;
export const MOCK_AI_MAX_LATENCY_MS = 240000;

// 用量区间（上限为 token 硬上限）
const TOKEN_RANGE_FIELDS = [
  { key: 'text_input', label: '文本输入' },
  { key: 'text_output', label: '文本输出' },
  { key: 'cache_read', label: '缓存读取' },
  { key: 'cache_creation', label: '缓存创建', messagesOnly: true },
];

// 时延区间（上限为时延硬上限）
const LATENCY_RANGE_FIELDS = [
  { key: 'latency_ms', label: '总用时 (ms)' },
  { key: 'first_token_ms', label: '流式首字 (ms)' },
];

const toSafeInt = (value) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? Math.trunc(parsed) : 0;
};

const toRange = (raw, fallback) => {
  const source = raw && typeof raw === 'object' ? raw : {};
  const min = Number(source.min);
  const max = Number(source.max);
  return {
    min: Number.isFinite(min) ? Math.trunc(min) : fallback.min,
    max: Number.isFinite(max) ? Math.trunc(max) : fallback.max,
  };
};

// 新建档位的兜底取值，保证 latency_ms.min > first_token_ms.max 这条跨字段约束天然成立。
export const createDefaultMockAIProfile = () => ({
  text_input: { min: 100, max: 1000 },
  text_output: { min: 100, max: 1000 },
  cache_read: { min: 0, max: 0 },
  cache_creation: { min: 0, max: 0 },
  cache_creation_1h_ratio: 0,
  reasoning: null,
  latency_ms: { min: 1000, max: 3000 },
  first_token_ms: { min: 100, max: 500 },
});

const normalizeProfile = (raw) => {
  const fallback = createDefaultMockAIProfile();
  const source = raw && typeof raw === 'object' ? raw : {};
  const ratio = Number(source.cache_creation_1h_ratio);
  return {
    text_input: toRange(source.text_input, fallback.text_input),
    text_output: toRange(source.text_output, fallback.text_output),
    cache_read: toRange(source.cache_read, fallback.cache_read),
    cache_creation: toRange(source.cache_creation, fallback.cache_creation),
    cache_creation_1h_ratio: Number.isFinite(ratio) ? ratio : 0,
    // reasoning 为 null 表示关闭思考输出，后端用 omitempty 的指针表达同一语义。
    reasoning: source.reasoning
      ? toRange(source.reasoning, { min: 0, max: 0 })
      : null,
    latency_ms: toRange(source.latency_ms, fallback.latency_ms),
    first_token_ms: toRange(source.first_token_ms, fallback.first_token_ms),
  };
};

// 把后端读回来的 settings.mock_ai 补齐成编辑器可直接渲染的结构。
// 缺失的 endpoints 视为全开（新建场景），已存在的 endpoints 按原值读取，
// 不替管理员把取消勾选的端点重新打开。
export const normalizeMockAIConfig = (raw) => {
  const source = raw && typeof raw === 'object' ? raw : {};
  const rawEndpoints = source.endpoints;
  const hasEndpoints = rawEndpoints && typeof rawEndpoints === 'object';
  const rawModels = source.models && typeof source.models === 'object' ? source.models : {};
  const models = {};
  Object.keys(rawModels).forEach((name) => {
    models[name] = normalizeProfile(rawModels[name]);
  });
  return {
    endpoints: {
      chat_completions: hasEndpoints
        ? rawEndpoints.chat_completions === true
        : true,
      responses: hasEndpoints ? rawEndpoints.responses === true : true,
      messages: hasEndpoints ? rawEndpoints.messages === true : true,
    },
    default: normalizeProfile(source.default),
    models,
  };
};

const validateRange = (range, limit) => {
  if (range.min < 0) return '下限不能小于 0';
  if (range.min > range.max) return '下限不能大于上限';
  if (range.max > limit) return `上限不能超过 ${limit}`;
  return '';
};

const validateProfile = (profile) => {
  const errors = {};
  TOKEN_RANGE_FIELDS.forEach(({ key }) => {
    const message = validateRange(profile[key], MOCK_AI_MAX_TOKENS);
    if (message) errors[key] = message;
  });
  if (profile.reasoning) {
    const message = validateRange(profile.reasoning, MOCK_AI_MAX_TOKENS);
    if (message) errors.reasoning = message;
  }
  LATENCY_RANGE_FIELDS.forEach(({ key }) => {
    const message = validateRange(profile[key], MOCK_AI_MAX_LATENCY_MS);
    if (message) errors[key] = message;
  });
  if (profile.cache_creation_1h_ratio < 0 || profile.cache_creation_1h_ratio > 1) {
    errors.cache_creation_1h_ratio = '1h 占比必须在 0 ~ 1 之间';
  }
  // 与后端一致：取严格大于，且比的是「总用时下限 vs 首字上限」，
  // 这样任意一次随机抽样都必然满足总用时 > 首字时间。
  if (!errors.latency_ms && profile.latency_ms.min <= profile.first_token_ms.max) {
    errors.latency_ms = '总用时下限必须大于流式首字上限';
  }
  return errors;
};

// 与 relaykit/dto/mock_ai.go 的 MockAIConfig.Validate() 等价的前端校验。
// 返回结构化错误供就地红字提示，messages 为扁平化后的提示语供提交时拦截。
export const validateMockAIConfig = (rawConfig) => {
  const config = normalizeMockAIConfig(rawConfig);
  const endpoints =
    !config.endpoints.chat_completions &&
    !config.endpoints.responses &&
    !config.endpoints.messages
      ? '至少需要勾选一个端点'
      : '';
  const defaultErrors = validateProfile(config.default);
  const models = {};
  Object.entries(config.models).forEach(([name, profile]) => {
    models[name] = {
      name: name.trim() ? '' : '请选择模型名称',
      fields: validateProfile(profile),
    };
  });

  const messages = [];
  if (endpoints) messages.push(endpoints);
  Object.values(defaultErrors).forEach((message) => {
    messages.push(`默认档位：${message}`);
  });
  Object.entries(models).forEach(([name, modelErrors]) => {
    const prefix = name.trim() ? `模型 ${name}：` : '按模型配置：';
    if (modelErrors.name) messages.push(`${prefix}${modelErrors.name}`);
    Object.values(modelErrors.fields).forEach((message) => {
      messages.push(`${prefix}${message}`);
    });
  });

  return { endpoints, default: defaultErrors, models, messages };
};

const RangeRow = ({ label, value, max, disabled, hint, error, onChange }) => (
  <div className='mb-2'>
    <div className='flex items-center gap-2'>
      <div className='w-24 shrink-0 text-xs text-gray-600'>{label}</div>
      <div className='flex-1'>
        <InputNumber
          size='small'
          style={{ width: '100%' }}
          min={0}
          max={max}
          disabled={disabled}
          value={value.min}
          onNumberChange={(next) =>
            onChange({ ...value, min: toSafeInt(next) })
          }
        />
      </div>
      <span className='text-xs text-gray-400'>~</span>
      <div className='flex-1'>
        <InputNumber
          size='small'
          style={{ width: '100%' }}
          min={0}
          max={max}
          disabled={disabled}
          value={value.max}
          onNumberChange={(next) =>
            onChange({ ...value, max: toSafeInt(next) })
          }
        />
      </div>
    </div>
    {hint && <div className='pl-24 text-xs text-gray-400 mt-1'>{hint}</div>}
    {error && <div className='pl-24 text-xs text-red-500 mt-1'>{error}</div>}
  </div>
);

const ProfileEditor = ({
  profile,
  errors,
  messagesEnabled,
  reasoningEffective,
  onChange,
}) => {
  const { t } = useTranslation();
  const messagesOnlyHint = messagesEnabled ? '' : t('仅 /v1/messages 生效');

  return (
    <>
      {TOKEN_RANGE_FIELDS.map(({ key, label, messagesOnly }) => (
        <RangeRow
          key={key}
          label={t(label)}
          value={profile[key]}
          max={MOCK_AI_MAX_TOKENS}
          disabled={messagesOnly && !messagesEnabled}
          hint={messagesOnly ? messagesOnlyHint : ''}
          error={errors[key] ? t(errors[key]) : ''}
          onChange={(range) => onChange({ ...profile, [key]: range })}
        />
      ))}

      <div className='mb-2'>
        <div className='flex items-center gap-2'>
          <div className='w-24 shrink-0 text-xs text-gray-600'>
            {t('1h 缓存占比')}
          </div>
          <div className='flex-1'>
            <InputNumber
              size='small'
              style={{ width: '100%' }}
              min={0}
              max={1}
              step={0.1}
              precision={2}
              disabled={!messagesEnabled}
              value={profile.cache_creation_1h_ratio}
              onNumberChange={(next) => {
                const parsed = Number(next);
                onChange({
                  ...profile,
                  cache_creation_1h_ratio: Number.isFinite(parsed) ? parsed : 0,
                });
              }}
            />
          </div>
        </div>
        {!messagesEnabled && (
          <div className='pl-24 text-xs text-gray-400 mt-1'>
            {t('仅 /v1/messages 生效')}
          </div>
        )}
        {errors.cache_creation_1h_ratio && (
          <div className='pl-24 text-xs text-red-500 mt-1'>
            {t(errors.cache_creation_1h_ratio)}
          </div>
        )}
      </div>

      <div className='mb-2'>
        <div className='flex items-center gap-2'>
          <div className='w-24 shrink-0 text-xs text-gray-600'>
            {t('思考输出')}
          </div>
          <Switch
            size='small'
            disabled={!reasoningEffective}
            checked={profile.reasoning !== null}
            onChange={(checked) =>
              onChange({
                ...profile,
                reasoning: checked ? { min: 100, max: 500 } : null,
              })
            }
          />
        </div>
        {!reasoningEffective && (
          <div className='pl-24 text-xs text-gray-400 mt-1'>
            {t(
              '/v1/messages 不下发思考输出，当前只勾选了该端点，此配置不会生效',
            )}
          </div>
        )}
      </div>
      {profile.reasoning !== null && (
        <RangeRow
          label={t('思考 token')}
          value={profile.reasoning}
          max={MOCK_AI_MAX_TOKENS}
          disabled={!reasoningEffective}
          error={errors.reasoning ? t(errors.reasoning) : ''}
          onChange={(range) => onChange({ ...profile, reasoning: range })}
        />
      )}

      {LATENCY_RANGE_FIELDS.map(({ key, label }) => (
        <RangeRow
          key={key}
          label={t(label)}
          value={profile[key]}
          max={MOCK_AI_MAX_LATENCY_MS}
          error={errors[key] ? t(errors[key]) : ''}
          onChange={(range) => onChange({ ...profile, [key]: range })}
        />
      ))}
    </>
  );
};

/**
 * Mock AI（渠道类型 66）配置编辑器。
 *
 * @param {string[]} channelModels 渠道「模型」字段里已填的模型名，作为按模型配置的下拉数据源
 * @param {object} config 归一化后的 mock_ai 配置（由父组件从 settings JSON 解析）
 * @param {function} onChange 配置变更回调，参数是完整的新配置对象
 */
const MockAISettingsPanel = ({ channelModels, config, onChange }) => {
  const { t } = useTranslation();
  const errors = useMemo(() => validateMockAIConfig(config), [config]);
  const modelEntries = useMemo(
    () => Object.entries(config.models || {}),
    [config],
  );

  const messagesEnabled = config.endpoints.messages === true;
  // Reasoning 档位只在 chat/completions 与 responses 下合成，
  // /v1/messages 的 usage 没有对应字段。只勾 messages 时该开关静默无效。
  const reasoningEffective =
    config.endpoints.chat_completions === true ||
    config.endpoints.responses === true;

  const configuredNames = new Set(modelEntries.map(([name]) => name));
  const hasUnnamedRow = configuredNames.has('');
  const availableModels = (channelModels || []).filter(
    (model) => model && !configuredNames.has(model),
  );

  const updateEndpoint = (key, checked) =>
    onChange({
      ...config,
      endpoints: { ...config.endpoints, [key]: checked },
    });

  const renameModel = (oldName, newName) => {
    const models = {};
    modelEntries.forEach(([name, profile]) => {
      models[name === oldName ? newName : name] = profile;
    });
    onChange({ ...config, models });
  };

  const removeModel = (name) => {
    const models = { ...config.models };
    delete models[name];
    onChange({ ...config, models });
  };

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center mb-4'>
        <IconServer size={16} className='mr-2' />
        <div>
          <Text className='text-lg font-medium'>{t('Mock AI 配置')}</Text>
          <div className='text-xs text-gray-600'>
            {t('配置合成响应的用量区间与时延区间，请求不会发往任何上游')}
          </div>
        </div>
      </div>

      <Banner
        fullMode={false}
        type='danger'
        closeIcon={null}
        className='mb-4'
        description={t(
          'Mock 渠道必须放在独立分组，不要与真实渠道共用分组，否则真实用户请求会被路由到此渠道。',
        )}
      />

      <div className='mb-4'>
        <Text className='text-sm font-medium block mb-2'>{t('支持的端点')}</Text>
        <div className='flex flex-col gap-1'>
          <Checkbox
            checked={config.endpoints.chat_completions}
            onChange={(e) =>
              updateEndpoint('chat_completions', e.target.checked)
            }
          >
            /v1/chat/completions
          </Checkbox>
          <Checkbox
            checked={config.endpoints.responses}
            onChange={(e) => updateEndpoint('responses', e.target.checked)}
          >
            /v1/responses
          </Checkbox>
          <Checkbox
            checked={config.endpoints.messages}
            onChange={(e) => updateEndpoint('messages', e.target.checked)}
          >
            /v1/messages
          </Checkbox>
        </div>
        {errors.endpoints && (
          <div className='text-xs text-red-500 mt-1'>{t(errors.endpoints)}</div>
        )}
      </div>

      <div className='mb-4'>
        <Text className='text-sm font-medium block mb-1'>{t('默认档位')}</Text>
        <div className='text-xs text-gray-400 mb-2'>
          {t('未单独配置的模型走这一档。时延上限 {{limit}} ms，受流式超时配置（STREAMING_TIMEOUT）约束', { limit: MOCK_AI_MAX_LATENCY_MS })}
        </div>
        <ProfileEditor
          profile={config.default}
          errors={errors.default}
          messagesEnabled={messagesEnabled}
          reasoningEffective={reasoningEffective}
          onChange={(profile) => onChange({ ...config, default: profile })}
        />
      </div>

      <div>
        <Text className='text-sm font-medium block mb-1'>
          {t('按模型配置')}
        </Text>
        <div className='text-xs text-gray-400 mb-2'>
          {t('按请求里的原始模型名匹配，未命中时回落到默认档位')}
        </div>
        {modelEntries.map(([name, profile]) => {
          const modelErrors = errors.models[name] || { name: '', fields: {} };
          return (
            <div
              key={name || '__unnamed__'}
              className='rounded-xl border border-gray-200 p-3 mb-3'
            >
              <div className='flex items-center gap-2 mb-2'>
                <Select
                  size='small'
                  className='flex-1'
                  style={{ width: '100%' }}
                  placeholder={t('选择模型')}
                  value={name || undefined}
                  optionList={(name ? [name] : [])
                    .concat(availableModels)
                    .map((model) => ({ label: model, value: model }))}
                  onChange={(value) => renameModel(name, value)}
                />
                <Button
                  size='small'
                  type='danger'
                  theme='borderless'
                  icon={<IconDelete />}
                  onClick={() => removeModel(name)}
                />
              </div>
              {modelErrors.name && (
                <div className='text-xs text-red-500 mb-2'>
                  {t(modelErrors.name)}
                </div>
              )}
              <ProfileEditor
                profile={profile}
                errors={modelErrors.fields}
                messagesEnabled={messagesEnabled}
                reasoningEffective={reasoningEffective}
                onChange={(next) =>
                  onChange({
                    ...config,
                    models: { ...config.models, [name]: next },
                  })
                }
              />
            </div>
          );
        })}
        <Button
          size='small'
          theme='light'
          type='primary'
          icon={<IconPlus />}
          disabled={hasUnnamedRow || availableModels.length === 0}
          onClick={() =>
            onChange({
              ...config,
              models: { ...config.models, '': createDefaultMockAIProfile() },
            })
          }
        >
          {t('添加模型配置')}
        </Button>
        {availableModels.length === 0 && !hasUnnamedRow && (
          <div className='text-xs text-gray-400 mt-1'>
            {t('渠道「模型」列表里的模型都已配置')}
          </div>
        )}
      </div>
    </Card>
  );
};

export default MockAISettingsPanel;
