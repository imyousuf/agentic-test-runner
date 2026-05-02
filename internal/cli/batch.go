package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// batchResult holds the result of a single command in a batch.
type batchResult struct {
	Index      int         `json:"index"`
	Command    string      `json:"command"`
	Status     string      `json:"status"`
	DurationMS int64       `json:"duration_ms"`
	Output     interface{} `json:"output"`
	Error      string      `json:"error,omitempty"`
	rawData    interface{} // full API response data for let extraction
}

func newBrowserBatchCmd() *cobra.Command {
	var onError string
	var batchTimeout int
	var inputFile string
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Execute multiple commands sequentially",
		Long: `Execute multiple browser commands from stdin or a file in a single invocation.

Commands execute sequentially in the same browser session. Use 'let' statements
to extract values from command output and '[[name]]' to interpolate variables.

Input: one command per line (without 'atr browser' prefix). Lines starting with
'#' are comments. Empty lines are ignored. Maximum 100 commands.

Variable extraction:
  let varName = $.fieldName         Extract from previous command's output
  let varName = $.output.subField   Nested field access
  [[varName]]                       Interpolate variable in subsequent commands

Error handling (--on-error):
  stop      Stop on first error (default)
  continue  Skip failures, continue with remaining commands
  retry:N   Retry failed commands up to N times with 500ms backoff`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var input io.Reader
			if inputFile != "" {
				f, err := os.Open(inputFile)
				if err != nil {
					return fmt.Errorf("failed to open file: %w", err)
				}
				defer f.Close()
				input = f
			} else {
				input = os.Stdin
			}

			lines, err := readBatchInput(input)
			if err != nil {
				return err
			}

			if len(lines) == 0 {
				return fmt.Errorf("no commands to execute")
			}
			if len(lines) > 100 {
				return fmt.Errorf("too many commands (%d) — maximum is 100", len(lines))
			}

			timeout := time.Duration(batchTimeout) * time.Second
			results, vars, err := executeBatch(lines, onError, timeout)

			if browserJSONOutput {
				return outputBatchJSON(results, vars, err)
			}
			return outputBatchHuman(results, err)
		},
	}
	cmd.Flags().StringVar(&onError, "on-error", "stop", "Error handling: stop, continue, retry:N")
	cmd.Flags().IntVar(&batchTimeout, "timeout", 60, "Total batch timeout in seconds")
	cmd.Flags().StringVar(&inputFile, "file", "", "Read commands from file instead of stdin")
	return cmd
}

// readBatchInput reads command lines from a reader, stripping comments and blanks.
func readBatchInput(r io.Reader) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, scanner.Err()
}

// executeBatch runs batch commands sequentially with variable support.
func executeBatch(lines []string, onError string, timeout time.Duration) ([]batchResult, map[string]string, error) {
	vars := make(map[string]string)
	var results []batchResult
	var lastData interface{} // raw data from last executed command

	deadline := time.Now().Add(timeout)

	retryMax := 0
	errorMode := onError
	if strings.HasPrefix(onError, "retry:") {
		n, err := strconv.Atoi(strings.TrimPrefix(onError, "retry:"))
		if err != nil || n < 1 {
			return nil, nil, fmt.Errorf("invalid retry count in --on-error: %s", onError)
		}
		retryMax = n
		errorMode = "retry"
	}

	cmdIndex := 0
	for i := 0; i < len(lines); i++ {
		if time.Now().After(deadline) {
			return results, vars, fmt.Errorf("batch timeout exceeded (%s)", timeout)
		}

		line := lines[i]

		// Handle let statements
		if strings.HasPrefix(line, "let ") {
			if err := handleLet(line, lastData, vars); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", err)
			}
			continue
		}

		// Interpolate variables
		interpolated, err := interpolateVars(line, vars)
		if err != nil {
			result := batchResult{
				Index:   cmdIndex,
				Command: line,
				Status:  "error",
				Error:   err.Error(),
			}
			results = append(results, result)
			if errorMode == "stop" || errorMode == "retry" {
				return results, vars, err
			}
			cmdIndex++
			continue
		}

		// Dispatch and execute
		var result batchResult
		attempts := 1
		if errorMode == "retry" {
			attempts = retryMax + 1
		}

		for attempt := 0; attempt < attempts; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
			result = executeOneCommand(cmdIndex, interpolated)
			if result.Status == "ok" {
				break
			}
		}

		// Build the $ context for let statements: merge raw API data with
		// normalized "output" field so both $.output and $.rawField work.
		lastData = buildLetContext(result)
		results = append(results, result)

		if result.Status == "error" {
			if errorMode == "stop" || errorMode == "retry" {
				return results, vars, fmt.Errorf("command failed: %s", result.Error)
			}
			// continue mode: keep going
		}

		cmdIndex++
	}

	return results, vars, nil
}

