import React, { useEffect, useMemo, useState } from 'react';
import {
  Button,
  Modal,
  Space,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconAlertCircle, IconEdit } from '@douyinfe/semi-icons';

const { Text } = Typography;

const OPER_TYPE_COLORS = {
  '模型价格': 'blue',
  '用户分组': 'green',
  '渠道配置': 'orange',
  '工具定价': 'purple',
};

export const FIELD_LABELS = {
  ModelPrice: '模型固定价格',
  ModelRatio: '模型倍率',
  CacheRatio: '提示缓存倍率',
  CreateCacheRatio: '缓存创建倍率',
  CompletionRatio: '模型补全倍率',
  ImageRatio: '图片输入倍率',
  AudioRatio: '音频倍率',
  AudioCompletionRatio: '音频补全倍率',
  ImageCompletionRatio: '图片补全倍率',
  VideoRatio: '视频倍率',
  VideoCompletionRatio: '视频补全倍率',
  ImageModelPricePerImage: '按张计费每张价格',
  VideoModelPricePerSecond: '按秒计费每秒价格',
  ExposeRatioEnabled: '暴露倍率接口',
  'billing_setting.billing_mode': '计费模式(阶梯计费)',
  'billing_setting.billing_expr': '计费表达式(阶梯计费)',
  'tool_price_setting.prices': '工具调用价格',
  GroupRatio: '分组倍率',
  UserUsableGroups: '用户可选分组',
  GroupGroupRatio: '分组特殊倍率',
  'group_ratio_setting.group_special_usable_group': '分组特殊可用分组',
  AutoGroups: '自动分组',
  DefaultUseAutoGroup: '默认使用auto分组',
};

export function fieldLabel(key) {
  return FIELD_LABELS[key] || key;
}

function toStr(v) {
  if (v === undefined || v === null || v === '') return '(空)';
  return String(v);
}

// 尝试把字符串解析为「纯对象」（非数组），失败返回 null
function tryParseObject(str) {
  if (typeof str !== 'string') return null;
  const s = str.trim();
  if (!s || s === '(空)') return {};
  try {
    const v = JSON.parse(s);
    if (v && typeof v === 'object' && !Array.isArray(v)) return v;
    return null;
  } catch {
    return null;
  }
}

// 对比新旧对象，仅保留发生变化（新增/删除/修改）的键
function diffObjects(oldObj, newObj) {
  const oldDiff = {};
  const newDiff = {};
  const keys = new Set([...Object.keys(oldObj), ...Object.keys(newObj)]);
  keys.forEach((k) => {
    const inOld = Object.prototype.hasOwnProperty.call(oldObj, k);
    const inNew = Object.prototype.hasOwnProperty.call(newObj, k);
    if (inOld && inNew) {
      if (JSON.stringify(oldObj[k]) !== JSON.stringify(newObj[k])) {
        oldDiff[k] = oldObj[k];
        newDiff[k] = newObj[k];
      }
    } else if (inOld) {
      oldDiff[k] = oldObj[k]; // 删除的键
    } else {
      newDiff[k] = newObj[k]; // 新增的键
    }
  });
  return { oldDiff, newDiff };
}

// 将单个变更项精简为「仅改动部分」：
//   - 新旧都是 JSON 对象时，只展示发生变化的键
//   - 否则原样返回
function refineChange(change) {
  const oldObj = tryParseObject(change.oldVal);
  const newObj = tryParseObject(change.newVal);
  if (oldObj && newObj) {
    const { oldDiff, newDiff } = diffObjects(oldObj, newObj);
    return {
      ...change,
      oldVal: JSON.stringify(oldDiff, null, 2),
      newVal: JSON.stringify(newDiff, null, 2),
    };
  }
  return change;
}

/**
 * 将变更项序列化为可读的日志正文（落库 content）。
 * 包含：配置项、原始值、新值。
 */
export function buildOperLogContent(changes) {
  return changes
    .map(
      (c) =>
        `【${fieldLabel(c.key)}】\n  原始值：${toStr(c.oldVal)}\n  新值：${toStr(c.newVal)}`,
    )
    .join('\n\n');
}

/**
 * 操作日志确认弹窗（切面式，不影响主流程）。
 *
 * Props:
 *   visible       {boolean}
 *   operType      {string}              日志类型（模型价格/用户分组/渠道配置）
 *   changes       {Array}               变更项，每项 { key, oldVal, newVal }
 *   defaultRemark {string}              预填备注
 *   onConfirm     {(remark, content)=>void}  确认并记录日志
 *   onSkip        {()=>void}            不记录，直接保存
 *   onCancel      {()=>void}            取消（不保存）
 */
