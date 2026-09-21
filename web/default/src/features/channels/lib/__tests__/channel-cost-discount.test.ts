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

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformFormDataToUpdatePayload,
} from '../channel-form'

function validForm(costDiscount: number | null) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Cost channel',
    models: 'gpt-5',
    cost_discount: costDiscount,
  }
}

function validModelCostDiscount(value: number) {
  return {
    ...validForm(null),
    model_cost_discount: JSON.stringify({ 'gpt-5': value }),
  }
}

describe('channel cost discount form', () => {
  test('preserves an explicit zero through validation and update payload', () => {
    const result = channelFormSchema.safeParse(validForm(0))

    expect(result.success).toBe(true)
    if (!result.success) return
    expect(result.data.cost_discount).toBe(0)
    expect(
      transformFormDataToUpdatePayload(result.data, 81).cost_discount,
    ).toBe(0)
  })

  test('accepts an unset discount and rejects values outside zero to one', () => {
    expect(channelFormSchema.safeParse(validForm(null)).success).toBe(true)
    expect(channelFormSchema.safeParse(validForm(-0.001)).success).toBe(false)
    expect(channelFormSchema.safeParse(validForm(1.001)).success).toBe(false)
  })

  test('accepts an explicit zero model override', () => {
    expect(channelFormSchema.safeParse(validModelCostDiscount(0)).success).toBe(true)
  })
})