// executeOneCommand dispatches a single command line and returns the result.
func executeOneCommand(index int, cmdLine string) batchResult {
	start := time.Now()
	result := batchResult{
		Index:   index,
		Command: cmdLine,
	}

	method, path, body, primaryField, err := dispatchBatchCommand(cmdLine)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	resp, err := apiRequestRaw(method, path, body)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	result.DurationMS = time.Since(start).Milliseconds()

	if !resp.Success {
		result.Status = "error"
		result.Error = resp.Error
		result.rawData = resp.Data
		return result
	}

	result.Status = "ok"
	result.rawData = resp.Data

	// Extract primary output value
	if dataMap, ok := resp.Data.(map[string]interface{}); ok {
		if primaryField != "" {
			if val, exists := dataMap[primaryField]; exists {
				result.Output = val
			} else {
				result.Output = dataMap
			}
		} else {
			result.Output = dataMap
		}
	} else {
		result.Output = resp.Data
	}

	return result
}

// dispatchBatchCommand parses a command line and returns the API call parameters.
// primaryField indicates which response field is the "output" for let extraction.
func dispatchBatchCommand(cmdLine string) (method, path string, body interface{}, primaryField string, err error) {
	tokens, err := shellSplit(cmdLine)
	if err != nil {
		return "", "", nil, "", fmt.Errorf("failed to parse command: %w", err)
	}
	if len(tokens) == 0 {
		return "", "", nil, "", fmt.Errorf("empty command")
	}

	cmd := tokens[0]
	args := tokens[1:]

	switch cmd {
	// Navigation
	case "navigate":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("navigate requires a URL")
		}
		return "POST", "/navigate", map[string]interface{}{"url": args[0]}, "url", nil

	case "back":
		return "POST", "/back", nil, "url", nil

	case "forward":
		return "POST", "/forward", nil, "url", nil

	case "reload":
		return "POST", "/reload", nil, "url", nil

	// Interaction
	case "click":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("click requires a target")
		}
		doubleClick := containsFlag(args, "--double")
		return "POST", "/click", map[string]interface{}{
			"selector": args[0], "double_click": doubleClick,
		}, "selector", nil

	case "fill":
		if len(args) < 2 {
			return "", "", nil, "", fmt.Errorf("fill requires target and value")
		}
		return "POST", "/fill", map[string]interface{}{
			"selector": args[0], "value": args[1],
		}, "selector", nil

	case "hover":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("hover requires a target")
		}
		return "POST", "/hover", map[string]interface{}{"selector": args[0]}, "selector", nil

	case "press-key":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("press-key requires a key")
		}
		return "POST", "/press-key", map[string]interface{}{"key": args[0]}, "pressed", nil

	case "drag":
		if len(args) < 2 {
			return "", "", nil, "", fmt.Errorf("drag requires from and to")
		}
		return "POST", "/drag", map[string]interface{}{
			"from": args[0], "to": args[1],
		}, "", nil

	// Wait
	case "wait":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("wait requires a selector")
		}
		timeout := 5000
		if v, ok := getFlagValue(args, "--timeout"); ok {
			if n, e := strconv.Atoi(v); e == nil {
				timeout = n
			}
		}
		visible := containsFlag(args, "--visible")
		return "POST", "/wait", map[string]interface{}{
			"selector": args[0], "timeout": timeout, "visible": visible,
		}, "found", nil

	// Scroll
	case "scroll":
		selector, _ := getFlagValue(args, "--selector")
		if selector == "" {
			selector, _ = getFlagValue(args, "-s")
		}
		if selector == "" {
			return "", "", nil, "", fmt.Errorf("scroll requires --selector")
		}
		x, y := 0, 0
		if v, ok := getFlagValue(args, "--x"); ok {
			x, _ = strconv.Atoi(v)
		}
		if v, ok := getFlagValue(args, "--y"); ok {
			y, _ = strconv.Atoi(v)
		}
		return "POST", "/scroll", map[string]interface{}{
			"selector": selector, "x": x, "y": y,
			"to_bottom": containsFlag(args, "--to-bottom"),
			"to_top":    containsFlag(args, "--to-top"),
		}, "scrollTop", nil

	// Eval
	case "eval":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("eval requires a script")
		}
		return "POST", "/eval", map[string]interface{}{"script": args[0]}, "result", nil

	// Screenshot
	case "screenshot":
		params := []string{}
		if sel, ok := getFlagValue(args, "--selector"); ok {
			params = append(params, "selector="+url.QueryEscape(sel))
		} else if sel, ok := getFlagValue(args, "-s"); ok {
			params = append(params, "selector="+url.QueryEscape(sel))
		}
		if sel, ok := getFlagValue(args, "--selector-all"); ok {
			params = append(params, "selector_all="+url.QueryEscape(sel))
			if dir, ok := getFlagValue(args, "--output-dir"); ok {
				params = append(params, "output_dir="+url.QueryEscape(dir))
			}
			if v, ok := getFlagValue(args, "--timeout"); ok {
				params = append(params, "timeout="+v)
			}
		}
		if containsFlag(args, "--full") {
			params = append(params, "full=true")
		}
		if containsFlag(args, "--file") {
			params = append(params, "format=file")
		}
		p := "/screenshot"
		if len(params) > 0 {
			p += "?" + strings.Join(params, "&")
		}
		return "GET", p, nil, "path", nil

	// Snapshot
	case "snapshot":
		p := "/snapshot"
		if containsFlag(args, "--verbose") {
			p += "?verbose=true"
		}
		return "GET", p, nil, "elements", nil

	// Inspection
	case "html":
		return "GET", "/html", nil, "html", nil

	case "url":
		return "GET", "/url", nil, "url", nil

	case "title":
		return "GET", "/title", nil, "title", nil

	// Styles
	case "computed-styles":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("computed-styles requires a selector")
		}
		p := "/computed-styles?selector=" + url.QueryEscape(args[0])
		if v, ok := getFlagValue(args, "--properties"); ok {
			p += "&properties=" + url.QueryEscape(v)
		}
		return "GET", p, nil, "styles", nil

	case "computed-styles-diff":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("computed-styles-diff requires a selector")
		}
		against := "0"
		if v, ok := getFlagValue(args, "--against"); ok {
			against = v
			if strings.HasPrefix(against, "page:") {
				against = strings.TrimPrefix(against, "page:")
			}
		}
		p := "/computed-styles-diff?selector=" + url.QueryEscape(args[0]) + "&against=" + against
		if v, ok := getFlagValue(args, "--properties"); ok {
			p += "&properties=" + url.QueryEscape(v)
		}
		if v, ok := getFlagValue(args, "--selector-target"); ok {
			p += "&selector_target=" + url.QueryEscape(v)
		}
		return "GET", p, nil, "score", nil

	// Text
	case "text":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("text requires a selector")
		}
		mode := "structured"
		if containsFlag(args, "--flat") {
			mode = "flat"
		} else if containsFlag(args, "--links") {
			mode = "links"
		} else if containsFlag(args, "--headings") {
			mode = "headings"
		}
		p := "/text?selector=" + url.QueryEscape(args[0]) + "&mode=" + mode
		return "GET", p, nil, "groups", nil

	// Font check
	case "font-check":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("font-check requires a font family")
		}
		return "GET", "/font-check?family=" + url.QueryEscape(args[0]), nil, "status", nil

	// Download images
	case "download-images":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("download-images requires a selector")
		}
		reqBody := map[string]interface{}{"selector": args[0]}
		if v, ok := getFlagValue(args, "--output-dir"); ok {
			reqBody["output_dir"] = v
		}
		if containsFlag(args, "--fallback-screenshot") {
			reqBody["fallback_screenshot"] = true
		}
		return "POST", "/download-images", reqBody, "files", nil

	// Debugging
	case "console":
		limit := "50"
		if v, ok := getFlagValue(args, "--limit"); ok {
			limit = v
		}
		return "GET", "/console?limit=" + limit, nil, "messages", nil

	case "network":
		limit := "50"
		if v, ok := getFlagValue(args, "--limit"); ok {
			limit = v
		}
		return "GET", "/network?limit=" + limit, nil, "requests", nil

	case "errors":
		return "GET", "/errors", nil, "failed_requests", nil

	// Page management
	case "new-page":
		pageURL := "about:blank"
		if len(args) > 0 {
			pageURL = args[0]
		}
		return "POST", "/pages", map[string]interface{}{"url": pageURL}, "pages", nil

	case "list-pages":
		return "GET", "/pages", nil, "pages", nil

	case "select-page":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("select-page requires an index")
		}
		return "PUT", "/pages/" + args[0], nil, "current", nil

	case "close-page":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("close-page requires an index")
		}
		return "DELETE", "/pages/" + args[0], nil, "", nil

	// AI
	case "ask":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("ask requires a question")
		}
		return "POST", "/ask", map[string]interface{}{"question": args[0]}, "answer", nil

	// Viewport
	case "viewport":
		if len(args) == 0 {
			return "GET", "/viewport", nil, "", nil
		}
		reqBody := map[string]interface{}{}
		if v, ok := getFlagValue(args, "--preset"); ok {
			reqBody["preset"] = v
		} else {
			if len(args) < 2 {
				return "", "", nil, "", fmt.Errorf("viewport requires width and height, or --preset")
			}
			w, err1 := strconv.Atoi(args[0])
			h, err2 := strconv.Atoi(args[1])
			if err1 != nil || err2 != nil {
				return "", "", nil, "", fmt.Errorf("viewport width and height must be integers")
			}
			reqBody["width"] = w
			reqBody["height"] = h
		}
		if v, ok := getFlagValue(args, "--dpr"); ok {
			dpr, _ := strconv.ParseFloat(v, 64)
			if dpr > 0 {
				reqBody["dpr"] = dpr
			}
		}
		return "POST", "/viewport", reqBody, "", nil

	// Clean snapshot
	case "clean-snapshot":
		if len(args) < 1 {
			return "", "", nil, "", fmt.Errorf("clean-snapshot requires a selector")
		}
		p := "/clean-snapshot?selector=" + url.QueryEscape(args[0])
		if v, ok := getFlagValue(args, "--depth"); ok {
			p += "&depth=" + v
		}
		if v, ok := getFlagValue(args, "--max-length"); ok {
			p += "&max_length=" + v
		}
		if containsFlag(args, "--svg-full") {
			p += "&svg_full=true"
		}
		if containsFlag(args, "--json") {
			p += "&format=json"
		}
		return "GET", p, nil, "html", nil

	default:
		return "", "", nil, "", fmt.Errorf("unknown command: %s", cmd)
	}
}

