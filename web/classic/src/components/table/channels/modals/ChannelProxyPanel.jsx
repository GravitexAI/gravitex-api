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

import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Radio,
  RadioGroup,
  Select,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { API } from '../../../../helpers';

const { Text } = Typography;

// setting.proxy 的随机哨兵值，与后端 relaykit/dto/channel_settings.go 的 ProxyRandom 一致
export const PROXY_RANDOM = 'random';
export const PROXY_RANDOM_SCOPE_PUBLIC = 'public';
export const PROXY_RANDOM_SCOPE_ALL = 'all';
// 随机池并发测试上限
const PROXY_POOL_TEST_LIMIT = 20;

export const getProxyMode = (proxy) => {
  const value = (proxy || '').trim();
  if (!value) return 'none';
  if (value === PROXY_RANDOM) return 'random';
  return 'fixed';
};

// 随机池候选：默认仅对外，scope=all 时全表；选了地区则只保留这些地区
const filterRandomPool = (items, scope, regions) => {
  const regionSet = new Set(regions || []);
  return items.filter((item) => {
    if (scope !== PROXY_RANDOM_SCOPE_ALL && item.publicFlag !== 1) return false;
    if (regionSet.size > 0 && !regionSet.has(item.region)) return false;
    return true;
  });
};

const proxyOptionLabel = (item) =>
  [item.proxy, item.region, item.remark]
    .filter((part) => part && part.trim())
    .join(' · ');

const probeProxy = async (proxy) => {
  try {
    const res = await API.post(
      '/api/channel/test_proxy',
      { proxy },
      { skipErrorHandler: true },
    );
    const { success, message, data } = res.data || {};
    if (!success || !data)
      return { state: 'failed', error: message || 'request failed' };
    if (!data.ok) return { state: 'failed', error: data.error };
    return { state: 'ok', latencyMs: data.latency_ms };
  } catch (error) {
    return { state: 'failed', error: error?.message || String(error) };
  }
};

