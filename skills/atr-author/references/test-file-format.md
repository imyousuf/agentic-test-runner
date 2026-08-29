# ATR Behavior Test File Format

Syntax reference for `.test.txt` behaviour specs. The judgement — which
assertions are worth writing, and which ones lie — is in the skill itself
(`../SKILL.md`). This file covers the shape.

The whole file is passed to the compiler verbatim. ATR does not parse section
headings; they are a convention that helps the model, and you.

## Basic Structure

```
Test: <descriptive test name>

Prerequisites:
- <condition that must be true>

Setup:
- <anything the test must create or reset first>

Steps:
1. <first action>
2. <second action>

Expected Results:
- <what would be different if the feature were broken>

Notes for the compiler:
- <what the model cannot learn by looking at one page>
```

## Sections

### Test (required)

Single line describing what the test validates.

```
Test: User can log in with valid credentials
Test: Shopping cart updates quantity correctly
Test: Search filters results by category
```

### Prerequisites (optional)

Conditions that must be true before test runs. Helps AI understand context.

```
Prerequisites:
- Application running at http://localhost:3000
- User is logged out
- Shopping cart is empty
- Test user exists: test@example.com / password123
- Product catalog has at least 5 items
```

### Steps (required)

Numbered actions to perform. Use natural language.

```
Steps:
1. Navigate to /login
2. Enter "test@example.com" in the email field
3. Enter "password123" in the password field
4. Click the "Sign In" button
5. Wait for the dashboard to load
```

### Setup (optional)

Anything the test must create or reset before the steps run. Compiles to
`atr.setup(...)`, which runs before the steps on every execution, is not
counted as a step, and reports a failure as the fixture failing rather than the
application misbehaving.

A compile drives the spec **more than once**, so a test that destroys what it
depends on needs this.

```
Setup:
- Create a project named "atr-fixture" if one does not already exist
- Empty the cart
```

### Expected Results (required)

What must be true after the steps — specifically, what would be **different**
if the feature were broken.

```
Expected Results:
- URL contains /dashboard
- The header greeting shows the signed-in user's name
- The navigation has a "Logout" item and no "Sign in" item
```

Name the element, not the page: a whole-page text match passes for the wrong
reason. Assert absence as well as presence. Count relative to before, never
absolutely.

`No console errors` and `No failed network requests` compile to real checks
(`atr.consoleErrors()`, `atr.failedRequests()`) and are worth adding — but they
are true of most broken pages, so they must never be the *only* expectations.

### Notes for the compiler (optional, and the one that matters)

Everything the model cannot learn from a single page: app-specific recipes,
which of several similar controls is correct, state carried between pages,
whatever you discovered by watching a compile fail.

```
Notes for the compiler:
- The description field is a rich-text editor and ignores a plain fill; click
  it, then type, then blur it before reading the value back.
- There are two "Save" buttons; the one in the dialog footer is the one that
  submits.
```

Every note should be a trap that produced a wrong result — notes are prompt,
and padding costs compile iterations.

### Test inputs

Values the test needs but should not hardcode go in `<spec>.test.properties`,
written by the compiler and committed:

```
base_url = http://localhost:3000
username = demo@example.com
expected_row_count = 3
```

`<spec>.test.override.properties` (gitignored) wins over it, and `ATR_VALUE_*`
environment variables win over both. Values support `$(command)` and `${VAR}`,
expanded when read — which means **a committed properties file executes**, on
every machine including CI.

A missing input is the `config` failure kind: not repairable, not retryable,
never sent to the model. Someone has to decide what the value should be.

## Writing Effective Steps

### Navigation

```
Navigate to /products
Navigate to https://example.com/checkout
Go back to the previous page
Reload the current page
```

### Clicking

```
Click the "Sign In" button
Click on "Add to Cart"
Click the submit button
Click the element with data-testid "checkout-btn"
Click the first search result
```

### Form Input

```
Enter "user@example.com" in the email field
Type "password123" in the password input
Fill the search box with "wireless headphones"
Select "United States" from the country dropdown
Check the "Remember me" checkbox
```

### Waiting

```
Wait for the page to load
Wait for "Loading..." to disappear
Wait for the product list to appear
Wait for the success message
```

