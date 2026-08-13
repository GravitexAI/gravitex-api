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
import { z } from 'zod'

import {
  type PermissionCatalog,
  type AdminPermissionMatrix,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { quotaUnitsToDollars } from '@/lib/format'
import { ROLE } from '@/lib/roles'

import { DEFAULT_GROUP } from '../constants'
import type {
  RequestHeadersLogMode,
  RequestHeadersLogSetting,
  UserFormData,
  User,
} from '../types'

export const DEFAULT_REQUEST_HEADERS_LOG_SETTING: RequestHeadersLogSetting = {
  enabled: false,
  mode: 'selected',
  headers: [],
}

export function parseRequestHeadersLogSetting(
  setting: string | null | undefined
): RequestHeadersLogSetting {
  if (!setting) return DEFAULT_REQUEST_HEADERS_LOG_SETTING

  try {
    const parsed: unknown = JSON.parse(setting)
    if (!parsed || typeof parsed !== 'object') {
      return DEFAULT_REQUEST_HEADERS_LOG_SETTING
    }

    const raw = (parsed as { request_headers_log?: unknown })
      .request_headers_log
    if (!raw || typeof raw !== 'object') {
      return DEFAULT_REQUEST_HEADERS_LOG_SETTING
    }

    const config = raw as {
      enabled?: unknown
      mode?: unknown
      headers?: unknown
    }
    const mode: RequestHeadersLogMode =
      config.mode === 'all' ? 'all' : 'selected'
    const headers = Array.isArray(config.headers)
      ? Array.from(
          new Set(
            config.headers.filter(
              (header): header is string => typeof header === 'string'
            )
          )
        )
      : []

    return {
      enabled: config.enabled === true,
      mode,
      headers,
    }
  } catch {
    return DEFAULT_REQUEST_HEADERS_LOG_SETTING
  }
}

// ============================================================================
// Form Schema
// ============================================================================

export const userFormSchema = z.object({
  username: z.string().min(1, 'Username is required'),
  display_name: z.string().optional(),
  password: z.string().optional(),
  role: z.number().optional(),
  quota_dollars: z.number().min(0).optional(),
  group: z.string().optional(),
  remark: z.string().optional(),
  request_headers_log_enabled: z.boolean(),
  request_headers_log_mode: z.enum(['all', 'selected']),
  request_headers_log_headers: z.string(),
  admin_permissions: z
    .record(z.string(), z.record(z.string(), z.boolean()))
    .optional(),
})

export type UserFormValues = z.infer<typeof userFormSchema>

// ============================================================================
// Form Defaults
// ============================================================================

export const USER_FORM_DEFAULT_VALUES: UserFormValues = {
  username: '',
  display_name: '',
  password: '',
  role: 1, // Default to common user
  quota_dollars: 0,
  group: DEFAULT_GROUP,
  remark: '',
  // Filled against the backend catalog at render time; see UsersMutateDrawer.
  admin_permissions: {},
  request_headers_log_enabled: false,
  request_headers_log_mode: 'selected',
  request_headers_log_headers: '',
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
// userId 是 number | string：用户 ID 来自 Java 端的 Snowflake（19 位），
// 超过 JS Number.MAX_SAFE_INTEGER，后端以字符串下发。这里只做 undefined 判断和原样透传，
// 绝不能 Number() 转换，否则精度丢失导致更新打到错误的用户上。
export function transformFormDataToPayload(
  data: UserFormValues,
  userId?: number | string,
  catalog?: PermissionCatalog
): UserFormData & { id?: number | string } {
  const payload: UserFormData & { id?: number | string } = {
    username: data.username,
    display_name: data.display_name || data.username,
    password: data.password || undefined,
  }

  const role = userId === undefined ? data.role || 1 : (data.role ?? 0)

  // Only send the permission matrix when the target is an admin and the catalog
  // is available; without the catalog we cannot build a full matrix, so we omit
  // the field (the backend then leaves existing permissions untouched).
  if (role >= ROLE.ADMIN && catalog) {
    payload.admin_permissions = normalizeAdminPermissions(
      data.admin_permissions as AdminPermissionMatrix | undefined,
      catalog
    )
  }

  // For create: only send required fields
  if (userId === undefined) {
    payload.role = role
  } else {
    // For update: quota is adjusted atomically via /api/user/manage, not sent here
    payload.group = data.group
    payload.remark = data.remark || undefined
    payload.setting = {
      request_headers_log: {
        enabled: data.request_headers_log_enabled,
        mode: data.request_headers_log_mode,
        headers: data.request_headers_log_headers
          .split(/[\n,]+/)
          .map((header) => header.trim().toLowerCase())
          .filter((header, index, headers) => header && headers.indexOf(header) === index),
      },
    }
    payload.id = userId
  }

  return payload
}

/**
 * Transform user data to form defaults. The admin permission matrix is passed
 * through as-is (the backend already returns a full matrix); it is filled against
 * the catalog at render time in UsersMutateDrawer.
 */
export function transformUserToFormDefaults(user: User): UserFormValues {
  const requestHeadersLog = parseRequestHeadersLogSetting(user.setting)

  return {
    username: user.username,
    display_name: user.display_name,
    password: '',
    role: user.role,
    quota_dollars: quotaUnitsToDollars(user.quota),
    group: user.group || DEFAULT_GROUP,
    remark: user.remark || '',
    admin_permissions: user.admin_permissions ?? {},
    request_headers_log_enabled: requestHeadersLog.enabled,
    request_headers_log_mode: requestHeadersLog.mode,
    request_headers_log_headers: requestHeadersLog.headers.join('\n'),
  }
}
