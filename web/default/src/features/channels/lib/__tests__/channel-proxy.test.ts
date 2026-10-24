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
import { describe, expect, test } from 'vitest'

import type { Channel } from '../../types'
import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
} from '../channel-form'
import {
  type ChannelProxyOption,
  filterRandomProxyPool,
  getProxyMode,
} from '../channel-proxy'

const POOL: ChannelProxyOption[] = [
  {
    id: 1,
    proxy: 'http://10.0.0.1:8080',
    region: 'us-west',
    publicFlag: 1,
    remark: '',
  },
  {
    id: 2,
    proxy: 'http://10.0.0.2:8080',
    region: 'ap-tokyo',
    publicFlag: 1,
    remark: '',
  },
  {
    id: 3,
    proxy: 'http://10.0.0.3:8080',
    region: 'ap-tokyo',
    publicFlag: 0,
    remark: '',
  },
]

describe('channel proxy mode', () => {
  test('derives the mode from setting.proxy', () => {
    expect(getProxyMode('')).toBe('none')
    expect(getProxyMode(undefined)).toBe('none')
    expect(getProxyMode(' random ')).toBe('random')
    expect(getProxyMode('socks5://u:p@1.2.3.4:1080')).toBe('fixed')
  })

  test('random pool keeps public rows unless scope is all, then filters regions', () => {
    expect(filterRandomProxyPool(POOL, 'public', []).map((p) => p.id)).toEqual([
      1, 2,
    ])
    expect(
      filterRandomProxyPool(POOL, 'all', ['ap-tokyo']).map((p) => p.id)
    ).toEqual([2, 3])
    expect(filterRandomProxyPool(POOL, undefined, ['eu'])).toEqual([])
  })

  test('schema accepts the random sentinel but still rejects bad URLs', () => {
    const base = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'c',
      type: 1,
      key: 'k',
      models: 'm',
      groups: ['default'],
    }
    const proxyIssues = (proxy: string) =>
      (
        channelFormSchema.safeParse({ ...base, proxy }).error?.issues || []
      ).filter((issue) => issue.path[0] === 'proxy')
    expect(proxyIssues('random')).toEqual([])
    expect(proxyIssues('ftp://1.2.3.4')).not.toEqual([])
  })
})

describe('channel proxy setting JSON', () => {
  test('writes random filters only for random channels', () => {
    const random = JSON.parse(
      buildSettingJSON({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        proxy: 'random',
        proxy_random_regions: ['us-west'],
        proxy_random_scope: 'all',
      })
    )
    expect(random).toMatchObject({
      proxy: 'random',
      proxy_random_regions: ['us-west'],
      proxy_random_scope: 'all',
    })

    const fixed = JSON.parse(
      buildSettingJSON({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        proxy: 'http://10.0.0.1:8080',
        proxy_random_regions: ['us-west'],
        proxy_random_scope: 'all',
      })
    )
    expect(fixed.proxy).toBe('http://10.0.0.1:8080')
    // The backend merges setting JSON on update and only deletes null keys.
    expect(fixed.proxy_random_regions).toBeNull()
    expect(fixed.proxy_random_scope).toBeNull()

    const randomWithoutRegions = JSON.parse(
      buildSettingJSON({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        proxy: 'random',
        proxy_random_regions: [],
        proxy_random_scope: 'public',
      })
    )
    expect(randomWithoutRegions.proxy_random_regions).toBeNull()
  })

  test('reads random filters back when editing', () => {
    const values = transformChannelToFormDefaults({
      id: 1,
      type: 1,
      name: 'c',
      channel_info: {},
      setting: JSON.stringify({
        proxy: 'random',
        proxy_random_regions: ['ap-tokyo'],
        proxy_random_scope: 'all',
      }),
    } as Channel)
    expect(values.proxy).toBe('random')
    expect(values.proxy_random_regions).toEqual(['ap-tokyo'])
    expect(values.proxy_random_scope).toBe('all')
  })
})
