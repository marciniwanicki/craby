package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/marciniwanicki/craby/internal/tools"
	"github.com/rs/zerolog"
)

// PipelineLLMClient extends LLMClient with a simple chat method for planning/synthesis
type PipelineLLMClient interface {
	LLMClient
	// ChatMessages sends messages without tools and streams the response
	ChatMessages(ctx context.Context, messages []Message, tokenChan chan<- string) (string, error)
}

// PipelineStepLogger is the interface for logging pipeline steps
type PipelineStepLogger interface {
	// Reset resets the step counter (typically called at start of new request)
	Reset()
	// LogPlan logs a generated plan
	LogPlan(log PlanStepLog) error
	// LogExecution logs a tool execution step
	LogExecution(log ExecutionStepLog) error
}

// PlanStepLog represents a generated plan to be logged
type PlanStepLog struct {
	Intent        string
	Complexity    string
	NeedsTools    bool
	ReadyToAnswer bool
	Context       []string
	Steps         []PlanStepEntry
	RawXML        string
}

// PlanStepEntry represents a single step in the plan
type PlanStepEntry struct {
	ID        string
	DependsOn string
	Tool      string
	Purpose   string
	Args      map[string]string
}

// ExecutionStepLog represents a tool execution to be logged
type ExecutionStepLog struct {
	StepID     string
	Tool       string
	Purpose    string
	Args       map[string]any
	Output     string
	Success    bool
	Error      string
	DurationMs int64
}

// PipelineTemplates holds the templates needed for the pipeline
type PipelineTemplates struct {
	Planning  string
	Synthesis string
	Identity  string
	User      string
}

// StepResult holds the result of executing a plan step
type StepResult struct {
	StepID  string
	Tool    string
	Purpose string
	Output  string
	Success bool
	Error   string
}

// Pipeline implements the 4-step pipeline: Planning → Validation → Execution → Synthesis
type Pipeline struct {
	llm           PipelineLLMClient
	registry      *tools.Registry
	logger        zerolog.Logger
	templates     PipelineTemplates
	externalTools map[string]bool    // Set of external tool/command names
	stepLogger    PipelineStepLogger // Optional step logger for debugging
}

// NewPipeline creates a new pipeline executor
func NewPipeline(llm PipelineLLMClient, registry *tools.Registry, logger zerolog.Logger, templates PipelineTemplates) *Pipeline {
	return &Pipeline{
		llm:           llm,
		registry:      registry,
		logger:        logger,
		templates:     templates,
		externalTools: make(map[string]bool),
	}
}

// NewPipelineWithExternalTools creates a pipeline with knowledge of external tools
func NewPipelineWithExternalTools(llm PipelineLLMClient, registry *tools.Registry, logger zerolog.Logger, templates PipelineTemplates, externalTools []string) *Pipeline {
	extToolsMap := make(map[string]bool, len(externalTools))
	for _, tool := range externalTools {
		extToolsMap[tool] = true
	}
	return &Pipeline{
		llm:           llm,
		registry:      registry,
		logger:        logger,
		templates:     templates,
		externalTools: extToolsMap,
	}
}

// SetStepLogger sets the step logger for debugging pipeline execution
func (p *Pipeline) SetStepLogger(stepLogger PipelineStepLogger) {
	p.stepLogger = stepLogger
}

// MaxIterations is the maximum number of plan-execute cycles to prevent infinite loops
const MaxIterations = 10

