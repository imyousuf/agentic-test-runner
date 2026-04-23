package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- FormatTestFile unit tests ---

func TestFormatTestFile(t *testing.T) {
	events := []RecordedEvent{
		{Sequence: 1, Type: "navigate", Value: "https://example.com/login", Timestamp: time.Now()},
		{Sequence: 2, Type: "fill", Selector: `input[name="email"]`, Value: "user@example.com", TagName: "input", InputType: "text", Timestamp: time.Now()},
		{Sequence: 3, Type: "click", Selector: "#submit-btn", TagName: "button", InnerText: "Sign In", Timestamp: time.Now()},
	}

	result := FormatTestFile(events, "Login Test")

	if !strings.Contains(result, "Test: Login Test") {
		t.Errorf("expected test name, got: %s", result)
	}
	if !strings.Contains(result, "Navigate to https://example.com/login") {
		t.Errorf("expected navigate step, got: %s", result)
	}
	if !strings.Contains(result, `Enter "user@example.com" in the email field`) {
		t.Errorf("expected fill step, got: %s", result)
	}
	if !strings.Contains(result, `Click the "Sign In" button`) {
		t.Errorf("expected click step, got: %s", result)
	}
	if !strings.Contains(result, "Expected Results:") {
		t.Errorf("expected results section, got: %s", result)
	}
}

func TestFormatTestFileMergesNavigation(t *testing.T) {
	now := time.Now()
	events := []RecordedEvent{
		{Sequence: 1, Type: "click", Selector: "#nav-link", TagName: "a", InnerText: "About", Timestamp: now},
		{Sequence: 2, Type: "navigate", Value: "https://example.com/about", Timestamp: now.Add(200 * time.Millisecond)},
	}

	result := FormatTestFile(events, "Nav Test")

	// Should have only 1 step (click), navigate merged
	lines := strings.Split(result, "\n")
	stepCount := 0
	for _, line := range lines {
		if len(line) > 2 && line[0] >= '1' && line[0] <= '9' && line[1] == '.' {
			stepCount++
		}
	}
	if stepCount != 1 {
		t.Errorf("expected 1 step after merging, got %d steps in:\n%s", stepCount, result)
	}
	if !strings.Contains(result, `Click the "About" link`) {
		t.Errorf("expected click step, got: %s", result)
	}
}

func TestFormatTestFileMasksPasswords(t *testing.T) {
	events := []RecordedEvent{
		{Sequence: 1, Type: "fill", Selector: `input[name="password"]`, Value: "secret123", TagName: "input", InputType: "password", Timestamp: time.Now()},
	}

	result := FormatTestFile(events, "Password Test")

	if strings.Contains(result, "secret123") {
		t.Errorf("password should be masked, got: %s", result)
	}
	if !strings.Contains(result, "[password]") {
		t.Errorf("expected [password] placeholder, got: %s", result)
	}
}

func TestFormatTestFileEmpty(t *testing.T) {
	result := FormatTestFile(nil, "Empty Test")

	if !strings.Contains(result, "Test: Empty Test") {
		t.Errorf("expected test name, got: %s", result)
	}
	if !strings.Contains(result, "no interactions recorded") {
		t.Errorf("expected empty message, got: %s", result)
	}
}

func TestFormatTestFileSequentialFills(t *testing.T) {
	now := time.Now()
	events := []RecordedEvent{
		{Sequence: 1, Type: "fill", Selector: "#email", Value: "u", TagName: "input", InputType: "text", Timestamp: now},
		{Sequence: 2, Type: "fill", Selector: "#email", Value: "us", TagName: "input", InputType: "text", Timestamp: now.Add(100 * time.Millisecond)},
		{Sequence: 3, Type: "fill", Selector: "#email", Value: "user@test.com", TagName: "input", InputType: "text", Timestamp: now.Add(200 * time.Millisecond)},
	}

	result := FormatTestFile(events, "Fill Test")

	// Should only have 1 fill step with the final value
	if strings.Count(result, "Enter") != 1 {
		t.Errorf("expected 1 fill step, got:\n%s", result)
	}
	if !strings.Contains(result, "user@test.com") {
		t.Errorf("expected final value, got: %s", result)
	}
}

