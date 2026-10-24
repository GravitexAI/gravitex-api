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

/** Sentinel stored in setting.proxy meaning "rotate over the proxy pool". */
export const PROXY_RANDOM = 'random'

export const PROXY_RANDOM_SCOPE_PUBLIC = 'public'
export const PROXY_RANDOM_SCOPE_ALL = 'all'

/** Max proxies probed concurrently when testing the random pool. */
export const PROXY_POOL_TEST_LIMIT = 20

export type ProxyMode = 'none' | 'random' | 'fixed'
export type ProxyRandomScope = 'public' | 'all'

export type ChannelProxyOption = {
  id: number
  proxy: string
  region: string
  publicFlag: number
  remark: string
}

/** Derive the editor mode from the stored setting.proxy value. */
export function getProxyMode(proxy: string | undefined): ProxyMode {
  const value = proxy?.trim() || ''
  if (!value) return 'none'
  if (value === PROXY_RANDOM) return 'random'
  return 'fixed'
}

/**
 * Proxies the backend may pick for a random channel: public rows only unless the
 * scope is "all", optionally limited to the selected regions (empty = all regions).
 */
export function filterRandomProxyPool(
  items: ChannelProxyOption[],
  scope: ProxyRandomScope | undefined,
  regions: string[] | undefined
): ChannelProxyOption[] {
  const regionSet = new Set(regions || [])
  return items.filter((item) => {
    if (scope !== PROXY_RANDOM_SCOPE_ALL && item.publicFlag !== 1) return false
    if (regionSet.size > 0 && !regionSet.has(item.region)) return false
    return true
  })
}
