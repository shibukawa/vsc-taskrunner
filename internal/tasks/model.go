package tasks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type File struct {
	Version      string        `json:"version"`
	Presentation *Presentation `json:"presentation,omitempty"`
	Options      *Options      `json:"options,omitempty"`
	RunOptions   *RunOptions   `json:"runOptions,omitempty"`
	Windows      *TaskDefaults `json:"windows,omitempty"`
	OSX          *TaskDefaults `json:"osx,omitempty"`
	Linux        *TaskDefaults `json:"linux,omitempty"`
	Inputs       []Input       `json:"inputs,omitempty"`
	Tasks        []Task        `json:"tasks"`
}

type Task struct {
	Label          string          `json:"label"`
	TaskName       string          `json:"taskName,omitempty"`
	Identifier     string          `json:"identifier,omitempty"`
	Type           string          `json:"type,omitempty"`
	Script         string          `json:"script,omitempty"`
	Path           string          `json:"path,omitempty"`
	TSConfig       string          `json:"tsconfig,omitempty"`
	Option         string          `json:"option,omitempty"`
	ProviderTask   string          `json:"task,omitempty"`
	ProviderFile   string          `json:"file,omitempty"`
	Command        TokenValue      `json:"command"`
	Args           []TokenValue    `json:"args,omitempty"`
	DependsOrder   string          `json:"dependsOrder,omitempty"`
	Dependencies   DependencyList  `json:"dependsOn"`
	Group          json.RawMessage `json:"group,omitempty"`
	Options        *Options        `json:"options,omitempty"`
	Presentation   *Presentation   `json:"presentation,omitempty"`
	RunOptions     *RunOptions     `json:"runOptions,omitempty"`
	ProblemMatcher json.RawMessage `json:"problemMatcher,omitempty"`
	Hide           bool            `json:"hide,omitempty"`
	IsBackground   bool            `json:"isBackground,omitempty"`
	Windows        *TaskDefaults   `json:"windows,omitempty"`
	OSX            *TaskDefaults   `json:"osx,omitempty"`
	Linux          *TaskDefaults   `json:"linux,omitempty"`
}

func (t Task) MarshalJSON() ([]byte, error) {
	object := map[string]any{}
	if t.Label != "" {
		object["label"] = t.Label
	}
	if t.TaskName != "" {
		object["taskName"] = t.TaskName
	}
	if t.Identifier != "" {
		object["identifier"] = t.Identifier
	}
	if t.Type != "" {
		object["type"] = t.Type
	}
	if t.Script != "" {
		object["script"] = t.Script
	}
	if t.Path != "" {
		object["path"] = t.Path
	}
	if t.TSConfig != "" {
		object["tsconfig"] = t.TSConfig
	}
	if t.Option != "" {
		object["option"] = t.Option
	}
	if t.ProviderTask != "" {
		object["task"] = t.ProviderTask
	}
	if t.ProviderFile != "" {
		object["file"] = t.ProviderFile
	}
	if t.Command.Set {
		object["command"] = t.Command
	}
	if len(t.Args) > 0 {
		object["args"] = t.Args
	}
	if t.DependsOrder != "" {
		object["dependsOrder"] = t.DependsOrder
	}
	if t.Dependencies.Set && len(t.Dependencies.Items) > 0 {
		object["dependsOn"] = t.Dependencies
	}
	if len(t.Group) > 0 {
		object["group"] = json.RawMessage(t.Group)
	}
	if t.Options != nil {
		object["options"] = t.Options
	}
	if t.Presentation != nil {
		object["presentation"] = t.Presentation
	}
	if t.RunOptions != nil {
		object["runOptions"] = t.RunOptions
	}
	if len(t.ProblemMatcher) > 0 {
		object["problemMatcher"] = json.RawMessage(t.ProblemMatcher)
	}
	if t.Hide {
		object["hide"] = t.Hide
	}
	if t.IsBackground {
		object["isBackground"] = t.IsBackground
	}
	if t.Windows != nil {
		object["windows"] = t.Windows
	}
	if t.OSX != nil {
		object["osx"] = t.OSX
	}
	if t.Linux != nil {
		object["linux"] = t.Linux
	}
	return json.Marshal(object)
}

func (t Task) EffectiveLabel() string {
	if t.Label != "" {
		return t.Label
	}
	if t.Identifier != "" {
		return t.Identifier
	}
	return t.TaskName
}

func (t Task) EffectiveType() string {
	if t.Type == "" {
		return "process"
	}
	return t.Type
}

type TaskDefaults struct {
	Type           string          `json:"type,omitempty"`
	Command        TokenValue      `json:"command"`
	Args           []TokenValue    `json:"args,omitempty"`
	DependsOrder   string          `json:"dependsOrder,omitempty"`
	Dependencies   DependencyList  `json:"dependsOn"`
	Group          json.RawMessage `json:"group,omitempty"`
	Options        *Options        `json:"options,omitempty"`
	Presentation   *Presentation   `json:"presentation,omitempty"`
	RunOptions     *RunOptions     `json:"runOptions,omitempty"`
	ProblemMatcher json.RawMessage `json:"problemMatcher,omitempty"`
	Hide           *bool           `json:"hide,omitempty"`
	IsBackground   *bool           `json:"isBackground,omitempty"`
}

