# Grok Video Wrapper

NewAPI 上游 wrapper，先接入 Google 文档里的 `https://api.119337.xyz` Grok 视频接口。

## 对 NewAPI 暴露

```text
GET  /v1/models
POST /v1/videos
GET  /v1/videos/{task_id}
GET  /v1/videos/{task_id}/content
```

额外保留便于测试的入口，返回仍是 OpenAI Video 结构：

```text
POST /v1/video/generations
GET  /v1/video/generations/{task_id}
```

## NewAPI 后台配置

渠道类型选择 `OpenAI`，不要选其它视频专用渠道。

```text
Base URL: http://你的wrapper地址/v1
API Key: WRAPPER_API_KEY
模型: grok-image-video,grok-video-1.5
```

模型价格在 NewAPI 的 `ModelPrice` 里按次配置，例如：

```json
{
  "grok-image-video": 0.35,
  "grok-video-1.5": 0.45
}
```

## 计费和失败退款

这个 wrapper 按 NewAPI 原生异步任务模式工作：

- 创建任务成功后立即返回 `queued` 和上游 `task_id`。
- NewAPI 保存任务并按自己的任务轮询逻辑查询 `/v1/videos/{task_id}`。
- 上游成功时 wrapper 返回 `status: "completed"`。
- 上游失败时 wrapper 返回 `status: "failed"` 和 `error.message`，NewAPI 的异步失败逻辑会退款。
- NewAPI 获取视频内容时会请求 `/v1/videos/{task_id}/content`，wrapper 会从上游 `result_url` 拉取并转发 mp4。

## 上游模型规则

`grok-image-video`

- 支持文生视频、单参考图、多参考图。
- 最多 7 张参考图。
- 文生/单图最长 15 秒，多图最长 10 秒。
- 支持比例：`1:1`、`16:9`、`9:16`、`4:3`、`3:4`、`3:2`、`2:3`。

`grok-video-1.5`

- 只支持 1 张参考图。
- 最长 15 秒。
- 支持比例：`16:9`、`9:16`。

## 部署

```bash
cp .env.example .env
docker compose up -d --build
```

默认宿主机端口是 `19484`，可在 `.env` 里加：

```env
HOST_PORT=19484
```
