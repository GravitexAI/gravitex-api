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

import { getAdminCostBreakdown } from '../format'

describe('admin cost breakdown', () => {
  test('treats an explicit zero discount as zero upstream cost', () => {
    expect(
      getAdminCostBreakdown(500, {
        official_quota: 300,
        admin_info: { cost_discount: 0 },
      })
    ).toEqual({
        costDiscount: 0,
        vendorQuota: 300,
        actualCost: 0,
        profit: 500,
      })
  })

  test('falls back to the effective group ratio when official quota is absent', () => {
    expect(
      getAdminCostBreakdown(1000, {
        user_group_ratio: 2,
        group_ratio: 3,
        admin_info: { cost_discount: 0.5 },
      })
    ).toEqual({
        costDiscount: 0.5,
        vendorQuota: 500,
        actualCost: 250,
        profit: 750,
      })
  })

  test('does not invent a cost when the discount is missing or invalid', () => {
    expect(getAdminCostBreakdown(500, {})).toBeNull()
    expect(
      getAdminCostBreakdown(500, {
        admin_info: { cost_discount: 1.01 },
      }),
    ).toBeNull()
  })

  test('preserves an explicit zero official quota', () => {
    expect(
      getAdminCostBreakdown(500, {
        official_quota: 0,
        admin_info: { cost_discount: 0.5 },
      })
    ).toEqual({
      costDiscount: 0.5,
      vendorQuota: 0,
      actualCost: 0,
      profit: 500,
    })
  })
})
