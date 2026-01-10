# ATR Behavior Test File Format

Complete reference for writing `.test.txt` behavior test files.

## Basic Structure

```
Test: <descriptive test name>

Prerequisites:
- <condition that must be true>
- <another condition>

Steps:
1. <first action>
2. <second action>
3. <third action>

Expected Results:
- <what should be true after steps>
- <another expected outcome>
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

### Expected Results (required)

Conditions to verify after steps complete.

```
Expected Results:
- URL contains /dashboard
- Welcome message displays user name
- Navigation shows "Logout" option
- No console errors
- No failed network requests
```

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
Wait 2 seconds
Wait for the success message
```

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
- Welcome message is visible
- No console errors
```

### E-commerce Cart

```
Test: Add item to cart and proceed to checkout

Prerequisites:
- User is logged in
- Product "Wireless Headphones" exists

Steps:
1. Navigate to /products
2. Search for "wireless headphones"
3. Click on the first product result
4. Click "Add to Cart" button
5. Wait for cart notification
6. Click the cart icon
7. Verify the cart shows 1 item
8. Click "Proceed to Checkout"

Expected Results:
- Cart total is greater than $0
- Checkout page is displayed
- Cart item matches selected product
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
- Email field shows "Email is required" error
- Password field shows "Password is required" error
- Form is not submitted
- User remains on registration page
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
- Navigation menu expands on click
- Menu items are clickable
- Page navigates correctly
- Menu closes after selection
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