const OperLogConfirmModal = ({
  visible,
  operType,
  changes = [],
  defaultRemark = '',
  onConfirm,
  onSkip,
  onCancel,
}) => {
  const [remark, setRemark] = useState('');

  useEffect(() => {
    if (visible) setRemark(defaultRemark);
  }, [visible, defaultRemark]);

  // 仅展示/记录改动的键
  const refinedChanges = useMemo(
    () => changes.map((c) => refineChange(c)),
    [changes],
  );

  const tagColor = OPER_TYPE_COLORS[operType] || 'grey';

  return (
    <Modal
      visible={visible}
      title={
        <Space spacing={8}>
          <IconEdit style={{ color: 'var(--semi-color-primary)' }} />
          <span>确认改动并填写操作日志</span>
          <Tag color={tagColor} size='small'>
            {operType}
          </Tag>
        </Space>
      }
      width={760}
      footer={null}
      onCancel={onCancel}
      maskClosable={false}
      bodyStyle={{ padding: '20px 24px 24px' }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '10px 12px',
          marginBottom: 16,
          borderRadius: 8,
          background: 'var(--semi-color-warning-light-default)',
        }}
      >
        <IconAlertCircle style={{ color: 'var(--semi-color-warning)', flexShrink: 0 }} />
        <Text type='tertiary' size='small'>
          以下配置项将被保存，请确认改动是否符合预期，并可选填写本次变更原因。
        </Text>
      </div>

      <div
        style={{
          border: '1px solid var(--semi-color-border)',
          borderRadius: 8,
          overflow: 'hidden',
          marginBottom: 20,
        }}
      >
        {/* 表头 */}
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '180px 1fr 1fr',
            background: 'var(--semi-color-fill-0)',
            fontWeight: 600,
            fontSize: 13,
          }}
        >
          <div style={{ padding: '10px 14px' }}>配置项</div>
          <div style={{ padding: '10px 14px' }}>原始值</div>
          <div style={{ padding: '10px 14px' }}>新值</div>
        </div>
        {/* 数据行 */}
        <div style={{ maxHeight: 320, overflowY: 'auto' }}>
          {refinedChanges.map((item, idx) => (
            <div
              key={item.key}
              style={{
                display: 'grid',
                gridTemplateColumns: '180px 1fr 1fr',
                borderTop: '1px solid var(--semi-color-border)',
                background:
                  idx % 2 === 1 ? 'var(--semi-color-fill-0)' : 'transparent',
              }}
            >
              <div style={{ padding: '12px 14px' }}>
                <Text strong size='small'>
                  {fieldLabel(item.key)}
                </Text>
              </div>
              <div style={{ padding: '12px 14px', minWidth: 0 }}>
                <pre
                  style={{
                    margin: 0,
                    fontFamily: 'monospace',
                    fontSize: 12,
                    lineHeight: 1.6,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-all',
                    color: 'var(--semi-color-text-2)',
                    maxHeight: 160,
                    overflow: 'auto',
                  }}
                >
                  {toStr(item.oldVal)}
                </pre>
              </div>
              <div style={{ padding: '12px 14px', minWidth: 0 }}>
                <pre
                  style={{
                    margin: 0,
                    fontFamily: 'monospace',
                    fontSize: 12,
                    lineHeight: 1.6,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-all',
                    color: 'var(--semi-color-success)',
                    maxHeight: 160,
                    overflow: 'auto',
                  }}
                >
                  {toStr(item.newVal)}
                </pre>
              </div>
            </div>
          ))}
        </div>
      </div>

      <div style={{ marginBottom: 20 }}>
        <Text strong size='small' style={{ display: 'block', marginBottom: 8 }}>
          备注（可选）
        </Text>
        <TextArea
          value={remark}
          onChange={setRemark}
          placeholder='请简述本次变更原因，如：新增模型 xxx 价格配置'
          autosize={{ minRows: 2, maxRows: 5 }}
          showClear
        />
      </div>

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
        <Button onClick={onCancel} type='tertiary'>
          取消
        </Button>
        <Button onClick={onSkip} type='secondary'>
          不记录，直接保存
        </Button>
        <Button
          theme='solid'
          type='primary'
          onClick={() => onConfirm(remark, buildOperLogContent(refinedChanges))}
        >
          确认并记录日志
        </Button>
      </div>
    </Modal>
  );
};

export default OperLogConfirmModal;