// buildLetContext creates the $ context for let extraction from a batch result.
// It merges the raw API response data with a normalized "output" field so that
// both $.output and $.rawField (e.g., $.result, $.styles) work.
func buildLetContext(r batchResult) interface{} {
	ctx := make(map[string]interface{})

	// Copy raw API response fields into the context
	if rawMap, ok := r.rawData.(map[string]interface{}); ok {
		for k, v := range rawMap {
			ctx[k] = v
		}
	}

	// Set "output" to the normalized primary value
	ctx["output"] = r.Output

	return ctx
}

// handleLet processes a "let name = $.path" statement.
func handleLet(line string, lastData interface{}, vars map[string]string) error {
	// Parse: let <name> = <jsonpath>
	re := regexp.MustCompile(`^let\s+(\w+)\s*=\s*(.+)$`)
	matches := re.FindStringSubmatch(line)
	if matches == nil {
		return fmt.Errorf("invalid let syntax: %s", line)
	}

	name := matches[1]
	path := strings.TrimSpace(matches[2])

	if lastData == nil {
		vars[name] = ""
		return fmt.Errorf("let %s: no previous command output", name)
	}

	val := extractJSONPath(lastData, path)
	vars[name] = fmt.Sprintf("%v", val)
	if val == nil {
		vars[name] = ""
		return fmt.Errorf("let %s: path %s resolved to nil", name, path)
	}

	return nil
}

