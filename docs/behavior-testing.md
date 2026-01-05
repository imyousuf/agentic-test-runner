# Browser Behavior Testing

ATR can run browser-based behavior tests using AI-driven automation. Write tests in natural language and let the AI execute them.

## Overview

Unlike traditional browser testing frameworks that require precise selectors and step-by-step code, ATR uses an AI agent to:

1. **Read** your natural language test specification
2. **Interpret** what actions to perform
3. **Execute** using browser automation tools
4. **Analyze** failures and provide recommendations

This approach is more flexible and resilient to UI changes.

## Writing Test Files

Test files use a simple `.test.txt` format with natural language instructions.

### Basic Structure

```
Test: <test name>

Prerequisites:
- <prerequisite 1>
- <prerequisite 2>

Steps:
1. <step 1>
2. <step 2>
3. <step 3>

Expected Results:
- <expected result 1>
- <expected result 2>
```

### Example: Login Test

```
Test: User can log in with valid credentials

Prerequisites:
- Application running at http://localhost:3000
- Test user exists: test@example.com / password123

Steps:
1. Navigate to /login
2. Enter "test@example.com" in the email field
3. Enter "password123" in the password field
4. Click the "Sign In" button
5. Wait for the dashboard to load

Expected Results:
- URL contains /dashboard
- Welcome message is visible
- No console errors
```

### Example: E-commerce Flow

```
Test: Add item to cart and checkout

Steps:
1. Navigate to https://shop.example.com
2. Search for "wireless headphones"
3. Click on the first product result
4. Click "Add to Cart" button
5. Click the cart icon
6. Verify the cart shows 1 item
7. Click "Proceed to Checkout"

Expected Results:
- Cart total is greater than $0
- Checkout page is displayed
```

### Example: Form Validation

```
Test: Registration form validates required fields

Steps:
1. Navigate to /register
2. Leave all fields empty
3. Click the "Create Account" button
4. Check for validation errors

Expected Results:
- Email field shows "Email is required" error
- Password field shows "Password is required" error
- Form is not submitted
```

## Running Tests

### Single Test File

```bash
atr run --behavior tests/login.test.txt
```

### Directory of Tests

```bash
atr run --behavior tests/e2e/
```

All `.test.txt` files in the directory will be executed.

### With Base URL

```bash
atr run --behavior tests/e2e/ --browser-url http://localhost:3000
```

The base URL is used for relative navigation (e.g., `/login` becomes `http://localhost:3000/login`).

## CLI Options

| Flag | Description | Default |
|------|-------------|---------|
| `--behavior <path>` | Test file or directory | (required) |
| `--browser-url <url>` | Base URL for tests | from config |
| `--headless` | Run browser headless | `true` |
| `--viewport <WxH>` | Viewport size | `1920x1080` |
| `--cdp-endpoint <url>` | Connect to existing browser | - |

### Non-Headless Mode

For debugging, run with a visible browser:

```bash
atr run --behavior tests/login.test.txt --headless=false
```

### Mobile Viewport

Test mobile layouts:

```bash
atr run --behavior tests/mobile.test.txt --viewport 375x667
```

### Connect to Existing Browser

For debugging or using browser DevTools:

1. Launch Chrome with remote debugging:
   ```bash
   google-chrome --remote-debugging-port=9222
   ```

2. Connect ATR:
   ```bash
   atr run --behavior tests/debug.test.txt --cdp-endpoint ws://localhost:9222
   ```

## How It Works

### AI Execution Flow

1. **Test Parsing**: ATR reads the `.test.txt` file as-is
2. **Browser Launch**: Chromium is launched (or connected via CDP)
3. **AI Agent Loop**:
   - Agent calls `browser_snapshot` to see page elements
   - Agent interprets the next test step
   - Agent executes using browser tools (click, fill, etc.)
   - Agent verifies expected results
4. **Result Reporting**: Success/failure with analysis

### Browser Tools Available

The AI agent has access to these browser automation tools:

#### Navigation
| Tool | Description |
|------|-------------|
| `browser_navigate` | Navigate to URL, back, forward, reload |
| `browser_new_page` | Open new tab |
| `browser_list_pages` | List all open tabs |
| `browser_select_page` | Switch to tab |
| `browser_close_page` | Close tab |
| `browser_wait_for` | Wait for text to appear |

#### Input
| Tool | Description |
|------|-------------|
| `browser_click` | Click on element |
| `browser_fill` | Type into input/textarea/select |
| `browser_hover` | Hover over element |
| `browser_press_key` | Press keyboard key |
| `browser_drag` | Drag element |
| `browser_upload_file` | Upload file |
| `browser_handle_dialog` | Handle alert/confirm/prompt |