// Run executes the full pipeline for a user message using iterative planning
func (p *Pipeline) Run(ctx context.Context, userMessage string, opts RunOptions, eventChan chan<- Event) ([]Message, error) {
	defer close(eventChan)

	// Reset step logger for new request
	if p.stepLogger != nil {
		p.stepLogger.Reset()
	}

	p.logger.Debug().
		Str("user_message", userMessage).
		Int("history_len", len(opts.History)).
		Msg("starting iterative pipeline run")

	// Accumulated results from all iterations
	var allResults []StepResult

	for iteration := 0; iteration < MaxIterations; iteration++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		p.logger.Debug().Int("iteration", iteration).Msg("starting planning iteration")

		// Plan with accumulated results
		plan, rawXML, err := p.planWithResults(ctx, userMessage, opts, allResults)
		if err != nil {
			return nil, fmt.Errorf("planning failed (iteration %d): %w", iteration, err)
		}

		p.logger.Info().
			Int("iteration", iteration).
			Str("intent", plan.Intent).
			Str("complexity", string(plan.Complexity)).
			Bool("needs_tools", plan.NeedsTools).
			Bool("ready_to_answer", plan.ReadyToAnswer).
			Int("steps", len(plan.Steps)).
			Msg("plan generated")

		// Log the plan
		p.logPlan(plan, rawXML)

		// Emit plan generated event
		eventChan <- Event{
			Type: EventPlanGenerated,
			Plan: plan,
		}

		// Check if ready to synthesize
		if plan.ReadyToAnswer || (!plan.NeedsTools && len(plan.Steps) == 0) {
			p.logger.Debug().Int("iteration", iteration).Msg("ready to answer, proceeding to synthesis")
			break
		}

		// Validate the plan
		if plan.NeedsTools && len(plan.Steps) > 0 {
			if err := p.validate(plan); err != nil {
				return nil, fmt.Errorf("validation failed (iteration %d): %w", iteration, err)
			}
			p.logger.Debug().Msg("plan validated successfully")

			// Execute steps
			results, err := p.execute(ctx, plan, eventChan)
			if err != nil {
				return nil, fmt.Errorf("execution failed (iteration %d): %w", iteration, err)
			}

			// Accumulate results
			allResults = append(allResults, results...)
			p.logger.Debug().
				Int("iteration", iteration).
				Int("new_results", len(results)).
				Int("total_results", len(allResults)).
				Msg("iteration complete")
		}
	}

	// Synthesis with all accumulated results
	answer, err := p.synthesize(ctx, userMessage, nil, allResults, opts, eventChan)
	if err != nil {
		return nil, fmt.Errorf("synthesis failed: %w", err)
	}

	// Build history: existing history + user message + assistant response
	history := make([]Message, 0, len(opts.History)+2)
	history = append(history, opts.History...)
	history = append(history, Message{Role: "user", Content: userMessage})
	history = append(history, Message{Role: "assistant", Content: answer})

	p.logger.Debug().Int("final_history_len", len(history)).Msg("pipeline run complete")
	return history, nil
}

// planWithResults generates a structured plan from the user message, including previous tool results
// Returns the plan, the raw XML response, and any error
func (p *Pipeline) planWithResults(ctx context.Context, userMessage string, opts RunOptions, previousResults []StepResult) (*Plan, string, error) {
	prompt := p.renderPlanningPromptWithResults(userMessage, opts, previousResults)

	messages := []Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: userMessage},
	}

	p.logger.Debug().Int("previous_results", len(previousResults)).Msg("calling LLM for planning")

	// Don't stream planning phase - we need the complete response
	response, err := p.llm.ChatMessages(ctx, messages, nil)
	if err != nil {
		return nil, "", err
	}

	p.logger.Debug().Str("response_len", fmt.Sprintf("%d", len(response))).Msg("received planning response")

	plan, err := ParsePlan(response)
	if err != nil {
		return nil, response, err
	}

	return plan, response, nil
}

// validate checks that all tools exist and arguments are valid
func (p *Pipeline) validate(plan *Plan) error {
	for _, step := range plan.Steps {
		_, ok := p.registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("step %s: unknown tool %q", step.ID, step.Tool)
		}

		// Validate dependencies exist within this plan iteration
		if step.DependsOn != "" {
			found := false
			for _, s := range plan.Steps {
				if s.ID == step.DependsOn {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("step %s: depends on unknown step %q", step.ID, step.DependsOn)
			}
		}
	}
	return nil
}

