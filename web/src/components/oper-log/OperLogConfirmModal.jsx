import React, { useEffect, useState } from 'react';
import {
  Button,
  Modal,
  Space,
  Table,
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
};

const FIELD_LABELS = {
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
  GroupRatio: '分组倍率',
  UserUsableGroups: '用户可选分组',
  GroupGroupRatio: '分组特殊倍率',
  'group_ratio_setting.group_special_usable_group': '分组特殊可用分组',
  AutoGroups: '自动分组',
  DefaultUseAutoGroup: '默认使用auto分组',
};

function truncate(str, max = 120) {
  if (!str) return '';
  const s = String(str);
  return s.length > max ? s.slice(0, max) + '…' : s;
}

/**
 * 操作日志确认弹窗（切面式，不影响主流程）。
 *
 * Props:
 *   visible       {boolean}      是否显示
 *   operType      {string}       日志类型（模型价格/用户分组/渠道配置）
 *   changes       {Array}        compareObjects 返回的变更项，每项 { key, oldVal, newVal }
 *   defaultRemark {string}       预填备注
 *   onConfirm     {(remark)=>void}  点击「确认并记录日志」
 *   onSkip        {()=>void}     点击「不记录，直接保存」
 *   onCancel      {()=>void}     点击取消（不保存）
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
    if (visible) {
      setRemark(defaultRemark);
    }
  }, [visible, defaultRemark]);

  const tagColor = OPER_TYPE_COLORS[operType] || 'grey';

  const columns = [
    {
      title: '配置项',
      dataIndex: 'key',
      width: 180,
      render: (key) => (
        <Text strong size='small'>
          {FIELD_LABELS[key] || key}
        </Text>
      ),
    },
    {
      title: '原始值',
      dataIndex: 'oldVal',
      render: (v) => (
        <Text
          type='tertiary'
          size='small'
          style={{ fontFamily: 'monospace', wordBreak: 'break-all' }}
        >
          {truncate(v)}
        </Text>
      ),
    },
    {
      title: '新值',
      dataIndex: 'newVal',
      render: (v) => (
        <Text
          size='small'
          style={{ fontFamily: 'monospace', wordBreak: 'break-all', color: 'var(--semi-color-success)' }}
        >
          {truncate(v)}
        </Text>
      ),
    },
  ];

  const dataSource = changes.map((item, i) => ({
    key: item.key,
    oldVal: item.oldVal !== undefined ? String(item.oldVal) : '-',
    newVal: item.newVal !== undefined ? String(item.newVal) : '-',
    _key: i,
  }));

  return (
    <Modal
      visible={visible}
      title={
        <Space>
          <IconEdit />
          <span>确认改动并填写操作日志</span>
          <Tag color={tagColor} size='small'>{operType}</Tag>
        </Space>
      }
      width={720}
      footer={null}
      onCancel={onCancel}
      maskClosable={false}
    >
      <div style={{ marginBottom: 12 }}>
        <Space style={{ marginBottom: 8 }}>
          <IconAlertCircle style={{ color: 'var(--semi-color-warning)' }} />
          <Text type='tertiary' size='small'>
            以下配置项将被保存，请确认改动是否符合预期，并可选填写本次变更原因。
          </Text>
        </Space>
        <Table
          columns={columns}
          dataSource={dataSource}
          rowKey='_key'
          size='small'
          pagination={false}
          bordered
          style={{ marginBottom: 16 }}
          scroll={{ y: 240 }}
        />
      </div>

      <div style={{ marginBottom: 16 }}>
        <Text strong size='small' style={{ display: 'block', marginBottom: 6 }}>
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

      <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
        <Button onClick={onCancel} type='tertiary'>
          取消
        </Button>
        <Button onClick={onSkip} type='secondary'>
          不记录，直接保存
        </Button>
        <Button
          theme='solid'
          type='primary'
          onClick={() => onConfirm(remark)}
        >
          确认并记录日志
        </Button>
      </Space>
    </Modal>
  );
};

export default OperLogConfirmModal;
