# gin-scheduler-json

> 🧭 **A lightweight JSON-based task scheduler built with Go + Gin.**  
> 一款基于 **Go + Gin** 的轻量级任务调度工具，通过 **JSON 文件持久化任务**，支持周期性执行、暂停、删除等操作。

---

## 🚀 功能概述

`gin-scheduler-json` 是一个简单易用的 **定时任务调度服务**，主要用于周期性地向指定 API 发送请求（GET 或 POST）。  
所有任务都存储在本地 `config.json` 文件中，无需数据库，**单文件即可运行**。

**主要特性：**
- 🧩 **轻量独立**：仅一个可执行文件 + 一个 JSON 配置文件  
- 🔁 **循环任务**：按秒周期性执行 HTTP 请求  
- 💾 **自动持久化**：任务信息保存在 `config.json` 中  
- ♻️ **自动恢复**：重启后自动恢复已启用任务  
- ⚙️ **配置端口**：服务端口可在 `config.json` 中修改（默认 `9000`）  
- 🔒 **零依赖部署**：无需任何外部服务或数据库  

---

## 📦 快速开始

```bash
# 编译
go build -trimpath -ldflags "-s -w" -o scheduler

# 运行（当前目录会自动生成 config.json）
./scheduler
```

首次运行自动生成：
```json
{
  "version": 1,
  "port": 9000,
  "tasks": []
}
```

---

## 🌐 API 简介

> 除查询接口（GET）外，其余操作均为 **POST + URL**。  
> 仅 `POST /tasks/add` 需要请求体。

### ✅ 健康检查
- `GET /healthz`  
- `POST /healthz`

返回：
```json
{"ok": true}
```

---

### ➕ 添加任务
- `POST /tasks/add`
```json
{
  "interval_seconds": 5,
  "url": "https://example.com/api",
  "method": "GET",
  "description": "每5秒请求一次API"
}
```
返回：
```json
{"id": "t-20251026T104512..."}
```

---

### 📋 查询全部任务
- `GET /tasks`

### 🔍 查询单个任务
- `GET /tasks/{id}`

---

### ⏸️ 暂停任务
- `POST /tasks/{id}/pause`

### ❌ 删除任务
- `POST /tasks/{id}/delete`

---

## 🧪 示例

```bash
# 添加任务
curl -X POST http://127.0.0.1:9000/tasks/add   -H "Content-Type: application/json"   -d '{"interval_seconds":5,"url":"https://httpbin.org/get","method":"GET","description":"探活"}'

# 查询全部任务
curl http://127.0.0.1:9000/tasks

# 暂停任务
curl -X POST http://127.0.0.1:9000/tasks/<id>/pause

# 删除任务
curl -X POST http://127.0.0.1:9000/tasks/<id>/delete
```

---

## 🧰 应用场景
- 定时探活 / Ping 外部服务  
- 简单 HTTP API 调度  
- 周期性同步或触发任务  
- 快速验证定时任务逻辑（无需部署复杂 cron）  

---

## 📝 License
MIT
