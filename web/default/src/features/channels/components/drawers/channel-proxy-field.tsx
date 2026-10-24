/*
Copyright (C) 2023-2026 QuantumNous

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
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import { getChannelProxyOptions, testChannelProxy } from '../../api'
import type { ChannelFormValues } from '../../lib/channel-form'
import {
  PROXY_POOL_TEST_LIMIT,
  PROXY_RANDOM,
  PROXY_RANDOM_SCOPE_ALL,
  PROXY_RANDOM_SCOPE_PUBLIC,
  type ChannelProxyOption,
  type ProxyMode,
  filterRandomProxyPool,
  getProxyMode,
} from '../../lib/channel-proxy'

type ProxyTestResult =
  | { state: 'testing' }
  | { state: 'ok'; latencyMs: number }
  | { state: 'failed'; error: string }

type ChannelProxyFieldProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled?: boolean
}

const SET_VALUE_OPTIONS = { shouldDirty: true, shouldValidate: true }

async function probeProxy(proxy: string): Promise<ProxyTestResult> {
  try {
    const res = await testChannelProxy(proxy)
    if (!res.success || !res.data) {
      return { state: 'failed', error: res.message || 'request failed' }
    }
    if (!res.data.ok) return { state: 'failed', error: res.data.error }
    return { state: 'ok', latencyMs: res.data.latency_ms }
  } catch (error) {
    return {
      state: 'failed',
      error: error instanceof Error ? error.message : String(error),
    }
  }
}

function ProxyTestBadge(props: { result?: ProxyTestResult }) {
  const { t } = useTranslation()
  if (!props.result) return null
  if (props.result.state === 'testing') {
    return <Badge variant='secondary'>{t('Testing...')}</Badge>
  }
  if (props.result.state === 'ok') {
    return <Badge variant='outline'>✓ {props.result.latencyMs} ms</Badge>
  }
  return (
    <Badge
      variant='destructive'
      className='max-w-[60%] min-w-0 shrink justify-start'
      title={props.result.error}
    >
      <span className='truncate'>✗ {props.result.error}</span>
    </Badge>
  )
}

function proxyOptionLabel(option: ChannelProxyOption): string {
  return [option.proxy, option.region, option.remark]
    .filter((part) => part && part.trim())
    .join(' · ')
}

/**
 * Three-state channel proxy editor: direct / random pool rotation / fixed proxy
 * picked from the supplier_channel_proxy list, with connectivity tests.
 */
