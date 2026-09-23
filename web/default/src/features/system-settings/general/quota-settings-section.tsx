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
import { zodResolver } from '@hookform/resolvers/zod'
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { formatQuota } from '@/lib/format'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
  SettingsFormGrid,
  SettingsFormGridItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const quotaSchema = z.object({
  QuotaForNewUser: z.coerce.number().min(0),
  PreConsumedQuota: z.coerce.number().min(0),
  QuotaForInviter: z.coerce.number().min(0),
  QuotaForInvitee: z.coerce.number().min(0),
  TopUpLink: z.string(),
  general_setting: z.object({
    docs_link: z.string(),
  }),
  quota_setting: z.object({
    enable_free_model_pre_consume: z.boolean(),
    minimum_remaining_quota: z.coerce.number().int().min(0),
    model_quota_reserve: z.string(),
  }),
})

type QuotaFormValues = z.infer<typeof quotaSchema>
type QuotaInputValue = number | ''

type ModelQuotaReserveRow = {
  id: string
  model: string
  quota: string
}

function parseModelQuotaReserve(value: string): ModelQuotaReserveRow[] {
  if (!value.trim()) {
    return []
  }
  try {
    const parsed = JSON.parse(value) as Record<string, unknown>
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
      return []
    }
    return Object.entries(parsed).map(([model, quota], index) => ({
      id: `${model}-${index}`,
      model,
      quota: String(quota),
    }))
  } catch {
    return []
  }
}

function serializeModelQuotaReserve(rows: ModelQuotaReserveRow[]): string {
  const result: Record<string, number> = {}
  for (const row of rows) {
    const model = row.model.trim()
    if (!model) {
      continue
    }
    const quota = Number(row.quota)
    if (Number.isInteger(quota) && quota >= 0) result[model] = quota
  }
  return JSON.stringify(result)
}

