# srchdsh

[English](README.md) | 中文

面向 [DSH](https://github.com/deepseek-ai/deepseek-harness) 的元搜索后端，fork 自
cmcevoy 的 [srchd](https://git.sr.ht/~cmcevoy/srchd)。

它同时查询多个引擎，合并并排序结果，输出一份 JSON 文档。`srchdsh search` 是一次性
命令，因此 DSH 的 Web 接缝可以在每次查询时直接运行它，无需常驻进程；`srchdsh serve`
通过 HTTP 暴露同一份文档，供第二个调用方使用。

srchdsh 不是给人用的搜索引擎。原项目的 Web 界面、模板、静态文件和设置页面都已移除；
唯一的契约就是下面描述的这份文档。

## 构建

```sh
go build -o srchdsh .
```

## 用法

```sh
# stdout 输出一份 JSON 文档，诊断信息走 stderr。
srchdsh search -q 'deepseek harness' | jq

# stdout 是终端时输出人类可读的表格；-json / -table 可强制指定。
srchdsh search -q 'deepseek harness' -max 5

# 限定引擎（沿用 srchd 的冒号操作符）。
srchdsh search -q ':ddg deepseek harness'

# 可选的 HTTP 模式，输出同一份文档。
srchdsh serve -addr 127.0.0.1:8089
curl -s localhost:8089/v1/search -d '{"query":"deepseek harness","maxResults":5}'

srchdsh engines          # 当前配置下启用的引擎
srchdsh version
```

### 在 Node 中使用

`js/` 是一个无依赖的 ESM 包，负责运行二进制并返回文档：

```js
import { available, search } from 'srchdsh';

if (!available()) throw new Error('srchdsh is not on PATH');

const doc = await search('deepseek harness', { max: 5, signal });
```

它把 `AbortSignal` 转发给子进程，并在所有引擎都失败时把文档附加到错误上，因此每个
引擎的失败原因不会丢失。安装方式为 `npm install ./js`，或直接导入 `./js/index.js`。
相比 CLI 的额外开销约为每次搜索 11 毫秒。

`plugin/` 是建立在该绑定之上的 DSH 提供方：它在 `ctx.web` 接缝上注册 `srchdsh`。
配置条目以及取代 DeepSeek 服务端搜索的固定绑定见
[plugin/README.md](plugin/README.md)。

### 返回的文档

```json
{
  "query": "deepseek harness",
  "tookMs": 612,
  "truncated": false,
  "sources": [
    {
      "url": "https://www.deepseek.com/en/harness/",
      "title": "DeepSeek Harness | Explore the limits of intelligence",
      "snippet": "Use DeepSeek Harness to work with documents, analyze spreadsheets, …"
    }
  ],
  "engines": [
    { "id": "ddg", "ms": 431, "count": 10 },
    { "id": "startpage", "ms": 502, "count": 0, "error": "captcha" }
  ]
}
```

- `sources` 已经去重并排序，并已按 `-max`（HTTP 下为 `maxResults`）截断；
  `truncated` 记录的是确实丢弃了内容。
- `publishedAt` 只在引擎提供日期时出现；没有引擎会凭空编造日期。
- `engines` 报告每个被选中的引擎，包括失败的引擎，因此慢引擎或坏引擎在同一次调用里
  就能看出来。错误是简短的机器可读字符串：`captcha`、`timeout`、`http 429`、`error`。

### 退出码

| 退出码 | 含义 |
|---|---|
| 0 | 至少有一个来源 |
| 1 | 没有来源，但也没有硬性失败 |
| 2 | 失败：参数错误、没有启用任何引擎，或所有引擎都失败 |

## 配置

YAML 格式，存在时从 `$XDG_CONFIG_HOME/srchdsh/config.yaml` 读取，或用 `-conf PATH`
指定路径。两者都没有时使用默认值：默认引擎集合，没有黑名单，没有重写规则。

示例见 [`docs/config.yaml.example`](docs/config.yaml.example)，各字段的说明见
[`docs/config.md`](docs/config.md)。

## 引擎

| 引擎 | 说明 |
|---|---|
| DuckDuckGo | 可用 |
| Startpage | Google 的索引；会解其 Anubis 工作量证明，然后缓存 Cookie 五分钟 |
| Mojeek | 自有索引；会解 Altcha 挑战，只有第一页，且 `robots.txt` 禁止 `/search` |
| Bing | 通过其 RSS 源工作，这是它仅剩的结构化输出；只有第一页 |
| Brave | 默认关闭：上游解析器取不到结果 |
| Google | 默认关闭：先是同意墙，然后是 JavaScript 挑战 |
| Marginalia、Wiby、Yahoo、Yandex | 上游引擎，未改动 |
| Mediawiki | 需要配置 |

`srchdsh engines` 会打印当前启用的集合。

### 有意不支持的引擎

以下每一条都在本代码树上实测过；请求结果来自 2026-10-24。

- **Qwant** —— 即便带上它自家单页应用设置的 Cookie，`api.qwant.com` 仍返回
  `403 {"error_code":50}`，而且 `lite.qwant.com` 现在会跳转到那个单页应用。拦截方是
  DataDome，一款带设备指纹的付费反爬服务，不是循环能解开的谜题。
- **Ecosia** —— 对普通 HTTP 客户端返回 `403`，即 "Ecosia Firewall"。它的网页结果本来
  就来自 Bing，所以索引基本不会有什么变化。
- **MetaGer** —— 不再是免费的元搜索：网站已变成预付费的「密钥」产品，前端建立在
  Startpage 的模板之上。它只会在 Startpage 已经覆盖的索引前多一道付费墙。
- **Google CSE** —— SearXNG 使用的无密钥 CSE 技巧需要共享的第三方 CSE id 及其公共
  配额；Startpage 不需要这两样就能提供同一份索引。
- **百度** —— 从欧洲地址访问时，桌面端和移动端、中文查询和英文查询都会被重定向到
  `wappass.baidu.com` 的图形验证码（「百度安全验证」）。它的结果链接还是跳转包装
  （`/link?url=…`），每条引用都要多花一次请求。它的索引以中文为先：即便是英文查询，
  返回的也多是中文页面。
- **搜狗 / 360** —— 搜狗返回 `399` 和反爬页面；360（so.com）只正常返回过一次结果页，
  之后便进入重定向循环。两者都是中文索引引擎，英文查询的问题与百度相同。

只有当 Qwant 或 Ecosia 提供官方 API、买下 MetaGer 的密钥，或通过百度付费的搜索
API 时，才值得重新考虑。中文查询并不需要它们：DuckDuckGo、Startpage 和 Yahoo 已经能
为中文查询返回中文来源（百度百科、中文维基）。

## 许可证与来源

AGPL-3.0。本项目是 srchd 的衍生作品；上游提交信息见 [`LICENSE`](LICENSE) 与
[`NOTICE`](NOTICE)。不可再许可为其他许可证。
