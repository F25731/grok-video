# Grok Video Wrapper

NewAPI to Grok video wrapper.

## Endpoints

Recommended per-call billing route:

```text
GET  /v1/models
POST /v1/images/generations
POST /v1/images/edits
```

Legacy NewAPI video task route:

```text
POST /v1/videos
GET  /v1/videos/{task_id}
GET  /v1/videos/{task_id}/content
POST /v1/video/generations
GET  /v1/video/generations/{task_id}
```

## NewAPI Config

For fixed per-call billing, configure this wrapper as an OpenAI-compatible image channel in NewAPI:

```text
Base URL: http://YOUR_SERVER:19484/v1
API Key: WRAPPER_API_KEY
Models: grok-image-video,grok-video-1.5
```

Set `ModelPrice` to the price for one generation. NewAPI image routes do not multiply the price by `seconds`.

Requests can still include video fields:

```json
{
  "model": "grok-image-video",
  "prompt": "make a short product video",
  "size": "16:9",
  "image": "https://example.com/reference.png",
  "extra_fields": {
    "seconds": "10",
    "resolution": "720p"
  }
}
```

When calling through NewAPI image routes, put video-only parameters such as `seconds`, `duration`, `aspect_ratio`, and `resolution` into `extra_fields`. NewAPI's image request object forwards known image fields plus `extra_fields`, but it may drop unknown top-level fields before the request reaches this wrapper.

Reference images are accepted from `image`, `images`, `input_image`, `input_images`, `reference_images`, `image_url`, `imageUrls`, and the same keys inside `extra_fields`. Multipart `/v1/images/edits` uploads are also supported; uploaded files are converted to data URLs before being sent upstream.

The wrapper submits a real upstream video task, keeps the HTTP connection alive while polling, and returns the final MP4 URL as an OpenAI image response:

```json
{
  "created": 1783510926,
  "data": [
    {
      "url": "https://example.com/result.mp4"
    }
  ]
}
```

On upstream failure or timeout, the wrapper returns an OpenAI-style error body so NewAPI can treat the generation as failed:

```json
{
  "error": {
    "message": "upstream failure reason",
    "type": "invalid_request_error",
    "code": "upstream_failed"
  }
}
```

## Admin

```text
http://YOUR_SERVER:19484/admin
```

The admin page configures the upstream API key and shows real task counts separately from worker HTTP job counts.

## Env

```env
WRAPPER_API_KEY=key-for-newapi
UPSTREAM_BASE_URL=https://api.119337.xyz
UPSTREAM_API_KEY=upstream-key
ADMIN_USERNAME=admin
ADMIN_PASSWORD=admin-password
MAX_WORKERS=2000
MAX_QUEUE=50000
REQUEST_TIMEOUT_SECONDS=300
HTTP_TIMEOUT_SECONDS=60
```
