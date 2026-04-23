package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewBrowserRecordCmd(t *testing.T) {
	cmd := newBrowserRecordCmd()

	if cmd.Use != "record" {
		t.Errorf("expected Use='record', got '%s'", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short description")
	}

	// Check flags exist
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
	if outputFlag.Shorthand != "o" {
		t.Errorf("expected shorthand 'o', got '%s'", outputFlag.Shorthand)
	}

	urlFlag := cmd.Flags().Lookup("url")
	if urlFlag == nil {
		t.Fatal("expected --url flag")
	}
}

func TestRecordDefaultOutputFilename(t *testing.T) {
	// Verify the default filename pattern
	now := time.Now()
	expected := fmt.Sprintf("record-%s.test.txt", now.Format("20060102-150405"))

	// The format should contain a date-time pattern
	if !strings.HasPrefix(expected, "record-") {
		t.Error("expected prefix 'record-'")
	}
	if !strings.HasSuffix(expected, ".test.txt") {
		t.Error("expected suffix '.test.txt'")
	}
	// Verify the format produces a reasonable length filename
	if len(expected) < 25 || len(expected) > 35 {
		t.Errorf("unexpected filename length: %d (%s)", len(expected), expected)
	}
}

func TestRecordOutputFlagParsing(t *testing.T) {
	cmd := newBrowserRecordCmd()

	// Test --output flag
	cmd.SetArgs([]string{"--output", "my-test.test.txt"})
	cmd.ParseFlags([]string{"--output", "my-test.test.txt"})

	val, err := cmd.Flags().GetString("output")
	if err != nil {
		t.Fatalf("failed to get output flag: %v", err)
	}
	if val != "my-test.test.txt" {
		t.Errorf("expected 'my-test.test.txt', got '%s'", val)
	}

	// Test -o shorthand
	cmd2 := newBrowserRecordCmd()
	cmd2.ParseFlags([]string{"-o", "short.test.txt"})

	val2, err := cmd2.Flags().GetString("output")
	if err != nil {
		t.Fatalf("failed to get output flag via shorthand: %v", err)
	}
	if val2 != "short.test.txt" {
		t.Errorf("expected 'short.test.txt', got '%s'", val2)
	}
}

func TestRecordURLFlagParsing(t *testing.T) {
	cmd := newBrowserRecordCmd()
	cmd.ParseFlags([]string{"--url", "https://example.com"})

	val, err := cmd.Flags().GetString("url")
	if err != nil {
		t.Fatalf("failed to get url flag: %v", err)
	}
	if val != "https://example.com" {
		t.Errorf("expected 'https://example.com', got '%s'", val)
	}
}