func TestFormatTestFileAllEventTypes(t *testing.T) {
	now := time.Now()
	events := []RecordedEvent{
		{Sequence: 1, Type: "navigate", Value: "https://example.com", Timestamp: now},
		{Sequence: 2, Type: "click", Selector: "#btn", TagName: "button", InnerText: "OK", Timestamp: now},
		{Sequence: 3, Type: "double_click", Selector: "#item", TagName: "div", InnerText: "Item", Timestamp: now},
		{Sequence: 4, Type: "fill", Selector: "#input", Value: "hello", TagName: "input", InputType: "text", Timestamp: now},
		{Sequence: 5, Type: "select_option", Selector: `select[name="color"]`, Value: "Red", TagName: "select", Timestamp: now},
		{Sequence: 6, Type: "keypress", Value: "Enter", Timestamp: now},
		{Sequence: 7, Type: "scroll", Selector: "window", Timestamp: now},
	}

	result := FormatTestFile(events, "All Types")

	if !strings.Contains(result, "Navigate to https://example.com") {
		t.Errorf("missing navigate step")
	}
	if !strings.Contains(result, `Click the "OK" button`) {
		t.Errorf("missing click step")
	}
	if !strings.Contains(result, `Double-click the "Item" div`) {
		t.Errorf("missing double_click step")
	}
	if !strings.Contains(result, `Enter "hello"`) {
		t.Errorf("missing fill step")
	}
	if !strings.Contains(result, `Select "Red"`) {
		t.Errorf("missing select step")
	}
	if !strings.Contains(result, "Press Enter") {
		t.Errorf("missing keypress step")
	}
	if !strings.Contains(result, "Scroll down on the page") {
		t.Errorf("missing scroll step")
	}
}

// --- Recording session integration tests ---