// extractJSONPath extracts a value from data using a simple dot-path like $.field.sub or $.field[0]
func extractJSONPath(data interface{}, path string) interface{} {
	// Strip leading $
	path = strings.TrimPrefix(path, "$")
	if path == "" || path == "." {
		return data
	}
	path = strings.TrimPrefix(path, ".")

	parts := splitJSONPath(path)
	current := data

	for _, part := range parts {
		if current == nil {
			return nil
		}

		// Check for array index
		if strings.HasSuffix(part, "]") {
			bracketIdx := strings.Index(part, "[")
			if bracketIdx >= 0 {
				field := part[:bracketIdx]
				idxStr := part[bracketIdx+1 : len(part)-1]
				idx, err := strconv.Atoi(idxStr)
				if err != nil {
					return nil
				}

				// Navigate to field first if non-empty
				if field != "" {
					current = getMapField(current, field)
					if current == nil {
						return nil
					}
				}

				// Index into array
				if arr, ok := current.([]interface{}); ok && idx >= 0 && idx < len(arr) {
					current = arr[idx]
				} else {
					return nil
				}
				continue
			}
		}

		current = getMapField(current, part)
	}

	return current
}

func getMapField(data interface{}, field string) interface{} {
	if m, ok := data.(map[string]interface{}); ok {
		return m[field]
	}
	return nil
}

