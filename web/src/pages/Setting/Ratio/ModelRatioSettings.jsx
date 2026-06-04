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

import React, { useEffect, useState, useRef } from 'react';
import {
  Button,
  Col,
  Form,
  Popconfirm,
  Row,
  Space,
  Spin,
} from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
  verifyJSON,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import OperLogConfirmModal from '../../../components/oper-log/OperLogConfirmModal';
import { createOperLog } from '../../../components/oper-log/operLogApi';

export default function ModelRatioSettings(props) {
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    ModelPrice: '',
    ModelRatio: '',
    CacheRatio: '',
    CreateCacheRatio: '',
    CompletionRatio: '',
    ImageRatio: '',
    AudioRatio: '',
    AudioCompletionRatio: '',
    ImageCompletionRatio: '',
    VideoRatio: '',
    VideoCompletionRatio: '',
    ImageModelPricePerImage: '',
    VideoModelPricePerSecond: '',
    ExposeRatioEnabled: false,
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);
  const { t } = useTranslation();

  // 操作日志弹窗 state
  const [logModal, setLogModal] = useState({ visible: false, changes: [], updateArray: [] });

  // 实际执行保存（经日志弹窗确认或跳过后调用）
  async function doSave(updateArray, logRemark) {
    const requestQueue = updateArray.map((item) => {
      const value =
        typeof inputs[item.key] === 'boolean'
          ? String(inputs[item.key])
          : inputs[item.key];
      return API.put('/api/option/', { key: item.key, value });
    });

    setLoading(true);
    try {
      const res = await Promise.all(requestQueue);
      if (res.includes(undefined)) {
        return showError(
          requestQueue.length > 1 ? t('部分保存失败，请重试') : t('保存失败'),
        );
      }
      for (let i = 0; i < res.length; i++) {
        if (!res[i].data.success) {
          return showError(res[i].data.message);
        }
      }
      showSuccess(t('保存成功'));
      // 写操作日志（logRemark 为 null 表示运维选择「不记录」）
      if (logRemark !== null) {
        const content = updateArray.map((item) => item.key).join(', ');
        await createOperLog({ oper_type: '模型价格', content, remark: logRemark });
      }
      props.refresh();
    } catch (error) {
      console.error('Unexpected error:', error);
      showError(t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  }

  async function onSubmit() {
    try {
      await refForm.current.validate();
    } catch {
      showError(t('请检查输入'));
      return;
    }

    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));

    // 构建变更列表供弹窗展示
    const changes = updateArray.map((item) => ({
      key: item.key,
      oldVal: inputsRow[item.key],
      newVal: inputs[item.key],
    }));
    const defaultRemark = `修改了 ${updateArray.map((i) => i.key).join('、')}`;
    setLogModal({ visible: true, changes, updateArray, defaultRemark });
  }

  async function resetModelRatio() {
    try {
      let res = await API.post(`/api/option/rest_model_ratio`);
      if (res.data.success) {
        showSuccess(res.data.message);
        props.refresh();
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(error);
    }
  }

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('模型固定价格')}
              extraText={t('一次调用消耗多少刀，优先级大于模型倍率')}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为一次调用消耗多少刀，比如 "gpt-4-gizmo-*": 0.1，一次消耗0.1刀',
              )}
              field={'ModelPrice'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) => setInputs({ ...inputs, ModelPrice: value })}
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('模型倍率')}
              placeholder={t('为一个 JSON 文本，键为模型名称，值为倍率')}
              field={'ModelRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) => setInputs({ ...inputs, ModelRatio: value })}
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('提示缓存倍率')}
              placeholder={t('为一个 JSON 文本，键为模型名称，值为倍率')}
              field={'CacheRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) => setInputs({ ...inputs, CacheRatio: value })}
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('缓存创建倍率')}
              extraText={t(
                '默认为 5m 缓存创建倍率；1h 缓存创建倍率按固定乘法自动计算（当前为 1.6x）',
              )}
              placeholder={t('为一个 JSON 文本，键为模型名称，值为倍率')}
              field={'CreateCacheRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, CreateCacheRatio: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('模型补全倍率（仅对自定义模型有效）')}
              extraText={t('仅对自定义模型有效')}
              placeholder={t('为一个 JSON 文本，键为模型名称，值为倍率')}
              field={'CompletionRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, CompletionRatio: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('图片输入倍率（仅部分模型支持该计费）')}
              extraText={t(
                '图片输入相关的倍率设置，键为模型名称，值为倍率，仅部分模型支持该计费',
              )}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为倍率，例如：{"gpt-image-1": 2}',
              )}
              field={'ImageRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) => setInputs({ ...inputs, ImageRatio: value })}
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('音频倍率（仅部分模型支持该计费）')}
              extraText={t('音频输入相关的倍率设置，键为模型名称，值为倍率')}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为倍率，例如：{"gpt-4o-audio-preview": 16}',
              )}
              field={'AudioRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) => setInputs({ ...inputs, AudioRatio: value })}
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('音频补全倍率（仅部分模型支持该计费）')}
              extraText={t(
                '音频输出补全相关的倍率设置，键为模型名称，值为倍率',
              )}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为倍率，例如：{"gpt-4o-realtime": 2}',
              )}
              field={'AudioCompletionRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, AudioCompletionRatio: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('图片补全倍率（仅部分模型支持该计费）')}
              extraText={t(
                '图片输出补全相关的倍率设置，键为模型名称，值为倍率。如果图片倍率为空，则使用文本倍率作为基准',
              )}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为倍率，例如：{"gpt-image-1": 4}',
              )}
              field={'ImageCompletionRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, ImageCompletionRatio: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('视频倍率（仅部分模型支持该计费）')}
              extraText={t(
                '视频输入相关的倍率设置，键为模型名称，值为倍率',
              )}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为倍率，例如：{"veo-2.0-generate": 5}',
              )}
              field={'VideoRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, VideoRatio: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('视频补全倍率（仅部分模型支持该计费）')}
              extraText={t(
                '视频输出补全相关的倍率设置，键为模型名称，值为倍率。如果视频倍率为空则使用文本倍率作为基准，如果文本倍率也为空则直接使用此处配置的价格',
              )}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为倍率，例如：{"kling-v2": 10}',
              )}
              field={'VideoCompletionRatio'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, VideoCompletionRatio: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('按张计费模型每张价格（单位美元）')}
              extraText={t(
                '按张计费的图片模型，每张图片的价格（美元），键为模型名称，值为每张价格',
              )}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为每张价格，例如：{"dall-e-3": 0.04}',
              )}
              field={'ImageModelPricePerImage'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, ImageModelPricePerImage: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col xs={24} sm={16}>
            <Form.TextArea
              label={t('按秒计费模型每秒价格（单位美元）')}
              extraText={t(
                '按秒计费的视频模型，每秒视频的价格（美元），键为模型名称，值为每秒价格',
              )}
              placeholder={t(
                '为一个 JSON 文本，键为模型名称，值为每秒价格，例如：{"kling-v1": 0.01}',
              )}
              field={'VideoModelPricePerSecond'}
              autosize={{ minRows: 6, maxRows: 12 }}
              trigger='blur'
              stopValidateWithError
              rules={[
                {
                  validator: (rule, value) => verifyJSON(value),
                  message: '不是合法的 JSON 字符串',
                },
              ]}
              onChange={(value) =>
                setInputs({ ...inputs, VideoModelPricePerSecond: value })
              }
            />
          </Col>
        </Row>
        <Row gutter={16}>
          <Col span={16}>
            <Form.Switch
              label={t('暴露倍率接口')}
              field={'ExposeRatioEnabled'}
              onChange={(value) =>
                setInputs({ ...inputs, ExposeRatioEnabled: value })
              }
            />
          </Col>
        </Row>
      </Form>
      <Space>
        <Button onClick={onSubmit}>{t('保存模型倍率设置')}</Button>
        <Popconfirm
          title={t('确定重置模型倍率吗？')}
          content={t('此修改将不可逆')}
          okType={'danger'}
          position={'top'}
          onConfirm={resetModelRatio}
        >
          <Button type={'danger'}>{t('重置模型倍率')}</Button>
        </Popconfirm>
      </Space>

      <OperLogConfirmModal
        visible={logModal.visible}
        operType='模型价格'
        changes={logModal.changes}
        defaultRemark={logModal.defaultRemark}
        onConfirm={(remark) => {
          setLogModal((s) => ({ ...s, visible: false }));
          doSave(logModal.updateArray, remark);
        }}
        onSkip={() => {
          setLogModal((s) => ({ ...s, visible: false }));
          doSave(logModal.updateArray, null);
        }}
        onCancel={() => setLogModal((s) => ({ ...s, visible: false }))}
      />
    </Spin>
  );
}