export function ChannelProxyField(props: ChannelProxyFieldProps) {
  const { t } = useTranslation()
  const proxy = props.form.watch('proxy') || ''
  const regions = props.form.watch('proxy_random_regions')
  const scope = props.form.watch('proxy_random_scope')
  const [mode, setMode] = useState<ProxyMode>(() => getProxyMode(proxy))
  const [testResults, setTestResults] = useState<
    Record<string, ProxyTestResult>
  >({})

  // Follow external value changes (form reset after the channel loads).
  useEffect(() => {
    const derived = getProxyMode(proxy)
    if (derived !== 'none') {
      setMode(derived)
    } else {
      setMode((current) => (current === 'random' ? 'none' : current))
    }
  }, [proxy])

  const optionsQuery = useQuery({
    queryKey: ['channel-proxy-options'],
    queryFn: getChannelProxyOptions,
    staleTime: 60 * 1000,
  })
  const items = useMemo(
    () => optionsQuery.data?.data?.items || [],
    [optionsQuery.data]
  )
  const regionOptions = useMemo(
    () =>
      (optionsQuery.data?.data?.regions || []).map((region) => ({
        label: region,
        value: region,
      })),
    [optionsQuery.data]
  )
  const randomPool = useMemo(
    () => filterRandomProxyPool(items, scope, regions),
    [items, scope, regions]
  )
  const fixedValueInList = items.some((item) => item.proxy === proxy.trim())
  const showLegacyFixedValue =
    mode === 'fixed' && proxy.trim() !== '' && !fixedValueInList

  const changeMode = (nextMode: ProxyMode) => {
    setMode(nextMode)
    setTestResults({})
    if (nextMode === 'random') {
      props.form.setValue('proxy', PROXY_RANDOM, SET_VALUE_OPTIONS)
      return
    }
    props.form.setValue('proxy', '', SET_VALUE_OPTIONS)
  }

  const testFixedProxy = async () => {
    const target = proxy.trim()
    setTestResults({ [target]: { state: 'testing' } })
    const result = await probeProxy(target)
    setTestResults({ [target]: result })
  }

  const testRandomPool = async () => {
    const targets = randomPool.slice(0, PROXY_POOL_TEST_LIMIT)
    setTestResults(
      Object.fromEntries(
        targets.map((item) => [item.proxy, { state: 'testing' } as const])
      )
    )
    await Promise.all(
      targets.map(async (item) => {
        const result = await probeProxy(item.proxy)
        setTestResults((current) => ({ ...current, [item.proxy]: result }))
      })
    )
  }

  const selectItems = [
    ...(showLegacyFixedValue
      ? [
          {
            value: proxy,
            label: `${proxy} · ${t('Current value (not in proxy list)')}`,
          },
        ]
      : []),
    ...items.map((item) => ({
      value: item.proxy,
      label: proxyOptionLabel(item),
    })),
  ]

  return (
    <FormField
      control={props.form.control}
      name='proxy'
      render={() => (
        <FormItem>
          <FormLabel>{t('Proxy')}</FormLabel>
          <ToggleGroup
            value={[mode]}
            onValueChange={(value) => {
              const nextMode = value.find((item) => item !== mode)
              if (nextMode) changeMode(nextMode as ProxyMode)
            }}
            aria-label={t('Proxy mode')}
            variant='outline'
            size='sm'
            disabled={props.disabled}
          >
            <ToggleGroupItem value='none'>{t('No proxy')}</ToggleGroupItem>
            <ToggleGroupItem value='random'>{t('Random')}</ToggleGroupItem>
            <ToggleGroupItem value='fixed'>{t('Fixed proxy')}</ToggleGroupItem>
          </ToggleGroup>

          {optionsQuery.data &&
            !optionsQuery.data.success &&
            mode !== 'none' && (
              <p className='text-destructive text-xs'>
                {t('Failed to load proxy list')}: {optionsQuery.data.message}
              </p>
            )}

          {mode === 'none' && (
            <FormDescription>
              {t('Requests go out directly without any proxy.')}
            </FormDescription>
          )}

          {mode === 'random' && (
            <div className='min-w-0 space-y-3'>
              <FormDescription>
                {t(
                  'Rotates over the proxy pool by time slice (5 minutes by default, independent per channel). Falls back to a direct connection when the pool is empty.'
                )}
              </FormDescription>
              <ToggleGroup
                value={[scope || PROXY_RANDOM_SCOPE_PUBLIC]}
                onValueChange={(value) => {
                  const nextScope = value.find((item) => item !== scope)
                  if (nextScope) {
                    props.form.setValue(
                      'proxy_random_scope',
                      nextScope === PROXY_RANDOM_SCOPE_ALL
                        ? PROXY_RANDOM_SCOPE_ALL
                        : PROXY_RANDOM_SCOPE_PUBLIC,
                      SET_VALUE_OPTIONS
                    )
                  }
                }}
                aria-label={t('Proxy pool scope')}
                variant='outline'
                size='sm'
                disabled={props.disabled}
              >
                <ToggleGroupItem value={PROXY_RANDOM_SCOPE_PUBLIC}>
                  {t('Public only')}
                </ToggleGroupItem>
                <ToggleGroupItem value={PROXY_RANDOM_SCOPE_ALL}>
                  {t('All proxies')}
                </ToggleGroupItem>
              </ToggleGroup>
              <MultiSelect
                options={regionOptions}
                selected={regions || []}
                onChange={(values) =>
                  props.form.setValue(
                    'proxy_random_regions',
                    values,
                    SET_VALUE_OPTIONS
                  )
                }
                placeholder={t('All regions')}
                disabled={props.disabled}
              />
              <div className='flex flex-wrap items-center gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={randomPool.length === 0}
                  onClick={testRandomPool}
                >
                  {t('Test proxy pool')}
                </Button>
                <span className='text-muted-foreground text-xs'>
                  {t('{{count}} proxies in the pool', {
                    count: randomPool.length,
                  })}
                  {randomPool.length > PROXY_POOL_TEST_LIMIT &&
                    ` · ${t('Only the first {{limit}} will be tested', { limit: PROXY_POOL_TEST_LIMIT })}`}
                </span>
              </div>
              {Object.keys(testResults).length > 0 && (
                <ul className='space-y-1'>
                  {randomPool.slice(0, PROXY_POOL_TEST_LIMIT).map((item) => (
                    <li
                      key={item.id}
                      className='flex min-w-0 items-center justify-between gap-2 text-xs'
                    >
                      <span className='min-w-0 flex-1 truncate font-mono'>
                        {item.proxy}
                        {item.region && (
                          <span className='text-muted-foreground'>
                            {' '}
                            · {item.region}
                          </span>
                        )}
                      </span>
                      <ProxyTestBadge result={testResults[item.proxy]} />
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {mode === 'fixed' && (
            <div className='min-w-0 space-y-2'>
              <Select
                items={selectItems}
                value={proxy || null}
                onValueChange={(value) => {
                  setTestResults({})
                  props.form.setValue('proxy', value || '', SET_VALUE_OPTIONS)
                }}
                disabled={props.disabled}
              >
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder={t('Select a proxy')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {showLegacyFixedValue && (
                      <SelectItem value={proxy}>
                        <span className='truncate'>{proxy}</span>
                        <Badge variant='warning'>
                          {t('Current value (not in proxy list)')}
                        </Badge>
                      </SelectItem>
                    )}
                    {items.map((item) => (
                      <SelectItem key={item.id} value={item.proxy}>
                        <span className='truncate'>
                          {proxyOptionLabel(item)}
                        </span>
                        {item.publicFlag !== 1 && (
                          <Badge variant='secondary'>{t('Internal')}</Badge>
                        )}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {showLegacyFixedValue && (
                <FormDescription className='text-warning'>
                  {t(
                    'This proxy is not in the proxy list. It is kept as long as you do not change it; picking another option replaces it.'
                  )}
                </FormDescription>
              )}
              <div className='flex flex-wrap items-center gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={!proxy.trim()}
                  onClick={testFixedProxy}
                >
                  {t('Test proxy')}
                </Button>
                <ProxyTestBadge result={testResults[proxy.trim()]} />
              </div>
            </div>
          )}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
