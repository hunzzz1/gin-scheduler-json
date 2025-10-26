# gin-scheduler-json

> A tiny, zero-dependency task scheduler built with **Go + Gin**, persisting jobs to a **JSON file**.  
> 一款 **Go + Gin** 开发、**JSON 文件**持久化的轻量级定时任务服务，零依赖、单文件二进制即可部署。

## ✨ Highlights
- **One binary, one JSON**：丢到任意目录即可运行，`config.json` 同目录生成/读取  
- **POST 优先的简洁 API**：除查询外，全部用 `POST + URL`（添加任务除外需 JSON）  
- **Auto-recover**：重启后自动恢复 `enabled=true` 的任务  
- **Zero deps**：只依赖标准库 + Gin  
- **Config-driven port**：端口在 `config.json` 配置，默认 `9000`

## 📦 Quick Start

```bash
# 初始化
go mod init gin-scheduler-json
go get github.com/gin-gonic/gin

# 构建
go build -trimpath -ldflags "-s -w" -o scheduler

# 运行（当前目录将生成 config.json）
./scheduler
```

首次运行会生成最小配置：
```json
{
  "version": 1,
  "port": 9000,
  "tasks": []
}
```

## ⚙️ Configuration (`config.json`)
- `port`: 服务监听端口（默认 9000）
- `tasks`: 任务数组（`enabled:true` 重启后自动启动）

任务项结构：
```json
{
  "id": "自动生成",
  "interval_seconds": 5,
  "url": "https://example.com/ping",
  "method": "GET",
  "description": "每5秒探活",
  "enabled": true,
  "created_at": "自动填充",
  "updated_at": "自动填充"
}
```

## 🌐 API
> 仅查询使用 GET，其它均为 POST + URL。
> 添加任务是唯一需要 JSON 请求体 的接口。

### 健康检查
- `GET /healthz`
- `POST /healthz`
**返回**：`{"ok": true}`

### 添加任务
- `POST /tasks/add`
```json
{
  "interval_seconds": 5,
  "url": "https://httpbin.org/get",
  "method": "GET",
  "description": "每5秒请求一次 httpbin"
}
```

### 查询全部任务
- `GET /tasks`

### 查询单个任务
- `GET /tasks/{id}`

### 暂停任务
- `POST /tasks/{id}/pause`

### 删除任务
- `POST /tasks/{id}/delete`

## 🧪 cURL 示例

```bash
# 健康检查
curl -s http://127.0.0.1:9000/healthz

# 添加任务
curl -X POST http://127.0.0.1:9000/tasks \
  -H "Content-Type: application/json" \
  -d '{"interval_seconds":3,"url":"https://httpbin.org/get","method":"GET","description":"探活"}'

# 查询全部
curl -s http://127.0.0.1:9000/tasks | jq

# 暂停任务
curl -X POST http://127.0.0.1:9000/tasks/<id>/pause

# 删除任务
curl -X POST http://127.0.0.1:9000/tasks/<id>/delete
```

## 🛠️ Build & Run

```bash
go build -trimpath -ldflags "-s -w" -o scheduler
./scheduler
```

后台运行：
```bash
nohup ./scheduler > run.log 2>&1 &
tail -f run.log
```

## 📝 License
MIT
