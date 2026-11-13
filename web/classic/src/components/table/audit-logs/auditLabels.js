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

// 审计日志 action 的中文名称；未登记的 action 原样显示。
export const AUDIT_ACTION_LABELS = {
  'channel.create': '新增渠道',
  'channel.update': '修改渠道',
  'channel.delete': '删除渠道',
  'channel.delete_batch': '批量删除渠道',
  'channel.delete_disabled': '删除所有已禁用渠道',
  'channel.key_view': '查看渠道密钥',
  'channel.status_update': '修改渠道状态',
  'channel.status_update_batch': '批量修改渠道状态',
  'channel.tag_disable': '按标签禁用渠道',
  'channel.tag_enable': '按标签启用渠道',
  'channel.tag_edit': '按标签编辑渠道',
  'channel.tag_batch_set': '批量设置渠道标签',
  'channel.copy': '复制渠道',
  'channel.multi_key_manage': '多密钥管理',
  'channel.upstream_apply': '应用上游模型变更',
  'channel.upstream_apply_all': '批量应用上游模型变更',
  'channel.upstream_detect_all': '检测全部渠道上游模型',
  'user.create': '新增用户',
  'user.update': '修改用户',
  'user.delete': '删除用户',
  'user.manage': '管理用户',
  'user.quota_add': '增加用户额度',
  'user.quota_subtract': '扣减用户额度',
  'user.quota_override': '覆盖用户额度',
  'user.binding_clear': '清除用户绑定',
  'user.2fa_disable': '关闭用户两步验证',
  'user.passkey_register': '注册 Passkey',
  'user.passkey_delete': '删除 Passkey',
  'user.reset_passkey': '重置用户 Passkey',
  'user.topup_complete': '手动补单',
  'option.update': '修改系统设置',
  'option.reset_ratio': '重置模型倍率',
  'option.payment_compliance': '修改支付合规设置',
  'option.clear_affinity_cache': '清空渠道亲和缓存',
  'model.create': '新增模型',
  'model.update': '修改模型',
  'model.delete': '删除模型',
  'model.sync_upstream': '同步上游模型',
  'model.metadata.sync': '同步模型元数据',
  'model.pricing.update': '修改模型定价',
  'vendor.create': '新增供应商',
  'vendor.update': '修改供应商',
  'vendor.delete': '删除供应商',
  'vendor.metadata.save': '保存供应商',
  'vendor.metadata.delete': '删除供应商',
  'redemption.create': '新增兑换码',
  'redemption.update': '修改兑换码',
  'redemption.delete': '删除兑换码',
  'redemption.delete_batch': '批量删除兑换码',
  'redemption.delete_invalid': '删除无效兑换码',
  'subscription.plan_create': '新增订阅套餐',
  'subscription.plan_update': '修改订阅套餐',
  'subscription.plan_reset': '重置套餐订阅',
  'subscription.user_plan_reset': '重置用户套餐订阅',
  'subscription.user_quota_reset': '重置用户套餐额度',
  'subscription.bind': '绑定订阅',
  'token.create': '创建 API 密钥',
  'token.update': '修改 API 密钥',
  'token.status_update': '修改 API 密钥状态',
  'token.delete': '删除 API 密钥',
  'token.delete_batch': '批量删除 API 密钥',
  'token.key_view': '查看 API 密钥',
  'token.key_view_batch': '批量查看 API 密钥',
  generic: '其他管理操作',
};

export const AUDIT_CATEGORY_LABELS = {
  operation: '管理操作',
  security: '安全操作',
};

// 角色值与后端 common.Role* 一致
export const AUDIT_ROLE_LABELS = {
  0: '未知',
  1: '普通用户',
  10: '管理员',
  100: '超级管理员',
};
