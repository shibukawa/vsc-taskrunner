package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type problemCollector struct {
	matchers []*compiledProblemMatcher
	problems []Problem
	closed   bool
}

type compiledProblemMatcher struct {
	task         string
	owner        string
	source       string
	severity     string
	fileLocation problemFileLocation
	patterns     []compiledProblemPattern
	state        *matcherState
}

type compiledProblemPattern struct {
	regexp    *regexp.Regexp
	kind      string
	file      int
	line      int
	column    int
	message   int
	severity  int
	code      int
	location  int
	endLine   int
	endColumn int
	loop      bool
}

type matcherState struct {
	index  int
	fields partialProblem
}

type partialProblem struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	severity  string
	code      string
	message   string
}

type matcherConfig struct {
	Base         string          `json:"base,omitempty"`
	Owner        string          `json:"owner,omitempty"`
	Source       string          `json:"source,omitempty"`
	Severity     string          `json:"severity,omitempty"`
	FileLocation json.RawMessage `json:"fileLocation,omitempty"`
	Pattern      json.RawMessage `json:"pattern,omitempty"`
}

type patternConfig struct {
	Regexp    string `json:"regexp"`
	Kind      string `json:"kind,omitempty"`
	File      int    `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	Message   int    `json:"message,omitempty"`
	Severity  int    `json:"severity,omitempty"`
	Code      int    `json:"code,omitempty"`
	Location  int    `json:"location,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
	Loop      bool   `json:"loop,omitempty"`
}

type problemFileLocation struct {
	kind string
	base string
}

func newProblemCollector(taskLabel string, raw json.RawMessage, workspaceRoot string) (*problemCollector, error) {
	matchers, err := compileProblemMatchers(taskLabel, raw, workspaceRoot)
	if err != nil {
		return nil, err
	}
	return &problemCollector{matchers: matchers}, nil
}

func (c *problemCollector) ProcessLine(line string) {
	if c == nil || c.closed {
		return
	}
	for _, matcher := range c.matchers {
		c.problems = append(c.problems, matcher.ProcessLine(line)...)
	}
}

func (c *problemCollector) Close() []Problem {
	if c == nil || c.closed {
		return nil
	}
	c.closed = true
	return append([]Problem(nil), c.problems...)
}

func compileProblemMatchers(taskLabel string, raw json.RawMessage, workspaceRoot string) ([]*compiledProblemMatcher, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		compiled := make([]*compiledProblemMatcher, 0, len(items))
		for _, item := range items {
			matcher, err := compileSingleProblemMatcher(taskLabel, item, workspaceRoot)
			if err != nil {
				return nil, err
			}
			if matcher != nil {
				compiled = append(compiled, matcher)
			}
		}
		return compiled, nil
	}

	matcher, err := compileSingleProblemMatcher(taskLabel, raw, workspaceRoot)
	if err != nil {
		return nil, err
	}
	if matcher == nil {
		return nil, nil
	}
	return []*compiledProblemMatcher{matcher}, nil
}

func compileSingleProblemMatcher(taskLabel string, raw json.RawMessage, workspaceRoot string) (*compiledProblemMatcher, error) {
	var builtinID string
	if err := json.Unmarshal(raw, &builtinID); err == nil {
		config, ok := builtinProblemMatchers()[builtinID]
		if !ok {
			return nil, fmt.Errorf("unsupported problem matcher: %s", builtinID)
		}
		if config.Base != "" {
			base, ok := builtinProblemMatchers()[config.Base]
			if !ok {
				return nil, fmt.Errorf("unsupported base problem matcher: %s", config.Base)
			}
			config = mergeMatcherConfig(base, config)
		}
		return buildCompiledMatcher(taskLabel, config, workspaceRoot)
	}

	var config matcherConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("invalid problem matcher: %w", err)
	}
	if config.Base != "" {
		base, ok := builtinProblemMatchers()[config.Base]
		if !ok {
			return nil, fmt.Errorf("unsupported base problem matcher: %s", config.Base)
		}
		config = mergeMatcherConfig(base, config)
	}
	return buildCompiledMatcher(taskLabel, config, workspaceRoot)
}

