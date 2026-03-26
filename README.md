# BetGame

实时多人对赌游戏。每 10 秒结算一轮，50/50 胜负 — 赢了翻倍，输了归零。

## 技术栈

- **后端:** Go + [Chi](https://github.com/go-chi/chi) 路由
- **数据库:** SQLite（[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)，纯 Go 实现，无需 CGO）
- **前端:** 原生 JS + Tailwind CSS
- **认证:** 邮箱 OTP（通过 [Resend](https://resend.com)）+ [Cloudflare Turnstile](https://www.cloudflare.com/products/turnstile/) 人机验证 + JWT

## 功能

- 邮箱验证码登录
- 10 秒一轮的实时对赌
- 实时排行榜与本轮下注列表
- 网络延迟指示器
- 余额不足时可重置

## 快速开始

### 前置条件

- Go 1.21+
- [Resend](https://resend.com) API Key（用于发送验证码邮件）
- [Cloudflare Turnstile](https://dash.cloudflare.com/?to=/:account/turnstile) Site Key + Secret Key

### 部署

```bash
git clone <repo-url> && cd bet-game

# 配置环境变量
cp .env.example .env
# 编辑 .env，填入你的密钥

# 编译运行
go build -o bet-game .
./bet-game
```

服务启动在 `http://localhost:4444`（端口可在 `.env` 中配置）。

### 环境变量

| 变量 | 说明 |
|---|---|
| `RESEND_KEY` | Resend API 密钥，用于发送 OTP 邮件 |
| `TURNSTILE_SECRET` | Cloudflare Turnstile 服务端密钥 |
| `TURNSTILE_SITEKEY` | Cloudflare Turnstile 客户端站点密钥 |
| `JWT_SECRET` | JWT 签名密钥 |
| `DB_PATH` | SQLite 数据库文件路径（默认 `./game.db`） |
| `PORT` | 服务端口（默认 `4444`） |

## 项目结构

```
.
├── main.go          # 服务入口、路由、游戏循环（10 秒一轮）
├── auth.go          # JWT 中间件、OTP 生成、Turnstile 验证、频率限制
├── handlers.go      # HTTP 处理器（认证、下注、排行榜、游戏状态）
├── db.go            # SQLite 建表初始化
├── public/
│   └── index.html   # 单页前端
├── go.mod
├── .env.example
└── .gitignore
```

## 玩法

1. 通过邮箱验证码登录
2. 每轮 10 秒倒计时，在结算前下注
3. 结算：50% 概率 **胜利**（下注金额 x2 返还）或 **落败**（下注金额扣除）
4. 排行榜实时更新，所有玩家可看到本轮下注情况

## 许可证

MIT
