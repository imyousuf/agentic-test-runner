# MCP Tools Gap List

CLI/API commands that have no MCP tool equivalent yet.

## Status

All previously identified gaps have been closed. The MCP server now exposes 30 tools
covering navigation, page management, interaction, inspection, AI, and debugging.

## Remaining Gaps

| CLI Command | API Endpoint | Notes |
|-------------|-------------|-------|
| `batch` | N/A (CLI-only) | CLI pipeline mode — no server equivalent by design |

## MCP Tools (30 total)

browser_navigate, browser_go_back, browser_go_forward, browser_reload,
browser_new_page, browser_list_pages, browser_select_page, browser_close_page,
browser_click, browser_fill, browser_hover, browser_press_key, browser_drag,
browser_wait, browser_scroll,
browser_get_url, browser_get_title, browser_get_html, browser_snapshot,
browser_screenshot, browser_eval, browser_computed_styles,
browser_computed_styles_diff, browser_text, browser_clean_snapshot,
browser_font_check, browser_viewport, browser_download_images,
browser_ask,
browser_console, browser_network, browser_errors