func TestStartStopRecording(t *testing.T) {
	resetFixture(t)

	if testBrowser.IsRecording() {
		t.Fatal("should not be recording initially")
	}

	err := testBrowser.StartRecording("")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}
	if !testBrowser.IsRecording() {
		t.Fatal("should be recording after start")
	}

	events, err := testBrowser.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}
	if testBrowser.IsRecording() {
		t.Fatal("should not be recording after stop")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestRecordClick(t *testing.T) {
	resetFixture(t)

	err := testBrowser.StartRecording("")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	// Give the recorder time to inject
	time.Sleep(300 * time.Millisecond)

	// Click the test button programmatically
	page, _ := testBrowser.CurrentPage()
	el := page.MustElement("#test-button")
	el.MustClick()

	// Wait for deferred click (300ms dblclick window + buffer)
	time.Sleep(500 * time.Millisecond)

	events, err := testBrowser.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}

	// Find click event
	found := false
	for _, evt := range events {
		if evt.Type == "click" && strings.Contains(evt.Selector, "test-button") {
			found = true
			if evt.TagName != "button" {
				t.Errorf("expected tagName 'button', got '%s'", evt.TagName)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected click event on test-button, got events: %+v", events)
	}
}

func TestRecordFill(t *testing.T) {
	resetFixture(t)

	err := testBrowser.StartRecording("")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	// Give the recorder time to inject
	time.Sleep(300 * time.Millisecond)

	// Fill the test input programmatically
	page, _ := testBrowser.CurrentPage()
	el := page.MustElement("#test-input")
	el.MustInput("hello world")

	// Wait for debounce (500ms) + buffer
	time.Sleep(800 * time.Millisecond)

	events, err := testBrowser.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}

	// Find fill event
	found := false
	for _, evt := range events {
		if evt.Type == "fill" {
			found = true
			if evt.Value != "hello world" {
				t.Errorf("expected value 'hello world', got '%s'", evt.Value)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected fill event, got events: %+v", events)
	}
}

func TestRecordSurvivesNavigation(t *testing.T) {
	resetFixture(t)

	err := testBrowser.StartRecording("")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	// Give the recorder time to inject
	time.Sleep(300 * time.Millisecond)

	// Navigate to a different page (same fixture, but triggers navigation)
	ctx := context.Background()
	testBrowser.Navigate(ctx, testFixtureURL+"/test_fixture.html?reloaded=1")
	time.Sleep(500 * time.Millisecond)

	// Click on the test button on the new page
	page, _ := testBrowser.CurrentPage()
	el := page.MustElement("#test-button")
	el.MustClick()
	time.Sleep(500 * time.Millisecond) // wait for deferred click

	events, err := testBrowser.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}

	// Should have at least a click event (navigate events may or may not be captured via SPA detection)
	hasClick := false
	for _, evt := range events {
		if evt.Type == "click" {
			hasClick = true
			break
		}
	}
	if !hasClick {
		t.Errorf("expected click event after navigation, got events: %+v", events)
	}
}

func TestRecordDoubleStart(t *testing.T) {
	resetFixture(t)

	err := testBrowser.StartRecording("")
	if err != nil {
		t.Fatalf("first StartRecording failed: %v", err)
	}
	defer testBrowser.StopRecording()

	err = testBrowser.StartRecording("")
	if err == nil {
		t.Fatal("expected error on second StartRecording")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("expected 'already in progress' error, got: %v", err)
	}
}

func TestStopWhenNotRecording(t *testing.T) {
	resetFixture(t)

	// Clear any leftover session by starting and stopping
	testBrowser.StartRecording("")
	testBrowser.StopRecording()
	// Clear the inactive session
	testBrowser.ClearRecording()

	_, err := testBrowser.StopRecording()
	if err == nil {
		t.Fatal("expected error when stopping without recording")
	}
	if !strings.Contains(err.Error(), "no recording") {
		t.Errorf("expected 'no recording' error, got: %v", err)
	}
}

// TestStopIdempotent verifies that StopRecording can be called twice
// and returns events both times (overlay stop button + CLI stop).
func TestStopIdempotent(t *testing.T) {
	resetFixture(t)

	err := testBrowser.StartRecording("")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	// Click to capture an event
	time.Sleep(300 * time.Millisecond)
	page, _ := testBrowser.CurrentPage()
	page.MustElement("#test-button").MustClick()
	time.Sleep(500 * time.Millisecond)

	// First stop (simulates overlay stop button)
	events1, err := testBrowser.StopRecording()
	if err != nil {
		t.Fatalf("first StopRecording failed: %v", err)
	}
	if len(events1) == 0 {
		t.Fatal("expected events from first stop")
	}

	// Second stop (simulates CLI stop after polling)
	events2, err := testBrowser.StopRecording()
	if err != nil {
		t.Fatalf("second StopRecording failed: %v", err)
	}
	if len(events2) != len(events1) {
		t.Errorf("expected same events on second stop: got %d, want %d", len(events2), len(events1))
	}

	testBrowser.ClearRecording()
}

// TestRecordWithURL verifies that recording with a URL navigates
// first and then injects the recorder (navigate-before-inject fix).
func TestRecordWithURL(t *testing.T) {
	resetFixture(t)

	err := testBrowser.StartRecording(testFixtureURL + "/test_fixture.html")
	if err != nil {
		t.Fatalf("StartRecording with URL failed: %v", err)
	}

	// Verify we're on the fixture page
	url := testBrowser.CurrentURL()
	if !strings.Contains(url, "test_fixture.html") {
		t.Errorf("expected fixture URL, got: %s", url)
	}

	// Verify recording is active
	if !testBrowser.IsRecording() {
		t.Fatal("should be recording after start with URL")
	}

	// Click to verify events are captured (binding works after navigate)
	time.Sleep(300 * time.Millisecond)
	page, _ := testBrowser.CurrentPage()
	page.MustElement("#test-button").MustClick()
	time.Sleep(500 * time.Millisecond)

	events, err := testBrowser.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}

	hasClick := false
	for _, evt := range events {
		if evt.Type == "click" {
			hasClick = true
			break
		}
	}
	if !hasClick {
		t.Errorf("expected click event after recording with URL, got: %+v", events)
	}
	testBrowser.ClearRecording()
}
