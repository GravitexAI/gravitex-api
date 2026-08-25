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
import { useEffect, useMemo, useRef, useState } from 'react';
import { API, showError, showSuccess, showWarning } from '../../../../helpers';
import {
  combineBillingExpr,
  splitBillingExprAndRequestRules,
} from '../components/requestRuleExpr';
import { createOperLog } from '../../../../components/oper-log/operLogApi';

export const PAGE_SIZE = 10;
export const PRICE_SUFFIX = '$/1M tokens';
const EMPTY_CANDIDATE_MODEL_NAMES = [];

const EMPTY_MODEL = {
  name: '',
  billingMode: 'per-token',
  fixedPrice: '',
  inputPrice: '',
  completionPrice: '',
  lockedCompletionRatio: '',
  completionRatioLocked: false,
  cachePrice: '',
  createCachePrice: '',
  imagePrice: '',
  imageCompletionPrice: '',
  audioInputPrice: '',
  audioOutputPrice: '',
  videoInputPrice: '',
  videoCompletionPrice: '',
  imagePricePerImage: '',
  videoPricePerSecond: '',
  imagePriceStructure: null,
  videoPriceStructure: null,
  billingExpr: '',
  requestRuleExpr: '',
  rawRatios: {
    modelRatio: '',
    completionRatio: '',
    cacheRatio: '',
    createCacheRatio: '',
    imageRatio: '',
    imageCompletionRatio: '',
    audioRatio: '',
    audioCompletionRatio: '',
    videoRatio: '',
    videoCompletionRatio: '',
  },
  hasConflict: false,
};

const NUMERIC_INPUT_REGEX = /^(\d+(\.\d*)?|\.\d*)?$/;

export const hasValue = (value) =>
  value !== '' && value !== null && value !== undefined && value !== false;

const toNumericString = (value) => {
  if (!hasValue(value) && value !== 0) {
    return '';
  }
  const num = Number(value);
  return Number.isFinite(num) ? String(num) : '';
};

const toNumberOrNull = (value) => {
  if (!hasValue(value) && value !== 0) {
    return null;
  }
  const num = Number(value);
  return Number.isFinite(num) ? num : null;
};

const formatNumber = (value) => {
  const num = toNumberOrNull(value);
  if (num === null) {
    return '';
  }
  return parseFloat(num.toFixed(12)).toString();
};

const toNormalizedNumber = (value) => {
  const formatted = formatNumber(value);
  return formatted === '' ? null : Number(formatted);
};

// 解析按档计费的存储值（数字字符串或 JSON 字符串）
const parsePricingStructure = (value) => {
  const rawString = toNumericString(value);
  if (rawString !== '') {
    return {
      kind: 'simple',
      price: rawString,
      rawString,
    };
  }
  if (value === undefined || value === null || value === '') {
    return null;
  }
  let parsed;
  try {
    parsed = typeof value === 'string' ? JSON.parse(value) : value;
  } catch (e) {
    return null;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null;
  }
  // veo 类型：{noAudio:{...}, audio:{...}}
  // kling 类型：{noAudio: number, audio: number}
  // wan/happyhorse 类型：{resolutions:{480p:..., 720p:..., 1080p:...}}
  if (
    (parsed.noAudio !== undefined || parsed.audio !== undefined) &&
    !parsed.resolutions
  ) {
    // kling 风格：noAudio / audio 直接是数字，没有分辨率维度。
    // 用一个特殊的 'default' 键承载这一行，UI 渲染时第一列展示为「默认」，
    // 序列化时识别出只有 default 一个键时还原为平铺 JSON。
    if (typeof parsed.noAudio === 'number' || typeof parsed.audio === 'number') {
      return {
        kind: 'audio-resolution',
        noAudio:
          typeof parsed.noAudio === 'number'
            ? { default: String(parsed.noAudio) }
            : {},
        audio:
          typeof parsed.audio === 'number'
            ? { default: String(parsed.audio) }
            : {},
        rawString: JSON.stringify(parsed),
      };
    }
    const noAudio = parseCategoryMap(parsed.noAudio);
    const audio = parseCategoryMap(parsed.audio);
    return {
      kind: 'audio-resolution',
      noAudio,
      audio,
      rawString: JSON.stringify(parsed),
    };
  }
  if (parsed.resolutions && typeof parsed.resolutions === 'object') {
    const resolutions = parseCategoryMap(parsed.resolutions);
    return {
      kind: 'resolutions',
      resolutions,
      rawString: JSON.stringify(parsed),
    };
  }
  const custom = parseCategoryMap(parsed);
  return {
    kind: 'custom',
    entries: custom,
    rawString: JSON.stringify(parsed),
  };
};

const parseCategoryMap = (raw) => {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
  const result = {};
  Object.entries(raw).forEach(([key, val]) => {
    if (val && typeof val === 'object' && !Array.isArray(val)) {
      // 嵌套对象：递归解析后用 __nested 标记，子键值仍然递归。
      const nested = parseCategoryMap(val);
      if (Object.keys(nested).length > 0) {
        result[key] = { __nested: true, entries: nested };
      }
      return;
    }
    const num = Number(val);
    if (Number.isFinite(num)) {
      result[key] = String(num);
    }
  });
  return result;
};

const stringifyPricingStructure = (structure) => {
  if (!structure) return '';
  if (structure.kind === 'simple') {
    return structure.price || '';
  }
  if (structure.kind === 'audio-resolution') {
    const noAudio = stringifyEntries(structure.noAudio);
    const audio = stringifyEntries(structure.audio);
    // kling 平铺风格：仅含 default 一行，写回 {noAudio: number, audio: number}。
    if (
      Object.keys(noAudio).length === 1 &&
      Object.keys(noAudio)[0] === 'default' &&
      Object.keys(audio).length <= 1 &&
      (Object.keys(audio).length === 0 ||
        Object.keys(audio)[0] === 'default')
    ) {
      const payload = {};
      if (noAudio.default !== undefined)
        payload.noAudio = noAudio.default;
      if (audio.default !== undefined) payload.audio = audio.default;
      return Object.keys(payload).length === 0
        ? ''
        : JSON.stringify(payload);
    }
    const payload = {};
    if (Object.keys(noAudio).length > 0) payload.noAudio = noAudio;
    if (Object.keys(audio).length > 0) payload.audio = audio;
    return Object.keys(payload).length === 0
      ? ''
      : JSON.stringify(payload);
  }
  if (structure.kind === 'resolutions') {
    const resolutions = stringifyEntries(structure.resolutions);
    return Object.keys(resolutions).length === 0
      ? ''
      : JSON.stringify({ resolutions });
  }
  if (structure.kind === 'custom') {
    const entries = stringifyEntries(structure.entries);
    return Object.keys(entries).length === 0
      ? ''
      : JSON.stringify(entries);
  }
  return '';
};

