import { API } from '../../helpers';

/**
 * 创建操作日志记录。
 * @param {{oper_type: string, content: string, remark: string}} params
 */
export async function createOperLog({ oper_type, content, remark }) {
  try {
    await API.post('/api/oper-log/', { oper_type, content, remark });
  } catch (e) {
    // 日志写入失败不阻断主流程，静默处理
    console.warn('[OperLog] 写入操作日志失败', e);
  }
}

/**
 * 查询操作日志（分页）。
 * @param {{oper_type?: string, page?: number, page_size?: number}} params
 */
export async function listOperLogs({ oper_type = '', page = 1, page_size = 20 } = {}) {
  const params = new URLSearchParams({ page: String(page), page_size: String(page_size) });
  if (oper_type) params.set('oper_type', oper_type);
  const res = await API.get(`/api/oper-log/?${params.toString()}`);
  return res?.data;
}
