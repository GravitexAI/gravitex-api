# main-alpha 合并 origin/main 流程

> 每次把官方 QuantumNous/new-api 的更新吸收到 gravitex 的 main-alpha 分支时用这套流程。目标：**吸收官方新功能/修复的同时，不丢 main-alpha 的独有改动**。

---

## 一、合并原则

1. **main-alpha 优先**：main-alpha 是生产分支，合并冲突时以 main-alpha 的业务逻辑为准，除非官方修复了 main-alpha 也存在的 bug。
2. **用工作分支合，不直接在 main-alpha 上合**：先建 `main-alpha-merge` 工作分支（复制 main-alpha），在这个分支上合官方 main，跑测试、验证通过后再合回 main-alpha。
3. **每次合并都留档**：在 `docs/功能更新/功能合并/YYYYMMDD/` 目录下写文档（见后面第三节）。
4. **防丢清单必查**：合并前后都要按 [`main-alpha_独有功能清单.md`](main-alpha_独有功能清单.md) 的第九节检查清单逐项核对，不能省。

---

## 二、合并步骤

### 2.1 准备工作分支

```bash
# 用 worktree 隔离，不污染主工作区
git worktree add .claude/worktrees/merge-main-YYYYMMDD -b main-alpha-merge main-alpha

# 或者切到已有的 main-alpha-merge 分支，先把 main-alpha 的最新改动合过来
cd .claude/worktrees/merge-main-YYYYMMDD
git fetch origin main main-alpha
git merge origin/main-alpha  # 拉 main-alpha 的最新
```

### 2.2 合并 origin/main

```bash
git merge origin/main --no-ff --no-commit
```

冲突文件用 `git diff --name-only --diff-filter=U` 查看，逐个解决。

**解决冲突时，先读 [`main-alpha_独有功能清单.md`](main-alpha_独有功能清单.md)**，凡是清单里提到的点，都要保留 main-alpha 版本，不要被官方版本覆盖。

### 2.3 验证

```bash
# 依赖整理
go mod tidy

# build
go build ./...

# 核心回归测试
go test ./model/... ./relay/channel/claude/... ./controller/...
```

关键测试必须通过（见 `main-alpha_独有功能清单.md` 第九节）。

### 2.4 提交

```bash
git commit -m "feat:chz 合并 origin/main N 个 commit 到 main-alpha-merge，吸收 XXX/YYY，保留 ..."
```

### 2.5 写归档文档

在 `docs/功能更新/功能合并/YYYYMMDD/` 下建三个文件：
- `官方更新内容.md` — 白话说明官方这次更新了啥
- `合并调整说明.md` — 冲突处理、哪些改动被拒了、哪些吸收了
- `新增表说明.md` — 如果有 DB schema 新增/变更（可选）

写完把这些文档一起 commit。

### 2.6 push + 让主人 review

```bash
git push origin main-alpha-merge
```

主人本地拉 `main-alpha-merge`，走一遍关键路径验证，通过后再 fast-forward 合回 main-alpha：

```bash
git checkout main-alpha
git merge --ff-only main-alpha-merge   # ff-only 要求不能有分叉
git push origin main-alpha
```

---

## 三、文档规范

### 3.1 目录命名

`docs/功能更新/功能合并/YYYYMMDD/`（YYYYMMDD 是合并**完成**日期，不是官方 commit 日期）

### 3.2 官方更新内容.md

- **面向读者**：产品/运维/客服，不需要看代码就能懂
- **粒度**：按功能模块分节（如"渠道管理""模型定价""前端界面"），不按 commit 分节
- **不写**：具体代码 diff、行号、函数名（除非那是唯一识别方式）
- **要写**：新功能白话说明、修复了什么表现层 bug、新增了什么表/字段、有什么运维影响（重启/迁移等）

参考：[`20260617/官方更新内容.md`](20260617/官方更新内容.md)

### 3.3 合并调整说明.md

- **面向读者**：下一个合并者（可能是主人自己、也可能是 AI 助手）
- **要写**：冲突文件清单 + 每个文件的处理决策（保留 main-alpha / 接受 main / 二者合并）+ 为什么
- **重点**：把"如果官方下次又改了这里，怎么处理"讲清楚

参考：[`20260617/合并调整说明.md`](20260617/合并调整说明.md)

### 3.4 新增表说明.md（可选）

- 只在有 DB schema 变化时才写
- 每张表 / 每个字段变更单独一节
- 说明表干什么用、字段含义、是否自动迁移、对 Java 后端（Gravitex-API-End）的影响

参考：[`20260617/新增表说明.md`](20260617/新增表说明.md)

---

## 四、常见坑

### 4.1 `git commit --amend` 会丢 merge parent

如果 merge 之后需要补 stage 一些文件，**别用 `git commit --amend`**，用**新增 commit** 补——`amend` 会把 merge commit 的第二 parent 丢掉，导致 `git log HEAD..origin/main` 后续误报还有一大堆 commit 没合。

如果不小心 amend 了，可以事后用 `git merge -s ours origin/main` 补一个"仅记录 merge 关系不改内容"的 merge commit 修复。

### 4.2 silent overwrite

git merge 不冲突不代表你的改动没被覆盖。如果 main-alpha 在文件 A 加了个新字段，main 上把 A 整个重写了（但没触及那个字段所在的行），git 会 silently 保留 main 的版本、丢掉 main-alpha 的字段。

**防御方式**：合并后用 `main-alpha_独有功能清单.md` 第九节逐项核对，特别是 `ChannelOtherSettings`、`channelNonSensitiveFields`、`main.go` 的 goroutine 启动列表等容易被 silent overwrite 的地方。

### 4.3 test build failure

有时候合并后 `go build ./...` 过了但 `go test ./...` build fail——这是因为 test 文件独立编译。合并冲突常发生在实现和 test 签名不一致时（比如 main-alpha 加了 test 但 test 里用了旧签名）。

**检查方式**：`go test ./... -count=1 -run '^$'`（只 build 不跑）快速发现 test build error。

### 4.4 前端 build 依赖 dist 占位

worktree 里跑 `go build` 时，如果 `web/classic/dist/index.html` / `web/default/dist/index.html` 不存在，`//go:embed` 会失败：

```bash
mkdir -p web/classic/dist web/default/dist
touch web/classic/dist/index.html web/default/dist/index.html
```

`.gitignore` 已排除，不会被误提交。

---

## 五、历史合并索引

| 日期 | 官方 commit 数 | 主要更新 | 归档 |
|---|---|---|---|
| 2026-04-28 → 2026-06-17 | 202 | 双前端架构 / 模型性能看板 / 安全审计 / Waffo Pancake | [20260617/](20260617/) |
| 2026-06-17 → 2026-06-22 | (少量) | (见文档) | [20260622/](20260622/) |
| 2026-06-22 → 2026-07-02 | 48 | authz+SystemTaskRunner / relayconvert 重构 / channel 路由抽离 / advanced custom editor | [20260702/](20260702/) |
