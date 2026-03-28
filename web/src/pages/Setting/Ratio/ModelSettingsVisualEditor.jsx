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
  Table,
  Button,
  Input,
  Modal,
  Form,
  Space,
  RadioGroup,
  Radio,
  Checkbox,
  Tag,
} from '@douyinfe/semi-ui';
import {
  IconDelete,
  IconPlus,
  IconSearch,
  IconSave,
  IconEdit,
} from '@douyinfe/semi-icons';
import { API, showError, showSuccess, getQuotaPerUnit } from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function ModelSettingsVisualEditor(props) {
  const { t } = useTranslation();
  const [models, setModels] = useState([]);
  const [visible, setVisible] = useState(false);
  const [isEditMode, setIsEditMode] = useState(false);
  const [currentModel, setCurrentModel] = useState(null);
  const [searchText, setSearchText] = useState('');
  const [currentPage, setCurrentPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [pricingMode, setPricingMode] = useState('per-token'); // 'per-token' | 'per-request' | 'per-image' | 'per-second'
  const [pricingSubMode, setPricingSubMode] = useState('ratio'); // 'ratio' or 'token-price'
  const [conflictOnly, setConflictOnly] = useState(false);
  const formRef = useRef(null);
  const pageSize = 10;
  const quotaPerUnit = getQuotaPerUnit();

  useEffect(() => {
    try {
      const modelPrice = JSON.parse(props.options.ModelPrice || '{}');
      const modelRatio = JSON.parse(props.options.ModelRatio || '{}');
      const completionRatio = JSON.parse(props.options.CompletionRatio || '{}');
      const audioRatio = JSON.parse(props.options.AudioRatio || '{}');
      const audioCompletionRatio = JSON.parse(props.options.AudioCompletionRatio || '{}');
      const imageRatio = JSON.parse(props.options.ImageRatio || '{}');
      const imageCompletionRatio = JSON.parse(props.options.ImageCompletionRatio || '{}');
      const videoRatio = JSON.parse(props.options.VideoRatio || '{}');
      const videoCompletionRatio = JSON.parse(props.options.VideoCompletionRatio || '{}');
      const cacheRatio = JSON.parse(props.options.CacheRatio || '{}');
      const createCacheRatio = JSON.parse(props.options.CreateCacheRatio || '{}');
      const imageModelPricePerImage = JSON.parse(props.options.ImageModelPricePerImage || '{}');
      const videoModelPricePerSecond = JSON.parse(props.options.VideoModelPricePerSecond || '{}');

      // 合并所有模型名称
      const modelNames = new Set([
        ...Object.keys(modelPrice),
        ...Object.keys(modelRatio),
        ...Object.keys(completionRatio),
        ...Object.keys(audioRatio),
        ...Object.keys(audioCompletionRatio),
        ...Object.keys(imageRatio),
        ...Object.keys(imageCompletionRatio),
        ...Object.keys(videoRatio),
        ...Object.keys(videoCompletionRatio),
        ...Object.keys(cacheRatio),
        ...Object.keys(createCacheRatio),
        ...Object.keys(imageModelPricePerImage),
        ...Object.keys(videoModelPricePerSecond),
      ]);

      const getVal = (obj, key) => (obj[key] === undefined ? '' : obj[key]);

      // 四种计费类型互斥检测
      const ratioFields = ['ratio', 'completionRatio', 'audioRatio', 'audioCompletionRatio', 'imageRatio', 'imageCompletionRatio', 'videoRatio', 'videoCompletionRatio', 'cacheRatio', 'createCacheRatio'];
      const detectConflict = (m) => {
        const hasPerToken = ratioFields.some((f) => m[f] !== '');
        const hasPerRequest = m.price !== '';
        const hasPerImage = m.imageModelPricePerImage !== '';
        const hasPerSecond = m.videoModelPricePerSecond !== '';
        const count = [hasPerToken, hasPerRequest, hasPerImage, hasPerSecond].filter(Boolean).length;
        return count > 1;
      };

      const modelData = Array.from(modelNames).map((name) => {
        const price = getVal(modelPrice, name);
        const ratio = getVal(modelRatio, name);
        const comp = getVal(completionRatio, name);

        return {
          name,
          price,
          ratio,
          completionRatio: comp,
          audioRatio: getVal(audioRatio, name),
          audioCompletionRatio: getVal(audioCompletionRatio, name),
          imageRatio: getVal(imageRatio, name),
          imageCompletionRatio: getVal(imageCompletionRatio, name),
          videoRatio: getVal(videoRatio, name),
          videoCompletionRatio: getVal(videoCompletionRatio, name),
          cacheRatio: getVal(cacheRatio, name),
          createCacheRatio: getVal(createCacheRatio, name),
          imageModelPricePerImage: getVal(imageModelPricePerImage, name),
          videoModelPricePerSecond: getVal(videoModelPricePerSecond, name),
        };
        m.hasConflict = detectConflict(m);
        return m;
      });

      setModels(modelData);
    } catch (error) {
      console.error('JSON解析错误:', error);
    }
  }, [props.options]);

  // 首先声明分页相关的工具函数
  const getPagedData = (data, currentPage, pageSize) => {
    const start = (currentPage - 1) * pageSize;
    const end = start + pageSize;
    return data.slice(start, end);
  };

  // 在 return 语句之前，先处理过滤和分页逻辑
  const filteredModels = models.filter((model) => {
    const keywordMatch = searchText ? model.name.includes(searchText) : true;
    const conflictMatch = conflictOnly ? model.hasConflict : true;
    return keywordMatch && conflictMatch;
  });

  // 然后基于过滤后的数据计算分页数据
  const pagedData = getPagedData(filteredModels, currentPage, pageSize);

  const SubmitData = async () => {
    setLoading(true);
    const output = {
      ModelPrice: {},
      ModelRatio: {},
      CompletionRatio: {},
      AudioRatio: {},
      AudioCompletionRatio: {},
      ImageRatio: {},
      ImageCompletionRatio: {},
      VideoRatio: {},
      VideoCompletionRatio: {},
      CacheRatio: {},
      CreateCacheRatio: {},
      ImageModelPricePerImage: {},
      VideoModelPricePerSecond: {},
    };

    try {
      // 数据转换
      const setIfNotEmpty = (obj, name, val) => {
        if (val !== '') obj[name] = parseFloat(val);
      };

      models.forEach((model) => {
        // 四种计费类型互斥：按次 > 按张 > 按秒 > 按量
        if (model.price !== '') {
          output.ModelPrice[model.name] = parseFloat(model.price);
        } else if (model.imageModelPricePerImage !== '') {
          output.ImageModelPricePerImage[model.name] = parseFloat(model.imageModelPricePerImage);
        } else if (model.videoModelPricePerSecond !== '') {
          output.VideoModelPricePerSecond[model.name] = parseFloat(model.videoModelPricePerSecond);
        } else {
          // 按量计费：所有倍率字段
          setIfNotEmpty(output.ModelRatio, model.name, model.ratio);
          setIfNotEmpty(output.CompletionRatio, model.name, model.completionRatio);
          setIfNotEmpty(output.AudioRatio, model.name, model.audioRatio);
          setIfNotEmpty(output.AudioCompletionRatio, model.name, model.audioCompletionRatio);
          setIfNotEmpty(output.ImageRatio, model.name, model.imageRatio);
          setIfNotEmpty(output.ImageCompletionRatio, model.name, model.imageCompletionRatio);
          setIfNotEmpty(output.VideoRatio, model.name, model.videoRatio);
          setIfNotEmpty(output.VideoCompletionRatio, model.name, model.videoCompletionRatio);
          setIfNotEmpty(output.CacheRatio, model.name, model.cacheRatio);
          setIfNotEmpty(output.CreateCacheRatio, model.name, model.createCacheRatio);
        }
      });

      // 准备API请求数组
      const finalOutput = {};
      Object.entries(output).forEach(([key, val]) => {
        finalOutput[key] = JSON.stringify(val, null, 2);
      });

      const requestQueue = Object.entries(finalOutput).map(([key, value]) => {
        return API.put('/api/option/', {
          key,
          value,
        });
      });

      // 批量处理请求
      const results = await Promise.all(requestQueue);

      // 验证结果
      if (requestQueue.length === 1) {
        if (results.includes(undefined)) return;
      } else if (requestQueue.length > 1) {
        if (results.includes(undefined)) {
          return showError('部分保存失败，请重试');
        }
      }

      // 检查每个请求的结果
      for (const res of results) {
        if (!res.data.success) {
          return showError(res.data.message);
        }
      }

      showSuccess('保存成功');
      props.refresh();
    } catch (error) {
      console.error('保存失败:', error);
      showError('保存失败，请重试');
    } finally {
      setLoading(false);
    }
  };

  const columns = [
    {
      title: t('模型名称'),
      dataIndex: 'name',
      key: 'name',
      fixed: 'left',
      width: 200,
      render: (text, record) => (
        <span>
          {text}
          {record.hasConflict && (
            <Tag color='red' shape='circle' className='ml-2'>
              {t('矛盾')}
            </Tag>
          )}
        </span>
      ),
    },
    {
      title: t('模型固定价格'),
      dataIndex: 'price',
      key: 'price',
      width: 140,
      render: (text, record) => (
        <Input
          value={text}
          placeholder={t('按量计费')}
          onChange={(value) => updateModel(record.name, 'price', value)}
        />
      ),
    },
    {
      title: t('模型倍率'),
      dataIndex: 'ratio',
      key: 'ratio',
      width: 130,
      render: (text, record) => (
        <Input
          value={text}
          placeholder={record.price !== '' ? t('模型倍率') : t('默认补全倍率')}
          disabled={record.price !== ''}
          onChange={(value) => updateModel(record.name, 'ratio', value)}
        />
      ),
    },
    {
      title: t('补全倍率'),
      dataIndex: 'completionRatio',
      key: 'completionRatio',
      width: 130,
      render: (text, record) => (
        <Input
          value={text}
          placeholder={record.price !== '' ? t('补全倍率') : t('默认补全倍率')}
          disabled={record.price !== ''}
          onChange={(value) =>
            updateModel(record.name, 'completionRatio', value)
          }
        />
      ),
    },
    {
      title: t('音频倍率'),
      dataIndex: 'audioRatio',
      key: 'audioRatio',
      width: 130,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'audioRatio', value)}
        />
      ),
    },
    {
      title: t('音频补全倍率'),
      dataIndex: 'audioCompletionRatio',
      key: 'audioCompletionRatio',
      width: 140,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'audioCompletionRatio', value)}
        />
      ),
    },
    {
      title: t('图片倍率'),
      dataIndex: 'imageRatio',
      key: 'imageRatio',
      width: 130,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'imageRatio', value)}
        />
      ),
    },
    {
      title: t('图片补全倍率'),
      dataIndex: 'imageCompletionRatio',
      key: 'imageCompletionRatio',
      width: 140,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'imageCompletionRatio', value)}
        />
      ),
    },
    {
      title: t('视频倍率'),
      dataIndex: 'videoRatio',
      key: 'videoRatio',
      width: 130,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'videoRatio', value)}
        />
      ),
    },
    {
      title: t('视频补全倍率'),
      dataIndex: 'videoCompletionRatio',
      key: 'videoCompletionRatio',
      width: 140,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'videoCompletionRatio', value)}
        />
      ),
    },
    {
      title: t('缓存倍率'),
      dataIndex: 'cacheRatio',
      key: 'cacheRatio',
      width: 130,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'cacheRatio', value)}
        />
      ),
    },
    {
      title: t('缓存创建倍率'),
      dataIndex: 'createCacheRatio',
      key: 'createCacheRatio',
      width: 140,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'createCacheRatio', value)}
        />
      ),
    },
    {
      title: t('按张计费价格'),
      dataIndex: 'imageModelPricePerImage',
      key: 'imageModelPricePerImage',
      width: 140,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'imageModelPricePerImage', value)}
        />
      ),
    },
    {
      title: t('按秒计费价格'),
      dataIndex: 'videoModelPricePerSecond',
      key: 'videoModelPricePerSecond',
      width: 140,
      render: (text, record) => (
        <Input
          value={text}
          placeholder='-'
          onChange={(value) => updateModel(record.name, 'videoModelPricePerSecond', value)}
        />
      ),
    },
    {
      title: t('操作'),
      key: 'action',
      fixed: 'right',
      width: 120,
      render: (_, record) => (
        <Space>
          <Button
            type='primary'
            icon={<IconEdit />}
            onClick={() => editModel(record)}
          ></Button>
          <Button
            icon={<IconDelete />}
            type='danger'
            onClick={() => deleteModel(record.name)}
          />
        </Space>
      ),
    },
  ];

  // 四种计费类型互斥检测（组件级别复用）
  const ratioFieldNames = ['ratio', 'completionRatio', 'audioRatio', 'audioCompletionRatio', 'imageRatio', 'imageCompletionRatio', 'videoRatio', 'videoCompletionRatio', 'cacheRatio', 'createCacheRatio'];
  const checkConflict = (m) => {
    const hasPerToken = ratioFieldNames.some((f) => m[f] !== '');
    const hasPerRequest = m.price !== '';
    const hasPerImage = m.imageModelPricePerImage !== '';
    const hasPerSecond = m.videoModelPricePerSecond !== '';
    return [hasPerToken, hasPerRequest, hasPerImage, hasPerSecond].filter(Boolean).length > 1;
  };

  const updateModel = (name, field, value) => {
    if (isNaN(value)) {
      showError('请输入数字');
      return;
    }
    setModels((prev) =>
      prev.map((model) => {
        if (model.name !== name) return model;
        const updated = { ...model, [field]: value };
        updated.hasConflict = checkConflict(updated);
        return updated;
      }),
    );
  };

  const deleteModel = (name) => {
    setModels((prev) => prev.filter((model) => model.name !== name));
  };

  const calculateRatioFromTokenPrice = (tokenPrice) => {
    return tokenPrice / 2;
  };

  const calculateCompletionRatioFromPrices = (
    modelTokenPrice,
    completionTokenPrice,
  ) => {
    if (!modelTokenPrice || modelTokenPrice === '0') {
      showError('模型价格不能为0');
      return '';
    }
    return completionTokenPrice / modelTokenPrice;
  };

  const handleTokenPriceChange = (value) => {
    // Use a temporary variable to hold the new state
    let newState = {
      ...(currentModel || {}),
      tokenPrice: value,
      ratio: 0,
    };

    if (!isNaN(value) && value !== '') {
      const tokenPrice = parseFloat(value);
      const ratio = calculateRatioFromTokenPrice(tokenPrice);
      newState.ratio = ratio;
    }

    // Set the state with the complete updated object
    setCurrentModel(newState);
  };

  const handleCompletionTokenPriceChange = (value) => {
    // Use a temporary variable to hold the new state
    let newState = {
      ...(currentModel || {}),
      completionTokenPrice: value,
      completionRatio: 0,
    };

    if (!isNaN(value) && value !== '' && currentModel?.tokenPrice) {
      const completionTokenPrice = parseFloat(value);
      const modelTokenPrice = parseFloat(currentModel.tokenPrice);

      if (modelTokenPrice > 0) {
        const completionRatio = calculateCompletionRatioFromPrices(
          modelTokenPrice,
          completionTokenPrice,
        );
        newState.completionRatio = completionRatio;
      }
    }

    // Set the state with the complete updated object
    setCurrentModel(newState);
  };

  const extraFields = [
    'audioRatio', 'audioCompletionRatio', 'imageRatio', 'imageCompletionRatio',
    'videoRatio', 'videoCompletionRatio', 'cacheRatio', 'createCacheRatio',
    'imageModelPricePerImage', 'videoModelPricePerSecond',
  ];

  const addOrUpdateModel = (values) => {
    // Check if we're editing an existing model or adding a new one
    const existingModelIndex = models.findIndex(
      (model) => model.name === values.name,
    );

    const buildModel = (base) => {
      const updated = {
        name: values.name,
        price: values.price || '',
        ratio: values.ratio || '',
        completionRatio: values.completionRatio || '',
      };
      extraFields.forEach((f) => {
        updated[f] = values[f] || '';
      });
      updated.hasConflict = checkConflict(updated);
      return updated;
    };

    if (existingModelIndex >= 0) {
      // Update existing model
      setModels((prev) =>
        prev.map((model, index) => {
          if (index !== existingModelIndex) return model;
          return buildModel(model);
        }),
      );
      setVisible(false);
      showSuccess(t('更新成功'));
    } else {
      // Add new model
      // Check if model name already exists
      if (models.some((model) => model.name === values.name)) {
        showError(t('模型名称已存在'));
        return;
      }

      setModels((prev) => {
        const newModel = buildModel({});
        return [newModel, ...prev];
      });
      setVisible(false);
      showSuccess(t('添加成功'));
    }
  };

  const calculateTokenPriceFromRatio = (ratio) => {
    return ratio * 2;
  };

  const resetModalState = () => {
    setCurrentModel(null);
    setPricingMode('per-token');
    setPricingSubMode('ratio');
    setIsEditMode(false);
  };

  const editModel = (record) => {
    setIsEditMode(true);
    // Determine which pricing mode to use based on the model's current configuration
    let initialPricingMode = 'per-token';
    let initialPricingSubMode = 'ratio';

    if (record.price !== '') {
      initialPricingMode = 'per-request';
    } else if (record.imageModelPricePerImage !== '') {
      initialPricingMode = 'per-image';
    } else if (record.videoModelPricePerSecond !== '') {
      initialPricingMode = 'per-second';
    } else {
      initialPricingMode = 'per-token';
    }

    // Set the pricing modes for the form
    setPricingMode(initialPricingMode);
    setPricingSubMode(initialPricingSubMode);

    // Create a copy of the model data to avoid modifying the original
    const modelCopy = { ...record };

    // If the model has ratio data and we want to populate token price fields
    if (record.ratio) {
      modelCopy.tokenPrice = calculateTokenPriceFromRatio(
        parseFloat(record.ratio),
      ).toString();

      if (record.completionRatio) {
        modelCopy.completionTokenPrice = (
          parseFloat(modelCopy.tokenPrice) * parseFloat(record.completionRatio)
        ).toString();
      }
    }

    // Set the current model
    setCurrentModel(modelCopy);

    // Open the modal
    setVisible(true);

    // Use setTimeout to ensure the form is rendered before setting values
    setTimeout(() => {
      if (formRef.current) {
        // Update the form fields based on pricing mode
        const formValues = {
          name: modelCopy.name,
        };

        if (initialPricingMode === 'per-request') {
          formValues.priceInput = modelCopy.price;
        } else if (initialPricingMode === 'per-token') {
          formValues.ratioInput = modelCopy.ratio;
          formValues.completionRatioInput = modelCopy.completionRatio;
          formValues.modelTokenPrice = modelCopy.tokenPrice;
          formValues.completionTokenPrice = modelCopy.completionTokenPrice;
        }

        // 额外倍率字段
        formValues.audioRatioInput = modelCopy.audioRatio || '';
        formValues.audioCompletionRatioInput = modelCopy.audioCompletionRatio || '';
        formValues.imageRatioInput = modelCopy.imageRatio || '';
        formValues.imageCompletionRatioInput = modelCopy.imageCompletionRatio || '';
        formValues.videoRatioInput = modelCopy.videoRatio || '';
        formValues.videoCompletionRatioInput = modelCopy.videoCompletionRatio || '';
        formValues.cacheRatioInput = modelCopy.cacheRatio || '';
        formValues.createCacheRatioInput = modelCopy.createCacheRatio || '';
        formValues.imageModelPricePerImageInput = modelCopy.imageModelPricePerImage || '';
        formValues.videoModelPricePerSecondInput = modelCopy.videoModelPricePerSecond || '';

        formRef.current.setValues(formValues);
      }
    }, 0);
  };

  return (
    <>
      <Space vertical align='start' style={{ width: '100%' }}>
        <Space className='mt-2'>
          <Button
            icon={<IconPlus />}
            onClick={() => {
              resetModalState();
              setVisible(true);
            }}
          >
            {t('添加模型')}
          </Button>
          <Button type='primary' icon={<IconSave />} onClick={SubmitData}>
            {t('应用更改')}
          </Button>
          <Input
            prefix={<IconSearch />}
            placeholder={t('搜索模型名称')}
            value={searchText}
            onChange={(value) => {
              setSearchText(value);
              setCurrentPage(1);
            }}
            style={{ width: 200 }}
            showClear
          />
          <Checkbox
            checked={conflictOnly}
            onChange={(e) => {
              setConflictOnly(e.target.checked);
              setCurrentPage(1);
            }}
          >
            {t('仅显示矛盾倍率')}
          </Checkbox>
        </Space>
        <Table
          columns={columns}
          dataSource={pagedData}
          scroll={{ x: 2200 }}
          pagination={{
            currentPage: currentPage,
            pageSize: pageSize,
            total: filteredModels.length,
            onPageChange: (page) => setCurrentPage(page),
            showTotal: true,
            showSizeChanger: false,
          }}
        />
      </Space>

      <Modal
        title={isEditMode ? t('编辑模型') : t('添加模型')}
        visible={visible}
        onCancel={() => {
          resetModalState();
          setVisible(false);
        }}
        onOk={() => {
          if (currentModel) {
            const valuesToSave = { ...currentModel };

            if (
              pricingMode === 'per-token' &&
              pricingSubMode === 'token-price' &&
              currentModel.tokenPrice
            ) {
              const tokenPrice = parseFloat(currentModel.tokenPrice);
              valuesToSave.ratio = (tokenPrice / 2).toString();
              if (
                currentModel.completionTokenPrice &&
                currentModel.tokenPrice
              ) {
                const completionPrice = parseFloat(
                  currentModel.completionTokenPrice,
                );
                const modelPrice = parseFloat(currentModel.tokenPrice);
                if (modelPrice > 0) {
                  valuesToSave.completionRatio = (
                    completionPrice / modelPrice
                  ).toString();
                }
              }
            }

            // 四种计费类型互斥：清除非当前类型的字段
            const perTokenFields = ['ratio', 'completionRatio', 'audioRatio', 'audioCompletionRatio', 'imageRatio', 'imageCompletionRatio', 'videoRatio', 'videoCompletionRatio', 'cacheRatio', 'createCacheRatio'];
            if (pricingMode !== 'per-token') {
              perTokenFields.forEach((f) => { valuesToSave[f] = ''; });
            }
            if (pricingMode !== 'per-request') {
              valuesToSave.price = '';
            }
            if (pricingMode !== 'per-image') {
              valuesToSave.imageModelPricePerImage = '';
            }
            if (pricingMode !== 'per-second') {
              valuesToSave.videoModelPricePerSecond = '';
            }

            addOrUpdateModel(valuesToSave);
          }
        }}
      >
        <Form getFormApi={(api) => (formRef.current = api)}>
          <Form.Input
            field='name'
            label={t('模型名称')}
            placeholder='strawberry'
            required
            disabled={isEditMode}
            onChange={(value) =>
              setCurrentModel((prev) => ({ ...prev, name: value }))
            }
          />

          <Form.Section text={t('定价模式')}>
            <div style={{ marginBottom: '16px' }}>
              <RadioGroup
                type='button'
                value={pricingMode}
                onChange={(e) => {
                  const newMode = e.target.value;
                  const oldMode = pricingMode;
                  setPricingMode(newMode);

                  // Instead of resetting all values, convert between modes
                  if (currentModel) {
                    const updatedModel = { ...currentModel };

                    // Update formRef with converted values
                    if (formRef.current) {
                      const formValues = {
                        name: updatedModel.name,
                      };

                      if (newMode === 'per-request') {
                        formValues.priceInput = updatedModel.price || '';
                      } else if (newMode === 'per-token') {
                        formValues.ratioInput = updatedModel.ratio || '';
                        formValues.completionRatioInput =
                          updatedModel.completionRatio || '';
                        formValues.modelTokenPrice =
                          updatedModel.tokenPrice || '';
                        formValues.completionTokenPrice =
                          updatedModel.completionTokenPrice || '';
                      }

                      formRef.current.setValues(formValues);
                    }

                    // Update the model state
                    setCurrentModel(updatedModel);
                  }
                }}
              >
                <Radio value='per-token'>{t('按量计费')}</Radio>
                <Radio value='per-request'>{t('按次计费')}</Radio>
                <Radio value='per-image'>{t('按张计费')}</Radio>
                <Radio value='per-second'>{t('按秒计费')}</Radio>
              </RadioGroup>
            </div>
          </Form.Section>

          {pricingMode === 'per-token' && (
            <>
              <Form.Section text={t('价格设置方式')}>
                <div style={{ marginBottom: '16px' }}>
                  <RadioGroup
                    type='button'
                    value={pricingSubMode}
                    onChange={(e) => {
                      const newSubMode = e.target.value;
                      const oldSubMode = pricingSubMode;
                      setPricingSubMode(newSubMode);

                      // Handle conversion between submodes
                      if (currentModel) {
                        const updatedModel = { ...currentModel };

                        // Convert between ratio and token price
                        if (
                          oldSubMode === 'ratio' &&
                          newSubMode === 'token-price'
                        ) {
                          if (updatedModel.ratio) {
                            updatedModel.tokenPrice =
                              calculateTokenPriceFromRatio(
                                parseFloat(updatedModel.ratio),
                              ).toString();

                            if (updatedModel.completionRatio) {
                              updatedModel.completionTokenPrice = (
                                parseFloat(updatedModel.tokenPrice) *
                                parseFloat(updatedModel.completionRatio)
                              ).toString();
                            }
                          }
                        } else if (
                          oldSubMode === 'token-price' &&
                          newSubMode === 'ratio'
                        ) {
                          // Ratio values should already be calculated by the handlers
                        }

                        // Update the form values
                        if (formRef.current) {
                          const formValues = {};

                          if (newSubMode === 'ratio') {
                            formValues.ratioInput = updatedModel.ratio || '';
                            formValues.completionRatioInput =
                              updatedModel.completionRatio || '';
                          } else if (newSubMode === 'token-price') {
                            formValues.modelTokenPrice =
                              updatedModel.tokenPrice || '';
                            formValues.completionTokenPrice =
                              updatedModel.completionTokenPrice || '';
                          }

                          formRef.current.setValues(formValues);
                        }

                        setCurrentModel(updatedModel);
                      }
                    }}
                  >
                    <Radio value='ratio'>{t('按倍率设置')}</Radio>
                    <Radio value='token-price'>{t('按价格设置')}</Radio>
                  </RadioGroup>
                </div>
              </Form.Section>

              {pricingSubMode === 'ratio' && (
                <>
                  <Form.Input
                    field='ratioInput'
                    label={t('模型倍率')}
                    placeholder={t('输入模型倍率')}
                    onChange={(value) =>
                      setCurrentModel((prev) => ({
                        ...(prev || {}),
                        ratio: value,
                      }))
                    }
                    initValue={currentModel?.ratio || ''}
                  />
                  <Form.Input
                    field='completionRatioInput'
                    label={t('补全倍率')}
                    placeholder={t('输入补全倍率')}
                    onChange={(value) =>
                      setCurrentModel((prev) => ({
                        ...(prev || {}),
                        completionRatio: value,
                      }))
                    }
                    initValue={currentModel?.completionRatio || ''}
                  />
                </>
              )}

              {pricingSubMode === 'token-price' && (
                <>
                  <Form.Input
                    field='modelTokenPrice'
                    label={t('输入价格')}
                    onChange={(value) => {
                      handleTokenPriceChange(value);
                    }}
                    initValue={currentModel?.tokenPrice || ''}
                    suffix={t('$/1M tokens')}
                  />
                  <Form.Input
                    field='completionTokenPrice'
                    label={t('输出价格')}
                    onChange={(value) => {
                      handleCompletionTokenPriceChange(value);
                    }}
                    initValue={currentModel?.completionTokenPrice || ''}
                    suffix={t('$/1M tokens')}
                  />
                </>
              )}

              <Form.Section text={t('音频倍率')}>
                <Form.Input
                  field='audioRatioInput'
                  label={t('音频倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), audioRatio: value }))
                  }
                  initValue={currentModel?.audioRatio || ''}
                />
                <Form.Input
                  field='audioCompletionRatioInput'
                  label={t('音频补全倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), audioCompletionRatio: value }))
                  }
                  initValue={currentModel?.audioCompletionRatio || ''}
                />
              </Form.Section>

              <Form.Section text={t('图片倍率')}>
                <Form.Input
                  field='imageRatioInput'
                  label={t('图片倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), imageRatio: value }))
                  }
                  initValue={currentModel?.imageRatio || ''}
                />
                <Form.Input
                  field='imageCompletionRatioInput'
                  label={t('图片补全倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), imageCompletionRatio: value }))
                  }
                  initValue={currentModel?.imageCompletionRatio || ''}
                />
              </Form.Section>

              <Form.Section text={t('视频倍率')}>
                <Form.Input
                  field='videoRatioInput'
                  label={t('视频倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), videoRatio: value }))
                  }
                  initValue={currentModel?.videoRatio || ''}
                />
                <Form.Input
                  field='videoCompletionRatioInput'
                  label={t('视频补全倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), videoCompletionRatio: value }))
                  }
                  initValue={currentModel?.videoCompletionRatio || ''}
                />
              </Form.Section>

              <Form.Section text={t('缓存倍率')}>
                <Form.Input
                  field='cacheRatioInput'
                  label={t('缓存倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), cacheRatio: value }))
                  }
                  initValue={currentModel?.cacheRatio || ''}
                />
                <Form.Input
                  field='createCacheRatioInput'
                  label={t('缓存创建倍率')}
                  placeholder={t('留空则不设置')}
                  onChange={(value) =>
                    setCurrentModel((prev) => ({ ...(prev || {}), createCacheRatio: value }))
                  }
                  initValue={currentModel?.createCacheRatio || ''}
                />
              </Form.Section>
            </>
          )}

          {pricingMode === 'per-request' && (
            <Form.Input
              field='priceInput'
              label={t('固定价格(每次)')}
              placeholder={t('输入每次价格')}
              onChange={(value) =>
                setCurrentModel((prev) => ({
                  ...(prev || {}),
                  price: value,
                }))
              }
              initValue={currentModel?.price || ''}
            />
          )}

          {pricingMode === 'per-image' && (
            <Form.Input
              field='imageModelPricePerImageInput'
              label={t('按张计费每张价格（美元）')}
              placeholder={t('输入每张价格')}
              onChange={(value) =>
                setCurrentModel((prev) => ({ ...(prev || {}), imageModelPricePerImage: value }))
              }
              initValue={currentModel?.imageModelPricePerImage || ''}
            />
          )}

          {pricingMode === 'per-second' && (
            <Form.Input
              field='videoModelPricePerSecondInput'
              label={t('按秒计费每秒价格（美元）')}
              placeholder={t('输入每秒价格')}
              onChange={(value) =>
                setCurrentModel((prev) => ({ ...(prev || {}), videoModelPricePerSecond: value }))
              }
              initValue={currentModel?.videoModelPricePerSecond || ''}
            />
          )}
        </Form>
      </Modal>
    </>
  );
}