func (d TaskDefaults) MarshalJSON() ([]byte, error) {
	object := map[string]any{}
	if d.Type != "" {
		object["type"] = d.Type
	}
	if d.Command.Set {
		object["command"] = d.Command
	}
	if len(d.Args) > 0 {
		object["args"] = d.Args
	}
	if d.DependsOrder != "" {
		object["dependsOrder"] = d.DependsOrder
	}
	if d.Dependencies.Set && len(d.Dependencies.Items) > 0 {
		object["dependsOn"] = d.Dependencies
	}
	if len(d.Group) > 0 {
		object["group"] = json.RawMessage(d.Group)
	}
	if d.Options != nil {
		object["options"] = d.Options
	}
	if d.Presentation != nil {
		object["presentation"] = d.Presentation
	}
	if d.RunOptions != nil {
		object["runOptions"] = d.RunOptions
	}
	if len(d.ProblemMatcher) > 0 {
		object["problemMatcher"] = json.RawMessage(d.ProblemMatcher)
	}
	if d.Hide != nil {
		object["hide"] = d.Hide
	}
	if d.IsBackground != nil {
		object["isBackground"] = d.IsBackground
	}
	return json.Marshal(object)
}

type Presentation struct {
	Echo             *bool  `json:"echo,omitempty"`
	Reveal           string `json:"reveal,omitempty"`
	RevealProblems   string `json:"revealProblems,omitempty"`
	Focus            *bool  `json:"focus,omitempty"`
	Panel            string `json:"panel,omitempty"`
	ShowReuseMessage *bool  `json:"showReuseMessage,omitempty"`
	Clear            *bool  `json:"clear,omitempty"`
	Close            *bool  `json:"close,omitempty"`
	Group            string `json:"group,omitempty"`
}

type Options struct {
	CWD   string              `json:"cwd,omitempty"`
	Env   map[string]string   `json:"env,omitempty"`
	Shell *ShellConfiguration `json:"shell,omitempty"`
}

type ShellConfiguration struct {
	Executable string   `json:"executable,omitempty"`
	Args       []string `json:"args,omitempty"`
}

type RunOptions struct {
	ReevaluateOnRerun *bool  `json:"reevaluateOnRerun,omitempty"`
	RunOn             string `json:"runOn,omitempty"`
	InstanceLimit     *int   `json:"instanceLimit,omitempty"`
	InstancePolicy    string `json:"instancePolicy,omitempty"`
}

type TokenValue struct {
	Value   string `json:"-"`
	Quoting string `json:"-"`
	Set     bool   `json:"-"`
}

func (t *TokenValue) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		t.Value = plain
		t.Set = true
		return nil
	}

	var parts []string
	if err := json.Unmarshal(data, &parts); err == nil {
		t.Value = strings.Join(parts, " ")
		t.Set = true
		return nil
	}

	var quoted struct {
		Value   any    `json:"value"`
		Quoting string `json:"quoting"`
	}
	if err := json.Unmarshal(data, &quoted); err != nil {
		return fmt.Errorf("unsupported token value: %w", err)
	}

	switch value := quoted.Value.(type) {
	case string:
		t.Value = value
	case []any:
		parts = parts[:0]
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return fmt.Errorf("unsupported command array value: %T", item)
			}
			parts = append(parts, text)
		}
		t.Value = strings.Join(parts, " ")
	default:
		return fmt.Errorf("unsupported token value type: %T", quoted.Value)
	}

	t.Quoting = quoted.Quoting
	t.Set = true
	return nil
}

func (t TokenValue) MarshalJSON() ([]byte, error) {
	if !t.Set {
		return []byte("null"), nil
	}
	if t.Quoting == "" {
		return json.Marshal(t.Value)
	}
	return json.Marshal(struct {
		Value   string `json:"value"`
		Quoting string `json:"quoting"`
	}{
		Value:   t.Value,
		Quoting: t.Quoting,
	})
}

type DependencyList struct {
	Items []Dependency `json:"-"`
	Set   bool         `json:"-"`
}

func (d DependencyList) IsZero() bool {
	return !d.Set || len(d.Items) == 0
}

func (d DependencyList) MarshalJSON() ([]byte, error) {
	if !d.Set || len(d.Items) == 0 {
		return []byte("null"), nil
	}
	if len(d.Items) == 1 && d.Items[0].Unsupported == "" {
		return json.Marshal(d.Items[0].Label)
	}
	labels := make([]string, 0, len(d.Items))
	for _, item := range d.Items {
		if item.Unsupported != "" {
			continue
		}
		labels = append(labels, item.Label)
	}
	return json.Marshal(labels)
}