// execute runs the plan steps in dependency order
func (p *Pipeline) execute(ctx context.Context, plan *Plan, eventChan chan<- Event) ([]StepResult, error) {
	// Get execution order via topological sort
	ordered, err := p.executionOrder(plan.Steps)
	if err != nil {
		return nil, err
	}

	results := make([]StepResult, 0, len(ordered))

	for _, step := range ordered {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Emit step started event
		eventChan <- Event{
			Type:     EventStepStarted,
			ToolName: step.Tool,
			ToolArgs: mustMarshalJSON(step.ArgsMap()),
		}

		// Execute the tool
		args := step.ArgsMap()
		argsJSON, _ := json.Marshal(args)

		// Emit tool call event
		eventChan <- Event{
			Type:     EventToolCall,
			ToolID:   step.ID,
			ToolName: step.Tool,
			ToolArgs: string(argsJSON),
		}

		p.logger.Info().
			Str("step", step.ID).
			Str("tool", step.Tool).
			Interface("args", args).
			Msg("executing step")

		startTime := time.Now()
		output, err := p.registry.Execute(step.Tool, args)
		execDuration := time.Since(startTime)
		success := err == nil
		errorMsg := ""
		if err != nil {
			p.logger.Warn().Err(err).Str("step", step.ID).Msg("step execution failed")
			output = fmt.Sprintf("Error: %v", err)
			errorMsg = err.Error()
		}

		// Log execution
		p.logExecution(step.ID, step.Tool, step.Purpose, args, output, success, errorMsg, execDuration)

		// Emit tool result event
		eventChan <- Event{
			Type:        EventToolResult,
			ToolID:      step.ID,
			ToolName:    step.Tool,
			ToolOutput:  output,
			ToolSuccess: success,
		}

		results = append(results, StepResult{
			StepID:  step.ID,
			Tool:    step.Tool,
			Purpose: step.Purpose,
			Output:  output,
			Success: success,
			Error:   errorMsg,
		})

		p.logger.Debug().
			Str("step", step.ID).
			Bool("success", success).
			Msg("step complete")
	}

	return results, nil
}

// executionOrder returns steps in dependency-resolved order (topological sort)
func (p *Pipeline) executionOrder(steps []PlanStep) ([]PlanStep, error) {
	if len(steps) == 0 {
		return nil, nil
	}

	// Build dependency graph
	stepMap := make(map[string]*PlanStep, len(steps))
	inDegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))

	for i := range steps {
		step := &steps[i]
		stepMap[step.ID] = step
		inDegree[step.ID] = 0
	}

	for _, step := range steps {
		if step.DependsOn != "" {
			inDegree[step.ID]++
			dependents[step.DependsOn] = append(dependents[step.DependsOn], step.ID)
		}
	}

	// Kahn's algorithm
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var result []PlanStep
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, *stepMap[id])

		for _, depID := range dependents[id] {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	if len(result) != len(steps) {
		return nil, fmt.Errorf("circular dependency detected in plan steps")
	}

	return result, nil
}

// synthesize generates the final answer from the plan and tool results
func (p *Pipeline) synthesize(ctx context.Context, userMessage string, plan *Plan, results []StepResult, opts RunOptions, eventChan chan<- Event) (string, error) {
	prompt := p.renderSynthesisPrompt(userMessage, plan, results, opts)

	messages := []Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: userMessage},
	}

	p.logger.Debug().Msg("calling LLM for synthesis")

	// Create a token channel for streaming
	tokenChan := make(chan string, 100)
	resultChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		response, err := p.llm.ChatMessages(ctx, messages, tokenChan)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- response
	}()

	// Stream tokens to the event channel
	var contentBuilder strings.Builder
	for token := range tokenChan {
		contentBuilder.WriteString(token)
		eventChan <- Event{
			Type: EventText,
			Text: token,
			Role: RoleAssistant,
		}
	}

	// Check for errors or get result
	select {
	case err := <-errChan:
		return "", err
	case response := <-resultChan:
		return response, nil
	}
}

// renderPlanningPromptWithResults builds the planning prompt with template substitutions and previous results
func (p *Pipeline) renderPlanningPromptWithResults(userMessage string, opts RunOptions, previousResults []StepResult) string {
	prompt := p.templates.Planning

	// Format history
	historyStr := p.formatHistory(opts.History)
	prompt = strings.ReplaceAll(prompt, "{{HISTORY}}", historyStr)

	// Format tools
	toolsStr := p.formatTools()
	prompt = strings.ReplaceAll(prompt, "{{TOOLS}}", toolsStr)

	// User hints (context)
	userHints := ""
	if opts.Context != "" {
		userHints = opts.Context
	}
	prompt = strings.ReplaceAll(prompt, "{{USER_HINTS}}", userHints)

	// Format previous tool results for iterative planning
	toolResultsStr := p.formatToolResults(previousResults)
	prompt = strings.ReplaceAll(prompt, "{{TOOL_RESULTS}}", toolResultsStr)

	return prompt
}