function ModelQuotaReserveEditor({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<ModelQuotaReserveRow[]>(() =>
    parseModelQuotaReserve(value)
  )
  const [mode, setMode] = useState<'table' | 'manual'>('table')
  const lastValueRef = useRef(value)

  useEffect(() => {
    if (value === lastValueRef.current) return
    lastValueRef.current = value
    setRows(parseModelQuotaReserve(value))
  }, [value])

  const updateRows = (nextRows: ModelQuotaReserveRow[]) => {
    const serializedValue = serializeModelQuotaReserve(nextRows)
    setRows(nextRows)
    lastValueRef.current = serializedValue
    onChange(serializedValue)
  }

  return (
    <div
      className='space-y-3 rounded-md border p-3'
      style={{ width: 560, maxWidth: '100%' }}
    >
      <div className='flex items-center justify-between gap-3'>
        <div className='flex gap-2'>
          <Button
            type='button'
            variant={mode === 'table' ? 'default' : 'outline'}
            size='sm'
            onClick={() => setMode('table')}
          >
            {t('Table input')}
          </Button>
          <Button
            type='button'
            variant={mode === 'manual' ? 'default' : 'outline'}
            size='sm'
            onClick={() => setMode('manual')}
          >
            {t('Manual JSON')}
          </Button>
        </div>
        {mode === 'table' ? (
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() =>
              updateRows([
                ...rows,
                { id: crypto.randomUUID(), model: '', quota: '0' },
              ])
            }
          >
            <Plus className='mr-1 h-4 w-4' />
            {t('Add rule')}
          </Button>
        ) : null}
      </div>
      {mode === 'manual' ? (
        <Textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder='{"seedance*": 10000, "seedance-2-0": 20000}'
          rows={6}
        />
      ) : (
        <div className='space-y-3'>
          {rows.length ? (
            <div className='overflow-x-auto'>
              <table className='w-full min-w-[520px] text-sm'>
                <thead>
                  <tr className='border-b text-left'>
                    <th className='px-2 py-2 font-medium'>{t('Model name')}</th>
                    <th className='px-2 py-2 font-medium'>
                      {t('Reserved quota')}
                    </th>
                    <th className='w-12 px-2 py-2' />
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row, index) => (
                    <tr key={row.id} className='border-b last:border-0'>
                      <td className='px-2 py-2'>
                        <Input
                          value={row.model}
                          placeholder='seedance*'
                          onChange={(event) => {
                            const nextRows = [...rows]
                            nextRows[index] = {
                              ...row,
                              model: event.target.value,
                            }
                            updateRows(nextRows)
                          }}
                        />
                      </td>
                      <td className='px-2 py-2'>
                        <Input
                          type='number'
                          min={0}
                          step={1}
                          value={row.quota}
                          onChange={(event) => {
                            const nextRows = [...rows]
                            nextRows[index] = {
                              ...row,
                              quota: event.target.value,
                            }
                            updateRows(nextRows)
                          }}
                        />
                      </td>
                      <td className='px-2 py-2'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          aria-label={t('Remove rule')}
                          onClick={() =>
                            updateRows(
                              rows.filter((_, rowIndex) => rowIndex !== index)
                            )
                          }
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className='bg-muted/40 text-muted-foreground rounded-md px-3 py-4 text-center text-sm'>
              {t('No model quota reserve rules configured.')}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function formatQuotaInputValue(value: QuotaInputValue): string {
  return formatQuota(value === '' ? 0 : value)
}

type QuotaSettingsSectionProps = {
  defaultValues: QuotaFormValues
  complianceConfirmed?: boolean
}

export function QuotaSettingsSection({
  defaultValues,
  complianceConfirmed = true,
}: QuotaSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const handleNumberChange =
    (onChange: (value: QuotaInputValue) => void) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      const value = event.currentTarget.valueAsNumber
      onChange(Number.isNaN(value) ? '' : value)
    }

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<QuotaFormValues>({
      resolver: zodResolver(quotaSchema) as Resolver<
        QuotaFormValues,
        unknown,
        QuotaFormValues
      >,
      defaultValues,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: value as string | number | boolean,
          })
        }
      },
    })

  return (
    <SettingsSection title={t('Quota Settings')}>
      <FormNavigationGuard when={isDirty} />

      {!complianceConfirmed ? (
        <Alert variant='destructive'>
          <AlertDescription>
            {t(
              'Non-zero invitation rewards require compliance confirmation in Payment Gateway settings.'
            )}
          </AlertDescription>
        </Alert>
      ) : null}

      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={updateOption.isPending || isSubmitting}
          />
          <FormDirtyIndicator isDirty={isDirty} />
          <SettingsFormGrid>
            <FormField
              control={form.control}
              name='QuotaForNewUser'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('New User Quota')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Initial quota given to new users ({{formattedQuota}})',
                      {
                        formattedQuota: formatQuotaInputValue(field.value),
                      }
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='PreConsumedQuota'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Pre-Consumed Quota')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Quota consumed before charging users')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='QuotaForInviter'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Inviter Reward')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Quota given to users who invite others ({{formattedQuota}})',
                      {
                        formattedQuota: formatQuotaInputValue(field.value),
                      }
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='QuotaForInvitee'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Invitee Reward')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Quota given to invited users ({{formattedQuota}})', {
                      formattedQuota: formatQuotaInputValue(field.value),
                    })}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <SettingsFormGridItem span='full'>
              <FormField
                control={form.control}
                name='quota_setting.minimum_remaining_quota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum Remaining Quota')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        value={field.value ?? ''}
                        onChange={handleNumberChange(field.onChange)}
                        name={field.name}
                        onBlur={field.onBlur}
                        ref={field.ref}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Quota that must remain after a model request is pre-consumed.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='quota_setting.model_quota_reserve'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Model Quota Reserve Rules')}</FormLabel>
                    <FormControl>
                      <ModelQuotaReserveEditor
                        value={field.value}
                        onChange={field.onChange}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Example: seedance* applies to all matching models; an exact model rule takes priority.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGridItem>

            <SettingsFormGridItem span='full'>
              <FormField
                control={form.control}
                name='quota_setting.enable_free_model_pre_consume'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Pre-Consume for Free Models')}</FormLabel>
                      <FormDescription>
                        {t(
                          'When enabled, zero-cost models also pre-consume quota before final settlement.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={updateOption.isPending}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsFormGridItem>

            <FormField
              control={form.control}
              name='TopUpLink'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Top-Up Link')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('https://example.com/topup')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('External link for users to purchase quota')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='general_setting.docs_link'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Documentation Link')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('https://docs.example.com')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Link to your documentation site')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
