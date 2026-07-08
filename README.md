# Grok Video Wrapper

给 NewAPI 使用的 Grok 视频中转 wrapper，当前接入 `https://api.119337.xyz` 的视频任务接口。

## 后台

启动后打开：

```text
http://服务器:19484/admin
```

后台可配置上游 API Key，并查看 NewAPI 接入信息、模型列表和 worker 状态。上游密钥保存到 `/data/config.json`，保存后下一次请求立即生效，不需要重启容器。

## 暴露给 NewAPI

```text
GET  /v1/models
POST /v1/videos
GET  /v1/videos/{task_id}
GET  /v1/videos/{task_id}/content
```

额外保留测试入口：

```text
POST /v1/video/generations
GET  /v1/video/generations/{task_id}
```

## NewAPI 后台配置

渠道类型选择 `OpenAI`：

```text
Base URL: http://你的wrapper地址/v1
API Key: WRAPPER_API_KEY
模型: grok-image-video,grok-video-1.5
```

NewAPI 当前会按视频请求里的 `seconds` 参与计费；如果希望“每次生成”固定价格，需要把这里的单价按预计秒数折算成每秒价。例如希望 4 秒视频总价 0.35，则模型价格填 0.0875。

```json
{
  "grok-image-video": 0.0875,
  "grok-video-1.5": 0.1125
}
```

## 计费和失败退款

wrapper 使用 NewAPI 原生异步任务模式：

- 创建任务成功后立即返回 `queued` 和上游 `task_id`。
- NewAPI 保存任务，并按自己的任务轮询逻辑查询 `/v1/videos/{task_id}`。
- wrapper 后台也会用同一个上游 `task_id` 自动轮询，实时展示总任务、排队中、运行中、完成、失败和最近任务列表；worker 完成数只表示 HTTP 工作执行次数，不再作为任务完成数展示。
- 上游成功时 wrapper 返回 `status: "completed"`。
- 上游失败时 wrapper 返回 `status: "failed"` 和 `error.message`，NewAPI 的异步失败逻辑会退款。
- NewAPI 获取视频内容时请求 `/v1/videos/{task_id}/content`，wrapper 从上游 `result_url` 拉取并转发 mp4。

## 并发

默认配置：

```env
MAX_WORKERS=2000
MAX_QUEUE=50000
```

创建任务、轮询任务、下载视频内容都会经过 worker 池。队列满时返回 429。

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