#### Inspection
| Tool | Description |
|------|-------------|
| `browser_snapshot` | Get page elements with UIDs |
| `browser_screenshot` | Capture screenshot |
| `browser_evaluate` | Run JavaScript |
| `browser_list_console` | View console messages |
| `browser_list_network` | View network requests |

#### Emulation
| Tool | Description |
|------|-------------|
| `browser_resize` | Change viewport size |
| `browser_emulate` | Network/CPU throttling |

### Element Resolution

The AI finds elements using multiple strategies:

1. **Accessible name**: `[aria-label="Sign In"]`, button text
2. **Test ID**: `[data-testid="submit-btn"]`
3. **Name attribute**: `[name="email"]`
4. **Placeholder**: `[placeholder="Enter email"]`
5. **Text content**: Element containing "Sign In"
6. **CSS selector**: Direct selector like `#submit`

**Best Practice**: Use `aria-label` or `data-testid` for reliable element targeting.

## Failure Analysis

When a test fails, ATR captures:

- **Screenshot**: Current page state
- **Console logs**: JavaScript errors and warnings
- **Network requests**: Failed or pending requests
- **DOM snapshot**: Page HTML

The AI analyzes this context and provides:

- **Root cause**: What went wrong
- **Recommendations**: How to fix it

### Example Failure Output

```
======================================================================
ANALYSIS RESULTS
======================================================================

Status: FAILURE

Summary:
  The test failed at step 4 "Click the Sign In button". The button
  was not found on the page.

Root Cause:
  The login form uses "Log In" as the button text, not "Sign In".
  The element was not found because the text didn't match.

Recommendations:
  1. Update the test to use "Log In" instead of "Sign In"
  2. Or add aria-label="Sign In" to the button for reliable targeting
  3. Consider using data-testid for test stability

Steps Executed:
  1. ✓ Navigate to /login
  2. ✓ Enter "test@example.com" in email field
  3. ✓ Enter "password123" in password field
  4. ✗ Click "Sign In" button - Element not found
```

## Configuration

Configure behavior testing in `~/.atr/config.yaml`:

```yaml
behavior:
  base_url: "http://localhost:3000"

  browser:
    executable: "auto"    # or path to browser
    headless: true
    viewport:
      width: 1920
      height: 1080
    page_timeout: "30s"
    action_timeout: "10s"
    slow_motion: "0s"     # Set to "500ms" for debugging

  capture:
    screenshots: true
    console_logs: true
    network_har: true
    dom_snapshot: true
```

## Best Practices

### 1. Use Clear, Specific Descriptions

```
# Good
Click the "Add to Cart" button in the product details section

# Less clear
Click add to cart
```

### 2. Add Waits for Dynamic Content

```
Steps:
1. Click "Load More" button
2. Wait for "Loading..." to disappear
3. Verify new items are visible
```

### 3. Use Test IDs for Critical Elements

Add `data-testid` to important elements:
```html
<button data-testid="checkout-btn">Checkout</button>
```

Then reference in tests:
```
Click the checkout button (data-testid: checkout-btn)
```

### 4. Keep Tests Focused

One test = one user flow. Don't combine unrelated scenarios.

### 5. Use Prerequisites

Document what must be true before the test runs:
```
Prerequisites:
- User is logged out
- Shopping cart is empty
- Test products exist in database
```

### 6. Organize Tests by Feature

```
tests/
  e2e/
    auth/
      login.test.txt
      logout.test.txt
      password-reset.test.txt
    checkout/
      add-to-cart.test.txt
      payment.test.txt
    search/
      basic-search.test.txt
      filters.test.txt
```

## Troubleshooting

### Browser Won't Launch

- Check if Chromium was downloaded: `ls ~/.cache/rod/browser/`
- Try specifying browser path in config
- On Linux, install dependencies: `apt install libnss3 libatk1.0-0 libatk-bridge2.0-0`

### Element Not Found

- Run with `--headless=false` to see the page
- Check if element is in an iframe
- Add wait steps for dynamic content
- Use more specific element descriptions

### Timeouts

Increase timeouts in config:
```yaml
behavior:
  browser:
    page_timeout: "60s"
    action_timeout: "30s"
```

### Flaky Tests

- Add explicit waits: "Wait for the loading spinner to disappear"
- Use `data-testid` for element targeting
- Check for race conditions in your application