// renderSynthesisPrompt builds the synthesis prompt with template substitutions
func (p *Pipeline) renderSynthesisPrompt(userMessage string, plan *Plan, results []StepResult, opts RunOptions) string {
	prompt := p.templates.Synthesis

	// Identity
	prompt = strings.ReplaceAll(prompt, "{{IDENTITY}}", p.templates.Identity)

	// User profile
	prompt = strings.ReplaceAll(prompt, "{{USER}}", p.templates.User)

	// Format history
	historyStr := p.formatHistory(opts.History)
	prompt = strings.ReplaceAll(prompt, "{{HISTORY}}", historyStr)

	// Format tool results
	resultsStr := p.formatToolResults(results)
	prompt = strings.ReplaceAll(prompt, "{{TOOL_RESULTS}}", resultsStr)

	return prompt
}

// formatHistory formats conversation history for template insertion
func (p *Pipeline) formatHistory(history []Message) string {
	if len(history) == 0 {
		return "(No previous conversation)"
	}

	var sb strings.Builder
	for _, msg := range history {
		switch msg.Role {
		case "user":
			sb.WriteString("User: ")
		case "assistant":
			sb.WriteString("Assistant: ")
		default:
			continue
		}
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// formatTools formats available tools for the planning prompt
func (p *Pipeline) formatTools() string {
	toolList := p.registry.List()
	if len(toolList) == 0 {
		return "(No tools available)"
	}

	var sb strings.Builder
	for _, t := range toolList {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t.Name(), t.Description()))
		params := t.Parameters()
		if props, ok := params["properties"].(map[string]any); ok {
			for name, prop := range props {
				if propMap, ok := prop.(map[string]any); ok {
					desc := propMap["description"]
					sb.WriteString(fmt.Sprintf("  - `%s`: %v\n", name, desc))
				}
			}
		}
	}
	return sb.String()
}

// formatToolResults formats step results for the synthesis prompt
func (p *Pipeline) formatToolResults(results []StepResult) string {
	if len(results) == 0 {
		return "(No tool results - direct answer)"
	}

	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("### Step: %s\n", r.StepID))
		sb.WriteString(fmt.Sprintf("**Tool**: %s\n", r.Tool))
		sb.WriteString(fmt.Sprintf("**Purpose**: %s\n", r.Purpose))
		if r.Success {
			sb.WriteString(fmt.Sprintf("**Output**:\n```\n%s\n```\n\n", r.Output))
		} else {
			sb.WriteString(fmt.Sprintf("**Error**: %s\n\n", r.Error))
		}
	}
	return sb.String()
}

func mustMarshalJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// logPlan logs the generated plan to the step logger
func (p *Pipeline) logPlan(plan *Plan, rawXML string) {
	if p.stepLogger == nil {
		return
	}

	steps := make([]PlanStepEntry, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		args := make(map[string]string, len(step.Args))
		for _, arg := range step.Args {
			args[arg.Name] = arg.Value
		}
		steps = append(steps, PlanStepEntry{
			ID:        step.ID,
			DependsOn: step.DependsOn,
			Tool:      step.Tool,
			Purpose:   step.Purpose,
			Args:      args,
		})
	}

	_ = p.stepLogger.LogPlan(PlanStepLog{
		Intent:        plan.Intent,
		Complexity:    string(plan.Complexity),
		NeedsTools:    plan.NeedsTools,
		ReadyToAnswer: plan.ReadyToAnswer,
		Context:       plan.Context,
		Steps:         steps,
		RawXML:        rawXML,
	})
}

// logExecution logs a tool execution to the step logger
func (p *Pipeline) logExecution(stepID, tool, purpose string, args map[string]any, output string, success bool, errMsg string, duration time.Duration) {
	if p.stepLogger == nil {
		return
	}

	_ = p.stepLogger.LogExecution(ExecutionStepLog{
		StepID:     stepID,
		Tool:       tool,
		Purpose:    purpose,
		Args:       args,
		Output:     output,
		Success:    success,
		Error:      errMsg,
		DurationMs: duration.Milliseconds(),
	})
}