func splitJSONPath(path string) []string {
	var parts []string
	current := ""
	inBracket := false
	for i := 0; i < len(path); i++ {
		ch := path[i]
		if ch == '[' {
			inBracket = true
			current += string(ch)
		} else if ch == ']' {
			inBracket = false
			current += string(ch)
			// If next char is '.', the bracket part is complete
			if i+1 < len(path) && path[i+1] == '.' {
				parts = append(parts, current)
				current = ""
				i++ // skip the dot
			}
		} else if ch == '.' && !inBracket {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

var varPattern = regexp.MustCompile(`\[\[(\w+)\]\]`)

// interpolateVars replaces [[name]] with variable values in a command string.
func interpolateVars(s string, vars map[string]string) (string, error) {
	var missingErr error
	result := varPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-2] // strip [[ and ]]
		if val, ok := vars[name]; ok {
			return val
		}
		missingErr = fmt.Errorf("undefined variable: [[%s]]", name)
		return match
	})
	return result, missingErr
}

// shellSplit splits a command string into tokens, respecting single and double quotes.
func shellSplit(s string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '\\' && i+1 < len(s) && !inSingle {
			current.WriteByte(s[i+1])
			i++
			continue
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if ch == ' ' && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if inSingle || inDouble {
		return nil, fmt.Errorf("unclosed quote in: %s", s)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}

// containsFlag checks if a flag (e.g., "--verbose") is in the args list.
func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// getFlagValue returns the value after a flag (e.g., "--timeout" "5000").
func getFlagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
		// Handle --flag=value
		if strings.HasPrefix(a, flag+"=") {
			return strings.TrimPrefix(a, flag+"="), true
		}
	}
	return "", false
}

// outputBatchJSON writes batch results as JSON.
func outputBatchJSON(results []batchResult, vars map[string]string, batchErr error) error {
	totalMS := int64(0)
	succeeded := 0
	failed := 0
	for _, r := range results {
		totalMS += r.DurationMS
		if r.Status == "ok" {
			succeeded++
		} else {
			failed++
		}
	}

	output := map[string]interface{}{
		"results":           results,
		"total_duration_ms": totalMS,
		"succeeded":         succeeded,
		"failed":            failed,
	}
	if len(vars) > 0 {
		output["variables"] = vars
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputBatchHuman writes batch results in human-readable format.
func outputBatchHuman(results []batchResult, batchErr error) error {
	total := len(results)
	totalMS := int64(0)
	succeeded := 0

	for i, r := range results {
		totalMS += r.DurationMS
		prefix := fmt.Sprintf("[%d/%d]", i+1, total)

		if r.Status == "ok" {
			succeeded++
			fmt.Printf("%s %s ✓ (%dms)\n", prefix, r.Command, r.DurationMS)

			// Print primary output on next line for certain types
			switch v := r.Output.(type) {
			case string:
				if v != "" {
					fmt.Printf("      → %s\n", v)
				}
			case map[string]interface{}:
				for k, val := range v {
					fmt.Printf("      %s: %v\n", k, val)
				}
			}
		} else {
			fmt.Printf("%s %s ✗ (%dms)\n", prefix, r.Command, r.DurationMS)
			fmt.Printf("      Error: %s\n", r.Error)
		}
	}

	fmt.Printf("Total: %dms (%d/%d succeeded)\n", totalMS, succeeded, total)

	if batchErr != nil {
		return batchErr
	}
	return nil
}