func buildCompiledMatcher(taskLabel string, config matcherConfig, workspaceRoot string) (*compiledProblemMatcher, error) {
	patterns, err := compilePatterns(config.Pattern)
	if err != nil {
		return nil, err
	}
	return &compiledProblemMatcher{
		task:         taskLabel,
		owner:        config.Owner,
		source:       config.Source,
		severity:     config.Severity,
		fileLocation: parseFileLocation(config.FileLocation, workspaceRoot),
		patterns:     patterns,
	}, nil
}

func compilePatterns(raw json.RawMessage) ([]compiledProblemPattern, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var many []patternConfig
	if err := json.Unmarshal(raw, &many); err == nil {
		patterns := make([]compiledProblemPattern, 0, len(many))
		for _, item := range many {
			compiled, err := compilePattern(item)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, compiled)
		}
		return patterns, nil
	}

	var single patternConfig
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("invalid problem pattern: %w", err)
	}
	compiled, err := compilePattern(single)
	if err != nil {
		return nil, err
	}
	return []compiledProblemPattern{compiled}, nil
}

func compilePattern(config patternConfig) (compiledProblemPattern, error) {
	compiled, err := regexp.Compile(config.Regexp)
	if err != nil {
		return compiledProblemPattern{}, fmt.Errorf("compile problem matcher regexp %q: %w", config.Regexp, err)
	}
	return compiledProblemPattern{
		regexp:    compiled,
		kind:      config.Kind,
		file:      config.File,
		line:      config.Line,
		column:    config.Column,
		message:   config.Message,
		severity:  config.Severity,
		code:      config.Code,
		location:  config.Location,
		endLine:   config.EndLine,
		endColumn: config.EndColumn,
		loop:      config.Loop,
	}, nil
}

func parseFileLocation(raw json.RawMessage, workspaceRoot string) problemFileLocation {
	if len(raw) == 0 {
		return problemFileLocation{kind: "relative", base: workspaceRoot}
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return problemFileLocation{kind: single, base: workspaceRoot}
	}
	var pair []string
	if err := json.Unmarshal(raw, &pair); err == nil && len(pair) > 0 {
		location := problemFileLocation{kind: pair[0], base: workspaceRoot}
		if len(pair) > 1 {
			location.base = pair[1]
		}
		return location
	}
	return problemFileLocation{kind: "relative", base: workspaceRoot}
}

func mergeMatcherConfig(base matcherConfig, overlay matcherConfig) matcherConfig {
	result := base
	if overlay.Owner != "" {
		result.Owner = overlay.Owner
	}
	if overlay.Source != "" {
		result.Source = overlay.Source
	}
	if overlay.Severity != "" {
		result.Severity = overlay.Severity
	}
	if len(overlay.FileLocation) > 0 {
		result.FileLocation = overlay.FileLocation
	}
	if len(overlay.Pattern) > 0 {
		result.Pattern = overlay.Pattern
	}
	return result
}

func (m *compiledProblemMatcher) ProcessLine(line string) []Problem {
	if len(m.patterns) == 0 {
		return nil
	}

	if len(m.patterns) == 1 {
		if problem, ok := m.tryPattern(m.patterns[0], partialProblem{}, line); ok {
			return []Problem{problem}
		}
		return nil
	}

	if m.state != nil {
		current := m.patterns[m.state.index]
		if problem, nextState, matched := m.applyState(current, m.state.fields, line, m.state.index); matched {
			m.state = nextState
			if problem != nil {
				return []Problem{*problem}
			}
			return nil
		}
		m.state = nil
	}

	match := m.patterns[0].regexp.FindStringSubmatch(line)
	if len(match) == 0 {
		return nil
	}
	fields := captureFields(m.patterns[0], match, partialProblem{})
	m.state = &matcherState{index: 1, fields: fields}
	return nil
}