Always wait for a **state**, never for a duration. A fixed sleep is a race that
has not happened yet; it passes on your machine and fails in CI.

### Verification

```
Verify the cart shows 2 items
Check that the total is $99.99
Confirm the success message appears
Ensure no error messages are displayed
```

### Keyboard

```
Press Enter
Press Tab to move to next field
Press Escape to close modal
Press Control+A to select all
```

## Complete Examples

### Login Flow

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
- The header greeting shows "test@example.com"
- The navigation has a "Logout" item and no "Sign in" item
```

### E-commerce Cart

```
Test: Add item to cart and proceed to checkout

Prerequisites:
- User is logged in
- Product "Wireless Headphones" exists

Setup:
- Empty the cart, so the count below starts from zero

Steps:
1. Navigate to /products
2. Search for "wireless headphones"
3. Click on the first product result
4. Click "Add to Cart" and wait for the cart notification
5. Click the cart icon

Expected Results:
- The cart badge reads 1
- The cart's first line item is named "Wireless Headphones"
- The order total element is greater than $0
- The "Your cart is empty" message is no longer on the page
```

### Form Validation

```
Test: Registration form validates required fields

Prerequisites:
- User is not logged in

Steps:
1. Navigate to /register
2. Leave all fields empty
3. Click the "Create Account" button
4. Check for validation errors

Expected Results:
- The email field's error text reads "Email is required"
- The password field's error text reads "Password is required"
- URL is still /register
```

### Mobile Responsive

```
Test: Mobile navigation menu works correctly

Prerequisites:
- Viewport set to 375x667 (iPhone SE)

Steps:
1. Navigate to /
2. Click the hamburger menu icon
3. Wait for menu to expand
4. Click "Products" in the menu
5. Verify menu closes
6. Verify Products page loads

Expected Results:
- URL is /products
- The menu panel is no longer on the page
```

## Tips for Reliable Tests

### Use Test IDs

```
Click the checkout button (data-testid: checkout-btn)
Enter email in field with data-testid: email-input
```

### Be Specific

Good:
```
Click the "Add to Cart" button in the product details section
```

Less reliable:
```
Click add to cart
```

### Handle Dynamic Content

```
Wait for "Loading..." spinner to disappear
Wait for the product list to have at least 3 items
Wait for the success message to appear
```

### Describe Context

```
In the shipping address section, enter "123 Main St" in the street field
On the payment form, select "Credit Card" as payment method
```

## What the wording compiles to

The compiler picks an API call from what you wrote, and the call decides how a
failure is classified. This is the part worth knowing:

| you write | it compiles to | a failure is |
|---|---|---|
| "the X is visible / present" | `atr.expectExists("X")` | `assertion` — the app is wrong |
| "the X is gone / no longer shown" | `atr.expectMissing("X")` | `assertion` |
| "the X reads Y" | `expect(atr.text("X")).toBe("Y")` | `assertion` |
| "the X looks like an order number" | `expect(atr.text("X")).toMatch(/^ORD-\d+$/)` | `assertion` |
| "wait for X" | `atr.waitFor("X")` | `timeout` — retried, then triaged |
| "click X" | `atr.click("X")` | `not_found` — repaired automatically |
| "if the cookie banner is shown, dismiss it" | `if (atr.exists(...))` | not a failure; a branch |

Two consequences:

- **A bare click is not an assertion.** If a control disappearing should fail
  the test, say so in Expected Results — otherwise a real regression is
  classified as drift and quietly repaired away.
- **A wait is not an assertion either.** "Wait for the confirmation" times out
  and is retried; "the confirmation is visible" fails the test. Use the first
  to reach a state and the second to assert it.

Text-qualified targets are supported directly: `button:has-text("Save")`
matches by tag and text together, which is what you want when a page has six
buttons and only one says Save.

`toMatch` takes a JavaScript regular expression literal and matches it with the
JavaScript engine, so lookahead and the other JS-only constructs work. Use it
when the exact value varies per run — an order number, a timestamp, a
generated name — and phrase the expectation as a shape rather than a value:
"the order reference starts with ORD- followed by digits".

## Organizing Test Files

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
      order-confirmation.test.txt
    search/
      basic-search.test.txt
      filters.test.txt
```