const ProxyTestTag = ({ result }) => {
  const { t } = useTranslation();
  if (!result) return null;
  if (result.state === 'testing')
    return <Tag color='blue'>{t('测试中...')}</Tag>;
  if (result.state === 'ok')
    return <Tag color='green'>✓ {result.latencyMs} ms</Tag>;
  return (
    <Tag
      color='red'
      title={result.error}
      style={{ maxWidth: '60%', minWidth: 0, flexShrink: 1 }}
    >
      <span
        style={{
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        ✗ {result.error}
      </span>
    </Tag>
  );
};

/**
 * 渠道代理三态设置：不设置（直连）/ 随机（按时间片轮换代理池）/ 指定代理机（从代理表选择），附带连通性测试。
 * proxy / regions / scope 由父组件持有，变更通过 onChange(key, value) 回传。
 */
const ChannelProxyPanel = ({ proxy, regions, scope, onChange }) => {
  const { t } = useTranslation();
  const [mode, setMode] = useState(() => getProxyMode(proxy));
  const [options, setOptions] = useState({ items: [], regions: [] });
  const [loadError, setLoadError] = useState('');
  const [testResults, setTestResults] = useState({});

  // 跟随外部值变化（编辑时渠道数据异步加载回填）
  useEffect(() => {
    const derived = getProxyMode(proxy);
    if (derived !== 'none') {
      setMode(derived);
    } else {
      setMode((current) => (current === 'random' ? 'none' : current));
    }
  }, [proxy]);

  useEffect(() => {
    let cancelled = false;
    API.get('/api/channel/proxy_options', { skipErrorHandler: true })
      .then((res) => {
        if (cancelled) return;
        const { success, message, data } = res.data || {};
        if (!success) {
          setLoadError(message || '');
          return;
        }
        setOptions({ items: data?.items || [], regions: data?.regions || [] });
      })
      .catch((error) => {
        if (!cancelled) setLoadError(error?.message || String(error));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const proxyValue = (proxy || '').trim();
  const randomPool = useMemo(
    () => filterRandomPool(options.items, scope, regions),
    [options.items, scope, regions],
  );
  const showLegacyFixedValue =
    mode === 'fixed' &&
    proxyValue !== '' &&
    !options.items.some((item) => item.proxy === proxyValue);

  const changeMode = (nextMode) => {
    setMode(nextMode);
    setTestResults({});
    onChange('proxy', nextMode === 'random' ? PROXY_RANDOM : '');
  };

  const testFixedProxy = async () => {
    setTestResults({ [proxyValue]: { state: 'testing' } });
    const result = await probeProxy(proxyValue);
    setTestResults({ [proxyValue]: result });
  };

  const testRandomPool = async () => {
    const targets = randomPool.slice(0, PROXY_POOL_TEST_LIMIT);
    setTestResults(
      Object.fromEntries(
        targets.map((item) => [item.proxy, { state: 'testing' }]),
      ),
    );
    await Promise.all(
      targets.map(async (item) => {
        const result = await probeProxy(item.proxy);
        setTestResults((current) => ({ ...current, [item.proxy]: result }));
      }),
    );
  };

  return (
    <div className='semi-form-field' style={{ paddingBottom: 12 }}>
      <div className='semi-form-field-label'>
        <div className='semi-form-field-label-text'>{t('代理')}</div>
      </div>
      <RadioGroup
        type='button'
        value={mode}
        onChange={(e) => changeMode(e.target.value)}
      >
        <Radio value='none'>{t('不设置')}</Radio>
        <Radio value='random'>{t('随机')}</Radio>
        <Radio value='fixed'>{t('指定代理机')}</Radio>
      </RadioGroup>

      {loadError && mode !== 'none' && (
        <div className='mt-2'>
          <Text type='danger' size='small'>
            {t('加载代理列表失败')}: {loadError}
          </Text>
        </div>
      )}

      {mode === 'none' && (
        <div className='mt-2'>
          <Text type='tertiary' size='small'>
            {t('出站直连，不走任何代理。')}
          </Text>
        </div>
      )}

      {mode === 'random' && (
        <div className='mt-2 flex flex-col gap-2'>
          <Text type='tertiary' size='small'>
            {t(
              '从代理池按时间片轮换（默认 5 分钟/片，每渠道独立）；池为空时降级直连。',
            )}
          </Text>
          <div className='flex items-center gap-2'>
            <Text size='small'>{t('池范围')}</Text>
            <RadioGroup
              type='button'
              buttonSize='small'
              value={scope || PROXY_RANDOM_SCOPE_PUBLIC}
              onChange={(e) => onChange('proxy_random_scope', e.target.value)}
            >
              <Radio value={PROXY_RANDOM_SCOPE_PUBLIC}>{t('仅对外')}</Radio>
              <Radio value={PROXY_RANDOM_SCOPE_ALL}>{t('全部代理')}</Radio>
            </RadioGroup>
          </div>
          <Select
            multiple
            filter
            showClear
            placeholder={t('全部地区')}
            value={regions || []}
            optionList={options.regions.map((region) => ({
              label: region,
              value: region,
            }))}
            onChange={(values) =>
              onChange('proxy_random_regions', values || [])
            }
            style={{ width: '100%' }}
          />
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              size='small'
              disabled={randomPool.length === 0}
              onClick={testRandomPool}
            >
              {t('测试随机池')}
            </Button>
            <Text type='tertiary' size='small'>
              {t('当前池子 {{count}} 条', { count: randomPool.length })}
              {randomPool.length > PROXY_POOL_TEST_LIMIT &&
                ` · ${t('池子过大，仅测试前 {{limit}} 个', { limit: PROXY_POOL_TEST_LIMIT })}`}
            </Text>
          </div>
          {Object.keys(testResults).length > 0 &&
            randomPool.slice(0, PROXY_POOL_TEST_LIMIT).map((item) => (
              <div
                key={item.id}
                className='flex items-center justify-between gap-2'
              >
                <Text
                  size='small'
                  ellipsis={{ showTooltip: true }}
                  style={{ fontFamily: 'monospace', flex: 1, minWidth: 0 }}
                >
                  {item.proxy}
                  {item.region ? ` · ${item.region}` : ''}
                </Text>
                <ProxyTestTag result={testResults[item.proxy]} />
              </div>
            ))}
        </div>
      )}

      {mode === 'fixed' && (
        <div className='mt-2 flex flex-col gap-2'>
          <Select
            filter
            placeholder={t('请选择代理机')}
            value={proxyValue || undefined}
            onChange={(value) => {
              setTestResults({});
              onChange('proxy', value || '');
            }}
            style={{ width: '100%' }}
          >
            {showLegacyFixedValue && (
              <Select.Option
                value={proxyValue}
                label={`${proxyValue} · ⚠ ${t('当前值（不在代理表）')}`}
              />
            )}
            {options.items.map((item) => (
              <Select.Option
                key={item.id}
                value={item.proxy}
                label={
                  item.publicFlag === 1
                    ? proxyOptionLabel(item)
                    : `${proxyOptionLabel(item)} · ${t('内部')}`
                }
              />
            ))}
          </Select>
          {showLegacyFixedValue && (
            <Text type='warning' size='small'>
              {t('当前值不在代理表里，不改动即保留；切换其他选项即替换。')}
            </Text>
          )}
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              size='small'
              disabled={!proxyValue}
              onClick={testFixedProxy}
            >
              {t('测试该代理')}
            </Button>
            <ProxyTestTag result={testResults[proxyValue]} />
          </div>
        </div>
      )}
    </div>
  );
};

export default ChannelProxyPanel;