func (m *compiledProblemMatcher) tryPattern(pattern compiledProblemPattern, seed partialProblem, line string) (Problem, bool) {
	match := pattern.regexp.FindStringSubmatch(line)
	if len(match) == 0 {
		return Problem{}, false
	}
	fields := captureFields(pattern, match, seed)
	return m.makeProblem(fields), true
}

func (m *compiledProblemMatcher) applyState(pattern compiledProblemPattern, seed partialProblem, line string, index int) (*Problem, *matcherState, bool) {
	match := pattern.regexp.FindStringSubmatch(line)
	if len(match) == 0 {
		return nil, nil, false
	}
	fields := captureFields(pattern, match, seed)
	if index == len(m.patterns)-1 {
		problem := m.makeProblem(fields)
		if pattern.loop {
			return &problem, &matcherState{index: index, fields: seed}, true
		}
		return &problem, nil, true
	}
	return nil, &matcherState{index: index + 1, fields: fields}, true
}

func captureFields(pattern compiledProblemPattern, match []string, seed partialProblem) partialProblem {
	result := seed
	if value := capture(match, pattern.file); value != "" {
		result.file = value
	}
	if value := capture(match, pattern.message); value != "" {
		result.message = value
	}
	if value := capture(match, pattern.severity); value != "" {
		result.severity = value
	}
	if value := capture(match, pattern.code); value != "" {
		result.code = value
	}
	if value := capture(match, pattern.location); value != "" {
		line, column, endLine, endColumn := parseLocation(value)
		if line > 0 {
			result.line = line
		}
		if column > 0 {
			result.column = column
		}
		if endLine > 0 {
			result.endLine = endLine
		}
		if endColumn > 0 {
			result.endColumn = endColumn
		}
	}
	if value := parseInt(capture(match, pattern.line)); value > 0 {
		result.line = value
	}
	if value := parseInt(capture(match, pattern.column)); value > 0 {
		result.column = value
	}
	if value := parseInt(capture(match, pattern.endLine)); value > 0 {
		result.endLine = value
	}
	if value := parseInt(capture(match, pattern.endColumn)); value > 0 {
		result.endColumn = value
	}
	return result
}

func (m *compiledProblemMatcher) makeProblem(fields partialProblem) Problem {
	severity := fields.severity
	if severity == "" {
		severity = m.severity
	}
	severity = normalizeProblemSeverity(severity)
	if severity == "" {
		severity = "error"
	}
	return Problem{
		Task:      m.task,
		Owner:     m.owner,
		Source:    m.source,
		File:      resolveProblemFile(m.fileLocation, fields.file),
		Line:      fields.line,
		Column:    fields.column,
		EndLine:   fields.endLine,
		EndColumn: fields.endColumn,
		Severity:  severity,
		Code:      fields.code,
		Message:   fields.message,
	}
}

func normalizeProblemSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fatal error", "error", "e":
		return "error"
	case "warning", "w":
		return "warning"
	case "info", "i":
		return "info"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func resolveProblemFile(location problemFileLocation, file string) string {
	if file == "" {
		return ""
	}
	if filepath.IsAbs(file) {
		return filepath.Clean(file)
	}
	switch location.kind {
	case "absolute":
		return filepath.Clean(file)
	case "autoDetect":
		if _, err := os.Stat(file); err == nil {
			return filepath.Clean(file)
		}
		fallthrough
	default:
		return filepath.Clean(filepath.Join(location.base, file))
	}
}

func capture(match []string, index int) string {
	if index <= 0 || index >= len(match) {
		return ""
	}
	return match[index]
}

func parseInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func parseLocation(value string) (int, int, int, int) {
	parts := strings.Split(value, ",")
	switch len(parts) {
	case 1:
		return parseInt(parts[0]), 0, 0, 0
	case 2:
		return parseInt(parts[0]), parseInt(parts[1]), 0, 0
	case 4:
		return parseInt(parts[0]), parseInt(parts[1]), parseInt(parts[2]), parseInt(parts[3])
	default:
		return 0, 0, 0, 0
	}
}
