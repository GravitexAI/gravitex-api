import React, { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Descriptions,
  Empty,
  Form,
  Input,
  Modal,
  Pagination,
  SideSheet,
  Space,
  Spin,
  Table,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconPlusCircle } from '@douyinfe/semi-icons';
import { createOperLog, listOperLogs } from '../../../oper-log/operLogApi';

const { Text } = Typography;

const OPER_TYPE = '渠道配置';

function formatTs(ts) {
  if (!ts) return '-';
  return new Date(ts * 1000).toLocaleString('zh-CN');
}

/**
 * 渠道操作日志侧边抽屉。
 *
 * Props:
 *   visible  {boolean}
 *   onClose  {()=>void}
 */
const ChannelLogModal = ({ visible, onClose }) => {
  const [logs, setLogs] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loadingList, setLoadingList] = useState(false);
  const [showAddForm, setShowAddForm] = useState(false);
  const [addContent, setAddContent] = useState('');
  const [addRemark, setAddRemark] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const PAGE_SIZE = 20;

  const loadLogs = useCallback(async (p = 1) => {
    setLoadingList(true);
    try {
      const res = await listOperLogs({ oper_type: OPER_TYPE, page: p, page_size: PAGE_SIZE });
      if (res?.success) {
        setLogs(res.data || []);
        setTotal(res.total || 0);
        setPage(p);
      }
    } catch (e) {
      Toast.error('加载日志失败');
    } finally {
      setLoadingList(false);
    }
  }, []);

  useEffect(() => {
    if (visible) {
      loadLogs(1);
      setShowAddForm(false);
      setAddContent('');
      setAddRemark('');
    }
  }, [visible, loadLogs]);

  async function handleSubmitAdd() {
    if (!addContent.trim() && !addRemark.trim()) {
      Toast.warning('请填写改动内容或备注');
      return;
    }
    setSubmitting(true);
    try {
      await createOperLog({ oper_type: OPER_TYPE, content: addContent.trim(), remark: addRemark.trim() });
      Toast.success('日志已记录');
      setShowAddForm(false);
      setAddContent('');
      setAddRemark('');
      loadLogs(1);
    } catch (e) {
      Toast.error('记录日志失败');
    } finally {
      setSubmitting(false);
    }
  }

  const columns = [
    {
      title: '操作人',
      dataIndex: 'operator',
      width: 120,
      render: (v) => <Text size='small'>{v || '-'}</Text>,
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      width: 160,
      render: (v) => <Text size='small' type='tertiary'>{formatTs(v)}</Text>,
    },
    {
      title: '改动内容',
      dataIndex: 'content',
      render: (v) => (
        <Text size='small' style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
          {v || '-'}
        </Text>
      ),
    },
    {
      title: '备注',
      dataIndex: 'remark',
      render: (v) => (
        <Text size='small' type='secondary' style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
          {v || '-'}
        </Text>
      ),
    },
  ];

  return (
    <SideSheet
      title={
        <Space>
          <Tag color='orange'>渠道配置</Tag>
          <span>操作日志</span>
        </Space>
      }
      visible={visible}
      onCancel={onClose}
      width={680}
      bodyStyle={{ padding: 0 }}
      footer={null}
    >
      <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--semi-color-border)' }}>
        {!showAddForm ? (
          <Button
            icon={<IconPlusCircle />}
            type='primary'
            theme='light'
            onClick={() => setShowAddForm(true)}
          >
            添加日志
          </Button>
        ) : (
          <div style={{ background: 'var(--semi-color-fill-0)', borderRadius: 8, padding: 16 }}>
            <Text strong style={{ display: 'block', marginBottom: 10 }}>
              添加渠道配置日志
            </Text>
            <TextArea
              value={addContent}
              onChange={setAddContent}
              placeholder='本次改动内容（如：修改了 xxx 渠道的 Key）'
              autosize={{ minRows: 2, maxRows: 4 }}
              style={{ marginBottom: 10 }}
            />
            <TextArea
              value={addRemark}
              onChange={setAddRemark}
              placeholder='备注（可选）'
              autosize={{ minRows: 1, maxRows: 3 }}
              style={{ marginBottom: 10 }}
            />
            <Space>
              <Button
                type='primary'
                loading={submitting}
                onClick={handleSubmitAdd}
              >
                保存
              </Button>
              <Button
                type='tertiary'
                onClick={() => {
                  setShowAddForm(false);
                  setAddContent('');
                  setAddRemark('');
                }}
              >
                取消
              </Button>
            </Space>
          </div>
        )}
      </div>

      <div style={{ padding: '12px 20px' }}>
        <Spin spinning={loadingList}>
          {logs.length === 0 && !loadingList ? (
            <Empty description='暂无日志' style={{ padding: 40 }} />
          ) : (
            <>
              <Table
                columns={columns}
                dataSource={logs}
                rowKey='id'
                size='small'
                pagination={false}
                bordered
              />
              {total > PAGE_SIZE && (
                <div style={{ marginTop: 12, textAlign: 'right' }}>
                  <Pagination
                    total={total}
                    pageSize={PAGE_SIZE}
                    currentPage={page}
                    onChange={(p) => loadLogs(p)}
                    size='small'
                  />
                </div>
              )}
            </>
          )}
        </Spin>
      </div>
    </SideSheet>
  );
};

export default ChannelLogModal;
