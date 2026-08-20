import { Code, Plus, Table, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

type ModelCostDiscountEditorProps = {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  modelOptions?: string[]
}

type DiscountRow = {
  id: string
  model: string
  discount: string
}

function parseDiscountObject(value: string): Record<string, number> {
  if (!value.trim()) return {}
  const parsed: unknown = JSON.parse(value)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('object')
  }
  for (const [model, discount] of Object.entries(parsed)) {
    if (
      !model.trim() ||
      typeof discount !== 'number' ||
      !Number.isFinite(discount) ||
      discount <= 0 ||
      discount > 1
    ) {
      throw new Error('value')
    }
  }
  return parsed as Record<string, number>
}

export function ModelCostDiscountEditor(props: ModelCostDiscountEditorProps) {
  const { t } = useTranslation()
  const listId = useId()
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const [jsonValue, setJsonValue] = useState(props.value)
  const [rows, setRows] = useState<DiscountRow[]>([])
  const [error, setError] = useState<string | null>(null)
  const nextId = useRef(0)

  const createId = () => `cost-discount-${++nextId.current}`

  const parseRows = useCallback(
    (value: string): boolean => {
      try {
        const parsed = parseDiscountObject(value)
        setRows(
          Object.entries(parsed).map(([model, discount]) => ({
            id: createId(),
            model,
            discount: String(discount),
          }))
        )
        setError(null)
        return true
      } catch {
        setError(
          t(
            'Model cost discount must be a JSON object with values between 0 and 1'
          )
        )
        return false
      }
    },
    [t]
  )

  useEffect(() => {
    setJsonValue(props.value)
    parseRows(props.value)
  }, [parseRows, props.value])

  const syncRows = (nextRows: DiscountRow[]) => {
    setRows(nextRows)
    const result: Record<string, number> = {}
    for (const row of nextRows) {
      const model = row.model.trim()
      const discount = Number(row.discount)
      if (!model) continue
      if (!Number.isFinite(discount) || discount <= 0 || discount > 1) {
        setError(
          t('Each model cost discount must be greater than 0 and at most 1')
        )
        return
      }
      result[model] = discount
    }
    const value = nextRows.length ? JSON.stringify(result, null, 2) : ''
    setError(null)
    setJsonValue(value)
    props.onChange(value)
  }

  const handleJsonChange = (value: string) => {
    setJsonValue(value)
    props.onChange(value)
    parseRows(value)
  }

  const handleModeChange = (value: string) => {
    if (value !== 'visual' && value !== 'json') return
    if (value === 'visual' && !parseRows(jsonValue)) return
    setMode(value)
  }

  return (
    <div className='space-y-2'>
      <Tabs value={mode} onValueChange={handleModeChange} className='space-y-2'>
        <div className='flex items-center justify-between gap-3'>
          <TabsList>
            <TabsTrigger value='visual'>
              <Table className='h-4 w-4' aria-hidden='true' />
              {t('Visual')}
            </TabsTrigger>
            <TabsTrigger value='json'>
              <Code className='h-4 w-4' aria-hidden='true' />
              {t('JSON')}
            </TabsTrigger>
          </TabsList>
        </div>
        {error && (
          <Alert variant='destructive'>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        <TabsContent value='visual' className='space-y-2'>
          {rows.map((row) => (
            <div key={row.id} className='grid grid-cols-[1fr_8rem_auto] gap-2'>
              <Input
                value={row.model}
                onChange={(e) =>
                  syncRows(
                    rows.map((item) =>
                      item.id === row.id
                        ? { ...item, model: e.target.value }
                        : item
                    )
                  )
                }
                placeholder='model-name'
                disabled={props.disabled}
                list={listId}
              />
              <Input
                value={row.discount}
                onChange={(e) =>
                  syncRows(
                    rows.map((item) =>
                      item.id === row.id
                        ? { ...item, discount: e.target.value }
                        : item
                    )
                  )
                }
                type='number'
                min='0.001'
                max='1'
                step='0.001'
                placeholder='0.75'
                disabled={props.disabled}
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                onClick={() =>
                  syncRows(rows.filter((item) => item.id !== row.id))
                }
                disabled={props.disabled}
                aria-label={t('Delete cost discount')}
              >
                <Trash2 className='h-4 w-4' aria-hidden='true' />
              </Button>
            </div>
          ))}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() =>
              setRows([...rows, { id: createId(), model: '', discount: '' }])
            }
            disabled={props.disabled}
            className='w-full'
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add Cost Discount')}
          </Button>
        </TabsContent>
        <TabsContent value='json'>
          <JsonCodeEditor
            value={jsonValue}
            onChange={handleJsonChange}
            placeholder={t('{"model-name": 0.75}')}
            disabled={props.disabled}
            className={error ? 'border-destructive' : undefined}
            aria-invalid={Boolean(error)}
            ariaLabel={t('Model Cost Discount')}
          />
        </TabsContent>
      </Tabs>
      {props.modelOptions && props.modelOptions.length > 0 && (
        <datalist id={listId}>
          {props.modelOptions.map((model) => (
            <option key={model} value={model} />
          ))}
        </datalist>
      )}
    </div>
  )
}
