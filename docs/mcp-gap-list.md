# MCP Tools Gap List

CLI/API commands that have no MCP tool equivalent yet.

## Existing Commands (pre-v1.2.0)

| CLI Command | API Endpoint | Priority |
|-------------|-------------|----------|
| `eval` | `POST /api/v1/eval` | High — agents need JS execution |
| `drag` | `POST /api/v1/drag` | Medium |
| `errors` | `GET /api/v1/errors` | Low |
| `new-page` | `POST /api/v1/pages` | Medium |
| `list-pages` | `GET /api/v1/pages` | Medium |
| `select-page` | `PUT /api/v1/pages/{index}` | Medium |
| `close-page` | `DELETE /api/v1/pages/{index}` | Low |

## New Commands (v1.2.0)

| CLI Command | API Endpoint | Priority |
|-------------|-------------|----------|
| `computed-styles` | `GET /api/v1/computed-styles` | High — most frequent operation in style pipelines |
| `computed-styles --selector-all` | `GET /api/v1/computed-styles?selector_all=` | High — bulk style verification |
| `computed-styles-diff` | `GET /api/v1/computed-styles-diff` | High — cross-page style comparison |
| `text` | `GET /api/v1/text` | High — structured text extraction |
| `wait` | `POST /api/v1/wait` | High — element readiness check |
| `scroll` | `POST /api/v1/scroll` | Medium — modal/dialog scrolling |
| `screenshot --selector-all` | `GET /api/v1/screenshot?selector_all=` | Medium — batch element screenshots |
| `screenshot --selector --full` | `GET /api/v1/screenshot?selector=&full=true` | Medium — full-height element capture |
| `font-check` | `GET /api/v1/font-check` | High — verifies actual font load status |
| `download-images` | `POST /api/v1/download-images` | Medium — download/screenshot images within elements |

## MCP Tools That Already Exist

browser_navigate, browser_click, browser_fill, browser_screenshot,
browser_get_url, browser_get_title, browser_get_html, browser_snapshot,
browser_console, browser_network, browser_press_key, browser_hover,
browser_go_back, browser_go_forward, browser_reload, browser_ask