const stringifyEntries = (entries) => {
  if (!entries || typeof entries !== 'object') return {};
  const result = {};
  Object.entries(entries).forEach(([key, val]) => {
    if (val && typeof val === 'object' && val.__nested) {
      const nested = stringifyEntries(val.entries);
      if (Object.keys(nested).length > 0) result[key] = nested;
      return;
    }
    const num = Number(val);
    if (Number.isFinite(num)) {
      result[key] = parseFloat(num.toFixed(12));
    }
  });
  return result;
};

const parseOptionJSON = (rawValue) => {
  if (rawValue && typeof rawValue === 'object' && !Array.isArray(rawValue)) {
    return rawValue;
  }
  if (rawValue === null || rawValue === undefined || rawValue === '') {
    return {};
  }
  try {
    const parsed = JSON.parse(String(rawValue));
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch (error) {
    console.error('JSON解析错误:', error);
    return {};
  }
};

const ratioToBasePrice = (ratio) => {
  const num = toNumberOrNull(ratio);
  if (num === null) return '';
  return formatNumber(num * 2);
};

const normalizeCompletionRatioMeta = (rawMeta) => {
  if (!rawMeta || typeof rawMeta !== 'object' || Array.isArray(rawMeta)) {
    return {
      locked: false,
      ratio: '',
    };
  }

  return {
    locked: Boolean(rawMeta.locked),
    ratio: toNumericString(rawMeta.ratio),
  };
};

const buildModelState = (name, sourceMaps) => {
  const billingMode = sourceMaps.ModelBillingMode?.[name];
  if (billingMode === 'tiered_expr') {
    const fullBillingExpr = sourceMaps.ModelBillingExpr?.[name] || '';
    const { billingExpr, requestRuleExpr } =
      splitBillingExprAndRequestRules(fullBillingExpr);
    return {
      ...EMPTY_MODEL,
      name,
      billingMode: 'tiered_expr',
      billingExpr,
      requestRuleExpr,
      rawRatios: { ...EMPTY_MODEL.rawRatios },
      hasConflict: false,
    };
  }

  const modelRatio = toNumericString(sourceMaps.ModelRatio[name]);
  const completionRatio = toNumericString(sourceMaps.CompletionRatio[name]);
  const completionRatioMeta = normalizeCompletionRatioMeta(
    sourceMaps.CompletionRatioMeta?.[name],
  );
  const cacheRatio = toNumericString(sourceMaps.CacheRatio[name]);
  const createCacheRatio = toNumericString(sourceMaps.CreateCacheRatio[name]);
  const imageRatio = toNumericString(sourceMaps.ImageRatio[name]);
  const imageCompletionRatio = toNumericString(
    sourceMaps.ImageCompletionRatio[name],
  );
  const audioRatio = toNumericString(sourceMaps.AudioRatio[name]);
  const audioCompletionRatio = toNumericString(
    sourceMaps.AudioCompletionRatio[name],
  );
  const videoRatio = toNumericString(sourceMaps.VideoRatio[name]);
  const videoCompletionRatio = toNumericString(
    sourceMaps.VideoCompletionRatio[name],
  );
  const fixedPrice = toNumericString(sourceMaps.ModelPrice[name]);
  const imageRaw = sourceMaps.ImageModelPricePerImage?.[name];
  const imagePriceStructure = parsePricingStructure(imageRaw);
  const imagePrice = imagePriceStructure?.kind === 'simple'
    ? imagePriceStructure.price
    : '';
  const videoRaw = sourceMaps.VideoModelPricePerSecond?.[name];
  const videoPriceStructure = parsePricingStructure(videoRaw);
  const videoPrice = videoPriceStructure?.kind === 'simple'
    ? videoPriceStructure.price
    : '';
  const inputPrice = ratioToBasePrice(modelRatio);
  const inputPriceNumber = toNumberOrNull(inputPrice);
  const audioInputPrice =
    inputPriceNumber !== null && hasValue(audioRatio)
      ? formatNumber(inputPriceNumber * Number(audioRatio))
      : '';
  const videoInputPrice =
    inputPriceNumber !== null && hasValue(videoRatio)
      ? formatNumber(inputPriceNumber * Number(videoRatio))
      : '';
  const imageCompletionPrice =
    inputPriceNumber !== null && hasValue(imageCompletionRatio)
      ? formatNumber(inputPriceNumber * Number(imageCompletionRatio))
      : '';
  const videoCompletionPrice =
    inputPriceNumber !== null && hasValue(videoCompletionRatio)
      ? formatNumber(inputPriceNumber * Number(videoCompletionRatio))
      : '';

  let resolvedBillingMode = 'per-token';
  if (hasValue(imagePrice) || imagePriceStructure) {
    resolvedBillingMode = 'per-image';
  } else if (hasValue(videoPrice) || videoPriceStructure) {
    resolvedBillingMode = 'per-second';
  } else if (hasValue(fixedPrice)) resolvedBillingMode = 'per-request';

  return {
    ...EMPTY_MODEL,
    name,
    billingMode: resolvedBillingMode,
    fixedPrice,
    inputPrice,
    completionRatioLocked: completionRatioMeta.locked,
    lockedCompletionRatio: completionRatioMeta.ratio,
    completionPrice:
      inputPriceNumber !== null &&
      hasValue(
        completionRatioMeta.locked
          ? completionRatioMeta.ratio
          : completionRatio,
      )
        ? formatNumber(
            inputPriceNumber *
              Number(
                completionRatioMeta.locked
                  ? completionRatioMeta.ratio
                  : completionRatio,
              ),
          )
        : '',
    cachePrice:
      inputPriceNumber !== null && hasValue(cacheRatio)
        ? formatNumber(inputPriceNumber * Number(cacheRatio))
        : '',
    createCachePrice:
      inputPriceNumber !== null && hasValue(createCacheRatio)
        ? formatNumber(inputPriceNumber * Number(createCacheRatio))
        : '',
    imagePrice:
      inputPriceNumber !== null && hasValue(imageRatio)
        ? formatNumber(inputPriceNumber * Number(imageRatio))
        : '',
    imageCompletionPrice,
    audioInputPrice,
    videoInputPrice,
    videoCompletionPrice,
    audioOutputPrice:
      toNumberOrNull(audioInputPrice) !== null && hasValue(audioCompletionRatio)
        ? formatNumber(Number(audioInputPrice) * Number(audioCompletionRatio))
        : '',
    imagePricePerImage: imagePrice,
    videoPricePerSecond: videoPrice,
    imagePriceStructure,
    videoPriceStructure,
    requestRuleExpr: '',
    rawRatios: {
      modelRatio,
      completionRatio,
      cacheRatio,
      createCacheRatio,
      imageRatio,
      imageCompletionRatio,
      audioRatio,
      audioCompletionRatio,
      videoRatio,
      videoCompletionRatio,
    },
    hasConflict:
      hasValue(fixedPrice) &&
      [
        modelRatio,
        completionRatio,
        cacheRatio,
        createCacheRatio,
        imageRatio,
        imageCompletionRatio,
        audioRatio,
        audioCompletionRatio,
        videoRatio,
        videoCompletionRatio,
      ].some(hasValue),
  };
};

export const isBasePricingUnset = (model) => {
  if (model.billingMode === 'tiered_expr') return false;
  switch (model.billingMode) {
    case 'per-image':
      return !hasValue(model.imagePricePerImage) && !model.imagePriceStructure;
    case 'per-second':
      return !hasValue(model.videoPricePerSecond) && !model.videoPriceStructure;
    case 'per-request':
      return !hasValue(model.fixedPrice);
    case 'per-token':
    default:
      return !hasValue(model.inputPrice);
  }
};

export const getModelWarnings = (model, t) => {
  if (!model) {
    return [];
  }
  if (model.billingMode === 'tiered_expr') {
    return [];
  }
  const warnings = [];
  const hasDerivedPricing = [
    model.inputPrice,
    model.completionPrice,
    model.cachePrice,
    model.createCachePrice,
    model.imagePrice,
    model.imageCompletionPrice,
    model.audioInputPrice,
    model.audioOutputPrice,
    model.videoInputPrice,
    model.videoCompletionPrice,
  ].some(hasValue);

  if (model.hasConflict) {
    warnings.push(
      t('当前模型同时存在按次价格和倍率配置，保存时会按当前计费方式覆盖。'),
    );
  }

  if (
    !hasValue(model.inputPrice) &&
    [
      model.rawRatios.completionRatio,
      model.rawRatios.cacheRatio,
      model.rawRatios.createCacheRatio,
      model.rawRatios.imageRatio,
      model.rawRatios.imageCompletionRatio,
      model.rawRatios.audioRatio,
      model.rawRatios.audioCompletionRatio,
      model.rawRatios.videoRatio,
      model.rawRatios.videoCompletionRatio,
    ].some(hasValue)
  ) {
    warnings.push(
      t(
        '当前模型存在未显式设置输入倍率的扩展倍率；填写输入价格后会自动换算为价格字段。',
      ),
    );
  }

  if (
    model.billingMode === 'per-token' &&
    hasDerivedPricing &&
    !hasValue(model.inputPrice)
  ) {
    warnings.push(t('按量计费下需要先填写输入价格，才能保存其它价格项。'));
  }

  if (
    model.billingMode === 'per-token' &&
    hasValue(model.audioOutputPrice) &&
    !hasValue(model.audioInputPrice)
  ) {
    warnings.push(t('填写音频补全价格前，需要先填写音频输入价格。'));
  }

  return warnings;
};

export const buildSummaryText = (model, t) => {
  const requestRuleSuffix =
    model.billingMode === 'tiered_expr' && model.requestRuleExpr
      ? `，${t('请求规则')}`
      : '';
  if (model.billingMode === 'tiered_expr') {
    const expr = model.billingExpr;
    if (!expr) return `${t('表达式计费')}${requestRuleSuffix}`;
    const tierCount = (expr.match(/tier\(/g) || []).length;
    if (tierCount === 0) {
      return `${t('表达式计费')}${requestRuleSuffix}`;
    }
    return `${t('阶梯计费')} (${tierCount} ${t('档')})${requestRuleSuffix}`;
  }

  if (model.billingMode === 'per-request' && hasValue(model.fixedPrice)) {
    return `${t('按次')} $${model.fixedPrice} / ${t('次')}${requestRuleSuffix}`;
  }

  if (model.billingMode === 'per-image') {
    if (hasValue(model.imagePricePerImage)) {
      return `${t('按张')} $${model.imagePricePerImage} / ${t('张')}${requestRuleSuffix}`;
    }
    if (model.imagePriceStructure) {
      return `${t('按张')}（${t('多档')}）${requestRuleSuffix}`;
    }
  }

  if (model.billingMode === 'per-second') {
    if (hasValue(model.videoPricePerSecond)) {
      return `${t('按秒')} $${model.videoPricePerSecond} / ${t('秒')}${requestRuleSuffix}`;
    }
    if (model.videoPriceStructure) {
      return `${t('按秒')}（${t('多档')}）${requestRuleSuffix}`;
    }
  }

  if (hasValue(model.inputPrice)) {
    const extraCount = [
      model.completionPrice,
      model.cachePrice,
      model.createCachePrice,
      model.imagePrice,
      model.audioInputPrice,
      model.audioOutputPrice,
      model.videoInputPrice,
    ].filter(hasValue).length;
    const extraLabel =
      extraCount > 0 ? `，${t('额外价格项')} ${extraCount}` : '';
    return `${t('输入')} $${model.inputPrice}${extraLabel}${requestRuleSuffix}`;
  }

  return `${t('未设置价格')}${requestRuleSuffix}`;
};

export const buildOptionalFieldToggles = (model) => ({
  completionPrice:
    model.completionRatioLocked || hasValue(model.completionPrice),
  cachePrice: hasValue(model.cachePrice),
  createCachePrice: hasValue(model.createCachePrice),
  imagePrice: hasValue(model.imagePrice),
  imageCompletionPrice: hasValue(model.imageCompletionPrice),
  audioInputPrice: hasValue(model.audioInputPrice),
  audioOutputPrice: hasValue(model.audioOutputPrice),
  videoInputPrice: hasValue(model.videoInputPrice),
  videoCompletionPrice: hasValue(model.videoCompletionPrice),
});

const serializeModel = (model, t) => {
  const result = {
    ModelPrice: null,
    ModelRatio: null,
    CompletionRatio: null,
    CacheRatio: null,
    CreateCacheRatio: null,
    ImageRatio: null,
    ImageCompletionRatio: null,
    AudioRatio: null,
    AudioCompletionRatio: null,
    VideoRatio: null,
    VideoCompletionRatio: null,
    ImageModelPricePerImage: null,
    VideoModelPricePerSecond: null,
  };

  if (model.billingMode === 'per-request') {
    if (hasValue(model.fixedPrice)) {
      result.ModelPrice = toNormalizedNumber(model.fixedPrice);
    }
    return result;
  }

  if (model.billingMode === 'per-image') {
    if (model.imagePriceStructure && model.imagePriceStructure.kind !== 'simple') {
      const encoded = stringifyPricingStructure(model.imagePriceStructure);
      if (encoded) {
        result.ImageModelPricePerImage = encoded;
      }
    } else if (hasValue(model.imagePricePerImage)) {
      result.ImageModelPricePerImage = toNormalizedNumber(
        model.imagePricePerImage,
      );
    }
    return result;
  }

  if (model.billingMode === 'per-second') {
    if (model.videoPriceStructure && model.videoPriceStructure.kind !== 'simple') {
      const encoded = stringifyPricingStructure(model.videoPriceStructure);
      if (encoded) {
        result.VideoModelPricePerSecond = encoded;
      }
    } else if (hasValue(model.videoPricePerSecond)) {
      result.VideoModelPricePerSecond = toNormalizedNumber(
        model.videoPricePerSecond,
      );
    }
    return result;
  }

  const inputPrice = toNumberOrNull(model.inputPrice);
  const completionPrice = toNumberOrNull(model.completionPrice);
  const cachePrice = toNumberOrNull(model.cachePrice);
  const createCachePrice = toNumberOrNull(model.createCachePrice);
  const imagePrice = toNumberOrNull(model.imagePrice);
  const imageCompletionPrice = toNumberOrNull(model.imageCompletionPrice);
  const audioInputPrice = toNumberOrNull(model.audioInputPrice);
  const audioOutputPrice = toNumberOrNull(model.audioOutputPrice);
  const videoInputPrice = toNumberOrNull(model.videoInputPrice);
  const videoCompletionPrice = toNumberOrNull(model.videoCompletionPrice);

  const hasDependentPrice = [
    completionPrice,
    cachePrice,
    createCachePrice,
    imagePrice,
    imageCompletionPrice,
    audioInputPrice,
    audioOutputPrice,
    videoInputPrice,
    videoCompletionPrice,
  ].some((value) => value !== null);

  if (inputPrice === null) {
    if (hasDependentPrice) {
      throw new Error(
        t(
          '模型 {{name}} 缺少输入价格，无法计算补全/缓存/图片/音频价格对应的倍率',
          {
            name: model.name,
          },
        ),
      );
    }

    if (hasValue(model.rawRatios.modelRatio)) {
      result.ModelRatio = toNormalizedNumber(model.rawRatios.modelRatio);
    }
    if (hasValue(model.rawRatios.completionRatio)) {
      result.CompletionRatio = toNormalizedNumber(
        model.rawRatios.completionRatio,
      );
    }
    if (hasValue(model.rawRatios.cacheRatio)) {
      result.CacheRatio = toNormalizedNumber(model.rawRatios.cacheRatio);
    }
    if (hasValue(model.rawRatios.createCacheRatio)) {
      result.CreateCacheRatio = toNormalizedNumber(
        model.rawRatios.createCacheRatio,
      );
    }
    if (hasValue(model.rawRatios.imageRatio)) {
      result.ImageRatio = toNormalizedNumber(model.rawRatios.imageRatio);
    }
    if (hasValue(model.rawRatios.imageCompletionRatio)) {
      result.ImageCompletionRatio = toNormalizedNumber(
        model.rawRatios.imageCompletionRatio,
      );
    }
    if (hasValue(model.rawRatios.audioRatio)) {
      result.AudioRatio = toNormalizedNumber(model.rawRatios.audioRatio);
    }
    if (hasValue(model.rawRatios.audioCompletionRatio)) {
      result.AudioCompletionRatio = toNormalizedNumber(
        model.rawRatios.audioCompletionRatio,
      );
    }
    if (hasValue(model.rawRatios.videoRatio)) {
      result.VideoRatio = toNormalizedNumber(model.rawRatios.videoRatio);
    }
    if (hasValue(model.rawRatios.videoCompletionRatio)) {
      result.VideoCompletionRatio = toNormalizedNumber(
        model.rawRatios.videoCompletionRatio,
      );
    }
    return result;
  }

  result.ModelRatio = toNormalizedNumber(inputPrice / 2);

  if (!model.completionRatioLocked && completionPrice !== null) {
    result.CompletionRatio = toNormalizedNumber(completionPrice / inputPrice);
  } else if (
    model.completionRatioLocked &&
    hasValue(model.rawRatios.completionRatio)
  ) {
    result.CompletionRatio = toNormalizedNumber(
      model.rawRatios.completionRatio,
    );
  }
  if (cachePrice !== null) {
    result.CacheRatio = toNormalizedNumber(cachePrice / inputPrice);
  }
  if (createCachePrice !== null) {
    result.CreateCacheRatio = toNormalizedNumber(createCachePrice / inputPrice);
  }
  if (imagePrice !== null) {
    result.ImageRatio = toNormalizedNumber(imagePrice / inputPrice);
  }
  if (imageCompletionPrice !== null) {
    result.ImageCompletionRatio = toNormalizedNumber(
      imageCompletionPrice / inputPrice,
    );
  }
  if (audioInputPrice !== null) {
    result.AudioRatio = toNormalizedNumber(audioInputPrice / inputPrice);
  }
  if (audioOutputPrice !== null) {
    if (audioInputPrice === null || audioInputPrice === 0) {
      throw new Error(
        t('模型 {{name}} 缺少音频输入价格，无法计算音频补全倍率', {
          name: model.name,
        }),
      );
    }
    result.AudioCompletionRatio = toNormalizedNumber(
      audioOutputPrice / audioInputPrice,
    );
  }
  if (videoInputPrice !== null) {
    result.VideoRatio = toNormalizedNumber(videoInputPrice / inputPrice);
  }
  if (videoCompletionPrice !== null) {
    result.VideoCompletionRatio = toNormalizedNumber(
      videoCompletionPrice / inputPrice,
    );
  }

  return result;
};

export const buildPreviewRows = (model, t) => {
  if (!model) return [];
  const finalBillingExpr = combineBillingExpr(
    model.billingExpr,
    model.requestRuleExpr,
  );

  if (model.billingMode === 'tiered_expr') {
    const rows = [
      {
        key: 'BillingMode',
        label: 'ModelBillingMode',
        value: 'tiered_expr',
      },
    ];
    if (finalBillingExpr) {
      const tierCount = (model.billingExpr.match(/tier\(/g) || []).length;
      rows.push({
        key: 'BillingExpr',
        label: 'ModelBillingExpr',
        value:
          tierCount > 0
            ? `${tierCount} ${t('档')} — ${
                finalBillingExpr.length > 60
                  ? finalBillingExpr.slice(0, 60) + '...'
                  : finalBillingExpr
              }`
            : finalBillingExpr.length > 60
              ? finalBillingExpr.slice(0, 60) + '...'
              : finalBillingExpr,
      });
    }
    return rows;
  }

  if (model.billingMode === 'per-request') {
    const rows = [
      {
        key: 'ModelPrice',
        label: 'ModelPrice',
        value: hasValue(model.fixedPrice) ? model.fixedPrice : t('空'),
      },
    ];
    return rows;
  }

  if (model.billingMode === 'per-image') {
    return [
      {
        key: 'ImageModelPricePerImage',
        label: 'ImageModelPricePerImage',
        value: hasValue(model.imagePricePerImage)
          ? model.imagePricePerImage
          : model.imagePriceStructure
            ? stringifyPricingStructure(model.imagePriceStructure)
            : t('空'),
      },
    ];
  }

  if (model.billingMode === 'per-second') {
    return [
      {
        key: 'VideoModelPricePerSecond',
        label: 'VideoModelPricePerSecond',
        value: hasValue(model.videoPricePerSecond)
          ? model.videoPricePerSecond
          : model.videoPriceStructure
            ? stringifyPricingStructure(model.videoPriceStructure)
            : t('空'),
      },
    ];
  }

  const inputPrice = toNumberOrNull(model.inputPrice);
  if (inputPrice === null) {
    const rows = [
      {
        key: 'ModelRatio',
        label: 'ModelRatio',
        value: hasValue(model.rawRatios.modelRatio)
          ? model.rawRatios.modelRatio
          : t('空'),
      },
      {
        key: 'CompletionRatio',
        label: 'CompletionRatio',
        value: hasValue(model.rawRatios.completionRatio)
          ? model.rawRatios.completionRatio
          : t('空'),
      },
      {
        key: 'CacheRatio',
        label: 'CacheRatio',
        value: hasValue(model.rawRatios.cacheRatio)
          ? model.rawRatios.cacheRatio
          : t('空'),
      },
      {
        key: 'CreateCacheRatio',
        label: 'CreateCacheRatio',
        value: hasValue(model.rawRatios.createCacheRatio)
          ? model.rawRatios.createCacheRatio
          : t('空'),
      },
      {
        key: 'ImageRatio',
        label: 'ImageRatio',
        value: hasValue(model.rawRatios.imageRatio)
          ? model.rawRatios.imageRatio
          : t('空'),
      },
      {
        key: 'AudioRatio',
        label: 'AudioRatio',
        value: hasValue(model.rawRatios.audioRatio)
          ? model.rawRatios.audioRatio
          : t('空'),
      },
      {
        key: 'AudioCompletionRatio',
        label: 'AudioCompletionRatio',
        value: hasValue(model.rawRatios.audioCompletionRatio)
          ? model.rawRatios.audioCompletionRatio
          : t('空'),
      },
      {
        key: 'VideoRatio',
        label: 'VideoRatio',
        value: hasValue(model.rawRatios.videoRatio)
          ? model.rawRatios.videoRatio
          : t('空'),
      },
    ];
    return rows;
  }

  const completionPrice = toNumberOrNull(model.completionPrice);
  const cachePrice = toNumberOrNull(model.cachePrice);
  const createCachePrice = toNumberOrNull(model.createCachePrice);
  const imagePrice = toNumberOrNull(model.imagePrice);
  const audioInputPrice = toNumberOrNull(model.audioInputPrice);
  const audioOutputPrice = toNumberOrNull(model.audioOutputPrice);
  const videoInputPrice = toNumberOrNull(model.videoInputPrice);

  const rows = [
    {
      key: 'ModelRatio',
      label: 'ModelRatio',
      value: formatNumber(inputPrice / 2),
    },
    {
      key: 'CompletionRatio',
      label: 'CompletionRatio',
      value: model.completionRatioLocked
        ? `${model.lockedCompletionRatio || t('空')} (${t('后端固定')})`
        : completionPrice !== null
          ? formatNumber(completionPrice / inputPrice)
          : t('空'),
    },
    {
      key: 'CacheRatio',
      label: 'CacheRatio',
      value:
        cachePrice !== null ? formatNumber(cachePrice / inputPrice) : t('空'),
    },
    {
      key: 'CreateCacheRatio',
      label: 'CreateCacheRatio',
      value:
        createCachePrice !== null
          ? formatNumber(createCachePrice / inputPrice)
          : t('空'),
    },
    {
      key: 'ImageRatio',
      label: 'ImageRatio',
      value:
        imagePrice !== null ? formatNumber(imagePrice / inputPrice) : t('空'),
    },
    {
      key: 'AudioRatio',
      label: 'AudioRatio',
      value:
        audioInputPrice !== null
          ? formatNumber(audioInputPrice / inputPrice)
          : t('空'),
    },
    {
      key: 'AudioCompletionRatio',
      label: 'AudioCompletionRatio',
      value:
        audioOutputPrice !== null &&
        audioInputPrice !== null &&
        audioInputPrice !== 0
          ? formatNumber(audioOutputPrice / audioInputPrice)
          : t('空'),
    },
    {
      key: 'VideoRatio',
      label: 'VideoRatio',
      value:
        videoInputPrice !== null
          ? formatNumber(videoInputPrice / inputPrice)
          : t('空'),
    },
  ];
  return rows;
};

export function useModelPricingEditorState({
  options = {},
  refresh,
  t,
  candidateModelNames = EMPTY_CANDIDATE_MODEL_NAMES,
  filterMode = 'all',
}) {
  const [models, setModels] = useState([]);
  const [initialVisibleModelNames, setInitialVisibleModelNames] = useState([]);
  const [selectedModelName, setSelectedModelName] = useState('');
  const [selectedModelNames, setSelectedModelNames] = useState([]);
  const [searchText, setSearchText] = useState('');
  const [currentPage, setCurrentPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [conflictOnly, setConflictOnly] = useState(false);
  const [billingModeFilter, setBillingModeFilter] = useState('');
  const [optionalFieldToggles, setOptionalFieldToggles] = useState({});
  const [draftBillingModes, setDraftBillingModes] = useState({});
  const [operLogModal, setOperLogModal] = useState({
    visible: false,
    changes: [],
    defaultRemark: '',
  });
  const pendingSaveRef = useRef(null);
  const originalModelsRef = useRef(new Map());

  useEffect(() => {
    const sourceMaps = {
      ModelPrice: parseOptionJSON(options.ModelPrice),
      ModelRatio: parseOptionJSON(options.ModelRatio),
      CompletionRatio: parseOptionJSON(options.CompletionRatio),
      CompletionRatioMeta: parseOptionJSON(options.CompletionRatioMeta),
      CacheRatio: parseOptionJSON(options.CacheRatio),
      CreateCacheRatio: parseOptionJSON(options.CreateCacheRatio),
      ImageRatio: parseOptionJSON(options.ImageRatio),
      ImageCompletionRatio: parseOptionJSON(options.ImageCompletionRatio),
      AudioRatio: parseOptionJSON(options.AudioRatio),
      AudioCompletionRatio: parseOptionJSON(options.AudioCompletionRatio),
      VideoRatio: parseOptionJSON(options.VideoRatio),
      VideoCompletionRatio: parseOptionJSON(
        options.VideoCompletionRatio,
      ),
      ImageModelPricePerImage: parseOptionJSON(
        options.ImageModelPricePerImage,
      ),
      VideoModelPricePerSecond: parseOptionJSON(
        options.VideoModelPricePerSecond,
      ),
      ModelBillingMode: parseOptionJSON(
        options['billing_setting.billing_mode'],
      ),
      ModelBillingExpr: parseOptionJSON(
        options['billing_setting.billing_expr'],
      ),
    };

    const names = new Set([
      ...candidateModelNames,
      ...Object.keys(sourceMaps.ModelPrice),
      ...Object.keys(sourceMaps.ModelRatio),
      ...Object.keys(sourceMaps.CompletionRatio),
      ...Object.keys(sourceMaps.CompletionRatioMeta),
      ...Object.keys(sourceMaps.CacheRatio),
      ...Object.keys(sourceMaps.CreateCacheRatio),
      ...Object.keys(sourceMaps.ImageRatio),
      ...Object.keys(sourceMaps.ImageCompletionRatio),
      ...Object.keys(sourceMaps.AudioRatio),
      ...Object.keys(sourceMaps.AudioCompletionRatio),
      ...Object.keys(sourceMaps.VideoRatio),
      ...Object.keys(sourceMaps.VideoCompletionRatio),
      ...Object.keys(sourceMaps.ImageModelPricePerImage),
      ...Object.keys(sourceMaps.VideoModelPricePerSecond),
      ...Object.keys(sourceMaps.ModelBillingMode),
      ...Object.keys(sourceMaps.ModelBillingExpr),
    ]);

    const nextModels = Array.from(names)
      .map((name) => buildModelState(name, sourceMaps))
      .sort((a, b) => a.name.localeCompare(b.name));

    setModels(nextModels);
    setDraftBillingModes({});
    originalModelsRef.current = new Map(
      nextModels.map((model) => [model.name, JSON.parse(JSON.stringify(model))]),
    );
    setInitialVisibleModelNames(
      filterMode === 'unset'
        ? nextModels
            .filter((model) => isBasePricingUnset(model))
            .map((model) => model.name)
        : nextModels.map((model) => model.name),
    );
    setOptionalFieldToggles(
      nextModels.reduce((acc, model) => {
        acc[model.name] = buildOptionalFieldToggles(model);
        return acc;
      }, {}),
    );
    setSelectedModelName((previous) => {
      if (previous && nextModels.some((model) => model.name === previous)) {
        return previous;
      }
      const nextVisibleModels =
        filterMode === 'unset'
          ? nextModels.filter((model) => isBasePricingUnset(model))
          : nextModels;
      return nextVisibleModels[0]?.name || '';
    });
  }, [candidateModelNames, filterMode, options]);

  const visibleModels = useMemo(() => {
    return filterMode === 'unset'
      ? models.filter((model) => initialVisibleModelNames.includes(model.name))
      : models;
  }, [filterMode, initialVisibleModelNames, models]);

  const filteredModels = useMemo(() => {
    return visibleModels.filter((model) => {
      const keyword = searchText.trim().toLowerCase();
      const keywordMatch = keyword
        ? model.name.toLowerCase().includes(keyword)
        : true;
      const conflictMatch = conflictOnly ? model.hasConflict : true;
      const billingMatch = billingModeFilter
        ? model.billingMode === billingModeFilter
        : true;
      return keywordMatch && conflictMatch && billingMatch;
    });
  }, [billingModeFilter, conflictOnly, searchText, visibleModels]);

  const pagedData = useMemo(() => {
    const start = (currentPage - 1) * PAGE_SIZE;
    return filteredModels.slice(start, start + PAGE_SIZE);
  }, [currentPage, filteredModels]);

  const selectedModel = useMemo(() => {
    const base =
      visibleModels.find((model) => model.name === selectedModelName) || null;
    if (!base) return null;
    const draft = draftBillingModes[base.name];
    return draft ? { ...base, billingMode: draft } : base;
  }, [draftBillingModes, selectedModelName, visibleModels]);

  const selectedWarnings = useMemo(
    () => getModelWarnings(selectedModel, t),
    [selectedModel, t],
  );

  const previewRows = useMemo(
    () => buildPreviewRows(selectedModel, t),
    [selectedModel, t],
  );

  useEffect(() => {
    setCurrentPage(1);
  }, [searchText, conflictOnly, billingModeFilter, filterMode, candidateModelNames]);

  useEffect(() => {
    setSelectedModelNames((previous) =>
      previous.filter((name) =>
        visibleModels.some((model) => model.name === name),
      ),
    );
  }, [visibleModels]);

  useEffect(() => {
    if (visibleModels.length === 0) {
      setSelectedModelName('');
      return;
    }
    if (!visibleModels.some((model) => model.name === selectedModelName)) {
      setSelectedModelName(visibleModels[0].name);
    }
  }, [selectedModelName, visibleModels]);

  const upsertModel = (name, updater) => {
    setModels((previous) =>
      previous.map((model) => {
        if (model.name !== name) return model;
        return typeof updater === 'function' ? updater(model) : updater;
      }),
    );
  };

  const isOptionalFieldEnabled = (model, field) => {
    if (!model) return false;
    const modelToggles = optionalFieldToggles[model.name];
    if (modelToggles && typeof modelToggles[field] === 'boolean') {
      return modelToggles[field];
    }
    return buildOptionalFieldToggles(model)[field];
  };

  const updateOptionalFieldToggle = (modelName, field, checked) => {
    setOptionalFieldToggles((prev) => ({
      ...prev,
      [modelName]: {
        ...(prev[modelName] || {}),
        [field]: checked,
      },
    }));
  };

  const handleOptionalFieldToggle = (field, checked) => {
    if (!selectedModel) return;

    updateOptionalFieldToggle(selectedModel.name, field, checked);

    if (checked) {
      return;
    }

    upsertModel(selectedModel.name, (model) => {
      const nextModel = { ...model, [field]: '' };

      if (field === 'audioInputPrice') {
        nextModel.audioOutputPrice = '';
        setOptionalFieldToggles((prev) => ({
          ...prev,
          [selectedModel.name]: {
            ...(prev[selectedModel.name] || {}),
            audioInputPrice: false,
            audioOutputPrice: false,
          },
        }));
      }

      return nextModel;
    });
  };

  const fillDerivedPricesFromBase = (model, nextInputPrice) => {
    const baseNumber = toNumberOrNull(nextInputPrice);
    if (baseNumber === null) {
      return model;
    }

    return {
      ...model,
      completionPrice:
        model.completionRatioLocked && hasValue(model.lockedCompletionRatio)
          ? formatNumber(baseNumber * Number(model.lockedCompletionRatio))
          : !hasValue(model.completionPrice) &&
              hasValue(model.rawRatios.completionRatio)
            ? formatNumber(baseNumber * Number(model.rawRatios.completionRatio))
            : model.completionPrice,
      cachePrice:
        !hasValue(model.cachePrice) && hasValue(model.rawRatios.cacheRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.cacheRatio))
          : model.cachePrice,
      createCachePrice:
        !hasValue(model.createCachePrice) &&
        hasValue(model.rawRatios.createCacheRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.createCacheRatio))
          : model.createCachePrice,
      imagePrice:
        !hasValue(model.imagePrice) && hasValue(model.rawRatios.imageRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.imageRatio))
          : model.imagePrice,
      audioInputPrice:
        !hasValue(model.audioInputPrice) && hasValue(model.rawRatios.audioRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.audioRatio))
          : model.audioInputPrice,
      videoInputPrice:
        !hasValue(model.videoInputPrice) && hasValue(model.rawRatios.videoRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.videoRatio))
          : model.videoInputPrice,
      audioOutputPrice:
        !hasValue(model.audioOutputPrice) &&
        hasValue(model.rawRatios.audioRatio) &&
        hasValue(model.rawRatios.audioCompletionRatio)
          ? formatNumber(
              baseNumber *
                Number(model.rawRatios.audioRatio) *
                Number(model.rawRatios.audioCompletionRatio),
            )
          : model.audioOutputPrice,
    };
  };

  const handleNumericFieldChange = (field, value) => {
    if (!selectedModel || !NUMERIC_INPUT_REGEX.test(value)) {
      return;
    }

    upsertModel(selectedModel.name, (model) => {
      const updatedModel = { ...model, [field]: value };

      if (field === 'inputPrice') {
        return fillDerivedPricesFromBase(updatedModel, value);
      }

      return updatedModel;
    });
  };

  const handleStructureFieldChange = (
    structureField,
    nextStructure,
    simpleField,
  ) => {
    if (!selectedModel) return;
    upsertModel(selectedModel.name, (model) => {
      const updated = { ...model, [structureField]: nextStructure };
      if (nextStructure?.kind === 'simple') {
        updated[simpleField] = nextStructure.price || '';
      } else if (
        model[simpleField] !== '' &&
        model[simpleField] !== undefined
      ) {
        updated[simpleField] = '';
      }
      return updated;
    });
  };

  const handleBillingModeChange = (value) => {
    if (!selectedModel) return;
    const targetName = selectedModel.name;
    setDraftBillingModes((previous) => {
      const nextDraft = { ...previous, [targetName]: value };
      if (value === selectedModel.billingMode) {
        delete nextDraft[targetName];
      }
      return nextDraft;
    });
    if (value === 'tiered_expr' && !selectedModel.billingExpr) {
      upsertModel(targetName, (model) => ({
        ...model,
        billingExpr: 'tier("base", p * 0 + c * 0)',
      }));
    }
  };

  const handleBillingExprChange = (newExpr) => {
    if (!selectedModel) return;
    upsertModel(selectedModel.name, (model) => ({
      ...model,
      billingExpr: newExpr,
    }));
  };

  const handleRequestRuleExprChange = (newExpr) => {
    if (!selectedModel) return;
    upsertModel(selectedModel.name, (model) => ({
      ...model,
      requestRuleExpr: newExpr,
    }));
  };

  const addModel = (modelName) => {
    const trimmedName = modelName.trim();
    if (!trimmedName) {
      showError(t('请输入模型名称'));
      return false;
    }
    if (models.some((model) => model.name === trimmedName)) {
      showError(t('模型名称已存在'));
      return false;
    }

    const nextModel = {
      ...EMPTY_MODEL,
      name: trimmedName,
      rawRatios: { ...EMPTY_MODEL.rawRatios },
    };

    setModels((previous) => [nextModel, ...previous]);
    setOptionalFieldToggles((prev) => ({
      ...prev,
      [trimmedName]: buildOptionalFieldToggles(nextModel),
    }));
    setSelectedModelName(trimmedName);
    setCurrentPage(1);
    return true;
  };

  const deleteModel = (name) => {
    const nextModels = models.filter((model) => model.name !== name);
    setModels(nextModels);
    setOptionalFieldToggles((prev) => {
      const next = { ...prev };
      delete next[name];
      return next;
    });
    setSelectedModelNames((previous) =>
      previous.filter((item) => item !== name),
    );
    if (selectedModelName === name) {
      setSelectedModelName(nextModels[0]?.name || '');
    }
  };

  const applySelectedModelPricing = () => {
    if (!selectedModel) {
      showError(t('请先选择一个作为模板的模型'));
      return false;
    }
    if (selectedModelNames.length === 0) {
      showError(t('请先勾选需要批量设置的模型'));
      return false;
    }

    const sourceToggles = optionalFieldToggles[selectedModel.name] || {};

    setModels((previous) =>
      previous.map((model) => {
        if (!selectedModelNames.includes(model.name)) {
          return model;
        }

        const nextModel = {
          ...model,
          billingMode: selectedModel.billingMode,
          fixedPrice: selectedModel.fixedPrice,
          inputPrice: selectedModel.inputPrice,
          completionPrice: selectedModel.completionPrice,
          cachePrice: selectedModel.cachePrice,
          createCachePrice: selectedModel.createCachePrice,
          imagePrice: selectedModel.imagePrice,
          imageCompletionPrice: selectedModel.imageCompletionPrice,
          audioInputPrice: selectedModel.audioInputPrice,
          audioOutputPrice: selectedModel.audioOutputPrice,
          videoInputPrice: selectedModel.videoInputPrice,
          videoCompletionPrice: selectedModel.videoCompletionPrice,
          imagePricePerImage: selectedModel.imagePricePerImage,
          videoPricePerSecond: selectedModel.videoPricePerSecond,
          imagePriceStructure: selectedModel.imagePriceStructure,
          videoPriceStructure: selectedModel.videoPriceStructure,
          billingExpr: selectedModel.billingExpr || '',
          requestRuleExpr: selectedModel.requestRuleExpr || '',
        };

        if (
          nextModel.billingMode === 'per-token' &&
          nextModel.completionRatioLocked &&
          hasValue(nextModel.inputPrice) &&
          hasValue(nextModel.lockedCompletionRatio)
        ) {
          nextModel.completionPrice = formatNumber(
            Number(nextModel.inputPrice) *
              Number(nextModel.lockedCompletionRatio),
          );
        }

        return nextModel;
      }),
    );

    setOptionalFieldToggles((previous) => {
      const next = { ...previous };
      selectedModelNames.forEach((modelName) => {
        const targetModel = models.find((item) => item.name === modelName);
        next[modelName] = {
          completionPrice: targetModel?.completionRatioLocked
            ? true
            : Boolean(sourceToggles.completionPrice),
          cachePrice: Boolean(sourceToggles.cachePrice),
          createCachePrice: Boolean(sourceToggles.createCachePrice),
          imagePrice: Boolean(sourceToggles.imagePrice),
          audioInputPrice: Boolean(sourceToggles.audioInputPrice),
          audioOutputPrice:
            Boolean(sourceToggles.audioInputPrice) &&
            Boolean(sourceToggles.audioOutputPrice),
          videoInputPrice: Boolean(sourceToggles.videoInputPrice),
        };
      });
      return next;
    });

    showSuccess(
      t('已将模型 {{name}} 的价格配置批量应用到 {{count}} 个模型', {
        name: selectedModel.name,
        count: selectedModelNames.length,
      }),
    );
    return true;
  };

  const buildSavePayload = () => {
    const output = {
      ModelPrice: {},
      ModelRatio: {},
      CompletionRatio: {},
      CacheRatio: {},
      CreateCacheRatio: {},
      ImageRatio: {},
      ImageCompletionRatio: {},
      AudioRatio: {},
      AudioCompletionRatio: {},
      VideoRatio: {},
      VideoCompletionRatio: {},
      ImageModelPricePerImage: {},
      VideoModelPricePerSecond: {},
    };

    const tieredOutput = {
      'billing_setting.billing_mode': {},
      'billing_setting.billing_expr': {},
    };

    for (const baseModel of models) {
      const draft = draftBillingModes[baseModel.name];
      const model = draft
        ? { ...baseModel, billingMode: draft }
        : baseModel;
      if (model.billingMode === 'tiered_expr') {
        const finalBillingExpr = combineBillingExpr(
          model.billingExpr,
          model.requestRuleExpr,
        );
        if (finalBillingExpr) {
          tieredOutput['billing_setting.billing_mode'][model.name] =
            'tiered_expr';
          tieredOutput['billing_setting.billing_expr'][model.name] =
            finalBillingExpr;
        }
      }

      // Always serialize ratio/price values for all models (including
      // tiered_expr) so they serve as fallback during multi-instance sync
      // delay.  ModelPriceHelper checks billing_mode first, so these values
      // are only used when billing_setting hasn't propagated yet.
      try {
        const serialized = serializeModel(model, t);
        Object.entries(serialized).forEach(([key, value]) => {
          if (value !== null) {
            output[key][model.name] = value;
          }
        });
      } catch (e) {
        if (model.billingMode !== 'tiered_expr') {
          throw e;
        }
      }
    }

    const payload = {};
    Object.entries(output).forEach(([key, value]) => {
      payload[key] = JSON.stringify(value, null, 2);
    });
    Object.entries(tieredOutput).forEach(([key, value]) => {
      payload[key] = JSON.stringify(value, null, 2);
    });
    return payload;
  };

  const normalizeJson = (str) => {
    if (str === undefined || str === null || str === '') return '';
    try {
      return JSON.stringify(JSON.parse(str));
    } catch {
      return String(str);
    }
  };

  const computeChanges = (payload) => {
    const changes = [];
    Object.entries(payload).forEach(([key, newStr]) => {
      const oldStr = options[key] ?? '';
      if (normalizeJson(oldStr) !== normalizeJson(newStr)) {
        changes.push({
          key,
          oldVal: oldStr && oldStr.trim() ? oldStr : '(空)',
          newVal: newStr,
        });
      }
    });
    return changes;
  };

  const commitSave = async (payload, logRemark, logContent) => {
    setLoading(true);
    try {
      const requestQueue = Object.entries(payload).map(([key, value]) =>
        API.put('/api/option/', { key, value }),
      );
      const results = await Promise.all(requestQueue);
      for (const res of results) {
        if (!res?.data?.success) {
          throw new Error(res?.data?.message || t('保存失败，请重试'));
        }
      }
      showSuccess(t('保存成功'));
      if (logRemark !== null) {
        await createOperLog({
          oper_type: '模型价格',
          content: logContent,
          remark: logRemark,
        });
      }
      await refresh();
    } catch (error) {
      console.error('保存失败:', error);
      showError(error.message || t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  };

  const handleSubmit = async () => {
    let payload;
    try {
      payload = buildSavePayload();
    } catch (error) {
      showError(error.message || t('保存失败，请重试'));
      return;
    }
    const changes = computeChanges(payload);
    if (!changes.length) {
      showWarning(t('你似乎并没有修改什么'));
      return;
    }
    pendingSaveRef.current = payload;
    setOperLogModal({
      visible: true,
      changes,
      defaultRemark: `修改了模型定价（${changes.length} 项）`,
    });
  };

  const confirmOperLogSave = (remark, content) => {
    const payload = pendingSaveRef.current;
    setOperLogModal((s) => ({ ...s, visible: false }));
    if (payload) commitSave(payload, remark, content);
  };

  const skipOperLogSave = () => {
    const payload = pendingSaveRef.current;
    setOperLogModal((s) => ({ ...s, visible: false }));
    if (payload) commitSave(payload, null, null);
  };

  const cancelOperLogSave = () => {
    pendingSaveRef.current = null;
    setOperLogModal((s) => ({ ...s, visible: false }));
  };

  const resetSelectedModel = () => {
    if (!selectedModelName) {
      showWarning(t('请先在左侧选择一个模型'));
      return;
    }
    const original = originalModelsRef.current.get(selectedModelName);
    if (!original) {
      showWarning(t('当前模型不存在原始值，无法还原'));
      return;
    }
    const clone = JSON.parse(JSON.stringify(original));
    setModels((previous) =>
      previous.map((model) =>
        model.name === selectedModelName ? clone : model,
      ),
    );
    setOptionalFieldToggles((prev) => ({
      ...prev,
      [selectedModelName]: buildOptionalFieldToggles(clone),
    }));
    setDraftBillingModes((prev) => {
      if (!(selectedModelName in prev)) return prev;
      const next = { ...prev };
      delete next[selectedModelName];
      return next;
    });
    showSuccess(t('已恢复原值'));
  };

  return {
    models,
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
  };
}
