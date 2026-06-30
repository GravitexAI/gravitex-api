# 前端双主题运行手册

## 目录结构

```
web/
├── classic/          ← classic 主题（Semi UI，已嵌入 Go）
│   ├── src/
│   ├── rsbuild.config.ts
│   └── package.json
├── default/          ← default 主题（Base UI + TypeScript，已嵌入 Go）
│   ├── src/
│   ├── rsbuild.config.ts
│   └── package.json
├── docs/             ← 本文档目录
└── package.json      ← workspace 根，统一管理依赖
```

---

## 一、依赖安装

两套前端共用一次安装，在 `web/` 根目录执行：

```bash
cd web
bun install
```

---

## 二、本地开发（dev server）

**classic 主题**（Semi UI）：

```bash
cd web/classic
bun run dev
```

**default 主题**（TypeScript，Base UI）：

```bash
cd web/default
bun run dev
```

dev server 会反代到配置文件里的后端地址，与 Go 服务无关，不需要重新编译 Go。

---

## 三、修改后端代理地址

两套配置文件位置和格式一致：

| 主题 | 配置文件 | 修改位置 |
|---|---|---|
| classic | `web/classic/rsbuild.config.ts` | `proxyServerUrl` 变量 |
| default | `web/default/rsbuild.config.ts` | `serverUrl` 变量 |

**classic** — `web/classic/rsbuild.config.ts`：

```ts
const proxyServerUrl =
  clientServerUrl ||
  // 'http://localhost:3000'       ← 本地 Go 服务
  'http://101.47.154.214:3003'     // ← 改这里
```

**default** — `web/default/rsbuild.config.ts`：

```ts
const serverUrl =
  process.env.VITE_REACT_APP_SERVER_URL ||
  env.rawPublicVars.VITE_REACT_APP_SERVER_URL ||
  // 'http://localhost:3000'
  'http://101.47.154.214:3003'     // ← 改这里
```

代理路径：`/api`、`/mj`、`/pg`，两套相同。

也可以用环境变量临时覆盖，不修改代码：

```bash
VITE_REACT_APP_SERVER_URL=http://localhost:3000 bun run dev
```

---

## 四、生产打包顺序

两个前端都必须先 build，Go 才能编译成功（embed 指令会在编译时读取 dist 目录，目录不存在则报错）。

```bash
# 第一步：构建 classic
cd web/classic
bun run build          # 产物输出到 web/classic/dist/

# 第二步：构建 default
cd ../default
bun run build          # 产物输出到 web/default/dist/

# 第三步：回到项目根，打包 Go 二进制
cd ../..
export GOOS=linux && export GOARCH=amd64 && export CGO_ENABLED=0
go build -o gravitesx_llm main.go
```

---

## 五、Go embed 说明

`main.go` 同时嵌入了两套主题：

```go
//go:embed web/default/dist
var buildFS embed.FS

//go:embed web/default/dist/index.html
var indexPage []byte

//go:embed web/classic/dist
var classicBuildFS embed.FS

//go:embed web/classic/dist/index.html
var classicIndexPage []byte
```

两套 dist 都打包进了 Go 二进制，运行时按主题配置决定 serve 哪一套，无需额外文件。

---

## 六、运行时切换主题

Go 服务支持**不重启**热切换主题，切换逻辑由数据库 `options` 表的 `theme` 配置项驱动（默认值 `classic`）。

### 方式一：管理后台（推荐）

系统设置 → 前端主题 → 选 `default` 或 `classic` → 保存，立即生效。

### 方式二：直接改数据库

在 `options` 表中找到 `key = 'theme'` 的行，将 value 改为：

```json
{"frontend":"default"}
```

修改后重启 Go 服务使配置生效。

---

## 七、nginx 配置变更

前端已通过 embed 打包进 Go 二进制，Go 服务自己 serve 静态文件，**nginx 不再需要托管静态文件目录**。

### 以前（前后端分离部署）

```
用户请求 → nginx
  ├── 静态文件（/、/console 等）→ 从 /usr/share/nginx/dist/ 读文件返回
  └── API 请求（/api、/mj 等）→ 反代到 Go 服务
```

### 现在（embed 部署）

```
用户请求 → nginx → 全部反代到 Go 服务
                     ├── 静态文件请求 → Go 从内存返回（embedded）
                     └── API 请求    → Go 处理业务逻辑
```

### nginx 配置

删掉原来的 `root` 和 `try_files`，改为全部反代给 Go：

```nginx
server {
    listen 443 ssl;
    # ... ssl 证书配置 ...

    location / {
        proxy_pass http://localhost:3000;   # 全部给 Go，包括静态文件
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

`/usr/share/nginx/dist`（或之前的 `admin_api`）目录以后无需维护，不再上传前端产物到服务器。
