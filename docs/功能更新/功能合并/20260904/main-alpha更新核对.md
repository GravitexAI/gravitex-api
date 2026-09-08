# main-alpha 更新核对与 main 差异说明（2026-09-04）

> **状态：合并前文档核对，未合入 main 的 44 个新提交。**
> 以 main-alpha 实现为准，沿用既有“功能说明 → 位置 → 合并保护点”记录方式。长期清单见 [main-alpha 独有功能清单](../main-alpha_独有功能清单.md)，后续进度见 [官方合并待办追踪](../官方合并待办追踪.md)。

---

## 一、分支基线与核对方法

| 项 | 值 |
|---|---|
| main-alpha | `662c3b2031425a8fd1d0b51024cef236c753ee44` |
| main-alpha-merge | 文档编辑前与 main-alpha 相同；执行 `git merge --ff-only main-alpha` 返回 Already up to date |
| main | `3a9f41ee85cc369f5b8d7fe6e62ff4e7bf3a9ec8` |
| 共同祖先 | `2d8e50bf36e94200b809dfb39e73624ec48b1e23` |
| 最近合并文档提交 | `cdc97eb38` |
| 最近 fork 增量 | 29 个提交：26 个非 merge 提交 + 3 个 merge 提交 |

复核命令（在仓库根目录执行；分支移动后结果会变化，复现本快照可将分支替换为上表 SHA）：

```bash
git merge-base main-alpha main
git rev-list --left-right --count main...main-alpha
git diff --shortstat main main-alpha
git diff --shortstat main...main-alpha
git log --no-merges --oneline cdc97eb38..main-alpha
git log --oneline main-alpha..main
```

| 统计口径 | 本次结果 | 如何理解 |
|---|---|---|
| 分支独有历史 | main 44 / main-alpha 5800 | Git 提交可达性，不等于功能数量；包含历史合并等提交 |
| 分支 tip 直接比较 | 2616 文件，+514593 / -88271 行 | 从 main 当前树变成 main-alpha 当前树的完整差异 |
| 共同祖先 → main-alpha | 2186 文件，+505878 / -14152 行 | 三点 diff 的单侧改动，不是两个分支当前完整差异 |

大量文件差异包含双前端、文档和历史结构差异。不能用文件数或提交数宣称有同样数量的特有功能。本次复核近期 26 个非 merge 提交的标题、相关 patch 和源码，并补充会被新 main 影响的保护点；不是重新审计 5800 个历史提交的全部行为。

## 二、main-alpha 新增或补齐的功能

| 功能 | 历史依据 | 当前行为与合并注意事项 |
|---|---|---|
| 虚拟模型 Auto 路由 | `7180b4f06` | auto / 成本档位选实际模型；分组、Token 权限、能力、任务偏好、权重和粘性共同约束。配置页在 classic；不要混同自动分组 |
| 渠道多 key 亲和性 | `c83aeca47`、`f1ca428e6` | 在渠道绑定上增加 key 身份；重排、禁用、替换、失败重试和历史绑定格式均需兼容；classic/default 均有配置入口 |
| Token 备注 | `c6218074f` | `remake` 字段贯穿新增、更新、查询；省略保留、空串清空、关键词可按备注查询 |
| 渠道编辑字段保留 | `c6218074f` | 同一次提交还修了 setting/settings 顶层合并、null 删除及缺省 cost_discount 保留；不能只按提交标题记录 Token |
| Seedream 拆图与多尺寸输出计费 | `f39bcce21`、`f6068c5d9` | 每张按实际尺寸档位累计；拆图输出价 ×0.5；结构化按张路径排除再次乘 n；保留分档账单字段 |
| Gemini 搜索工具识别与配价别名 | `71977a443` | grounding 的 webSearchQueries 非空作为成功标记；googleSearch 配置兼容内部 google_search；新 main 转换重构时防止重复计费 |
| Lyria 3 | `d3c730e46` | pro/clip 两模型，Google / Vertex Interactions 适配，同步不建任务、本地异步任务状态链路；保留定价和轮询集成 |
| Claude 原生请求 body 保留 | `225998c31` | beta header 白名单不再联动剥离原生 output_config/context_management；兼容目标媒体转换仍在 |
| Anthropic Responses 能力门禁 | `525af5ab9` | main-alpha 选中 Anthropic 渠道时返回 404、不重试、不写持久化错误日志；新 main 已实现转换，属于待评估行为差异 |
| classic 可视化定价与规则日志 | `981aa3328`、`1c1c434a9`、`c458817be` | 完善可视化编辑/保存、小于等于条件和时段倍率显示；引擎来自官方，fork 差异在 classic 配套 |
| Claude 渠道测试工具链 | `325f79243`、`ad8209b75`、`662c3b203` | 测试脚本、报告、调度服务、部署说明、venv 自检与排障；目录存在不代表真实渠道已通过 |

剩余近期提交主要为部署镜像 / 探针及文档调整，按运维配置保留，未逐条包装成独立业务功能。源码入口和已有回归文件详见防丢清单新增小节。

## 三、已有 main-alpha 定制继续以历史清单为准

| 功能组 | 历史清单位置 | 本轮合并关注点 |
|---|---|---|
| RuoYi SSO、A2、平台与企业账号 | 5.3 / 5.7 / 5.8 / 七点六 / 七点七 | 新 main 密码加密与鉴权代码修改不能覆盖现有登录入口和隔离语义 |
| 双前端和 OperLog | 四 / 四点五 / 七点五 | classic/default workspace、embed、Docker 构建、操作审计确认；官方 web/src 的更新须评估映射 |
| 渠道成本、号段、TokenHub / SeedanceGateway | 一 / 七点九 / 七点十 | 保留生产类型号及专属适配、成本字段和模型级折扣 |
| 视频与图片计费、透支、缓存配额 | 三 / 七点八 / 七点十一 | 保留预扣、结算、退款、CAS 及允许透支策略，结合官方新任务插件逐条核对 |
| 日志、ByteHouse、Snowflake ID | 二 / 六 / 七 / 七点十一 | 字符串 ID、共享表、用量审计和权限投影不能只按同名文件整体覆盖 |

此表用于引导后续核对，不追加“本轮所有历史功能已验证无回归”的结论。

## 四、官方 main 的待合入差异

44 个提交仍在 `main-alpha..main`，主要涉及沙箱 JS 任务插件及轮询、协议/工具/reasoning 转换、日志权限、密码传输加密、数据库迁移和时间规则修复。具体风险映射已更新到待办追踪第一节。

特别注意：官方删除多家 `relay/channel/task/*` 适配器是在迁移实现；main-alpha 在这些位置存在计费和协议定制，不能由删除动作推导“fork 功能已由插件等价接管”。同样，官方现在支持 Claude Responses，不能把 fork 当前 404 当成上游能力结论。

## 五、本次验证与交付边界

- 核对了分支 SHA、共同祖先、提交范围和两种 diff 口径。
- 对新增功能核对了提交 patch、当前源码及已有测试入口；测试文件作为后续回归入口，本次未执行业务测试。
- 更新防丢清单、README 与待办追踪；保留历次合并记录和历史验证结果，标明其基线。
- 本次无业务代码变更，无数据库执行，无官方 main 合并，无远程推送。
