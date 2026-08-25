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

import React, { useCallback, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Checkbox,
  Empty,
  Input,
  Modal,
  Radio,
  RadioGroup,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconDelete,
  IconPlus,
  IconSave,
  IconSearch,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import {
  PAGE_SIZE,
  PRICE_SUFFIX,
  buildSummaryText,
  hasValue,
  useModelPricingEditorState,
} from '../hooks/useModelPricingEditorState';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import TieredPricingEditor from './TieredPricingEditor';
import OperLogConfirmModal from '../../../../components/oper-log/OperLogConfirmModal';

const { Text } = Typography;
const EMPTY_CANDIDATE_MODEL_NAMES = [];

const PriceInput = ({
  label,
  value,
  placeholder,
  onChange,
  suffix = PRICE_SUFFIX,
  disabled = false,
  extraText = '',
  headerAction = null,
  hidden = false,
}) => (
  <div style={{ marginBottom: 16 }}>
    <div className='mb-1 font-medium text-gray-700 flex items-center justify-between gap-3'>
      <span>{label}</span>
      {headerAction}
    </div>
    {!hidden ? (
      <Input
        value={value}
        placeholder={placeholder}
        onChange={onChange}
        suffix={suffix}
        disabled={disabled}
      />
    ) : null}
    {extraText ? (
      <div className='mt-1 text-xs text-gray-500'>{extraText}</div>
    ) : null}
  </div>
);

const STRUCTURE_PRESETS = [
  {
    key: 'simple',
    label: '简单价格',
    description: '单值，适用所有参数。',
  },
  {
    key: 'audio-resolution',
    label: '按音频 × 分辨率',
    description: 'veo-3.1 等模型：有声音/无声音 × 720p/1080p/4k。',
  },
  {
    key: 'resolutions',
    label: '按分辨率',
    description: 'wan2.6/2.7/happyhorse 等模型：每个分辨率一个价格。',
  },
  {
    key: 'custom',
    label: '自定义 JSON 表格',
    description: '按需手动添加行，键为维度，值为价格。',
  },
];

const buildEmptyStructure = (kind, t) => {
  if (kind === 'audio-resolution') {
    return {
      kind,
      noAudio: {},
      audio: {},
    };
  }
  if (kind === 'resolutions') {
    return { kind, resolutions: {} };
  }
  if (kind === 'custom') {
    return { kind, entries: {} };
  }
  return { kind: 'simple', price: '' };
};

const PricingStructureEditor = ({
  label,
  structure,
  suffix,
  t,
  onChange,
  extraText = '',
  hiddenKinds = [],
}) => {
  const resolvedKind = structure?.kind || 'simple';
  const handleKindChange = (kind) => {
    onChange(buildEmptyStructure(kind, t));
  };
  const setSimplePrice = (price) => {
    onChange({ kind: 'simple', price });
  };
  const visiblePresets = STRUCTURE_PRESETS.filter(
    (preset) => !hiddenKinds.includes(preset.key),
  );

  return (
    <div className='mb-4'>
      <div className='mb-1 font-medium text-gray-700'>{label}</div>
      <RadioGroup
        type='button'
        value={resolvedKind}
        onChange={(event) => handleKindChange(event?.target?.value ?? event)}
        style={{ marginBottom: 12 }}
      >
        {visiblePresets.map((preset) => (
          <Radio key={preset.key} value={preset.key}>
            {t(preset.label)}
          </Radio>
        ))}
      </RadioGroup>

      {resolvedKind === 'simple' ? (
        <Input
          value={structure?.price || ''}
          placeholder={t('请输入价格')}
          suffix={suffix}
          onChange={setSimplePrice}
        />
      ) : null}

      {resolvedKind === 'audio-resolution' ? (
        <AudioResolutionEditor
          structure={structure}
          t={t}
          onChange={onChange}
          suffix={suffix}
        />
      ) : null}

      {resolvedKind === 'resolutions' ? (
        <ResolutionEditor
          structure={structure}
          t={t}
          onChange={onChange}
          suffix={suffix}
        />
      ) : null}

      {resolvedKind === 'custom' ? (
        <CustomEditor
          structure={structure}
          t={t}
          onChange={onChange}
          suffix={suffix}
        />
      ) : null}

      {extraText ? (
        <div className='mt-2 text-xs text-gray-500'>{extraText}</div>
      ) : null}
    </div>
  );
};

const AudioResolutionEditor = ({ structure, t, onChange, suffix }) => {
  const noAudio = structure?.noAudio || {};
  const audio = structure?.audio || {};
  const updateNoAudio = (key, value) =>
    onChange({ ...structure, noAudio: { ...noAudio, [key]: value } });
  const updateAudio = (key, value) =>
    onChange({ ...structure, audio: { ...audio, [key]: value } });
  const removeRow = (dimension, key) => {
    const next = { ...structure };
    if (dimension === 'noAudio') {
      const copy = { ...noAudio };
      delete copy[key];
      next.noAudio = copy;
    } else {
      const copy = { ...audio };
      delete copy[key];
      next.audio = copy;
    }
    onChange(next);
  };
  const renderCell = (dimension, key) => (
    <TableCell
      key={`${dimension}-${key}`}
      value={
        dimension === 'noAudio' ? noAudio[key] || '' : audio[key] || ''
      }
      suffix={suffix}
      onChange={(value) =>
        dimension === 'noAudio'
          ? updateNoAudio(key, value)
          : updateAudio(key, value)
      }
      onRemove={() => removeRow(dimension, key)}
      t={t}
    />
  );
  const allKeys = Array.from(
    new Set([...Object.keys(noAudio), ...Object.keys(audio)]),
  );
  const [draftKey, setDraftKey] = useState('');
  const handleAdd = () => {
    if (!draftKey.trim()) return;
    if (allKeys.includes(draftKey.trim())) return;
    onChange({
      ...structure,
      noAudio: { ...noAudio, [draftKey.trim()]: '' },
    });
    setDraftKey('');
  };
  return (
    <div className='rounded border border-gray-200 bg-gray-50 px-3 py-2'>
      <div className='mb-2 flex items-center justify-between text-xs text-gray-500'>
        <span>{t('对应「无音频」的价格')}</span>
        <span>{t('对应「有音频」的价格')}</span>
      </div>
      <table className='w-full table-fixed text-sm'>
        <tbody>
          {allKeys.map((key) => (
            <tr key={key} className='border-t border-gray-100'>
              <td className='w-20 py-2 pr-3 text-gray-600'>
                {key === 'default' ? t('默认') : key}
              </td>
              <td className='py-2'>{renderCell('noAudio', key)}</td>
              <td className='py-2 pl-3'>{renderCell('audio', key)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className='mt-2 flex items-center gap-2'>
        <Input
          size='small'
          value={draftKey}
          placeholder={t('输入档位键，如 480p')}
          onChange={setDraftKey}
          style={{ width: 160 }}
        />
        <Button
          size='small'
          theme='outline'
          type='tertiary'
          onClick={handleAdd}
        >
          {t('新增一行')}
        </Button>
      </div>
    </div>
  );
};

const ResolutionEditor = ({ structure, t, onChange, suffix }) => {
  const resolutions = structure?.resolutions || {};
  const knownKeys = Object.keys(resolutions);
  const [draftKey, setDraftKey] = useState('');
  const handleChange = (key, value) =>
    onChange({ ...structure, resolutions: { ...resolutions, [key]: value } });
  const handleRemove = (key) => {
    const next = { ...resolutions };
    delete next[key];
    onChange({ ...structure, resolutions: next });
  };
  const handleAdd = () => {
    const key = draftKey.trim();
    if (!key) return;
    if (knownKeys.includes(key)) return;
    onChange({
      ...structure,
      resolutions: { ...resolutions, [key]: '' },
    });
    setDraftKey('');
  };
  return (
    <div className='rounded border border-gray-200 bg-gray-50 px-3 py-2'>
      <table className='w-full table-fixed text-sm'>
        <tbody>
          {knownKeys.map((key) => (
            <TableCellRow
              key={key}
              label={key}
              value={resolutions[key] || ''}
              suffix={suffix}
              onChange={(value) => handleChange(key, value)}
              onRemove={() => handleRemove(key)}
              t={t}
            />
          ))}
        </tbody>
      </table>
      <div className='mt-2 flex items-center gap-2'>
        <Input
          size='small'
          value={draftKey}
          placeholder={t('输入档位键，如 480p')}
          onChange={setDraftKey}
          style={{ width: 160 }}
        />
        <Button
          size='small'
          theme='outline'
          type='tertiary'
          onClick={handleAdd}
        >
          {t('新增一行')}
        </Button>
      </div>
    </div>
  );
};

const CustomEntries = ({
  entries,
  t,
  onChange,
  suffix,
  allowAdd = false,
}) => {
  const [draftKey, setDraftKey] = useState('');
  const [draftValue, setDraftValue] = useState('');
  const handleChangeKey = (key, value) =>
    onChange({ ...entries, [key]: value });
  const handleRemove = (key) => {
    const next = { ...entries };
    delete next[key];
    onChange(next);
  };
  const handleAdd = () => {
    const key = draftKey.trim();
    if (!key) return;
    if (Object.keys(entries).includes(key)) return;
    onChange({ ...entries, [key]: draftValue });
    setDraftKey('');
    setDraftValue('');
  };
  return (
    <div className='flex flex-col'>
      {Object.entries(entries).map(([key, val]) => {
        const isNested = val && typeof val === 'object' && val.__nested;
        if (isNested) {
          return (
            <div
              key={key}
              className='border-t border-gray-100 py-2'
            >
              <div className='flex items-center justify-between gap-2'>
                <div className='min-w-0 flex-1 break-all text-sm font-medium text-gray-700'>
                  {key}
                </div>
                <Button
                  size='small'
                  theme='borderless'
                  type='danger'
                  onClick={() => handleRemove(key)}
                >
                  {t('remove')}
                </Button>
              </div>
              <div className='mt-2 border-l-2 border-gray-200 pl-3'>
                <CustomEntries
                  entries={val.entries}
                  t={t}
                  suffix={suffix}
                  allowAdd
                  onChange={(nextNested) =>
                    onChange({
                      ...entries,
                      [key]: { __nested: true, entries: nextNested },
                    })
                  }
                />
              </div>
            </div>
          );
        }
        return (
          <div
            key={key}
            className='flex items-center gap-2 border-t border-gray-100 py-2'
          >
            <div className='w-48 break-all text-gray-600'>{key}</div>
            <div className='flex-1'>
              <Input
                size='small'
                value={val || ''}
                suffix={suffix}
                onChange={(v) => handleChangeKey(key, v)}
              />
            </div>
            <Button
              size='small'
              theme='borderless'
              type='danger'
              onClick={() => handleRemove(key)}
            >
              {t('remove')}
            </Button>
          </div>
        );
      })}
      {allowAdd ? (
        <div className='mt-2 flex items-center gap-2'>
          <Input
            size='small'
            value={draftKey}
            placeholder={t('子键，如分辨率或资源类型')}
            onChange={setDraftKey}
            style={{ width: 200 }}
          />
          <Input
            size='small'
            value={draftValue}
            placeholder={t('价格')}
            suffix={suffix}
            onChange={setDraftValue}
            style={{ width: 160 }}
          />
          <Button
            size='small'
            theme='outline'
            type='tertiary'
            onClick={handleAdd}
          >
            {t('新增一行')}
          </Button>
        </div>
      ) : null}
    </div>
  );
};

const CustomEditor = ({ structure, t, onChange, suffix }) => {
  const entries = structure?.entries || {};
  const [groupKey, setGroupKey] = useState('');
  const handleAddGroup = () => {
    const key = groupKey.trim();
    if (!key) return;
    if (Object.keys(entries).includes(key)) return;
    onChange({
      ...structure,
      entries: { ...entries, [key]: { __nested: true, entries: {} } },
    });
    setGroupKey('');
  };
  return (
    <div className='rounded border border-gray-200 bg-gray-50 px-3 py-2'>
      <CustomEntries
        entries={entries}
        t={t}
        suffix={suffix}
        allowAdd
        onChange={(nextEntries) =>
          onChange({ ...structure, entries: nextEntries })
        }
      />
      <div className='mt-2 flex items-center gap-2 border-t border-gray-200 pt-2'>
        <Input
          size='small'
          value={groupKey}
          placeholder={t('嵌套组名，如 outputImage')}
          onChange={setGroupKey}
          style={{ width: 200 }}
        />
        <Button
          size='small'
          theme='outline'
          type='tertiary'
          onClick={handleAddGroup}
        >
          {t('新增嵌套组')}
        </Button>
      </div>
    </div>
  );
};

const TableCell = ({ value, suffix, onChange, onRemove, t }) => (
  <div className='flex items-center gap-2'>
    <Input
      size='small'
      value={value}
      suffix={suffix}
      onChange={onChange}
      style={{ width: '100%' }}
    />
    {onRemove ? (
      <Button
        size='small'
        theme='borderless'
        type='danger'
        onClick={onRemove}
      >
        {t('remove')}
      </Button>
    ) : null}
  </div>
);

const TableCellRow = ({
  label,
  value,
  suffix,
  onChange,
  onRemove,
  t,
  labelWidth = 'w-20',
}) => (
  <tr className='border-t border-gray-100'>
    <td
      className={`${labelWidth} py-2 pr-3 align-top text-gray-600 break-all`}
    >
      {label}
    </td>
    <td className='py-2'>
      <Input
        size='small'
        value={value}
        suffix={suffix}
        onChange={onChange}
      />
    </td>
    <td className='py-2 pl-3 align-middle'>
      {onRemove ? (
        <Button
          size='small'
          theme='borderless'
          type='danger'
          onClick={onRemove}
        >
          {t('remove')}
        </Button>
      ) : null}
    </td>
  </tr>
);

export default function ModelPricingEditor({
  options,
  refresh,
  candidateModelNames = EMPTY_CANDIDATE_MODEL_NAMES,
  filterMode = 'all',
  allowAddModel = true,
  allowDeleteModel = true,
  showConflictFilter = true,
  listDescription = '',
  emptyTitle = '',
  emptyDescription = '',
}) {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const [addVisible, setAddVisible] = useState(false);
  const [batchVisible, setBatchVisible] = useState(false);
  const [newModelName, setNewModelName] = useState('');

  const {
    selectedModel,
    selectedModelName,
    selectedModelNames,
    setSelectedModelName,
    setSelectedModelNames,
    searchText,
    setSearchText,
    currentPage,
    setCurrentPage,
    loading,
    conflictOnly,
    setConflictOnly,
    billingModeFilter,
    setBillingModeFilter,
    filteredModels,
    pagedData,
    selectedWarnings,
    previewRows,
    isOptionalFieldEnabled,
    handleOptionalFieldToggle,
    handleNumericFieldChange,
    handleStructureFieldChange,
    handleBillingModeChange,
    handleBillingExprChange,
    handleRequestRuleExprChange,
    handleSubmit,
    addModel,
    deleteModel,
    applySelectedModelPricing,
    resetSelectedModel,
    operLogModal,
    confirmOperLogSave,
    skipOperLogSave,
    cancelOperLogSave,
  } = useModelPricingEditorState({
    options,
    refresh,
    t,
    candidateModelNames,
    filterMode,
  });

  const getExprModeLabel = useCallback(
    (model) => {
      if (model?.billingMode !== 'tiered_expr') {
        return '';
      }
      return (model.billingExpr || '').includes('tier(')
        ? t('阶梯计费')
        : t('表达式计费');
    },
    [t],
  );

  const columns = useMemo(
    () => [
      {
        title: t('模型名称'),
        dataIndex: 'name',
        key: 'name',
        render: (text, record) => (
          <Space>
            <Button
              theme='borderless'
              type='tertiary'
              onClick={() => setSelectedModelName(record.name)}
              style={{
                padding: 0,
                color:
                  record.name === selectedModelName
                    ? 'var(--semi-color-primary)'
                    : undefined,
              }}
            >
              {text}
            </Button>
            {selectedModelNames.includes(record.name) ? (
              <Tag color='green' shape='circle'>
                {t('已勾选')}
              </Tag>
            ) : null}
            {record.hasConflict ? (
              <Tag color='red' shape='circle'>
                {t('矛盾')}
              </Tag>
            ) : null}
          </Space>
        ),
      },
      {
        title: t('计费方式'),
        dataIndex: 'billingMode',
        key: 'billingMode',
        render: (_, record) => (
          <Tag
            color={
              record.billingMode === 'per-request'
                ? 'teal'
                : record.billingMode === 'tiered_expr'
                  ? 'amber'
                  : record.billingMode === 'per-image'
                    ? 'green'
                    : record.billingMode === 'per-second'
                      ? 'cyan'
                      : 'violet'
            }
          >
            {record.billingMode === 'per-request'
              ? t('按次计费')
              : record.billingMode === 'tiered_expr'
                ? getExprModeLabel(record)
                : record.billingMode === 'per-image'
                  ? t('按张计费')
                  : record.billingMode === 'per-second'
                    ? t('按秒计费')
                    : t('按量计费')}
          </Tag>
        ),
      },
      {
        title: t('价格摘要'),
        dataIndex: 'summary',
        key: 'summary',
        render: (_, record) => buildSummaryText(record, t),
      },
      {
        title: t('操作'),
        key: 'action',
        render: (_, record) => (
          <Space>
            {allowDeleteModel ? (
              <Button
                size='small'
                type='danger'
                icon={<IconDelete />}
                onClick={() => deleteModel(record.name)}
              />
            ) : null}
          </Space>
        ),
      },
    ],
    [
      allowDeleteModel,
      deleteModel,
      getExprModeLabel,
      selectedModelName,
      selectedModelNames,
      setSelectedModelName,
      t,
    ],
  );

  const handleAddModel = () => {
    if (addModel(newModelName)) {
      setNewModelName('');
      setAddVisible(false);
    }
  };

  const rowSelection = {
    selectedRowKeys: selectedModelNames,
    onChange: (selectedRowKeys) => setSelectedModelNames(selectedRowKeys),
  };

  return (
    <>
      <Space vertical align='start' style={{ width: '100%' }}>
        <Space wrap className='mt-2'>
          {allowAddModel ? (
            <Button
              icon={<IconPlus />}
              onClick={() => setAddVisible(true)}
              style={isMobile ? { width: '100%' } : undefined}
            >
              {t('添加模型')}
            </Button>
          ) : null}
          <Button
            type='primary'
            icon={<IconSave />}
            loading={loading}
            onClick={handleSubmit}
            style={isMobile ? { width: '100%' } : undefined}
          >
            {t('应用更改')}
          </Button>
          <Button
            disabled={!selectedModel || selectedModelNames.length === 0}
            onClick={() => setBatchVisible(true)}
            style={isMobile ? { width: '100%' } : undefined}
          >
            {t('批量应用当前模型价格')}
            {selectedModelNames.length > 0
              ? ` (${selectedModelNames.length})`
              : ''}
          </Button>
          <Input
            prefix={<IconSearch />}
            placeholder={t('搜索模型名称')}
            value={searchText}
            onChange={(value) => setSearchText(value)}
            style={{ width: isMobile ? '100%' : 220 }}
            showClear
          />
          <Select
            placeholder={t('计费方式')}
            value={billingModeFilter || undefined}
            onChange={(value) => setBillingModeFilter(value || '')}
            style={{ width: isMobile ? '100%' : 180 }}
            showClear
            optionList={[
              { value: 'per-token', label: t('按量计费') },
              { value: 'per-request', label: t('按次计费') },
              { value: 'per-image', label: t('按张计费') },
              { value: 'per-second', label: t('按秒计费') },
              { value: 'tiered_expr', label: t('阶梯计费') },
            ]}
          />
          {showConflictFilter ? (
            <Checkbox
              checked={conflictOnly}
              onChange={(event) => setConflictOnly(event.target.checked)}
            >
              {t('仅显示矛盾倍率')}
            </Checkbox>
          ) : null}
        </Space>

        {listDescription ? (
          <div className='text-sm text-gray-500'>{listDescription}</div>
        ) : null}
        {selectedModelNames.length > 0 ? (
          <div
            style={{
              width: '100%',
              padding: '10px 12px',
              borderRadius: 8,
              background: 'var(--semi-color-primary-light-default)',
              border: '1px solid var(--semi-color-primary)',
              color: 'var(--semi-color-primary)',
              fontWeight: 600,
            }}
          >
            {t('已勾选 {{count}} 个模型', { count: selectedModelNames.length })}
          </div>
        ) : null}

        <div
          style={{
            width: '100%',
            display: 'grid',
            gap: 16,
            gridTemplateColumns: isMobile
              ? 'minmax(0, 1fr)'
              : 'minmax(300px, 0.8fr) minmax(480px, 1.2fr)',
          }}
        >
          <Card
            bodyStyle={{ padding: 0 }}
            style={isMobile ? { order: 2 } : undefined}
          >
            <div style={{ overflowX: 'auto' }}>
              <Table
                columns={columns}
                dataSource={pagedData}
                rowKey='name'
                rowSelection={rowSelection}
                pagination={{
                  currentPage,
                  pageSize: PAGE_SIZE,
                  total: filteredModels.length,
                  onPageChange: (page) => setCurrentPage(page),
                  showTotal: true,
                  showSizeChanger: false,
                }}
                empty={
                  <div style={{ textAlign: 'center', padding: '20px' }}>
                    {emptyTitle || t('暂无模型')}
                  </div>
                }
                onRow={(record) => ({
                  style: {
                    background: selectedModelNames.includes(record.name)
                      ? 'var(--semi-color-success-light-default)'
                      : record.name === selectedModelName
                        ? 'var(--semi-color-primary-light-default)'
                        : undefined,
                    boxShadow: selectedModelNames.includes(record.name)
                      ? 'inset 4px 0 0 var(--semi-color-success)'
                      : record.name === selectedModelName
                        ? 'inset 4px 0 0 var(--semi-color-primary)'
                        : undefined,
                    transition: 'background 0.2s ease, box-shadow 0.2s ease',
                  },
                  onClick: () => setSelectedModelName(record.name),
                })}
                scroll={isMobile ? { x: 720 } : undefined}
              />
            </div>
          </Card>

          <Card
            style={isMobile ? { order: 1 } : undefined}
            title={selectedModel ? selectedModel.name : t('模型计费编辑器')}
            headerExtraContent={
              selectedModel ? (
                <div className='flex items-center gap-2'>
                  <Tag
                    color={
                      selectedModel.billingMode === 'per-request'
                        ? 'teal'
                        : selectedModel.billingMode === 'tiered_expr'
                          ? 'amber'
                          : 'blue'
                    }
                  >
                    {selectedModel.billingMode === 'per-request'
                      ? t('按次计费')
                      : selectedModel.billingMode === 'tiered_expr'
                        ? getExprModeLabel(selectedModel)
                        : selectedModel.billingMode === 'per-image'
                          ? t('按张计费')
                          : selectedModel.billingMode === 'per-second'
                            ? t('按秒计费')
                            : t('按量计费')}
                  </Tag>
                  <Button
                    size='small'
                    theme='borderless'
                    type='tertiary'
                    onClick={resetSelectedModel}
                  >
                    {t('还原')}
                  </Button>
                </div>
              ) : null
            }
          >
            {!selectedModel ? (
              <Empty
                title={emptyTitle || t('暂无模型')}
                description={
                  emptyDescription || t('请先新增模型或从左侧列表选择一个模型')
                }
              />
            ) : (
              <div>
                <div className='mb-4'>
                  <div className='mb-2 font-medium text-gray-700'>
                    {t('计费方式')}
                  </div>
                  <RadioGroup
                    type='button'
                    value={selectedModel.billingMode}
                    onChange={(event) =>
                      handleBillingModeChange(event?.target?.value ?? event)
                    }
                  >
                    <Radio value='per-token'>{t('按量计费')}</Radio>
                    <Radio value='per-request'>{t('按次计费')}</Radio>
                    <Radio value='per-image'>{t('按张计费')}</Radio>
                    <Radio value='per-second'>{t('按秒计费')}</Radio>
                    <Radio value='tiered_expr'>{t('表达式/阶梯计费')}</Radio>
                  </RadioGroup>
                  <div className='mt-2 text-xs text-gray-500'>
                    {t(
                      '普通按量/按次/按张/按秒直接填价格就行；如果价格要跟请求参数或请求头联动，请切到表达式/阶梯计费。',
                    )}
                  </div>
                </div>

                {selectedWarnings.length > 0 ? (
                  <Card
                    bodyStyle={{ padding: 12 }}
                    style={{
                      marginBottom: 16,
                      background: 'var(--semi-color-warning-light-default)',
                    }}
                  >
                    <div className='font-medium mb-2'>{t('当前提示')}</div>
                    {selectedWarnings.map((warning) => (
                      <div key={warning} className='text-sm text-gray-700 mb-1'>
                        {warning}
                      </div>
                    ))}
                  </Card>
                ) : null}

                {selectedModel.billingMode === 'per-request' ? (
                  <PriceInput
                    label={t('固定价格')}
                    value={selectedModel.fixedPrice}
                    placeholder={t('输入每次调用价格')}
                    suffix={t('$/次')}
                    onChange={(value) =>
                      handleNumericFieldChange('fixedPrice', value)
                    }
                    extraText={t('适合 MJ / 任务类等按次收费模型。')}
                  />
                ) : selectedModel.billingMode === 'per-image' ? (
                  <PricingStructureEditor
                    label={t('每张价格（按张）')}
                    structure={selectedModel.imagePriceStructure}
                    suffix={t('$/张')}
                    t={t}
                    onChange={(next) =>
                      handleStructureFieldChange(
                        'imagePriceStructure',
                        next,
                        'imagePricePerImage',
                      )
                    }
                    extraText={t(
                      '适合图像生成类模型，可按张直接计费，也可以按图像类别/分辨率等维度分别配置；留空则使用默认0.28美元。',
                    )}
                    hiddenKinds={['audio-resolution']}
                  />
                ) : selectedModel.billingMode === 'per-second' ? (
                  <PricingStructureEditor
                    label={t('每秒价格（按秒）')}
                    structure={selectedModel.videoPriceStructure}
                    suffix={t('$/秒')}
                    t={t}
                    onChange={(next) =>
                      handleStructureFieldChange(
                        'videoPriceStructure',
                        next,
                        'videoPricePerSecond',
                      )
                    }
                    extraText={t(
                      '适合视频或实时音频类模型，可按秒直接计费，也可以按「无音频 / 有音频」或分辨率多档配置。',
                    )}
                  />
                ) : selectedModel.billingMode === 'tiered_expr' ? (
                  <TieredPricingEditor
                    model={selectedModel}
                    onExprChange={handleBillingExprChange}
                    requestRuleExpr={selectedModel.requestRuleExpr}
                    onRequestRuleExprChange={handleRequestRuleExprChange}
                    t={t}
                  />
                ) : (
                  <>
                    <Card
                      bodyStyle={{ padding: 16 }}
                      style={{
                        marginBottom: 16,
                        background: 'var(--semi-color-fill-0)',
                      }}
                    >
                      <div className='font-medium mb-3'>{t('基础价格')}</div>
                      <PriceInput
                        label={t('输入价格')}
                        value={selectedModel.inputPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('inputPrice', value)
                        }
                      />
                      {selectedModel.completionRatioLocked ? (
                        <Banner
                          type='warning'
                          bordered
                          fullMode={false}
                          closeIcon={null}
                          style={{ marginBottom: 12 }}
                          title={t('补全价格已锁定')}
                          description={t(
                            '该模型补全倍率由后端固定为 {{ratio}}。补全价格不能在这里修改。',
                            {
                              ratio: selectedModel.lockedCompletionRatio || '-',
                            },
                          )}
                        />
                      ) : null}
                      <PriceInput
                        label={t('补全价格')}
                        value={selectedModel.completionPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('completionPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'completionPrice',
                            )}
                            disabled={selectedModel.completionRatioLocked}
                            onChange={(checked) =>
                              handleOptionalFieldToggle(
                                'completionPrice',
                                checked,
                              )
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'completionPrice',
                          )
                        }
                        disabled={
                          !hasValue(selectedModel.inputPrice) ||
                          selectedModel.completionRatioLocked
                        }
                        extraText={
                          selectedModel.completionRatioLocked
                            ? t(
                                '后端固定倍率：{{ratio}}。该字段仅展示换算后的价格。',
                                {
                                  ratio:
                                    selectedModel.lockedCompletionRatio || '-',
                                },
                              )
                            : !isOptionalFieldEnabled(
                                  selectedModel,
                                  'completionPrice',
                                )
                              ? t('当前未启用，需要时再打开即可。')
                              : ''
                        }
                      />
                      <PriceInput
                        label={t('缓存读取价格')}
                        value={selectedModel.cachePrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('cachePrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'cachePrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('cachePrice', checked)
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(selectedModel, 'cachePrice')
                        }
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(selectedModel, 'cachePrice')
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                      <PriceInput
                        label={t('缓存创建价格')}
                        value={selectedModel.createCachePrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('createCachePrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'createCachePrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle(
                                'createCachePrice',
                                checked,
                              )
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'createCachePrice',
                          )
                        }
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'createCachePrice',
                          )
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                    </Card>

                    <Card
                      bodyStyle={{ padding: 16 }}
                      style={{
                        marginBottom: 16,
                        background: 'var(--semi-color-fill-0)',
                      }}
                    >
                      <div className='mb-3'>
                        <div className='font-medium'>{t('扩展价格')}</div>
                        <div className='text-xs text-gray-500 mt-1'>
                          {t('这些价格都是可选项，不填也可以。')}
                        </div>
                      </div>
                      <PriceInput
                        label={t('图片输入价格')}
                        value={selectedModel.imagePrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('imagePrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'imagePrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('imagePrice', checked)
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(selectedModel, 'imagePrice')
                        }
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(selectedModel, 'imagePrice')
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                      <PriceInput
                        label={t('图片补全价格')}
                        value={selectedModel.imageCompletionPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('imageCompletionPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'imageCompletionPrice',
                            )}
                            disabled={
                              !isOptionalFieldEnabled(
                                selectedModel,
                                'imagePrice',
                              )
                            }
                            onChange={(checked) =>
                              handleOptionalFieldToggle(
                                'imageCompletionPrice',
                                checked,
                              )
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'imageCompletionPrice',
                          )
                        }
                        disabled={!hasValue(selectedModel.imagePrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'imagePrice',
                          )
                            ? t('请先开启并填写图片输入价格。')
                            : !isOptionalFieldEnabled(
                                  selectedModel,
                                  'imageCompletionPrice',
                                )
                              ? t('当前未启用，需要时再打开即可。')
                              : ''
                        }
                      />
                      <PriceInput
                        label={t('音频输入价格')}
                        value={selectedModel.audioInputPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('audioInputPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'audioInputPrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle(
                                'audioInputPrice',
                                checked,
                              )
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'audioInputPrice',
                          )
                        }
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'audioInputPrice',
                          )
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                      <PriceInput
                        label={t('音频补全价格')}
                        value={selectedModel.audioOutputPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('audioOutputPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'audioOutputPrice',
                            )}
                            disabled={
                              !isOptionalFieldEnabled(
                                selectedModel,
                                'audioInputPrice',
                              )
                            }
                            onChange={(checked) =>
                              handleOptionalFieldToggle(
                                'audioOutputPrice',
                                checked,
                              )
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'audioOutputPrice',
                          )
                        }
                        disabled={!hasValue(selectedModel.audioInputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'audioInputPrice',
                          )
                            ? t('请先开启并填写音频输入价格。')
                            : !isOptionalFieldEnabled(
                                  selectedModel,
                                  'audioOutputPrice',
                                )
                              ? t('当前未启用，需要时再打开即可。')
                              : ''
                        }
                      />
                      <PriceInput
                        label={t('视频输入价格')}
                        value={selectedModel.videoInputPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('videoInputPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'videoInputPrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle(
                                'videoInputPrice',
                                checked,
                              )
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'videoInputPrice',
                          )
                        }
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'videoInputPrice',
                          )
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                      <PriceInput
                        label={t('视频补全价格')}
                        value={selectedModel.videoCompletionPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('videoCompletionPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'videoCompletionPrice',
                            )}
                            disabled={
                              !isOptionalFieldEnabled(
                                selectedModel,
                                'videoInputPrice',
                              )
                            }
                            onChange={(checked) =>
                              handleOptionalFieldToggle(
                                'videoCompletionPrice',
                                checked,
                              )
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'videoCompletionPrice',
                          )
                        }
                        disabled={!hasValue(selectedModel.videoInputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'videoInputPrice',
                          )
                            ? t('请先开启并填写视频输入价格。')
                            : !isOptionalFieldEnabled(
                                  selectedModel,
                                  'videoCompletionPrice',
                                )
                              ? t('当前未启用，需要时再打开即可。')
                              : ''
                        }
                      />
                    </Card>
                  </>
                )}

                <Card
                  bodyStyle={{ padding: 16 }}
                  style={{ background: 'var(--semi-color-fill-0)' }}
                >
                  <div className='font-medium mb-3'>{t('保存预览')}</div>
                  <div className='text-xs text-gray-500 mb-3'>
                    {t(
                      '下面展示这个模型保存后会写入哪些后端字段，便于和原始 JSON 编辑框保持一致。',
                    )}
                  </div>
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'minmax(140px, 180px) 1fr',
                      gap: 8,
                    }}
                  >
                    {previewRows.map((row) => (
                      <React.Fragment key={row.key}>
                        <Text strong>{row.label}</Text>
                        <Text>{row.value}</Text>
                      </React.Fragment>
                    ))}
                  </div>
                </Card>
              </div>
            )}
          </Card>
        </div>
      </Space>

      {allowAddModel ? (
        <Modal
          title={t('添加模型')}
          visible={addVisible}
          onCancel={() => {
            setAddVisible(false);
            setNewModelName('');
          }}
          onOk={handleAddModel}
        >
          <Input
            value={newModelName}
            placeholder={t('输入模型名称，例如 gpt-4.1')}
            onChange={(value) => setNewModelName(value)}
          />
        </Modal>
      ) : null}

      <Modal
        title={t('批量应用当前模型价格')}
        visible={batchVisible}
        onCancel={() => setBatchVisible(false)}
        onOk={() => {
          if (applySelectedModelPricing()) {
            setBatchVisible(false);
          }
        }}
      >
        <div className='text-sm text-gray-600'>
          {selectedModel
            ? t(
                '将把当前编辑中的模型 {{name}} 的价格配置，批量应用到已勾选的 {{count}} 个模型。',
                {
                  name: selectedModel.name,
                  count: selectedModelNames.length,
                },
              )
            : t('请先选择一个作为模板的模型')}
        </div>
        {selectedModel ? (
          <div className='text-xs text-gray-500 mt-3'>
            {t(
              '适合同系列模型一起定价，例如把 gpt-5.1 的价格批量同步到 gpt-5.1-high、gpt-5.1-low 等模型。',
            )}
          </div>
        ) : null}
      </Modal>

      <OperLogConfirmModal
        visible={operLogModal.visible}
        operType='模型价格'
        changes={operLogModal.changes}
        defaultRemark={operLogModal.defaultRemark}
        onConfirm={confirmOperLogSave}
        onSkip={skipOperLogSave}
        onCancel={cancelOperLogSave}
      />
    </>
  );
}