func (d *DependencyList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		d.Items = []Dependency{{Label: plain}}
		d.Set = true
		return nil
	}

	var rawList []json.RawMessage
	if err := json.Unmarshal(data, &rawList); err == nil {
		d.Set = true
		d.Items = make([]Dependency, 0, len(rawList))
		for _, item := range rawList {
			dep, err := parseDependency(item)
			if err != nil {
				return err
			}
			d.Items = append(d.Items, dep)
		}
		return nil
	}

	dep, err := parseDependency(data)
	if err != nil {
		return err
	}
	d.Items = []Dependency{dep}
	d.Set = true
	return nil
}

func (d DependencyList) Labels() []string {
	labels := make([]string, 0, len(d.Items))
	for _, item := range d.Items {
		if item.Label != "" {
			labels = append(labels, item.Label)
		}
	}
	return labels
}

type Dependency struct {
	Label       string `json:"label,omitempty"`
	Unsupported string `json:"unsupported,omitempty"`
}

func parseDependency(data []byte) (Dependency, error) {
	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		return Dependency{Label: plain}, nil
	}

	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return Dependency{}, fmt.Errorf("unsupported dependency reference: %w", err)
	}
	if label, ok := object["label"].(string); ok {
		return Dependency{Label: label}, nil
	}
	if taskName, ok := object["task"].(string); ok {
		return Dependency{Label: taskName}, nil
	}
	return Dependency{Unsupported: "object dependency references are not supported"}, nil
}

type Input struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	Description string        `json:"description,omitempty"`
	Default     string        `json:"default,omitempty"`
	Options     []InputOption `json:"options,omitempty"`
}

func (i *Input) UnmarshalJSON(data []byte) error {
	type rawInput struct {
		ID          string            `json:"id"`
		Type        string            `json:"type"`
		Description string            `json:"description,omitempty"`
		Default     string            `json:"default,omitempty"`
		Options     []json.RawMessage `json:"options,omitempty"`
	}

	var raw rawInput
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	i.ID = raw.ID
	i.Type = raw.Type
	i.Description = raw.Description
	i.Default = raw.Default
	i.Options = make([]InputOption, 0, len(raw.Options))
	for _, option := range raw.Options {
		parsed, err := parseInputOption(option)
		if err != nil {
			return err
		}
		i.Options = append(i.Options, parsed)
	}
	return nil
}

type InputOption struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value"`
}

func parseInputOption(data []byte) (InputOption, error) {
	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		return InputOption{Label: plain, Value: plain}, nil
	}

	var option InputOption
	if err := json.Unmarshal(data, &option); err != nil {
		return InputOption{}, fmt.Errorf("unsupported input option: %w", err)
	}
	if option.Label == "" {
		option.Label = option.Value
	}
	return option, nil
}

type ResolvedTask struct {
	Label          string          `json:"label"`
	Type           string          `json:"type"`
	Group          json.RawMessage `json:"group,omitempty"`
	SourceTaskID   string          `json:"sourceTaskId,omitempty"`
	WorkspaceRoot  string          `json:"workspaceRoot,omitempty"`
	TaskFilePath   string          `json:"taskFilePath,omitempty"`
	Command        string          `json:"command"`
	CommandToken   ResolvedToken   `json:"commandToken"`
	Args           []string        `json:"args,omitempty"`
	ArgTokens      []ResolvedToken `json:"argTokens,omitempty"`
	DisplayCommand string          `json:"displayCommand,omitempty"`
	DisplayArgs    []string        `json:"displayArgs,omitempty"`
	DependsOn      []string        `json:"dependsOn,omitempty"`
	DependsOrder   string          `json:"dependsOrder,omitempty"`
	Options        ResolvedOptions `json:"options"`
	Presentation   *Presentation   `json:"presentation,omitempty"`
	RunOptions     *RunOptions     `json:"runOptions,omitempty"`
	ProblemMatcher json.RawMessage `json:"problemMatcher,omitempty"`
	Hide           bool            `json:"hide,omitempty"`
	IsBackground   bool            `json:"isBackground,omitempty"`
}

type ResolvedToken struct {
	Value        string `json:"value"`
	DisplayValue string `json:"displayValue,omitempty"`
	Quoting      string `json:"quoting,omitempty"`
}

type ResolvedOptions struct {
	CWD   string            `json:"cwd"`
	Env   map[string]string `json:"env,omitempty"`
	Shell ShellRuntime      `json:"shell"`
}

type ShellRuntime struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Family     string   `json:"family"`
}

type Catalog struct {
	WorkspaceRoot string                  `json:"workspaceRoot"`
	TaskFilePath  string                  `json:"taskFilePath"`
	Tasks         map[string]ResolvedTask `json:"tasks"`
	Order         []string                `json:"order"`
}

type Problem struct {
	Task      string `json:"task"`
	Owner     string `json:"owner,omitempty"`
	Source    string `json:"source,omitempty"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
}

type LoadOptions struct {
	Path          string
	WorkspaceRoot string
}

type ResolveOptions struct {
	WorkspaceRoot  string
	TaskFilePath   string
	Inputs         []Input
	InputValues    map[string]string
	Redaction      RedactionPolicy
	Env            []string
	Stdin          io.Reader
	Stdout         io.Writer
	NonInteractive bool
}
